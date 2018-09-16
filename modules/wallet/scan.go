package wallet

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
)

var errMaxKeys = fmt.Errorf("refused to generate more than %v keys from seed", maxScanKeys)

// maxScanKeys is the number of maximum number of keys the seedScanner will
// generate before giving up.
var maxScanKeys = func() uint64 {
	switch build.Release {
	case "dev":
		return 1e6
	case "standard":
		return 100e6
	case "testing":
		return 100e3
	default:
		panic("unrecognized build.Release")
	}
}()

// A scannedOutput is an output found in the blockchain that was generated
// from a given seed.
type scannedOutput struct {
	id        types.OutputID
	value     types.Currency
	seedIndex uint64
}

// A seedScanner scans the blockchain for addresses that belong to a given
// seed.
type seedScanner struct {
	dustThreshold        types.Currency              // minimum value of outputs to be included
	keys                 map[types.UnlockHash]uint64 // map address to seed index
	keysArray            [][]byte
	minimumIndex         uint64
	maximumInternalIndex uint64
	maximumExternalIndex uint64
	seed                 modules.Seed
	siacoinOutputs       map[types.SiacoinOutputID]scannedOutput
	cs                   modules.ConsensusSet
	log                  *persist.Logger
}

func (s *seedScanner) numKeys() uint64 {
	return uint64(len(s.keys))
}

// generateKeys generates n additional keys from the seedScanner's seed.
func (s *seedScanner) generateKeys(n uint64) {
	initialProgress := s.numKeys()
	for i, k := range generateKeys(s.seed, initialProgress, n) {
		s.keys[k.UnlockConditions.UnlockHash()] = initialProgress + uint64(i)
		u := k.UnlockConditions.UnlockHash()
		s.keysArray = append(s.keysArray, u[:])
	}
	s.maximumInternalIndex += n
}

// ProcessHeaderConsensusChange match consensus change headers with generated seeds
// It needs to look for two types new outputs:
//
// 1) Delayed outputs that have matured during this block. These outputs come
// attached to the HeaderConsensusChange via the output diff.
//
// 2) Fresh outputs that were created and activated during this block. If the
// current block contains these outputs, the header filter will match the wallet's
// keys.
//
// In a full node, we read the block directly from the consensus db and grab the
// outputs from the block output diff.
func (s *seedScanner) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange,
	getSiacoinOutputDiff func(types.BlockID, modules.DiffDirection) ([]modules.SiacoinOutputDiff, error)) {

	var siacoinOutputDiffs []modules.SiacoinOutputDiff

	// grab matured outputs
	siacoinOutputDiffs = append(siacoinOutputDiffs, hcc.MaturedSiacoinOutputDiffs...)

	// grab applied active outputs from full blocks
	for _, pbh := range hcc.AppliedBlockHeaders {
		blockID := pbh.BlockHeader.ID()
		if pbh.GCSFilter.MatchUnlockHash(blockID[:], s.keysArray) {
			// read the block, process the output
			blockSiacoinOutputDiffs, err := getSiacoinOutputDiff(blockID, modules.DiffApply)
			if err != nil {
				panic(err)
			}
			siacoinOutputDiffs = append(siacoinOutputDiffs, blockSiacoinOutputDiffs...)
		}
	}

	// grab reverted active outputs from full blocks
	for _, pbh := range hcc.RevertedBlockHeaders {
		blockID := pbh.BlockHeader.ID()
		if pbh.GCSFilter.MatchUnlockHash(blockID[:], s.keysArray) {
			blockSiacoinOutputDiffs, err := getSiacoinOutputDiff(blockID, modules.DiffRevert)
			if err != nil {
				panic(err)
			}
			siacoinOutputDiffs = append(siacoinOutputDiffs, blockSiacoinOutputDiffs...)
		}
	}

	// apply the aggregated output diffs
	for _, diff := range siacoinOutputDiffs {
		if diff.Direction == modules.DiffApply {
			if index, exists := s.keys[diff.SiacoinOutput.UnlockHash]; exists && diff.SiacoinOutput.Value.Cmp(s.dustThreshold) > 0 {
				s.siacoinOutputs[diff.ID] = scannedOutput{
					id:        types.OutputID(diff.ID),
					value:     diff.SiacoinOutput.Value,
					seedIndex: index,
				}
			}
		} else if diff.Direction == modules.DiffRevert {
			// NOTE: DiffRevert means the output was either spent or was in a
			// block that was reverted.
			if _, exists := s.keys[diff.SiacoinOutput.UnlockHash]; exists {
				delete(s.siacoinOutputs, diff.ID)
			}
		}
	}

	for _, diff := range siacoinOutputDiffs {
		index, exists := s.keys[diff.SiacoinOutput.UnlockHash]
		if exists {
			s.log.Debugln("Seed scanner found a key used at index", index)
			if index > s.maximumExternalIndex {
				s.maximumExternalIndex = index
			}
		}
	}
	gap := s.maximumInternalIndex - s.maximumExternalIndex
	if gap > 0 {
		toGrow := AddressGapLimit - gap
		s.generateKeys(uint64(toGrow))
	}
}

// scan subscribes s to cs and scans the blockchain for addresses that belong
// to s's seed. If scan returns errMaxKeys, additional keys may need to be
// generated to find all the addresses.
func (s *seedScanner) scan(cs modules.ConsensusSet, cancel <-chan struct{}) error {
	numKeys := uint64(AddressGapLimit)
	s.generateKeys(numKeys)
	if err := cs.HeaderConsensusSetSubscribe(s, modules.ConsensusChangeBeginning, cancel); err != nil {
		return err
	}
	cs.HeaderUnsubscribe(s)
	if numKeys > maxScanKeys-s.numKeys() {
		numKeys = maxScanKeys - s.numKeys()
	}
	return errMaxKeys
}

// newSeedScanner returns a new seedScanner.
func newSeedScanner(seed modules.Seed, cs modules.ConsensusSet, log *persist.Logger) *seedScanner {
	return &seedScanner{
		seed:                 seed,
		minimumIndex:         0,
		maximumInternalIndex: 0,
		maximumExternalIndex: 0,
		keys:                 make(map[types.UnlockHash]uint64, numInitialKeys),
		siacoinOutputs:       make(map[types.SiacoinOutputID]scannedOutput),
		cs:                   cs,
		log:                  log,
	}
}

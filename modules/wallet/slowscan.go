package wallet

import (
	"log"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
)

const scanMultiplier = 4 // how many more keys to generate after each scan iteration

// This is legacy code from the bad old days of terrible seed scanning and improper
// management of pubkey generation. It will be useful for grabbing addresses made by
// wallets not behaving in accordance with the AddressGapLimit specified by BIP 44.

// numInitialKeys is the number of keys generated by the seedScanner before
// scanning the blockchain for the first time.
var numInitialKeys = func() uint64 {
	switch build.Release {
	case "dev":
		return 10e3
	case "standard":
		return 1e6
	case "testing":
		return 1e3
	default:
		panic("unrecognized build.Release")
	}
}()

// A slowSeedScanner scans the blockchain for addresses that belong to a given
// seed. This is for legacy scanning.
type slowSeedScanner struct {
	dustThreshold    types.Currency              // minimum value of outputs to be included
	keys             map[types.UnlockHash]uint64 // map address to seed index
	keysArray        [][]byte
	largestIndexSeen uint64 // largest index that has appeared in the blockchain
	seed             modules.Seed
	siacoinOutputs   map[types.SiacoinOutputID]scannedOutput
	cs               modules.ConsensusSet

	log *persist.Logger
}

func (s *slowSeedScanner) numKeys() uint64 {
	return uint64(len(s.keys))
}

// generateKeys generates n additional keys from the slowSeedScanner's seed.
func (s *slowSeedScanner) generateKeys(n uint64) {
	initialProgress := s.numKeys()
	for i, k := range generateKeys(s.seed, initialProgress, n) {
		s.keys[k.UnlockConditions.UnlockHash()] = initialProgress + uint64(i)
		u := k.UnlockConditions.UnlockHash()
		s.keysArray = append(s.keysArray, u[:])
	}
}

// ProcessConsensusChange scans the blockchain for information relevant to the
// slowSeedScanner.
func (s *slowSeedScanner) ProcessConsensusChange(cc modules.ConsensusChange) {
	// update outputs
	for _, diff := range cc.SiacoinOutputDiffs {
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

	// update s.largestIndexSeen
	for _, diff := range cc.SiacoinOutputDiffs {
		index, exists := s.keys[diff.SiacoinOutput.UnlockHash]
		if exists {
			s.log.Debugln("Seed scanner found a key used at index", index)
			if index > s.largestIndexSeen {
				s.largestIndexSeen = index
			}
		}
	}
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
func (s *slowSeedScanner) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange,
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
			log.Printf("found in %s, \n", blockID.String())
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

	// update s.largestIndexSeen
	for _, diff := range siacoinOutputDiffs {
		index, exists := s.keys[diff.SiacoinOutput.UnlockHash]
		if exists {
			s.log.Debugln("Seed scanner found a key used at index", index)
			if index > s.largestIndexSeen {
				s.largestIndexSeen = index
			}
		}
	}
}

// scan subscribes s to cs and scans the blockchain for addresses that belong
// to s's seed. If scan returns errMaxKeys, additional keys may need to be
// generated to find all the addresses.
func (s *slowSeedScanner) scan(cs modules.ConsensusSet, cancel <-chan struct{}) error {
	// generate a bunch of keys and scan the blockchain looking for them. If
	// none of the 'upper' half of the generated keys are found, we are done;
	// otherwise, generate more keys and try again (bounded by a sane
	// default).
	//
	// NOTE: since scanning is very slow, we aim to only scan once, which
	// means generating many keys.
	numKeys := numInitialKeys
	for s.numKeys() < maxScanKeys {
		s.generateKeys(numKeys)
		if cs.SpvMode() {
			if err := cs.HeaderConsensusSetSubscribe(s, modules.ConsensusChangeBeginning, cancel); err != nil {
				return err
			}
			cs.HeaderUnsubscribe(s)
		} else {
			if err := cs.ConsensusSetSubscribe(s, modules.ConsensusChangeBeginning, cancel); err != nil {
				return err
			}
			cs.Unsubscribe(s)
		}
		if s.largestIndexSeen < s.numKeys()/2 {
			return nil
		}
		// increase number of keys generated each iteration, capping so that
		// we do not exceed maxScanKeys
		numKeys *= scanMultiplier
		if numKeys > maxScanKeys-s.numKeys() {
			numKeys = maxScanKeys - s.numKeys()
		}
	}
	return errMaxKeys
}

// newSlowSeedScanner returns a new slowSeedScanner.
func newSlowSeedScanner(seed modules.Seed, cs modules.ConsensusSet, log *persist.Logger) *slowSeedScanner {
	return &slowSeedScanner{
		seed:           seed,
		keys:           make(map[types.UnlockHash]uint64, numInitialKeys),
		siacoinOutputs: make(map[types.SiacoinOutputID]scannedOutput),
		cs:             cs,

		log: log,
	}
}
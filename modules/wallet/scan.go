package wallet

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// SeedScanner will auto detect scan airdrop or not and use right scanner
type SeedScanner interface {
	scan(<-chan struct{}) error
	getMaximumExternalIndex() uint64
	// getMaximumInternalIndex() uint64
	setDustThreshold(d types.Currency)
	getSiacoinOutputs() map[types.SiacoinOutputID]scannedOutput
}

func newSeedScanner(seed modules.Seed, addressGapLimit uint64,
	cs modules.ConsensusSet, log *persist.Logger, scanAirdrop bool) SeedScanner {
	if scanAirdrop {
		return newSlowSeedScanner(seed, addressGapLimit, cs, log)
	}
	return newFastSeedScanner(seed, addressGapLimit, cs, log)
}

package wallet

import (
	"github.com/HyperspaceApp/Hyperspace/build"
)

const (
	// defragBatchSize defines how many outputs are combined during one defrag.
	defragBatchSize = 35

	// defragStartIndex is the number of outputs to skip over when performing a
	// defrag.
	defragStartIndex = 10

	// defragThreshold is the number of outputs a wallet is allowed before it is
	// defragmented.
	defragThreshold = 50
)

var (
	// lookaheadRescanThreshold is the number of keys in the lookahead that will be
	// generated before a complete wallet rescan is initialized.
	lookaheadRescanThreshold = build.Select(build.Var{
		Dev:      uint64(100),
		Standard: uint64(1000),
		Testing:  uint64(10),
	}).(uint64)
)

func init() {
	// Sanity check - the defrag threshold needs to be higher than the batch
	// size plus the start index.
	if build.DEBUG && defragThreshold <= defragBatchSize+defragStartIndex {
		panic("constants are incorrect, defragThreshold needs to be larger than the sum of defragBatchSize and defragStartIndex")
	}
}

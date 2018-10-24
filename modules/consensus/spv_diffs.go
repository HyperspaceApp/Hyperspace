package consensus

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

func commitHeaderDiffSetSanity(tx *bolt.Tx, pbh *modules.ProcessedBlockHeader, dir modules.DiffDirection) {
	// This function is purely sanity checks.
	if !build.DEBUG {
		return
	}

	// Current node must be the input node's parent if applying, and
	// current node must be the input node if reverting.
	if dir == modules.DiffApply {
		parent, err := getBlockHeaderMap(tx, pbh.BlockHeader.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		if parent.BlockHeader.ID() != currentBlockID(tx) {
			panic(errWrongAppliedDiffSet)
		}
	} else {
		if pbh.BlockHeader.ID() != currentBlockID(tx) {
			panic(errWrongRevertDiffSet)
		}
	}
}

func commitSingleNodeDiffs(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		for _, scod := range pb.SiacoinOutputDiffs {
			commitSiacoinOutputDiff(tx, scod, dir)
		}
		// for _, fcd := range pb.FileContractDiffs {
		// 	commitFileContractDiff(tx, fcd, dir)
		// }
	} else {
		for i := len(pb.SiacoinOutputDiffs) - 1; i >= 0; i-- {
			commitSiacoinOutputDiff(tx, pb.SiacoinOutputDiffs[i], dir)
		}
		// for i := len(pb.FileContractDiffs) - 1; i >= 0; i-- {
		// 	commitFileContractDiff(tx, pb.FileContractDiffs[i], dir)
		// }
	}
}

func commitSingleBlockDiffSet(tx *bolt.Tx, pb *processedBlock, dir modules.DiffDirection) {
	// Sanity checks - there are a few so they were moved to another function.
	// TODO: add back check
	// can't check block with currentBlockID, cause currentBlockID is current header id
	// if build.DEBUG {
	// 	commitSingleBlockDiffSetSanity(tx, pb, dir)
	// }

	commitSingleNodeDiffs(tx, pb, dir)
}

func commitHeaderDiffs(tx *bolt.Tx, pbh *modules.ProcessedBlockHeader, dir modules.DiffDirection) {
	if dir == modules.DiffApply {
		for _, scod := range pbh.SiacoinOutputDiffs {
			commitSiacoinOutputDiff(tx, scod, dir)
		}
		for _, dscod := range pbh.DelayedSiacoinOutputDiffs {
			commitDelayedSiacoinOutputDiff(tx, dscod, dir)
		}
	} else {
		for i := len(pbh.SiacoinOutputDiffs) - 1; i >= 0; i-- {
			commitSiacoinOutputDiff(tx, pbh.SiacoinOutputDiffs[i], dir)
		}
		for i := len(pbh.DelayedSiacoinOutputDiffs) - 1; i >= 0; i-- {
			commitDelayedSiacoinOutputDiff(tx, pbh.DelayedSiacoinOutputDiffs[i], dir)
		}
	}
}

func commitHeaderDiffSet(tx *bolt.Tx, pbh *modules.ProcessedBlockHeader, dir modules.DiffDirection) {
	// Sanity checks - there are a few so they were moved to another function.
	// TODO: add back check
	if build.DEBUG {
		commitHeaderDiffSetSanity(tx, pbh, dir)
	}

	createUpcomingDelayedOutputMaps(tx, pbh.Height, dir)
	commitHeaderDiffs(tx, pbh, dir)
	deleteObsoleteDelayedOutputMaps(tx, pbh.Height, dir)
	updateCurrentPath(tx, pbh.BlockHeader.ID(), dir)
}

func (cs *ConsensusSet) generateAndApplyDiffForSPV(tx *bolt.Tx, pb *processedBlock) error {
	// Sanity check - the block being applied should have the current block as
	// a parent.
	// if build.DEBUG && pb.Block.ParentID != currentBlockID(tx) {
	// 	panic(errInvalidSuccessor)
	// }

	// Create the bucket to hold all of the delayed siacoin outputs created by
	// transactions this block. Needs to happen before any transactions are
	// applied.
	createDSCOBucketIfNotExist(tx, pb.Height+types.MaturityDelay) // have done this in header

	// Validate and apply each transaction in the block. They cannot be
	// validated all at once because some transactions may not be valid until
	// previous transactions have been applied.
	for _, txn := range pb.Block.Transactions {
		// TODO: won't pass becaues of no valid output in bucket for inputs
		err := validTransactionForSPV(tx, txn)
		if err != nil {
			return err
		}
		applyTransactionForSPV(tx, pb, txn)
	}

	pbh, exists := cs.processedBlockHeaders[pb.Block.ID()]
	if !exists {
		panic(fmt.Errorf("generateAndApplyDiffForSPV: header not exists %s", pb.Block.ID()))
	}

	// After all of the transactions have been applied, 'maintenance' is
	// applied on the block. This includes adding any outputs that have reached
	// maturity, applying any contracts with missed storage proofs, and adding
	// the miner payouts to the list of delayed outputs.
	applyMaintenanceForSPV(tx, pb, pbh)

	// DiffsGenerated are only set to true after the block has been fully
	// validated and integrated. This is required to prevent later blocks from
	// being accepted on top of an invalid block - if the consensus set ever
	// forks over an invalid block, 'DiffsGenerated' will be set to 'false',
	// requiring validation to occur again. when 'DiffsGenerated' is set to
	// true, validation is skipped, therefore the flag should only be set to
	// true on fully validated blocks.
	pb.DiffsGenerated = true

	// Add the block to the current path and block map.
	bid := pb.Block.ID()
	blockMap := tx.Bucket(BlockMap)
	// updateCurrentPath(tx, pb, modules.DiffApply)
	blockHeaderMap := tx.Bucket(BlockHeaderMap)
	err := blockHeaderMap.Put(bid[:], encoding.Marshal(*pbh))
	if err != nil {
		return err
	}

	// Sanity check preparation - set the consensus hash at this height so that
	// during reverting a check can be performed to assure consistency when
	// adding and removing blocks. Must happen after the block is added to the
	// path.
	if build.DEBUG {
		pb.ConsensusChecksum = consensusChecksum(tx)
	}

	return blockMap.Put(bid[:], encoding.Marshal(*pb))
}

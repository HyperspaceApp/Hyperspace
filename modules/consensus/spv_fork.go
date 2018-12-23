package consensus

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// backtrackHeadersToCurrentPath traces backwards from 'pb' until it reaches a header
// in the ConsensusSet's current path (the "common parent"). It returns the
// (inclusive) set of blocks between the common parent and 'pb', starting from
// the former.
func backtrackHeadersToCurrentPath(tx *bolt.Tx, ph *modules.ProcessedBlockHeader) []*modules.ProcessedBlockHeader {
	path := []*modules.ProcessedBlockHeader{ph}
	for {
		// Error is not checked in production code - an error can only indicate
		// that pb.Height > blockHeight(tx).
		currentPathID, err := getPath(tx, ph.Height)
		if currentPathID == ph.BlockHeader.ID() {
			break
		}
		// Sanity check - an error should only indicate that pb.Height >
		// blockHeight(tx).
		if build.DEBUG && err != nil && ph.Height <= blockHeight(tx) {
			panic(err)
		}
		// Prepend the next block to the list of blocks leading from the
		// current path to the input block.
		ph, err = getBlockHeaderMap(tx, ph.BlockHeader.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		path = append([]*modules.ProcessedBlockHeader{ph}, path...)
	}
	return path
}

// revertToHeader will revert headers from the ConsensusSet's current path until
// 'ph' is the current block. Blocks are returned in the order that they were
// reverted.  'ph' is not reverted.
func (cs *ConsensusSet) revertToHeader(tx *bolt.Tx, ph *modules.ProcessedBlockHeader) (revertedHeaders []*modules.ProcessedBlockHeader) {
	// Sanity check - make sure that pb is in the current path.
	currentPathID, err := getPath(tx, ph.Height)
	if build.DEBUG && (err != nil || currentPathID != ph.BlockHeader.ID()) {
		panic(errExternalRevert)
	}
	// log.Printf("reverting header: %d %d %s %s", blockHeight(tx), ph.Height, currentBlockID(tx), ph.BlockHeader.ID())

	for currentBlockID(tx) != ph.BlockHeader.ID() {
		header := currentProcessedHeader(tx)
		commitHeaderDiffSet(tx, header, modules.DiffRevert)
		// if block exist, need to revert diffs too
		block, err := getBlockMap(tx, header.BlockHeader.ID())
		if err == nil {
			commitSingleBlockDiffSet(tx, block, modules.DiffRevert)
		} else {
			if err != errNilItem {
				panic(err)
			}
		}

		revertedHeaders = append(revertedHeaders, header)
		// Sanity check - after removing a block, check that the consensus set
		// has maintained consistency.
		if build.Release == "testing" {
			cs.checkHeaderConsistency(tx)
		} else {
			cs.maybeCheckHeaderConsistency(tx)
		}
	}
	return revertedHeaders
}

func (cs *ConsensusSet) checkSingleBlockApplyConsistency(tx *bolt.Tx, block *processedBlock) {
	scoBucket := tx.Bucket(SiacoinOutputs)
	for _, txn := range block.Block.Transactions {
		for i := range txn.SiacoinOutputs {
			scoid := txn.SiacoinOutputID(uint64(i))
			scoBytes := scoBucket.Get(scoid[:])
			if scoBytes == nil {
				panic(fmt.Errorf("checkSingleBlockApplyConsistency fail: %s", scoid))
			}
		}
	}
}

func (cs *ConsensusSet) applySingleBlock(tx *bolt.Tx, block *processedBlock) (err error) {
	// Backtrack to the common parent of 'bn' and current path and then apply the new blocks.

	if block.DiffsGenerated {
		commitSingleBlockDiffSet(tx, block, modules.DiffApply)
	} else {
		// log.Printf("before generateAndApplyDiffForSPV: %s", block.Block.ID())
		err := cs.generateAndApplyDiffForSPV(tx, block)
		// log.Printf("after generateAndApplyDiffForSPV: %s", block.Block.ID())
		if err != nil {
			// Mark the block as invalid.
			cs.dosBlocks[block.Block.ID()] = struct{}{}
			return err
		}
	}

	// Sanity check - after applying a block, check that the consensus set
	// has maintained consistency.
	if build.Release == "testing" {
		cs.checkHeaderConsistency(tx)
		cs.checkSingleBlockApplyConsistency(tx, block)
	} else {
		cs.maybeCheckHeaderConsistency(tx)
	}

	return nil
}

// applyUntilHeader will successively apply the headers between the consensus
// set's current path and 'ph'.
func (cs *ConsensusSet) applyUntilHeader(tx *bolt.Tx, ph *modules.ProcessedBlockHeader) (headers []*modules.ProcessedBlockHeader, err error) {
	// Backtrack to the common parent of 'bn' and current path and then apply the new blocks.
	// log.Printf("applyUntilHeader height:%d, %s", ph.Height, ph.BlockHeader.ID())
	newPath := backtrackHeadersToCurrentPath(tx, ph)
	for _, header := range newPath[1:] {
		id := header.BlockHeader.ID()
		// log.Printf("createDSCOBucket&applyMaturedSiacoinOutputsForHeader %d", len(header.SiacoinOutputDiffs))
		if len(header.SiacoinOutputDiffs) == 0 {
			createDSCOBucket(tx, header.Height+types.MaturityDelay)
			applyMaturedSiacoinOutputsForHeader(tx, header)
			updateCurrentPath(tx, id, modules.DiffApply)
			headerMap := tx.Bucket(BlockHeaderMap)
			headerMap.Put(id[:], encoding.Marshal(*header))
			// log.Printf("after createDSCOBucket&applyMaturedSiacoinOutputsForHeader %d", len(header.SiacoinOutputDiffs))
		} else {
			commitHeaderDiffSet(tx, header, modules.DiffApply)
		}

		block, err := getBlockMap(tx, id)
		if err == nil {
			commitSingleBlockDiffSet(tx, block, modules.DiffApply)
		} else {
			if err != errNilItem {
				panic(err)
			}
		}
		headers = append(headers, header)

		// Sanity check - after applying a block, check that the consensus set
		// has maintained consistency.
		// NOTE: no every block payout info, no totally supply, no parent block, so won't check consistency in spv
		if build.Release == "testing" {
			cs.checkHeaderConsistency(tx)
		} else {
			cs.maybeCheckHeaderConsistency(tx)
		}
	}
	return headers, nil
}

// forkHeadersBlockchain will move the consensus set onto the 'newHeaders' fork. An
// error will be returned if any of the blocks headers in the transition are
// found to be invalid. forkHeadersBlockchain is atomic; the ConsensusSet is only
// updated if the function returns nil.
func (cs *ConsensusSet) forkHeadersBlockchain(tx *bolt.Tx, newHeader *modules.ProcessedBlockHeader) (revertedHeaders, appliedHeaders []*modules.ProcessedBlockHeader, err error) {
	// log.Printf("\n\nforkHeadersBlockchain height:%d, %s", newHeader.Height, newHeader.BlockHeader.ID())
	commonParent := backtrackHeadersToCurrentPath(tx, newHeader)[0]
	// log.Printf("commonParent:%d, %s", commonParent.Height, commonParent.BlockHeader.ID())
	revertedHeaders = cs.revertToHeader(tx, commonParent)
	appliedHeaders, err = cs.applyUntilHeader(tx, newHeader)
	if err != nil {
		return revertedHeaders, appliedHeaders, err
	}

	// log.Printf("forkHeadersBlockchain height end:%d, %s\n\n", newHeader.Height, newHeader.BlockHeader.ID())
	return revertedHeaders, appliedHeaders, nil
}

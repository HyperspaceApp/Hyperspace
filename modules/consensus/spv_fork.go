package consensus

import (
	"log"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// revertToHeader will revert headers from the ConsensusSet's current path until
// 'ph' is the current block. Blocks are returned in the order that they were
// reverted.  'ph' is not reverted.
func (cs *ConsensusSet) revertToHeader(tx *bolt.Tx, ph *modules.ProcessedBlockHeader) (revertedHeaders []*modules.ProcessedBlockHeader) {
	// Sanity check - make sure that pb is in the current path.
	currentPathID, err := getPath(tx, ph.Height)
	if build.DEBUG && (err != nil || currentPathID != ph.BlockHeader.ID()) {
		panic(errExternalRevert)
	}

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
			cs.checkConsistency(tx)
		} else {
			cs.maybeCheckConsistency(tx)
		}
	}
	return revertedHeaders
}

func (cs *ConsensusSet) applySingleBlock(tx *bolt.Tx, block *processedBlock) (err error) {
	// Backtrack to the common parent of 'bn' and current path and then apply the new blocks.

	if block.DiffsGenerated {
		commitSingleBlockDiffSet(tx, block, modules.DiffApply)
	} else {
		err := generateAndApplyDiffForSPV(tx, block)
		if err != nil {
			// Mark the block as invalid.
			cs.dosBlocks[block.Block.ID()] = struct{}{}
			return err
		}
	}

	// Sanity check - after applying a block, check that the consensus set
	// has maintained consistency.
	if build.Release == "testing" {
		cs.checkConsistency(tx)
	} else {
		cs.maybeCheckConsistency(tx)
	}

	return nil
}

// applyUntilHeader will successively apply the headers between the consensus
// set's current path and 'ph'.
func (cs *ConsensusSet) applyUntilHeader(tx *bolt.Tx, ph *modules.ProcessedBlockHeader) (headers []*modules.ProcessedBlockHeader) {
	// Backtrack to the common parent of 'bn' and current path and then apply the new blocks.
	newPath := backtrackHeadersToCurrentPath(tx, ph)
	for _, header := range newPath[1:] {
		headerMap := tx.Bucket(BlockHeaderMap)
		id := ph.BlockHeader.ID()
		headerMap.Put(id[:], encoding.Marshal(*header))
		headers = append(headers, header)
		createDSCOBucket(tx, ph.Height+types.MaturityDelay)

		applyMaturedSiacoinOutputsForHeader(tx, header) // deal delay stuff in header accpetance

		// Sanity check - after applying a block, check that the consensus set
		// has maintained consistency.
		// TODO: figure out what to check later
		if build.Release == "testing" {
			cs.checkConsistency(tx)
		} else {
			cs.maybeCheckConsistency(tx)
		}
	}
	return headers
}

// forkHeadersBlockchain will move the consensus set onto the 'newHeaders' fork. An
// error will be returned if any of the blocks headers in the transition are
// found to be invalid. forkHeadersBlockchain is atomic; the ConsensusSet is only
// updated if the function returns nil.
func (cs *ConsensusSet) forkHeadersBlockchain(tx *bolt.Tx, newHeader *modules.ProcessedBlockHeader) (revertedBlocks, appliedHeaders []*modules.ProcessedBlockHeader) {
	commonParent := backtrackHeadersToCurrentPath(tx, newHeader)[0]
	revertedBlocks = cs.revertToHeader(tx, commonParent)
	for _, pbh := range revertedBlocks {
		updateCurrentPath(tx, pbh.BlockHeader.ID(), modules.DiffRevert) // fix getPath function
	}
	log.Printf("apply until height:%d, %s", newHeader.Height, newHeader.BlockHeader.ID())
	appliedHeaders = cs.applyUntilHeader(tx, newHeader)
	for _, pbh := range appliedHeaders {
		updateCurrentPath(tx, pbh.BlockHeader.ID(), modules.DiffApply)
	}

	return revertedBlocks, appliedHeaders
}

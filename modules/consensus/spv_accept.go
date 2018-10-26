package consensus

import (
	"fmt"
	"log"
	"os"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

func (cs *ConsensusSet) validateSingleHeaderAndBlockForSPV(tx dbTx, b types.Block, id types.BlockID) (parentHeader *modules.ProcessedBlockHeader, err error) {
	// Check if the block is a DoS block - a known invalid block that is expensive
	// to validate.
	_, exists := cs.dosBlocks[id]
	if exists {
		return nil, errDoSBlock
	}

	// Check if the block is already known.
	blockMap := tx.Bucket(BlockMap)
	if blockMap == nil {
		return nil, errNoBlockMap
	}
	blockHeaderMap := tx.Bucket(BlockHeaderMap)
	if blockHeaderMap == nil {
		return nil, errNoHeaderMap
	}
	if blockMap.Get(id[:]) != nil {
		return nil, modules.ErrBlockKnown
	}

	// Check for the parent.
	parentID := b.ParentID
	parentHeader, exists = cs.processedBlockHeaders[parentID]
	if !exists {
		return nil, errOrphan
	}
	// Check that the timestamp is not too far in the past to be acceptable.
	minTimestamp := cs.blockRuleHelper.minimumValidChildTimestamp(blockHeaderMap, parentID, parentHeader.BlockHeader.Timestamp)

	err = cs.blockValidator.ValidateBlock(b, id, minTimestamp, parentHeader.ChildTarget, parentHeader.Height+1, cs.log)
	if err != nil {
		return nil, err
	}
	return parentHeader, nil
}

func (cs *ConsensusSet) addSingleBlock(tx *bolt.Tx, b types.Block,
	parentHeader *modules.ProcessedBlockHeader) (newNode *processedBlock, err error) {
	// Prepare the child processed block associated with the parent block.
	newNode, _ = cs.newSingleChild(tx, parentHeader, b)

	// Fork the blockchain and put the new heaviest block at the tip of the
	// chain.
	// revertedBlocks, appliedBlocks, err = cs.forkBlockchain(tx, newNode, newNodeHeader)
	// log.Printf("before applySingleBlock: %s", b.ID())
	cs.applySingleBlock(tx, newNode)
	// log.Printf("after applySingleBlock: %s", b.ID())

	return
}

// addHeaderToTree adds a headerNode to the tree by adding it to it's parents list of children.
func (cs *ConsensusSet) addHeaderToTree(tx *bolt.Tx, parentHeader *modules.ProcessedBlockHeader, header modules.TransmittedBlockHeader) (ce changeEntry, err error) {
	// Prepare the child header node associated with the parent header.
	newNode := cs.newHeaderChild(tx, parentHeader, header)
	// Check whether the new node is part of a chain that is heavier than the
	// current node. If not, return ErrNonExtending and don't fork the
	// blockchain.
	currentNode := currentProcessedHeader(tx)
	if !newNode.HeavierThan(currentNode) {
		return changeEntry{}, modules.ErrNonExtendingBlock
	}
	// Fork the blockchain and put the new heaviest headers at the tip of the
	// chain.
	var revertedBlocks, appliedBlocks []*modules.ProcessedBlockHeader
	revertedBlocks, appliedBlocks, err = cs.forkHeadersBlockchain(tx, newNode)
	if err != nil {
		return ce, err
	}
	for _, rn := range revertedBlocks {
		ce.RevertedBlocks = append(ce.RevertedBlocks, rn.BlockHeader.ID())
	}
	for _, an := range appliedBlocks {
		ce.AppliedBlocks = append(ce.AppliedBlocks, an.BlockHeader.ID())
	}
	err = appendChangeLog(tx, ce)
	if err != nil {
		return changeEntry{}, err
	}
	return ce, nil
}

// managedAcceptBlocksForSPV deal with a new block for spv comes in
func (cs *ConsensusSet) managedAcceptSingleBlock(tx *bolt.Tx, block types.Block) (*processedBlock, error) {
	// deal the lock outside to avoid lock twice
	// cs.mu.Lock()
	// defer cs.mu.Unlock()
	log.Printf("managedAcceptSingleBlock: %s", block.ID())

	parentHeader, setErr := cs.validateSingleHeaderAndBlockForSPV(boltTxWrapper{tx}, block, block.ID())
	// if err == errFutureTimestamp { // header should already downloaded
	// 	// Queue the block to be tried again if it is a future block.
	// 	go cs.threadedSleepOnFutureBlock(block)
	// }
	var pb *processedBlock
	if setErr == nil {
		// log.Printf("before add single block: %s", block.ID())
		// Try adding the block to consensus.
		pb, setErr = cs.addSingleBlock(tx, block, parentHeader)
		// TODO: still have tryTransactionSet deadlock
		// setErr = cs.db.Update(func(updateTx *bolt.Tx) error {
		// 	var errAddSingleBlock error
		// 	pb, errAddSingleBlock = cs.addSingleBlock(updateTx, block, parentHeader)
		// 	return errAddSingleBlock
		// })
		// log.Printf("after add single block: %s", block.ID())
	}

	if _, ok := setErr.(bolt.MmapError); ok {
		cs.log.Println("ERROR: Bolt mmap failed:", setErr)
		fmt.Println("Blockchain database has run out of disk space!")
		os.Exit(1)
	}
	if setErr != nil {
		fmt.Println("Received an invalid single block.")
		cs.log.Println("Consensus received an invalid single block:", setErr)
		return nil, setErr
	}

	return pb, nil
}

// managedAcceptHeaders will try to add headers to the consensus set. If the
// headers do not extend the longest currently known chain, an error is
// returned but the headers are still kept in memory. If the headers extend a fork
// such that the fork becomes the longest currently known chain, the consensus
// set will reorganize itself to recognize the new longest fork. Accepted
// headers are not relayed.
func (cs *ConsensusSet) managedAcceptHeaders(headers []modules.TransmittedBlockHeader) (bool, []changeEntry, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	// Make sure that headers are consecutive. Though this isn't a strict
	// requirement, if blocks are not consecutive then it becomes a lot harder
	// to maintain correctness when adding multiple blocks in a single tx.
	// This is the first time that IDs on the blocks have been computed.
	headerIds := make([]types.BlockID, 0, len(headers))
	for i := 0; i < len(headers); i++ {
		headerIds = append(headerIds, headers[i].BlockHeader.ID())
		if i > 0 && headers[i].BlockHeader.ParentID != headerIds[i-1] {
			return false, []changeEntry{}, errNonLinearChain
		}
	}
	// Verify the headers, throw out known headers, and the
	// invalid headers (which includes the children of invalid headers).
	chainExtended := false
	changes := make([]changeEntry, 0, len(headers))
	setErr := cs.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < len(headers); i++ {
			//start by checking the header of the block
			parentHeader, err := cs.validateHeader(boltTxWrapper{tx}, headers[i].BlockHeader)
			if err == modules.ErrHeaderKnown {
				// Skip over known headers.
				continue
			}
			if err == errFutureTimestamp {
				// Queue the block to be tried again if it is a future block.
				go cs.threadedSleepOnFutureHeader(headers[i])
			}
			if err != nil {
				return err
			}
			// Try adding the header to consensus.
			changeEntry, err := cs.addHeaderToTree(tx, parentHeader, headers[i])
			if err == nil {
				chainExtended = true
			}
			if err == modules.ErrNonExtendingBlock {
				err = nil
			}
			if err != nil {
				return err
			}
			// Sanity check - If reverted blocks is zero, applied blocks should also
			// be zero.
			if build.DEBUG && len(changeEntry.AppliedBlocks) == 0 && len(changeEntry.RevertedBlocks) != 0 {
				panic("after adding a change entry, there are no applied blocks but there are reverted blocks")
			}
			// Append to the set of changes, and append the valid block.
			changes = append(changes, changeEntry)
		}
		return nil
	})

	if _, ok := setErr.(bolt.MmapError); ok {
		cs.log.Println("ERROR: Bolt mmap failed:", setErr)
		fmt.Println("Blockchain database has run out of disk space!")
		os.Exit(1)
	}
	if setErr != nil {
		if len(changes) == 0 {
			if setErr != modules.ErrBlockKnown {
				log.Println("Received an invalid block header set.")
			}
			cs.log.Println("Consensus received an invalid block header:", setErr)
		} else {
			fmt.Println("Received a partially valid block header set.")
			cs.log.Println("Consensus received a chain of block headers, where one was valid, but others were not:", setErr)
		}
		return false, []changeEntry{}, setErr
	}
	// Stop here if the blocks did not extend the longest blockchain.
	if !chainExtended {
		return false, []changeEntry{}, modules.ErrNonExtendingBlock
	}
	// log.Printf("managedAcceptHeaders changes: %d", len(changes))
	// Send any changes to subscribers.
	for i := 0; i < len(changes); i++ {
		cs.updateHeaderSubscribers(changes[i])
	}
	return chainExtended, changes, nil
}

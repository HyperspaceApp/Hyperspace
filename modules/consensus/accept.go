package consensus

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

const (
	minimumSendProcessedHeaderPeerVersion = "0.2.1"
)

var (
	errDoSBlock        = errors.New("block is known to be invalid")
	errInconsistentSet = errors.New("consensus set is not in a consistent state")
	errNoBlockMap      = errors.New("block map is not in database")
	errNonLinearChain  = errors.New("block set is not a contiguous chain")
	errOrphan          = errors.New("block has no known parent")
	errNoHeaderMap     = errors.New("header map is not in database")
	errHeaderNotExist  = errors.New("header is not in database")
)

func remoteSupportsSPVHeader(v string) bool {
	return build.VersionCmp(v, minimumSendProcessedHeaderPeerVersion) >= 0
}

// managedBroadcastBlock will broadcast a block to the consensus set's peers.
func (cs *ConsensusSet) managedBroadcastBlock(bh types.BlockHeader) {
	// broadcast the block header to all peers
	// broadcast header and processed header to different version
	var bhPeers, pbhPeers []modules.Peer
	for _, p := range cs.gateway.Peers() {
		if remoteSupportsSPVHeader(p.Version) {
			pbhPeers = append(pbhPeers, p)
		} else {
			// old version
			bhPeers = append(bhPeers, p)
		}
	}
	if len(bhPeers) > 0 {
		go cs.gateway.Broadcast(modules.RelayHeaderCmd, bh, bhPeers)
	}
	if len(pbhPeers) > 0 {
		pbh, exist := cs.processedBlockHeaders[bh.ID()]
		if !exist {
			cs.log.Printf("broadcast header don't have related processed header")
		}
		go cs.gateway.Broadcast(modules.RelayHeaderCmd, pbh.ForSend(), pbhPeers)
	}
}

// validateHeaderAndBlock does some early, low computation verification on the
// block. Callers should not assume that validation will happen in a particular
// order.
func (cs *ConsensusSet) validateHeaderAndBlock(tx dbTx, b types.Block, id types.BlockID) (parent *processedBlock, err error) {
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
	if blockMap.Get(id[:]) != nil {
		return nil, modules.ErrBlockKnown
	}

	// Check for the parent.
	parentID := b.ParentID
	parentBytes := blockMap.Get(parentID[:])
	if parentBytes == nil {
		return nil, errOrphan
	}
	parent = new(processedBlock)
	err = cs.marshaler.Unmarshal(parentBytes, parent)
	if err != nil {
		return nil, err
	}
	// Check that the timestamp is not too far in the past to be acceptable.
	minTimestamp := cs.blockRuleHelper.minimumValidChildTimestamp(blockMap, parent.Block.ParentID, parent.Block.Timestamp)

	err = cs.blockValidator.ValidateBlock(b, id, minTimestamp, parent.ChildTarget, parent.Height+1, cs.log)
	if err != nil {
		return nil, err
	}
	return parent, nil
}

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
	minTimestamp := cs.blockRuleHelper.minimumValidChildTimestamp(blockMap, parentID, parentHeader.BlockHeader.Timestamp)

	err = cs.blockValidator.ValidateBlock(b, id, minTimestamp, parentHeader.ChildTarget, parentHeader.Height+1, cs.log)
	if err != nil {
		return nil, err
	}
	return parentHeader, nil
}

// checkHeaderTarget returns true if the header's ID meets the given target.
func checkHeaderTarget(h types.BlockHeader, target types.Target) bool {
	blockHash := h.ID()
	return bytes.Compare(target[:], blockHash[:]) >= 0
}

// validateHeader does some early, low computation verification on the header
// to determine if the block should be downloaded. Callers should not assume
// that validation will happen in a particular order.
func (cs *ConsensusSet) validateHeader(tx dbTx, h types.BlockHeader) (parentHeader *modules.ProcessedBlockHeader, err error) {
	// Check if the block is a DoS block - a known invalid block that is expensive
	// to validate.
	id := h.ID()
	_, exists := cs.dosBlocks[id]
	if exists {
		return nil, errDoSBlock
	}

	// Check if the header is already known.
	headerMap := tx.Bucket(BlockHeaderMap)
	if headerMap == nil {
		return nil, errNoHeaderMap
	}

	_, exists = cs.processedBlockHeaders[id]
	if exists {
		return nil, modules.ErrBlockKnown
	}

	// Check for the parent.
	parentID := h.ParentID

	parentHeader, exists = cs.processedBlockHeaders[parentID]
	if !exists {
		return nil, errOrphan
	}

	// Check that the target of the new block is sufficient.
	if !checkHeaderTarget(h, parentHeader.ChildTarget) {
		return nil, modules.ErrBlockUnsolved
	}

	// TODO: check if the block is a non extending block once headers-first
	// downloads are implemented.

	// TODO: change header to use memory key map too, should be faster
	// Check that the timestamp is not too far in the past to be acceptable.
	minTimestamp := cs.blockRuleHelper.minimumValidChildTimestamp(headerMap, parentHeader.BlockHeader.ParentID, parentHeader.BlockHeader.Timestamp)
	if minTimestamp > h.Timestamp {
		return nil, errEarlyTimestamp
	}

	// Check if the block is in the extreme future. We make a distinction between
	// future and extreme future because there is an assumption that by the time
	// the extreme future arrives, this block will no longer be a part of the
	// longest fork because it will have been ignored by all of the miners.
	if h.Timestamp > types.CurrentTimestamp()+types.ExtremeFutureThreshold {
		return nil, errExtremeFutureTimestamp
	}

	// We do not check if the header is in the near future here, because we want
	// to get the corresponding block as soon as possible, even if the block is in
	// the near future.

	return parentHeader, nil
}

// addBlockToTree inserts a block into the blockNode tree by adding it to its
// parent's list of children. If the new blockNode is heavier than the current
// node, the blockchain is forked to put the new block and its parents at the
// tip. An error will be returned if block verification fails or if the block
// does not extend the longest fork.
//
// addBlockToTree might need to modify the database while returning an error
// on the block. Such errors are handled outside of the transaction by the
// caller. Switching to a managed tx through bolt will make this complexity
// unneeded.
func (cs *ConsensusSet) addBlockToTree(tx *bolt.Tx, b types.Block, parent *processedBlock) (ce changeEntry, err error) {
	// Prepare the child processed block associated with the parent block.
	newNode, newNodeHeader := cs.newChild(tx, parent, b)

	// Check whether the new node is part of a chain that is heavier than the
	// current node. If not, return ErrNonExtending and don't fork the
	// blockchain.
	currentNode := currentProcessedBlock(tx)
	if !newNode.heavierThan(currentNode) {
		return changeEntry{}, modules.ErrNonExtendingBlock
	}

	// Fork the blockchain and put the new heaviest block at the tip of the
	// chain.
	var revertedBlocks, appliedBlocks []*processedBlock
	revertedBlocks, appliedBlocks, err = cs.forkBlockchain(tx, newNode, newNodeHeader)
	if err != nil {
		return changeEntry{}, err
	}
	for _, rn := range revertedBlocks {
		ce.RevertedBlocks = append(ce.RevertedBlocks, rn.Block.ID())
	}
	for _, an := range appliedBlocks {
		ce.AppliedBlocks = append(ce.AppliedBlocks, an.Block.ID())
	}
	err = appendChangeLog(tx, ce)
	if err != nil {
		return changeEntry{}, err
	}

	return ce, nil
}

func (cs *ConsensusSet) addSingleBlock(tx *bolt.Tx, b types.Block,
	parentHeader *modules.ProcessedBlockHeader) (newNode *processedBlock, err error) {
	// Prepare the child processed block associated with the parent block.
	newNode, _ = cs.newSingleChildForSPV(tx, parentHeader, b)

	// Fork the blockchain and put the new heaviest block at the tip of the
	// chain.
	// revertedBlocks, appliedBlocks, err = cs.forkBlockchain(tx, newNode, newNodeHeader)
	// TODO: generate diffs for current block
	cs.applyUntilBlockForSPV(tx, newNode)

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
	revertedBlocks, appliedBlocks = cs.forkHeadersBlockchain(tx, newNode)
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

// threadedSleepOnFutureBlock will sleep until the timestamp of a future block
// has arrived.
//
// TODO: An attacker can broadcast a future block multiple times, resulting in a
// goroutine spinup for each future block.  Need to prevent that.
//
// TODO: An attacker could produce a very large number of future blocks,
// consuming memory. Need to prevent that.
func (cs *ConsensusSet) threadedSleepOnFutureBlock(b types.Block) {
	// Add this thread to the threadgroup.
	err := cs.tg.Add()
	if err != nil {
		return
	}
	defer cs.tg.Done()

	// Perform a soft-sleep while we wait for the block to become valid.
	select {
	case <-cs.tg.StopChan():
		return
	case <-time.After(time.Duration(b.Timestamp-(types.CurrentTimestamp()+types.FutureThreshold)) * time.Second):
		_, err := cs.managedAcceptBlocks([]types.Block{b})
		if err != nil {
			cs.log.Debugln("WARN: failed to accept a future block:", err)
		}
		cs.managedBroadcastBlock(b.Header())
	}
}

// threadedSleepOnFutureHeader will sleep until the timestamp of a future header
// has arrived.
//
// TODO: An attacker can broadcast a future block multiple times, resulting in a
// goroutine spinup for each future block.  Need to prevent that.
//
// TODO: An attacker could produce a very large number of future blocks,
// consuming memory. Need to prevent that.
func (cs *ConsensusSet) threadedSleepOnFutureHeader(bh modules.TransmittedBlockHeader) {
	// Add this thread to the threadgroup.
	err := cs.tg.Add()
	if err != nil {
		return
	}
	defer cs.tg.Done()
	// Perform a soft-sleep while we wait for the block to become valid.
	select {
	case <-cs.tg.StopChan():
		return
	case <-time.After(time.Duration(bh.BlockHeader.Timestamp-(types.CurrentTimestamp()+types.FutureThreshold)) * time.Second):
		_, _, err := cs.managedAcceptHeaders([]modules.TransmittedBlockHeader{bh})
		if err != nil {
			cs.log.Debugln("WARN: failed to accept a future block header:", err)
		}
	}
}

// managedAcceptBlocks will try to add blocks to the consensus set. If the
// blocks do not extend the longest currently known chain, an error is
// returned but the blocks are still kept in memory. If the blocks extend a fork
// such that the fork becomes the longest currently known chain, the consensus
// set will reorganize itself to recognize the new longest fork. Accepted
// blocks are not relayed.
//
// Typically AcceptBlock should be used so that the accepted block is relayed.
// This method is typically only be used when there would otherwise be multiple
// consecutive calls to AcceptBlock with each successive call accepting the
// child block of the previous call.
func (cs *ConsensusSet) managedAcceptBlocks(blocks []types.Block) (blockchainExtended bool, err error) {
	// Grab a lock on the consensus set.
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Make sure that blocks are consecutive. Though this isn't a strict
	// requirement, if blocks are not consecutive then it becomes a lot harder
	// to maintain correctness when adding multiple blocks in a single tx.
	//
	// This is the first time that IDs on the blocks have been computed.
	blockIDs := make([]types.BlockID, 0, len(blocks))
	for i := 0; i < len(blocks); i++ {
		blockIDs = append(blockIDs, blocks[i].ID())
		if i > 0 && blocks[i].ParentID != blockIDs[i-1] {
			return false, errNonLinearChain
		}
	}

	// Verify the headers for every block, throw out known blocks, and the
	// invalid blocks (which includes the children of invalid blocks).
	chainExtended := false
	changes := make([]changeEntry, 0, len(blocks))
	setErr := cs.db.Update(func(tx *bolt.Tx) error {
		for i := 0; i < len(blocks); i++ {
			// Start by checking the header of the block.
			parent, err := cs.validateHeaderAndBlock(boltTxWrapper{tx}, blocks[i], blockIDs[i])
			if err == modules.ErrBlockKnown {
				// Skip over known blocks.
				continue
			}
			if err == errFutureTimestamp {
				// Queue the block to be tried again if it is a future block.
				go cs.threadedSleepOnFutureBlock(blocks[i])
			}
			if err != nil {
				return err
			}

			// Try adding the block to consensus.
			changeEntry, err := cs.addBlockToTree(tx, blocks[i], parent)
			if err == nil {
				changes = append(changes, changeEntry)
				chainExtended = true
				var applied, reverted []string
				for _, b := range changeEntry.AppliedBlocks {
					applied = append(applied, b.String()[:6])
				}
				for _, b := range changeEntry.RevertedBlocks {
					reverted = append(reverted, b.String()[:6])
				}
			}
			if err == modules.ErrNonExtendingBlock {
				err = nil
			}
			if err != nil {
				return err
			}
			// Sanity check - we should never apply fewer blocks than we revert.
			if len(changeEntry.AppliedBlocks) < len(changeEntry.RevertedBlocks) {
				err := errors.New("after adding a change entry, there are more reverted blocks than applied ones")
				cs.log.Severe(err)
				return err
			}
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
			fmt.Println("Received an invalid block set.")
			cs.log.Println("Consensus received an invalid block:", setErr)
		} else {
			fmt.Println("Received a partially valid block set.")
			cs.log.Println("Consensus received a chain of blocks, where one was valid, but others were not:", setErr)
		}
		return false, setErr
	}
	// Stop here if the blocks did not extend the longest blockchain.
	if !chainExtended {
		return false, modules.ErrNonExtendingBlock
	}
	// Send any changes to subscribers.
	for i := 0; i < len(changes); i++ {
		cs.updateSubscribers(changes[i])
	}
	return chainExtended, nil
}

// managedAcceptBlocksForSPV deal with a new block for spv comes in
func (cs *ConsensusSet) managedAcceptSingleBlock(block types.Block, changes []changeEntry) (*processedBlock, error) {
	// Grab a lock on the consensus set.
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var pb *processedBlock

	setErr := cs.db.Update(func(tx *bolt.Tx) error {
		parentHeader, err := cs.validateSingleHeaderAndBlockForSPV(boltTxWrapper{tx}, block, block.ID())
		if err == modules.ErrBlockKnown {
			// Skip over known blocks.
			return err
		}
		// if err == errFutureTimestamp { // header should already downloaded
		// 	// Queue the block to be tried again if it is a future block.
		// 	go cs.threadedSleepOnFutureBlock(block)
		// }
		if err != nil {
			return err
		}

		// Try adding the block to consensus.
		pb, err = cs.addSingleBlock(tx, block, parentHeader)
		return err
	})
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
	for i := 0; i < len(changes); i++ {
		cs.updateHeaderSubscribers(changes[i])
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
			fmt.Println("Received an invalid block header set.")
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
	// Send any changes to subscribers.
	for i := 0; i < len(changes); i++ {
		cs.updateHeaderSubscribers(changes[i])
	}
	return chainExtended, changes, nil
}

// AcceptBlock will try to add a block to the consensus set. If the block does
// not extend the longest currently known chain, an error is returned but the
// block is still kept in memory. If the block extends a fork such that the
// fork becomes the longest currently known chain, the consensus set will
// reorganize itself to recognize the new longest fork. If a block is accepted
// without error, it will be relayed to all connected peers. This function
// should only be called for new blocks.
func (cs *ConsensusSet) AcceptBlock(b types.Block) error {
	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()

	chainExtended, err := cs.managedAcceptBlocks([]types.Block{b})
	if err != nil {
		return err
	}
	if chainExtended {
		cs.managedBroadcastBlock(b.Header())
	}
	return nil
}

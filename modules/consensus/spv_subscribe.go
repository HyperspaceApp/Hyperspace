package consensus

import (
	"fmt"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/coreos/bbolt"
)

func (cs *ConsensusSet) updateHeaderSubscribers(ce changeEntry) {
	if len(cs.headerSubscribers) == 0 {
		return
	}

	hcc, err := cs.matureHeaderAndComputeHeaderConsensusChange(ce)
	if err != nil {
		cs.log.Critical("computeConsensusChange failed:", err)
		return
	}
	hcc.GetSiacoinOutputDiff = cs.getSiacoinOutputDiff
	hcc.GetBlockByID = cs.getBlockByID
	hcc.TryTransactionSet = cs.tryTransactionSet

	for _, subscriber := range cs.headerSubscribers {
		subscriber.ProcessHeaderConsensusChange(hcc)
	}
}

// HeaderConsensusSetSubscribe will send change to subscriber from start
func (cs *ConsensusSet) HeaderConsensusSetSubscribe(subscriber modules.HeaderConsensusSetSubscriber, start modules.ConsensusChangeID,
	cancel <-chan struct{}) error {

	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()

	// Call managedInitializeSubscribe until the new module is up-to-date.
	for {
		start, err = cs.managedInitializeHeaderSubscribe(subscriber, start, cancel)
		if err != nil {
			return err
		}
		if cs.staticDeps.Disrupt("SleepAfterInitializeSubscribe") {
			time.Sleep(10 * time.Second)
		}
		// Check if the start equals the most recent change id. If it does we
		// are done. If it doesn't, we need to call managedInitializeSubscribe
		// again.
		cs.mu.Lock()
		recentID, err := cs.recentConsensusChangeID()
		if err != nil {
			cs.mu.Unlock()
			return err
		}
		if start == recentID {
			// break out of the loop while still holding to lock to avoid
			// updating subscribers before the new module is appended to the
			// list of subscribers.
			defer cs.mu.Unlock()
			break
		}
		cs.mu.Unlock()

		// Check for shutdown.
		select {
		case <-cs.tg.StopChan():
			return siasync.ErrStopped
		default:
		}
	}

	// Add the module to the list of subscribers.
	// Sanity check - subscriber should not be already subscribed.
	for _, s := range cs.headerSubscribers {
		if s == subscriber {
			build.Critical("refusing to double-subscribe subscriber")
		}
	}
	cs.headerSubscribers = append(cs.headerSubscribers, subscriber)
	return nil
}

// HeaderUnsubscribe will unsubscribe from the header change
func (cs *ConsensusSet) HeaderUnsubscribe(subscriber modules.HeaderConsensusSetSubscriber) {
	if cs.tg.Add() != nil {
		return
	}
	defer cs.tg.Done()
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Search for the subscriber in the list of subscribers and remove it if
	// found.
	for i := range cs.headerSubscribers {
		if cs.headerSubscribers[i] == subscriber {
			// nil the subscriber entry (otherwise it will not be GC'd if it's
			// at the end of the subscribers slice).
			cs.headerSubscribers[i] = nil
			// Delete the entry from the slice.
			cs.headerSubscribers = append(cs.headerSubscribers[0:i], cs.headerSubscribers[i+1:]...)
			break
		}
	}
}

func (cs *ConsensusSet) managedInitializeHeaderSubscribe(subscriber modules.HeaderConsensusSetSubscriber, start modules.ConsensusChangeID,
	cancel <-chan struct{}) (modules.ConsensusChangeID, error) {

	if start == modules.ConsensusChangeRecent {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return cs.recentConsensusChangeID()
	}

	// 'exists' and 'entry' are going to be pointed to the first entry that
	// has not yet been seen by subscriber.
	var exists bool
	var entry changeEntry
	cs.mu.RLock()
	err := cs.db.View(func(tx *bolt.Tx) error {
		if start == modules.ConsensusChangeBeginning {
			// Special case: for modules.ConsensusChangeBeginning, create an
			// initial node pointing to the genesis block. The subscriber will
			// receive the diffs for all blocks in the consensus set, including
			// the genesis block.
			entry = cs.genesisEntry()
			exists = true
		} else {
			// The subscriber has provided an existing consensus change.
			// Because the subscriber already has this consensus change,
			// 'entry' and 'exists' need to be pointed at the next consensus
			// change.
			entry, exists = getEntry(tx, start)
			if !exists {
				// modules.ErrInvalidConsensusChangeID is a named error that
				// signals a break in synchronization between the consensus set
				// persistence and the subscriber persistence. Typically,
				// receiving this error means that the subscriber needs to
				// perform a rescan of the consensus set.
				return modules.ErrInvalidConsensusChangeID
			}
			entry, exists = entry.NextEntry(tx)
		}
		return nil
	})
	cs.mu.RUnlock()
	if err != nil {
		return modules.ConsensusChangeID{}, err
	}

	// Nothing to do if the changeEntry doesn't exist.
	if !exists {
		return start, nil
	}

	// Send all remaining consensus changes to the subscriber.
	latestChangeID := entry.ID()
	for exists {
		// Send changes in batches of 100 so that we don't hold the
		// lock for too long.
		cs.mu.RLock()
		// err := cs.db.View(func(tx *bolt.Tx) error {
		for i := 0; i < 100 && exists; i++ {
			latestChangeID = entry.ID()
			select {
			case <-cancel:
				cs.mu.RUnlock()
				return modules.ConsensusChangeID{}, siasync.ErrStopped
			default:
			}
			hcc, err := cs.matureHeaderAndComputeHeaderConsensusChange(entry)
			if err != nil {
				cs.mu.RUnlock()
				return modules.ConsensusChangeID{}, err
			}
			hcc.GetSiacoinOutputDiff = cs.getSiacoinOutputDiff
			hcc.GetBlockByID = cs.getBlockByID
			hcc.TryTransactionSet = cs.tryTransactionSet
			subscriber.ProcessHeaderConsensusChange(hcc)
			err = cs.db.View(func(tx *bolt.Tx) error {
				entry, exists = entry.NextEntry(tx)
				return nil
			})
			if err != nil {
				cs.mu.RUnlock()
				return modules.ConsensusChangeID{}, err
			}
		}
		cs.mu.RUnlock()
	}
	return latestChangeID, nil
}

// matureAndApplyHeader this is intend for single block downloaded
// after first mature of header in header accept
func (cs *ConsensusSet) matureAndApplyHeader(tx *bolt.Tx, ce changeEntry) error {
	for _, appliedBlockID := range ce.AppliedBlocks {
		appliedBlockHeader, exist := cs.processedBlockHeaders[appliedBlockID]
		if exist {
			// because it is part of ProcessHeaderConsensus, can happen a lot of times
			// so if already matured, won't do it again
			if len(appliedBlockHeader.SiacoinOutputDiffs) > 0 {
				continue
			}
			applyMaturedSiacoinOutputsForHeader(tx, appliedBlockHeader)
			if len(appliedBlockHeader.SiacoinOutputDiffs) > 0 { // if we got some matured scod, save it to boltdb
				headerMap := tx.Bucket(BlockHeaderMap)
				id := appliedBlockHeader.BlockHeader.ID()
				err := headerMap.Put(id[:], encoding.Marshal(*appliedBlockHeader))
				if err != nil {
					return err
				}
			}
		} else {
			return fmt.Errorf("applied block header does not exist in the consensus set processed block headers: %s", appliedBlockID.String())
		}
	}
	return nil
}

func (cs *ConsensusSet) matureHeaderAndComputeHeaderConsensusChange(ce changeEntry) (modules.HeaderConsensusChange, error) {
	err := cs.db.Update(func(tx *bolt.Tx) error {
		return cs.matureAndApplyHeader(tx, ce)
	})
	if err != nil {
		return modules.HeaderConsensusChange{}, err
	}
	var hcc modules.HeaderConsensusChange
	err = cs.db.View(func(tx *bolt.Tx) error {
		var errHeaderConsensusChange error
		hcc, errHeaderConsensusChange = cs.computeHeaderConsensusChange(tx, ce)
		return errHeaderConsensusChange
	})
	if err != nil {
		return modules.HeaderConsensusChange{}, err
	}
	return hcc, nil
}

func (cs *ConsensusSet) computeHeaderConsensusChange(tx *bolt.Tx, ce changeEntry) (modules.HeaderConsensusChange, error) {
	hcc := modules.HeaderConsensusChange{
		ID: ce.ID(),
	}
	for _, revertedBlockID := range ce.RevertedBlocks {
		revertedBlockHeader, exist := cs.processedBlockHeaders[revertedBlockID]
		if exist {
			hcc.RevertedBlockHeaders = append(hcc.RevertedBlockHeaders, *revertedBlockHeader)
		} else {
			return modules.HeaderConsensusChange{}, fmt.Errorf("reverted block header does not exist in the consensus set processed block header: %s", revertedBlockID.String())
		}
		for i := len(revertedBlockHeader.SiacoinOutputDiffs) - 1; i >= 0; i-- {
			scod := revertedBlockHeader.SiacoinOutputDiffs[i]
			scod.Direction = !scod.Direction
			hcc.MaturedSiacoinOutputDiffs = append(hcc.MaturedSiacoinOutputDiffs, scod)
		}
		for i := len(revertedBlockHeader.DelayedSiacoinOutputDiffs) - 1; i >= 0; i-- {
			dscod := revertedBlockHeader.DelayedSiacoinOutputDiffs[i]
			dscod.Direction = !dscod.Direction
			hcc.DelayedSiacoinOutputDiffs = append(hcc.DelayedSiacoinOutputDiffs, dscod)
		}
	}
	for _, appliedBlockID := range ce.AppliedBlocks {
		appliedBlockHeader, exist := cs.processedBlockHeaders[appliedBlockID]
		if exist {
			hcc.AppliedBlockHeaders = append(hcc.AppliedBlockHeaders, *appliedBlockHeader)
		} else {
			return modules.HeaderConsensusChange{}, fmt.Errorf("applied block header does not exist in the consensus set processed block headers: %s", appliedBlockID.String())
		}
		for _, scod := range appliedBlockHeader.SiacoinOutputDiffs {
			hcc.MaturedSiacoinOutputDiffs = append(hcc.MaturedSiacoinOutputDiffs, scod)
		}
		for _, dscod := range appliedBlockHeader.DelayedSiacoinOutputDiffs {
			hcc.DelayedSiacoinOutputDiffs = append(hcc.DelayedSiacoinOutputDiffs, dscod)
		}
	}

	// Grab the child target and the minimum valid child timestamp.
	recentBlock := ce.AppliedBlocks[len(ce.AppliedBlocks)-1]
	// pbh, exists := cs.processedBlockHeaders[recentBlock]
	// if !exists {
	// 	cs.log.Critical("could not find process header for known header")
	// }
	// hcc.ChildTarget = pbh.ChildTarget
	// hcc.MinimumValidChildTimestamp = cs.blockRuleHelper.minimumValidChildTimestamp(tx.Bucket(BlockMap), pb.Block.ParentID, pb.Block.Timestamp)

	currentBlock := currentBlockID(tx)
	if cs.synced && recentBlock == currentBlock {
		hcc.Synced = true
	}

	return hcc, nil
}

func (cs *ConsensusSet) getSiacoinOutputDiff(id types.BlockID, direction modules.DiffDirection) (scods []modules.SiacoinOutputDiff, err error) {
	cs.log.Printf("getOrDownloadBlock: %s", id)
	pb, err := cs.getOrDownloadBlock(id)
	if err == errNilItem { // assume it is not related block, so not locally exist
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	pbScods := pb.SiacoinOutputDiffs
	if direction == modules.DiffRevert {
		for i := len(pbScods) - 1; i >= 0; i-- {
			pbScod := pbScods[i]
			pbScod.Direction = !pbScod.Direction
			scods = append(scods, pbScod)
		}
	} else {
		scods = pbScods
	}
	return
}

func (cs *ConsensusSet) getBlockByID(id types.BlockID) (types.Block, bool) {
	pb, err := cs.dbGetBlockMap(id)
	if err == errNilItem { // assume it is not related block, so not locally exist
		return types.Block{}, false
	} else if err != nil {
		panic(err)
	}
	return pb.Block, true
}

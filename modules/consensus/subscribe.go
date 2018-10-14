package consensus

import (
	"errors"
	"fmt"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/coreos/bbolt"
)

// computeConsensusChange computes the consensus change from the change entry
// at index 'i' in the change log. If i is out of bounds, an error is returned.
func (cs *ConsensusSet) computeConsensusChange(tx *bolt.Tx, ce changeEntry) (modules.ConsensusChange, error) {
	cc := modules.ConsensusChange{
		ID: ce.ID(),
	}
	for _, revertedBlockID := range ce.RevertedBlocks {
		revertedBlock, err := getBlockMap(tx, revertedBlockID)
		if err != nil {
			cs.log.Critical("getBlockMap failed in computeConsensusChange:", err)
			return modules.ConsensusChange{}, err
		}

		// Because the direction is 'revert', the order of the diffs needs to
		// be flipped and the direction of the diffs also needs to be flipped.
		cc.RevertedBlocks = append(cc.RevertedBlocks, revertedBlock.Block)
		for i := len(revertedBlock.SiacoinOutputDiffs) - 1; i >= 0; i-- {
			scod := revertedBlock.SiacoinOutputDiffs[i]
			scod.Direction = !scod.Direction
			cc.SiacoinOutputDiffs = append(cc.SiacoinOutputDiffs, scod)
		}
		for i := len(revertedBlock.FileContractDiffs) - 1; i >= 0; i-- {
			fcd := revertedBlock.FileContractDiffs[i]
			fcd.Direction = !fcd.Direction
			cc.FileContractDiffs = append(cc.FileContractDiffs, fcd)
		}
		for i := len(revertedBlock.DelayedSiacoinOutputDiffs) - 1; i >= 0; i-- {
			dscod := revertedBlock.DelayedSiacoinOutputDiffs[i]
			dscod.Direction = !dscod.Direction
			cc.DelayedSiacoinOutputDiffs = append(cc.DelayedSiacoinOutputDiffs, dscod)
		}
	}
	for _, appliedBlockID := range ce.AppliedBlocks {
		appliedBlock, err := getBlockMap(tx, appliedBlockID)
		if err != nil {
			cs.log.Critical("getBlockMap failed in computeConsensusChange:", err)
			return modules.ConsensusChange{}, err
		}

		cc.AppliedBlocks = append(cc.AppliedBlocks, appliedBlock.Block)
		for _, scod := range appliedBlock.SiacoinOutputDiffs {
			cc.SiacoinOutputDiffs = append(cc.SiacoinOutputDiffs, scod)
		}
		for _, fcd := range appliedBlock.FileContractDiffs {
			cc.FileContractDiffs = append(cc.FileContractDiffs, fcd)
		}
		for _, dscod := range appliedBlock.DelayedSiacoinOutputDiffs {
			cc.DelayedSiacoinOutputDiffs = append(cc.DelayedSiacoinOutputDiffs, dscod)
		}
	}

	// Grab the child target and the minimum valid child timestamp.
	recentBlock := ce.AppliedBlocks[len(ce.AppliedBlocks)-1]
	pb, err := getBlockMap(tx, recentBlock)
	if err != nil {
		cs.log.Critical("could not find process block for known block")
	}
	cc.ChildTarget = pb.ChildTarget
	cc.MinimumValidChildTimestamp = cs.blockRuleHelper.minimumValidChildTimestamp(tx.Bucket(BlockMap), pb.Block.ParentID, pb.Block.Timestamp)

	currentBlock := currentBlockID(tx)
	if cs.synced && recentBlock == currentBlock {
		cc.Synced = true
	}

	// Add the unexported tryTransactionSet function.
	cc.TryTransactionSet = cs.tryTransactionSet

	return cc, nil
}

// updateSubscribers will inform all subscribers of a new update to the
// consensus set. updateSubscribers does not alter the changelog, the changelog
// must be updated beforehand.
func (cs *ConsensusSet) updateSubscribers(ce changeEntry) {
	if len(cs.subscribers) == 0 {
		return
	}
	// Get the consensus change and send it to all subscribers.
	var cc modules.ConsensusChange
	err := cs.db.View(func(tx *bolt.Tx) error {
		// Compute the consensus change so it can be sent to subscribers.
		var err error
		cc, err = cs.computeConsensusChange(tx, ce)
		return err
	})
	if err != nil {
		cs.log.Critical("computeConsensusChange failed:", err)
		return
	}
	for _, subscriber := range cs.subscribers {
		subscriber.ProcessConsensusChange(cc)
	}
}

func (cs *ConsensusSet) buildDiffGetter(tx *bolt.Tx) func(types.BlockID, modules.DiffDirection) ([]modules.SiacoinOutputDiff, error) {
	return func(id types.BlockID, direction modules.DiffDirection) (scods []modules.SiacoinOutputDiff, err error) {
		pb, err := cs.getOrDownloadBlock(tx, id)
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
}

func (cs *ConsensusSet) buildBlockGetter(tx *bolt.Tx) func(types.BlockID) (types.Block, bool) {
	return func(id types.BlockID) (types.Block, bool) {
		pb, err := getBlockMap(tx, id)
		if err == errNilItem { // assume it is not related block, so not locally exist
			return types.Block{}, false
		} else if err != nil {
			panic(err)
		}
		return pb.Block, true
	}
}

func (cs *ConsensusSet) updateHeaderSubscribers(ce changeEntry) {
	if len(cs.subscribers) == 0 {
		return
	}
	// Get the consensus change and send it to all subscribers.
	var hcc modules.HeaderConsensusChange
	cs.db.View(func(tx *bolt.Tx) error {
		// Compute the consensus change so it can be sent to subscribers.
		var err error
		hcc, err = cs.computeHeaderConsensusChange(ce)
		if err != nil {
			cs.log.Critical("computeConsensusChange failed:", err)
			return nil
		}
		hcc.GetSiacoinOutputDiff = cs.buildDiffGetter(tx)
		hcc.GetBlockByID = cs.buildBlockGetter(tx)
		for _, subscriber := range cs.headerSubscribers {
			subscriber.ProcessHeaderConsensusChange(hcc)
		}
		return nil
	})
}

// managedInitializeSubscribe will take a subscriber and feed them all of the
// consensus changes that have occurred since the change provided.
//
// As a special case, using an empty id as the start will have all the changes
// sent to the modules starting with the genesis block.
func (cs *ConsensusSet) managedInitializeSubscribe(subscriber modules.ConsensusSetSubscriber, start modules.ConsensusChangeID,
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
		err = cs.db.View(func(tx *bolt.Tx) error {
			for i := 0; i < 100 && exists; i++ {
				latestChangeID = entry.ID()
				select {
				case <-cancel:
					return siasync.ErrStopped
				default:
				}
				cc, err := cs.computeConsensusChange(tx, entry)
				if err != nil {
					return err
				}
				subscriber.ProcessConsensusChange(cc)
				entry, exists = entry.NextEntry(tx)
			}
			return nil
		})
		cs.mu.RUnlock()
		if err != nil {
			return modules.ConsensusChangeID{}, err
		}
	}
	return latestChangeID, nil
}

// recentConsensusChangeID gets the ConsensusChangeID of the most recent
// change.
func (cs *ConsensusSet) recentConsensusChangeID() (cid modules.ConsensusChangeID, err error) {
	err = cs.db.View(func(tx *bolt.Tx) error {
		cl := tx.Bucket(ChangeLog)
		d := cl.Get(ChangeLogTailID)
		if d == nil {
			return errors.New("failed to retrieve recentConsensusChangeID")
		}
		copy(cid[:], d[:])
		return nil
	})
	return
}

// ConsensusSetSubscribe adds a subscriber to the list of subscribers, and
// gives them every consensus change that has occurred since the change with
// the provided id.
//
// As a special case, using an empty id as the start will have all the changes
// sent to the modules starting with the genesis block.
func (cs *ConsensusSet) ConsensusSetSubscribe(subscriber modules.ConsensusSetSubscriber, start modules.ConsensusChangeID,
	cancel <-chan struct{}) error {

	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()

	// Call managedInitializeSubscribe until the new module is up-to-date.
	for {
		start, err = cs.managedInitializeSubscribe(subscriber, start, cancel)
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
	for _, s := range cs.subscribers {
		if s == subscriber {
			build.Critical("refusing to double-subscribe subscriber")
		}
	}
	cs.subscribers = append(cs.subscribers, subscriber)
	return nil
}

// Unsubscribe removes a subscriber from the list of subscribers, allowing for
// garbage collection and rescanning. If the subscriber is not found in the
// subscriber database, no action is taken.
func (cs *ConsensusSet) Unsubscribe(subscriber modules.ConsensusSetSubscriber) {
	if cs.tg.Add() != nil {
		return
	}
	defer cs.tg.Done()
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Search for the subscriber in the list of subscribers and remove it if
	// found.
	for i := range cs.subscribers {
		if cs.subscribers[i] == subscriber {
			// nil the subscriber entry (otherwise it will not be GC'd if it's
			// at the end of the subscribers slice).
			cs.subscribers[i] = nil
			// Delete the entry from the slice.
			cs.subscribers = append(cs.subscribers[0:i], cs.subscribers[i+1:]...)
			break
		}
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
		err := cs.db.View(func(tx *bolt.Tx) error {
			for i := 0; i < 100 && exists; i++ {
				latestChangeID = entry.ID()
				select {
				case <-cancel:
					return siasync.ErrStopped
				default:
				}
				hcc, err := cs.computeHeaderConsensusChange(entry)
				if err != nil {
					return err
				}
				hcc.GetSiacoinOutputDiff = cs.buildDiffGetter(tx)
				hcc.GetBlockByID = cs.buildBlockGetter(tx)
				subscriber.ProcessHeaderConsensusChange(hcc)
				entry, exists = entry.NextEntry(tx)
			}
			return nil
		})
		cs.mu.RUnlock()
		if err != nil {
			return modules.ConsensusChangeID{}, err
		}
	}
	return latestChangeID, nil
}

func (cs *ConsensusSet) computeHeaderConsensusChange(ce changeEntry) (modules.HeaderConsensusChange, error) {
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

	return hcc, nil
}

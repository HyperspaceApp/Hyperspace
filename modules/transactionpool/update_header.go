package transactionpool

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// ProcessHeaderConsensusChange is spv version to handler consensus change
func (tp *TransactionPool) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange) {
	tp.mu.Lock()

	tp.log.Printf("CCID %v (height %v): %v applied blocks, %v reverted blocks", crypto.Hash(hcc.ID).String()[:8],
		tp.blockHeight, len(hcc.AppliedBlockHeaders), len(hcc.RevertedBlockHeaders))

	// for spv, it's about to delete txns from tpool when txn comes in

	// Get the recent block ID for a sanity check that the consensus change is
	// being provided to us correctly.
	resetSanityCheck := false
	recentID, err := tp.getRecentBlockID(tp.dbTx)
	if err == errNilRecentBlock {
		// This almost certainly means that the database hasn't been initialized
		// yet with a recent block, meaning the user was previously running
		// v1.3.1 or earlier.
		tp.log.Println("NOTE: Upgrading tpool database to support consensus change verification.")
		resetSanityCheck = true
	} else if err != nil {
		tp.log.Critical("ERROR: Could not access recentID from tpool:", err)
	}

	// Update the database of confirmed transactions.
	for _, header := range hcc.RevertedBlockHeaders {
		if header.BlockHeader.ID() != recentID && !resetSanityCheck {
			panic(fmt.Sprintf("Consensus change series appears to be inconsistent - we are reverting the wrong block. bid: %v recent: %v", header.BlockHeader.ID(), recentID))
		}
		recentID = header.BlockHeader.ParentID
		if tp.blockHeight > 0 || header.BlockHeader.ID() != types.GenesisID {
			tp.blockHeight--
		}
	}

	for _, header := range hcc.AppliedBlockHeaders {
		if header.BlockHeader.ParentID != recentID && !resetSanityCheck {
			panic(fmt.Sprintf("Consensus change series appears to be inconsistent - we are applying the wrong block. pid: %v recent: %v", header.BlockHeader.ParentID, recentID))
		}
		recentID = header.BlockHeader.ID()
		if tp.blockHeight > 0 || header.BlockHeader.ID() != types.GenesisID {
			tp.blockHeight++
		}

	}

	// // Update all the on-disk structures.
	err = tp.putRecentConsensusChange(tp.dbTx, hcc.ID)
	if err != nil {
		tp.log.Println("ERROR: could not update the recent consensus change:", err)
	}
	err = tp.putRecentBlockID(tp.dbTx, recentID)
	if err != nil {
		tp.log.Println("ERROR: could not store recent block id:", err)
	}
	err = tp.putBlockHeight(tp.dbTx, tp.blockHeight)
	if err != nil {
		tp.log.Println("ERROR: could not update the block height:", err)
	}
	// err = tp.putFeeMedian(tp.dbTx, medianPersist{
	// 	RecentMedians:   tp.recentMedians,
	// 	RecentMedianFee: tp.recentMedianFee,
	// })
	// if err != nil {
	// 	tp.log.Println("ERROR: could not update the transaction pool median fee information:", err)
	// }

	// // Scan the applied blocks for transactions that got accepted. This will
	// // help to determine which transactions to remove from the transaction
	// // pool. Having this list enables both efficiency improvements and helps to
	// // clean out transactions with no dependencies, such as arbitrary data
	// // transactions from the host.
	txids := make(map[types.TransactionID]struct{})

	for _, header := range hcc.AppliedBlockHeaders {
		// if block exists, relevent and already been download before
		block, exists := hcc.GetBlockByID(header.BlockHeader.ID())
		if exists {
			for _, txn := range block.Transactions {
				txids[txn.ID()] = struct{}{}
			}
		}
	}

	// // Save all of the current unconfirmed transaction sets into a list.
	var unconfirmedSets [][]types.Transaction
	for _, tSet := range tp.transactionSets {
		// Compile a new transaction set the removes all transactions duplicated
		// in the block. Though mostly handled by the dependency manager in the
		// transaction pool, this should both improve efficiency and will strip
		// out duplicate transactions with no dependencies (arbitrary data only
		// transactions)
		var newTSet []types.Transaction
		for _, txn := range tSet {
			_, exists := txids[txn.ID()]
			if !exists {
				newTSet = append(newTSet, txn)
			}
		}
		unconfirmedSets = append(unconfirmedSets, newTSet)
	}

	// // Purge the transaction pool. Some of the transactions sets may be invalid
	// // after the consensus change.
	tp.purge()

	// // prune transactions older than maxTxnAge.
	for i, tSet := range unconfirmedSets {
		var validTxns []types.Transaction
		for _, txn := range tSet {
			seenHeight, seen := tp.transactionHeights[txn.ID()]
			if tp.blockHeight-seenHeight <= maxTxnAge || !seen {
				validTxns = append(validTxns, txn)
			} else {
				delete(tp.transactionHeights, txn.ID())
			}
		}
		unconfirmedSets[i] = validTxns
	}

	keys, err := tp.getWalletKeysFunc()
	if err != nil {
		tp.log.Println("ERROR: get keysArray error:", err)
	}
	if (keys == nil) || (len(keys) == 0) {
		tp.log.Println("ERROR: keys empty:")
	}

	// // Scan through the reverted blocks and re-add any transactions that got
	// // reverted to the tpool.
	for i := len(hcc.RevertedBlockHeaders) - 1; i >= 0; i-- {
		header := hcc.RevertedBlockHeaders[i]
		block, exists := hcc.GetBlockByID(header.BlockHeader.ID())
		if exists {
			for _, txn := range block.Transactions {
				// Check whether this transaction has already be re-added to the
				// consensus set by the applied blocks.
				_, exists := txids[txn.ID()]
				if exists {
					continue
				}
				for _, t := range block.Transactions {
					for _, sci := range t.SiacoinInputs { // add back if it's what we send out
						uc := sci.UnlockConditions.UnlockHash()
						_, exists := keys[uc]
						if exists {
							// Try adding the transaction back into the transaction pool.
							tp.acceptTransactionSet([]types.Transaction{txn}, hcc.TryTransactionSet) // Error is ignored.
							break
						}
					}
				}
			}
		}
	}

	// // Add all of the unconfirmed transaction sets back to the transaction
	// // pool. The ones that are invalid will throw an error and will not be
	// // re-added.
	// //
	// // Accepting a transaction set requires locking the consensus set (to check
	// // validity). But, ProcessConsensusChange is only called when the consensus
	// // set is already locked, causing a deadlock problem. Therefore,
	// // transactions are readded to the pool in a goroutine, so that this
	// // function can finish and consensus can unlock. The tpool lock is held
	// // however until the goroutine completes.
	// //
	// // Which means that no other modules can require a tpool lock when
	// // processing consensus changes. Overall, the locking is pretty fragile and
	// // more rules need to be put in place.
	for _, set := range unconfirmedSets {
		for _, txn := range set {
			err := tp.acceptTransactionSet([]types.Transaction{txn}, hcc.TryTransactionSet)
			if err != nil {
				// The transaction is no longer valid, delete it from the
				// heights map to prevent a memory leak.
				delete(tp.transactionHeights, txn.ID())
			}
		}
	}

	// // Inform subscribers that an update has executed.
	tp.mu.Demote()
	tp.updateSubscribersTransactions()
	tp.mu.DemotedUnlock()
}

// StartSubscribeHeaders will subscribe after wallet unlock done
func (tp *TransactionPool) StartSubscribeHeaders() error {
	cc, err := tp.getRecentConsensusChange(tp.dbTx)
	if err == errNilConsensusChange {
		err = tp.putRecentConsensusChange(tp.dbTx, modules.ConsensusChangeBeginning)
	}
	if err != nil {
		return build.ExtendErr("unable to initialize the recent consensus change in the tpool", err)
	}

	err = tp.consensusSet.HeaderConsensusSetSubscribe(tp, cc, tp.tg.StopChan())
	if err == modules.ErrInvalidConsensusChangeID {
		tp.log.Println("Invalid consensus change loaded; resetting. This can take a while.")
		// Reset and rescan because the consensus set does not recognize the
		// provided consensus change id.
		resetErr := tp.resetDB(tp.dbTx)
		if resetErr != nil {
			return resetErr
		}
		freshScanErr := tp.consensusSet.HeaderConsensusSetSubscribe(tp, modules.ConsensusChangeBeginning, tp.tg.StopChan())
		if freshScanErr != nil {
			return freshScanErr
		}
		tp.tg.OnStop(func() {
			tp.consensusSet.Unsubscribe(tp)
		})
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

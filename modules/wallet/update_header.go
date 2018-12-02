package wallet

import (
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/errors"

	"github.com/coreos/bbolt"
)

// ProcessHeaderConsensusChange parses a header consensus change to update the set of
// confiremd outputs known to the wallet
func (w *Wallet) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange) {
	// should only pass related changes
	// log.Printf("ProcessHeaderConsensusChange")
	// for _, pbh := range hcc.AppliedBlockHeaders {
	// 	log.Printf("ProcessHeaderConsensusChange: %d %s", pbh.Height, pbh.BlockHeader.ID())
	// }

	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	keysArray, err := w.allAddressesInByteArray()
	if err != nil {
		panic(err)
	}
	lookaheadKeysArray, err := w.lookaheadAddressesInByteArray()
	if err != nil {
		panic(err)
	}
	keysArray = append(keysArray, lookaheadKeysArray...)
	// log.Printf("lookaheadKeysArray: %d, keysArray: %d", len(lookaheadKeysArray), len(keysArray))

	// This is probably not the best way to handle this. Here is the dilemma:
	// We're updating our lookahead with each block. We don't know how to
	// update the lookahead until we have the block's outputs, and we can't
	// do that until we've downloaded the full block. This means we have to
	// block and download blocks sequentially for the wallet. If we fail to
	// download, we just have to wait and then try again.
	siacoinOutputDiffs, err := hcc.FetchSpaceCashOutputDiffs(keysArray)
	for err != nil {
		w.log.Severe("ERROR: failed to fetch space cash outputs:", err)
		if err == siasync.ErrStopped {
			return
		}
		select {
		case <-w.tg.StopChan():
			return
		case <-time.After(50 * time.Millisecond):
			break // will not go out of forloop
		}
		siacoinOutputDiffs, err = hcc.FetchSpaceCashOutputDiffs(keysArray)
	}
	// for _, diff := range siacoinOutputDiffs {
	// 	log.Printf("siacoinOutputDiffs: %s %s %v %s", diff.SiacoinOutput.UnlockHash,
	// 		diff.SiacoinOutput.Value, diff.Direction, diff.ID)
	// }

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.updateLookahead(w.dbTx, siacoinOutputDiffs); err != nil {
		w.log.Severe("ERROR: failed to update lookahead:", err)
		w.dbRollback = true
	}
	if err := w.updateConfirmedSet(w.dbTx, siacoinOutputDiffs); err != nil {
		w.log.Severe("ERROR: failed to update confirmed set:", err)
		w.dbRollback = true
	}

	// this will revert tx to maintain a tx history which seems can not be maintained in spv mode
	if err := w.revertHistoryForSPV(w.dbTx, hcc); err != nil {
		w.log.Severe("ERROR: failed to revert consensus change:", err)
		w.dbRollback = true
	}
	if err := w.applyHistoryForSPV(w.dbTx, hcc, siacoinOutputDiffs); err != nil {
		w.log.Severe("ERROR: failed to apply consensus change:", err)
		w.dbRollback = true
	}
	if err := dbPutConsensusChangeID(w.dbTx, hcc.ID); err != nil {
		w.log.Severe("ERROR: failed to update consensus change ID:", err)
		w.dbRollback = true
	}
}

func (w *Wallet) applyHistoryForSPV(tx *bolt.Tx, hcc modules.HeaderConsensusChange,
	siacoinOutputDiffs []modules.SiacoinOutputDiff) error {
	spentSiacoinOutputs := computeSpentSiacoinOutputSet(siacoinOutputDiffs)

	for _, pbh := range hcc.AppliedBlockHeaders {
		consensusHeight, err := dbGetConsensusHeight(tx)
		if err != nil {
			return errors.AddContext(err, "failed to consensus height")
		}
		// Increment the consensus height.
		if pbh.BlockHeader.ID() != types.GenesisID {
			consensusHeight++
			err = dbPutConsensusHeight(tx, consensusHeight)
			if err != nil {
				return errors.AddContext(err, "failed to store consensus height in database")
			}
		}

		block, exists := hcc.GetBlockByID(pbh.BlockHeader.ID())
		if !exists {
			continue
		}

		// maintain processed txs
		pts := w.computeProcessedTransactionsFromBlock(tx, block, spentSiacoinOutputs, consensusHeight)
		for _, pt := range pts {
			err := dbAppendProcessedTransaction(tx, pt)
			if err != nil {
				return errors.AddContext(err, "could not put processed transaction")
			}
		}
	}

	return nil
}

func (w *Wallet) revertHistoryForSPV(tx *bolt.Tx, hcc modules.HeaderConsensusChange) error {
	for _, pbh := range hcc.RevertedBlockHeaders {
		// decrement the consensus height
		if pbh.BlockHeader.ID() != types.GenesisID {
			consensusHeight, err := dbGetConsensusHeight(tx)
			if err != nil {
				return err
			}
			err = dbPutConsensusHeight(tx, consensusHeight-1)
			if err != nil {
				return err
			}
		}

		block, exists := hcc.GetBlockByID(pbh.BlockHeader.ID())
		if !exists {
			continue
		}

		// Remove any transactions that have been reverted.
		for i := len(block.Transactions) - 1; i >= 0; i-- {
			// If the transaction is relevant to the wallet, it will be the
			// most recent transaction in bucketProcessedTransactions.
			txid := block.Transactions[i].ID()
			pt, err := dbGetLastProcessedTransaction(tx)
			if err != nil {
				break // bucket is empty
			}
			if txid == pt.TransactionID {
				w.log.Println("A wallet transaction has been reverted due to a reorg:", txid)
				if err := dbDeleteLastProcessedTransaction(tx); err != nil {
					w.log.Severe("Could not revert transaction:", err)
					return err
				}
			}
		}

		// Remove the miner payout transaction if applicable.
		for i, mp := range block.MinerPayouts {
			// If the transaction is relevant to the wallet, it will be the
			// most recent transaction in bucketProcessedTransactions.
			pt, err := dbGetLastProcessedTransaction(tx)
			if err != nil {
				break // bucket is empty
			}
			if types.TransactionID(block.ID()) == pt.TransactionID {
				w.log.Println("Miner payout has been reverted due to a reorg:", block.MinerPayoutID(uint64(i)), "::", mp.Value.HumanString())
				if err := dbDeleteLastProcessedTransaction(tx); err != nil {
					w.log.Severe("Could not revert transaction:", err)
					return err
				}
				break // there will only ever be one miner transaction
			}
		}
	}
	return nil
}

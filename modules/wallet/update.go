package wallet

import (
	//"fmt"

	"log"
	"math"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/errors"

	"github.com/coreos/bbolt"
)

type (
	spentSiacoinOutputSet map[types.SiacoinOutputID]types.SiacoinOutput
)

// advanceSeedLookahead advances the lookahead to start with a new external
// index
func (w *Wallet) advanceSeedLookahead(newExternalIndex uint64) error {
	internalIndex, err := dbGetPrimarySeedMaximumInternalIndex(w.dbTx)
	if err != nil {
		return err
	}
	externalIndex, err := dbGetPrimarySeedMaximumExternalIndex(w.dbTx)
	if err != nil {
		return err
	}
	numKeys := newExternalIndex - externalIndex
	var newInternalIndex uint64
	if newExternalIndex > internalIndex {
		newInternalIndex = newExternalIndex
	} else {
		newInternalIndex = internalIndex
	}
	//fmt.Printf("old internal index %v, new internal index: %v, advanceSeedLookahead to index %v\n", internalIndex, newInternalIndex, index)

	// Retrieve numKeys from the lookup buffer while replenishing it
	spendableKeys := w.lookahead.Advance(numKeys)
	for _, key := range spendableKeys {
		w.keys[key.UnlockConditions.UnlockHash()] = key
	}

	// Update the internalIndex
	err = dbPutPrimarySeedMaximumInternalIndex(w.dbTx, newInternalIndex)
	if err != nil {
		return err
	}
	// Update the externalIndex
	err = dbPutPrimarySeedMaximumExternalIndex(w.dbTx, newExternalIndex)
	if err != nil {
		return err
	}

	return nil
}

// isWalletAddress is a helper function that checks if an UnlockHash is
// derived from one of the wallet's spendable keys or is being explicitly watched.
func (w *Wallet) isWalletAddress(uh types.UnlockHash) bool {
	_, spendable := w.keys[uh]
	_, watchonly := w.watchedAddrs[uh]
	return spendable || watchonly
}

// updateLookahead uses a consensus change to update the seed progress if one of the outputs
// contains an unlock hash of the lookahead set. Returns true if a blockchain rescan is required
func (w *Wallet) updateLookahead(tx *bolt.Tx, sods []modules.SiacoinOutputDiff) error {
	externalIndex, err := dbGetPrimarySeedMaximumExternalIndex(w.dbTx)
	if err != nil {
		return err
	}
	for _, diff := range sods {
		//fmt.Printf("scanning %v\n", diff.SiacoinOutput.UnlockHash)
		if index, ok := w.lookahead.GetIndex(diff.SiacoinOutput.UnlockHash); ok {
			if index >= externalIndex {
				externalIndex = index + 1
			}
		}
	}
	if externalIndex > 0 {
		return w.advanceSeedLookahead(externalIndex)
	}

	return nil
}

// updateConfirmedSet uses a consensus change to update the confirmed set of
// outputs as understood by the wallet.
func (w *Wallet) updateConfirmedSet(tx *bolt.Tx, sods []modules.SiacoinOutputDiff) error {
	for _, diff := range sods {
		// Verify that the diff is relevant to the wallet.
		if !w.isWalletAddress(diff.SiacoinOutput.UnlockHash) {
			continue
		}

		var err error
		if diff.Direction == modules.DiffApply {
			w.log.Println("Wallet has gained a spendable siacoin output:", diff.ID, "::", diff.SiacoinOutput.Value.HumanString())
			err = dbPutSiacoinOutput(tx, diff.ID, diff.SiacoinOutput)
		} else {
			w.log.Println("Wallet has lost a spendable siacoin output:", diff.ID, "::", diff.SiacoinOutput.Value.HumanString())
			err = dbDeleteSiacoinOutput(tx, diff.ID)
		}
		if err != nil {
			w.log.Severe("Could not update siacoin output:", err)
			return err
		}
	}
	return nil
}

// revertHistory reverts any transaction history that was destroyed by reverted
// blocks in the consensus change.
func (w *Wallet) revertHistory(tx *bolt.Tx, reverted []types.Block) error {
	for _, block := range reverted {
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

		// decrement the consensus height
		if block.ID() != types.GenesisID {
			consensusHeight, err := dbGetConsensusHeight(tx)
			if err != nil {
				return err
			}
			err = dbPutConsensusHeight(tx, consensusHeight-1)
			if err != nil {
				return err
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

// outputs and collects them in a map of SiacoinOutputID -> SiacoinOutput.
func computeSpentSiacoinOutputSet(diffs []modules.SiacoinOutputDiff) spentSiacoinOutputSet {
	outputs := make(spentSiacoinOutputSet)
	for _, diff := range diffs {
		if diff.Direction == modules.DiffRevert {
			// DiffRevert means spent.
			outputs[diff.ID] = diff.SiacoinOutput
		}
	}
	return outputs
}

// computeProcessedTransactionsFromBlock searches all the miner payouts and
// transactions in a block and computes a ProcessedTransaction slice containing
// all of the transactions processed for the given block.
func (w *Wallet) computeProcessedTransactionsFromBlock(tx *bolt.Tx, block types.Block, spentSiacoinOutputs spentSiacoinOutputSet, consensusHeight types.BlockHeight) []modules.ProcessedTransaction {
	var pts []modules.ProcessedTransaction

	// Find ProcessedTransactions from miner payouts.
	relevant := false
	for _, mp := range block.MinerPayouts {
		relevant = relevant || w.isWalletAddress(mp.UnlockHash)
	}
	if relevant {
		w.log.Println("Wallet has received new miner payouts:", block.ID())
		// Apply the miner payout transaction if applicable.
		minerPT := modules.ProcessedTransaction{
			Transaction:           types.Transaction{},
			TransactionID:         types.TransactionID(block.ID()),
			ConfirmationHeight:    consensusHeight,
			ConfirmationTimestamp: block.Timestamp,
		}
		for i, mp := range block.MinerPayouts {
			w.log.Println("\tminer payout:", block.MinerPayoutID(uint64(i)), "::", mp.Value.HumanString())
			minerPT.Outputs = append(minerPT.Outputs, modules.ProcessedOutput{
				ID:             types.OutputID(block.MinerPayoutID(uint64(i))),
				FundType:       types.SpecifierMinerPayout,
				MaturityHeight: consensusHeight + types.MaturityDelay,
				WalletAddress:  w.isWalletAddress(mp.UnlockHash),
				RelatedAddress: mp.UnlockHash,
				Value:          mp.Value,
			})
		}
		pts = append(pts, minerPT)
	}

	// Find ProcessedTransactions from transactions.
	for _, txn := range block.Transactions {
		// Determine if transaction is relevant.
		relevant := false
		for _, sci := range txn.SiacoinInputs {
			relevant = relevant || w.isWalletAddress(sci.UnlockConditions.UnlockHash())
		}
		for _, sco := range txn.SiacoinOutputs {
			relevant = relevant || w.isWalletAddress(sco.UnlockHash)
		}

		// Only create a ProcessedTransaction if transaction is relevant.
		if !relevant {
			continue
		}
		w.log.Println("A transaction has been confirmed on the blockchain:", txn.ID())

		pt := modules.ProcessedTransaction{
			Transaction:           txn,
			TransactionID:         txn.ID(),
			ConfirmationHeight:    consensusHeight,
			ConfirmationTimestamp: block.Timestamp,
		}

		for _, sci := range txn.SiacoinInputs {
			pi := modules.ProcessedInput{
				ParentID:       types.OutputID(sci.ParentID),
				FundType:       types.SpecifierSiacoinInput,
				WalletAddress:  w.isWalletAddress(sci.UnlockConditions.UnlockHash()),
				RelatedAddress: sci.UnlockConditions.UnlockHash(),
				Value:          spentSiacoinOutputs[sci.ParentID].Value,
			}
			pt.Inputs = append(pt.Inputs, pi)

			// Log any wallet-relevant inputs.
			if pi.WalletAddress {
				w.log.Println("\tSiacoin Input:", pi.ParentID, "::", pi.Value.HumanString())
			}
		}

		for i, sco := range txn.SiacoinOutputs {
			po := modules.ProcessedOutput{
				ID:             types.OutputID(txn.SiacoinOutputID(uint64(i))),
				FundType:       types.SpecifierSiacoinOutput,
				MaturityHeight: consensusHeight,
				WalletAddress:  w.isWalletAddress(sco.UnlockHash),
				RelatedAddress: sco.UnlockHash,
				Value:          sco.Value,
			}
			pt.Outputs = append(pt.Outputs, po)

			// Log any wallet-relevant outputs.
			if po.WalletAddress {
				w.log.Println("\tSiacoin Output:", po.ID, "::", po.Value.HumanString())
			}
		}

		for _, fee := range txn.MinerFees {
			pt.Outputs = append(pt.Outputs, modules.ProcessedOutput{
				FundType:       types.SpecifierMinerFee,
				MaturityHeight: consensusHeight + types.MaturityDelay,
				Value:          fee,
			})
		}
		pts = append(pts, pt)
	}
	return pts
}

// applyHistory applies any transaction history that the applied blocks
// introduced.
func (w *Wallet) applyHistory(tx *bolt.Tx, cc modules.ConsensusChange) error {
	spentSiacoinOutputs := computeSpentSiacoinOutputSet(cc.SiacoinOutputDiffs)

	for _, block := range cc.AppliedBlocks {
		consensusHeight, err := dbGetConsensusHeight(tx)
		if err != nil {
			return errors.AddContext(err, "failed to consensus height")
		}
		// Increment the consensus height.
		if block.ID() != types.GenesisID {
			consensusHeight++
			err = dbPutConsensusHeight(tx, consensusHeight)
			if err != nil {
				return errors.AddContext(err, "failed to store consensus height in database")
			}
		}

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

// ProcessConsensusChange parses a consensus change to update the set of
// confirmed outputs known to the wallet.
func (w *Wallet) ProcessConsensusChange(cc modules.ConsensusChange) {
	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.updateLookahead(w.dbTx, cc.SiacoinOutputDiffs); err != nil {
		w.log.Severe("ERROR: failed to update lookahead:", err)
		w.dbRollback = true
	}
	if err := w.updateConfirmedSet(w.dbTx, cc.SiacoinOutputDiffs); err != nil {
		w.log.Severe("ERROR: failed to update confirmed set:", err)
		w.dbRollback = true
	}
	if err := w.revertHistory(w.dbTx, cc.RevertedBlocks); err != nil {
		w.log.Severe("ERROR: failed to revert consensus change:", err)
		w.dbRollback = true
	}
	if err := w.applyHistory(w.dbTx, cc); err != nil {
		w.log.Severe("ERROR: failed to apply consensus change:", err)
		w.dbRollback = true
	}
	if err := dbPutConsensusChangeID(w.dbTx, cc.ID); err != nil {
		w.log.Severe("ERROR: failed to update consensus change ID:", err)
		w.dbRollback = true
	}

	if cc.Synced {
		go w.threadedDefragWallet()
	}
}

// ProcessHeaderConsensusChange parses a header consensus change to update the set of
// confiremd outputs known to the wallet
func (w *Wallet) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange) {
	// should only pass related changes
	// log.Printf("ProcessHeaderConsensusChange")
	for _, pbh := range hcc.AppliedBlockHeaders {
		log.Printf("ProcessHeaderConsensusChange: %d %s", pbh.Height, pbh.BlockHeader.ID())
	}

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

	// This is probably not the best way to handle this. Here is the dilemma:
	// We're updating our lookahead with each block. We don't know how to
	// update the lookahead until we have the block's outputs, and we can't
	// do that until we've downloaded the full block. This means we have to
	// block and download blocks sequentially for the wallet. If we fail to
	// download, we just have to wait and then try again.
	siacoinOutputDiffs, err := hcc.FetchSpaceCashOutputDiffs(keysArray)
	for err != nil {
		w.log.Severe("ERROR: failed to fetch space cash outputs:", err)
		select {
		case <-time.After(2 * time.Second):
			break
		case <-w.tg.StopChan():
			break
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

	// defrag should also be removed in next version
	/*
		if hcc.Synced {
			go w.threadedDefragWallet()
		}
	*/
}

// ReceiveUpdatedUnconfirmedTransactions updates the wallet's unconfirmed
// transaction set.
func (w *Wallet) ReceiveUpdatedUnconfirmedTransactions(diff *modules.TransactionPoolDiff) {
	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()

	// Do the pruning first. If there are any pruned transactions, we will need
	// to re-allocate the whole processed transactions array.
	droppedTransactions := make(map[types.TransactionID]struct{})
	for i := range diff.RevertedTransactions {
		txids := w.unconfirmedSets[diff.RevertedTransactions[i]]
		for i := range txids {
			droppedTransactions[txids[i]] = struct{}{}
		}
		delete(w.unconfirmedSets, diff.RevertedTransactions[i])
	}

	// Skip the reallocation if we can, otherwise reallocate the
	// unconfirmedProcessedTransactions to no longer have the dropped
	// transactions.
	if len(droppedTransactions) != 0 {
		// Capacity can't be reduced, because we have no way of knowing if the
		// dropped transactions are relevant to the wallet or not, and some will
		// not be relevant to the wallet, meaning they don't have a counterpart
		// in w.unconfirmedProcessedTransactions.
		newUPT := make([]modules.ProcessedTransaction, 0, len(w.unconfirmedProcessedTransactions))
		for _, txn := range w.unconfirmedProcessedTransactions {
			_, exists := droppedTransactions[txn.TransactionID]
			if !exists {
				// Transaction was not dropped, add it to the new unconfirmed
				// transactions.
				newUPT = append(newUPT, txn)
			}
		}

		// Set the unconfirmed preocessed transactions to the pruned set.
		w.unconfirmedProcessedTransactions = newUPT
	}

	// Scroll through all of the diffs and add any new transactions.
	for _, unconfirmedTxnSet := range diff.AppliedTransactions {
		// Mark all of the transactions that appeared in this set.
		//
		// TODO: Technically only necessary to mark the ones that are relevant
		// to the wallet, but overhead should be low.
		w.unconfirmedSets[unconfirmedTxnSet.ID] = unconfirmedTxnSet.IDs

		// Get the values for the spent outputs.
		spentSiacoinOutputs := make(map[types.SiacoinOutputID]types.SiacoinOutput)
		for _, scod := range unconfirmedTxnSet.Change.SiacoinOutputDiffs {
			// Only need to grab the reverted ones, because only reverted ones
			// have the possibility of having been spent.
			if scod.Direction == modules.DiffRevert {
				spentSiacoinOutputs[scod.ID] = scod.SiacoinOutput
			}
		}

		// Add each transaction to our set of unconfirmed transactions.
		for i, txn := range unconfirmedTxnSet.Transactions {
			// determine whether transaction is relevant to the wallet
			relevant := false
			for _, sci := range txn.SiacoinInputs {
				relevant = relevant || w.isWalletAddress(sci.UnlockConditions.UnlockHash())
			}
			for _, sco := range txn.SiacoinOutputs {
				relevant = relevant || w.isWalletAddress(sco.UnlockHash)
			}

			// only create a ProcessedTransaction if txn is relevant
			if !relevant {
				continue
			}

			pt := modules.ProcessedTransaction{
				Transaction:           txn,
				TransactionID:         unconfirmedTxnSet.IDs[i],
				ConfirmationHeight:    types.BlockHeight(math.MaxUint64),
				ConfirmationTimestamp: types.Timestamp(math.MaxUint64),
			}
			for _, sci := range txn.SiacoinInputs {
				pt.Inputs = append(pt.Inputs, modules.ProcessedInput{
					ParentID:       types.OutputID(sci.ParentID),
					FundType:       types.SpecifierSiacoinInput,
					WalletAddress:  w.isWalletAddress(sci.UnlockConditions.UnlockHash()),
					RelatedAddress: sci.UnlockConditions.UnlockHash(),
					Value:          spentSiacoinOutputs[sci.ParentID].Value,
				})
			}
			for i, sco := range txn.SiacoinOutputs {
				pt.Outputs = append(pt.Outputs, modules.ProcessedOutput{
					ID:             types.OutputID(txn.SiacoinOutputID(uint64(i))),
					FundType:       types.SpecifierSiacoinOutput,
					MaturityHeight: types.BlockHeight(math.MaxUint64),
					WalletAddress:  w.isWalletAddress(sco.UnlockHash),
					RelatedAddress: sco.UnlockHash,
					Value:          sco.Value,
				})
			}
			for _, fee := range txn.MinerFees {
				pt.Outputs = append(pt.Outputs, modules.ProcessedOutput{
					FundType: types.SpecifierMinerFee,
					Value:    fee,
				})
			}
			w.unconfirmedProcessedTransactions = append(w.unconfirmedProcessedTransactions, pt)
		}
	}
}

package explorer

import (
	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/coreos/bbolt"
)

// Block takes a block ID and finds the corresponding block, provided that the
// block is in the consensus set.
func (e *Explorer) Block(id types.BlockID) (types.Block, types.BlockHeight, bool) {
	var height types.BlockHeight
	err := e.db.View(dbGetAndDecode(bucketBlockIDs, id, &height))
	if err != nil {
		e.log.Printf("Error decoding blockfacts: %s", err)
		return types.Block{}, 0, false
	}
	block, exists := e.cs.BlockAtHeight(height)
	if !exists {
		e.log.Printf("did not find block at height: %d", height)
		return types.Block{}, 0, false
	}
	return block, height, true
}

// BlockFacts returns a set of statistics about the blockchain as they appeared
// at a given block height, and a bool indicating whether facts exist for the
// given height.
func (e *Explorer) BlockFacts(height types.BlockHeight) (modules.BlockFacts, bool) {
	var bf blockFacts
	err := e.db.View(e.dbGetBlockFacts(height, &bf))
	if err != nil {
		e.log.Printf("Did not find block facts %s", err)
		return modules.BlockFacts{}, false
	}

	return bf.BlockFacts, true
}

// LatestBlockFacts returns a set of statistics about the blockchain as they appeared
// at the latest block height in the explorer's consensus set.
func (e *Explorer) LatestBlockFacts() modules.BlockFacts {
	var bf blockFacts
	err := e.db.View(func(tx *bolt.Tx) error {
		var height types.BlockHeight
		err := dbGetInternal(internalBlockHeight, &height)(tx)
		if err != nil {
			return err
		}
		return e.dbGetBlockFacts(height, &bf)(tx)
	})
	if err != nil {
		build.Critical(err)
	}
	return bf.BlockFacts
}

//PendingTransactions returns the current list of pending transactions in the mempool
func (e *Explorer) PendingTransactions() []types.Transaction {
	e.mu.Lock()
	defer e.mu.Unlock()
	var txs []types.Transaction
	for _, upt := range e.unconfirmedProcessedTransactions {
		txs = append(txs, upt.Transaction)
	}
	return txs
}

// Transaction takes a transaction ID and finds the block containing the
// transaction. Because of the miner payouts, the transacti on ID might be a
// block ID. To find the transaction, iterate through the block.
func (e *Explorer) Transaction(id types.TransactionID) (types.Block, types.BlockHeight, bool) {
	var height types.BlockHeight
	err := e.db.View(dbGetAndDecode(bucketTransactionIDs, id, &height))
	if err != nil {
		return types.Block{}, 0, false
	}
	block, exists := e.cs.BlockAtHeight(height)
	if !exists {
		return types.Block{}, 0, false
	}
	return block, height, true
}

// UnlockHash returns the IDs of all the transactions that contain the unlock
// hash. An empty set indicates that the unlock hash does not appear in the
// blockchain.
func (e *Explorer) UnlockHash(uh types.UnlockHash) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketUnlockHashes, uh, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}

// SiacoinOutput returns the siacoin output associated with the specified ID.
// This first checks the memPool for the ID, then goes to the DB to see if it's already been stored
func (e *Explorer) SiacoinOutput(id types.SiacoinOutputID) (types.SiacoinOutput, bool) {
	for _, upt := range e.unconfirmedProcessedTransactions {
		for _, uptSco := range upt.Outputs {
			if uptSco.FundType == types.SpecifierSiacoinOutput && uptSco.ID == types.OutputID(id) {
				return types.SiacoinOutput{
					UnlockHash: uptSco.RelatedAddress,
					Value:      uptSco.Value,
				}, true
			}
		}
	}
	var sco types.SiacoinOutput
	err := e.db.View(dbGetAndDecode(bucketSiacoinOutputs, id, &sco))
	if err != nil {
		return types.SiacoinOutput{}, false
	}
	return sco, true
}

// SiacoinOutputID returns all of the transactions that contain the specified
// siacoin output ID. An empty set indicates that the siacoin output ID does
// not appear in the blockchain.
func (e *Explorer) SiacoinOutputID(id types.SiacoinOutputID) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketSiacoinOutputIDs, id, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}

// FileContractHistory returns the history associated with the specified file
// contract ID, which includes the file contract itself and all of the
// revisions that have been submitted to the blockchain. The first bool
// indicates whether the file contract exists, and the second bool indicates
// whether a storage proof was successfully submitted for the file contract.
func (e *Explorer) FileContractHistory(id types.FileContractID) (fc types.FileContract, fcrs []types.FileContractRevision, fcE bool, spE bool) {
	var history fileContractHistory
	err := e.db.View(dbGetAndDecode(bucketFileContractHistories, id, &history))
	fc = history.Contract
	fcrs = history.Revisions
	fcE = err == nil
	spE = history.StorageProof.ParentID == id
	return
}

// FileContractID returns all of the transactions that contain the specified
// file contract ID. An empty set indicates that the file contract ID does not
// appear in the blockchain.
func (e *Explorer) FileContractID(id types.FileContractID) []types.TransactionID {
	var ids []types.TransactionID
	err := e.db.View(dbGetTransactionIDSet(bucketFileContractIDs, id, &ids))
	if err != nil {
		ids = nil
	}
	return ids
}

// FileContractPayouts returns all of the spendable siacoin outputs which are the
// result of a FileContract. An empty set indicates that the file contract is
// still open
func (e *Explorer) FileContractPayouts(id types.FileContractID) ([]types.SiacoinOutput, error) {
	var history fileContractHistory
	err := e.db.View(dbGetAndDecode(bucketFileContractHistories, id, &history))
	if err != nil {
		return []types.SiacoinOutput{}, err
	}

	fc := history.Contract
	var outputs []types.SiacoinOutput

	for i := range fc.ValidProofOutputs {
		scoid := id.StorageProofOutputID(types.ProofValid, uint64(i))

		sco, found := e.SiacoinOutput(scoid)
		if found {
			outputs = append(outputs, sco)
		}
	}
	for i := range fc.MissedProofOutputs {
		scoid := id.StorageProofOutputID(types.ProofMissed, uint64(i))

		sco, found := e.SiacoinOutput(scoid)
		if found {
			outputs = append(outputs, sco)
		}
	}

	return outputs, nil
}

//HashType returns the string representation of the hashtype.
func (e *Explorer) HashType(hash crypto.Hash) (hashType string, err error) {
	err = e.db.View(dbGetAndDecode(bucketHashType, hash, &hashType))
	if err != nil {
		return "", err
	}
	return hashType, nil
}

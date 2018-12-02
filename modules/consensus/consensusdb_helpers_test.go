package consensus

// database_test.go contains a bunch of legacy functions to preserve
// compatibility with the test suite.

import (
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// dbBlockHeight is a convenience function allowing blockHeight to be called
// without a bolt.Tx.
func (cs *ConsensusSet) dbBlockHeight() (bh types.BlockHeight) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		bh = blockHeight(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return bh
}

// dbCurrentProcessedBlock is a convenience function allowing
// currentProcessedBlock to be called without a bolt.Tx.
func (cs *ConsensusSet) dbCurrentProcessedBlock() (pb *processedBlock) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		pb = currentProcessedBlock(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return pb
}

func (cs *ConsensusSet) dbCurrentProcessedHeader() (pbh *modules.ProcessedBlockHeader) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		pbh = currentProcessedHeader(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return pbh
}

// dbGetPath is a convenience function allowing getPath to be called without a
// bolt.Tx.
func (cs *ConsensusSet) dbGetPath(bh types.BlockHeight) (id types.BlockID, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		id, err = getPath(tx, bh)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return id, err
}

// dbPushPath is a convenience function allowing pushPath to be called without a
// bolt.Tx.
func (cs *ConsensusSet) dbPushPath(bid types.BlockID) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		pushPath(tx, bid)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

func (cs *ConsensusSet) dbGetBlockHeaderMap(id types.BlockID) (pbh *modules.ProcessedBlockHeader, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		pbh, err = getBlockHeaderMap(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return pbh, err
}

// dbGetSiacoinOutput is a convenience function allowing getSiacoinOutput to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbGetSiacoinOutput(id types.SiacoinOutputID) (sco types.SiacoinOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		sco, err = getSiacoinOutput(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return sco, err
}

// getArbSiacoinOutput is a convenience function fetching a single random
// siacoin output from the database.
func (cs *ConsensusSet) getArbSiacoinOutput() (scoid types.SiacoinOutputID, sco types.SiacoinOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(SiacoinOutputs).Cursor()
		scoidBytes, scoBytes := cursor.First()
		copy(scoid[:], scoidBytes)
		return encoding.Unmarshal(scoBytes, &sco)
	})
	if dbErr != nil {
		panic(dbErr)
	}
	if err != nil {
		return types.SiacoinOutputID{}, types.SiacoinOutput{}, err
	}
	return scoid, sco, nil
}

// dbGetFileContract is a convenience function allowing getFileContract to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbGetFileContract(id types.FileContractID) (fc types.FileContract, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		fc, err = getFileContract(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return fc, err
}

// dbAddFileContract is a convenience function allowing addFileContract to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbAddFileContract(id types.FileContractID, fc types.FileContract) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		addFileContract(tx, id, fc)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbRemoveFileContract is a convenience function allowing removeFileContract
// to be called without a bolt.Tx.
func (cs *ConsensusSet) dbRemoveFileContract(id types.FileContractID) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		removeFileContract(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbAddFileContract is a convenience function allowing addSiacoinOutput to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbAddSiacoinOutput(id types.SiacoinOutputID, sco types.SiacoinOutput) {
	dbErr := cs.db.Update(func(tx *bolt.Tx) error {
		addSiacoinOutput(tx, id, sco)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
}

// dbGetDSCO is a convenience function allowing a delayed siacoin output to be
// fetched without a bolt.Tx. An error is returned if the delayed output is not
// found at the maturity height indicated by the input.
func (cs *ConsensusSet) dbGetDSCO(height types.BlockHeight, id types.SiacoinOutputID) (dsco types.SiacoinOutput, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		dscoBucketID := append(prefixDSCO, encoding.Marshal(height)...)
		dscoBucket := tx.Bucket(dscoBucketID)
		if dscoBucket == nil {
			err = errNilItem
			return nil
		}
		dscoBytes := dscoBucket.Get(id[:])
		if dscoBytes == nil {
			err = errNilItem
			return nil
		}
		err = encoding.Unmarshal(dscoBytes, &dsco)
		if err != nil {
			panic(err)
		}
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return dsco, err
}

// dbStorageProofSegment is a convenience function allowing
// 'storageProofSegment' to be called during testing without a tx.
func (cs *ConsensusSet) dbStorageProofSegment(fcid types.FileContractID) (index uint64, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		index, err = storageProofSegment(tx, fcid)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return index, err
}

// dbValidStorageProofs is a convenience function allowing 'validStorageProofs'
// to be called during testing without a tx.
func (cs *ConsensusSet) dbValidStorageProofs(t types.Transaction) (err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		err = validStorageProofs(tx, t)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return err
}

// dbValidFileContractRevisions is a convenience function allowing
// 'validFileContractRevisions' to be called during testing without a tx.
func (cs *ConsensusSet) dbValidFileContractRevisions(t types.Transaction) (err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		err = validFileContractRevisions(tx, t)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return err
}

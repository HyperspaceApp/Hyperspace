package wallet

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/fastrand"

	"github.com/coreos/bbolt"
)

const (
	compatFile = modules.WalletDir + ".json"
	dbFile     = modules.WalletDir + ".db"
	logFile    = modules.WalletDir + ".log"
)

var (
	dbMetadata = persist.Metadata{
		Header:  "Wallet Database",
		Version: "1.1.0",
	}
)

// spendableKeyFile stores an encrypted spendable key on disk.
type spendableKeyFile struct {
	UID                    uniqueID
	EncryptionVerification crypto.Ciphertext
	SpendableKey           crypto.Ciphertext
}

// openDB loads the set database and populates it with the necessary buckets.
func (w *Wallet) openDB(filename string) (err error) {
	w.db, err = persist.OpenDatabase(dbMetadata, filename)
	if err != nil {
		return err
	}
	// initialize the database
	err = w.db.Update(func(tx *bolt.Tx) error {
		// check whether we need to init bucketAddrTransactions
		buildAddrTxns := tx.Bucket(bucketAddrTransactions) == nil
		// ensure that all buckets exist
		for _, b := range dbBuckets {
			_, err := tx.CreateBucketIfNotExists(b)
			if err != nil {
				return fmt.Errorf("could not create bucket %v: %v", string(b), err)
			}
		}
		// if the wallet does not have a UID, create one
		if tx.Bucket(bucketWallet).Get(keyUID) == nil {
			uid := make([]byte, len(uniqueID{}))
			fastrand.Read(uid[:])
			tx.Bucket(bucketWallet).Put(keyUID, uid)
		}
		// if fields in bucketWallet are nil, set them to zero to prevent unmarshal errors
		wb := tx.Bucket(bucketWallet)
		if wb.Get(keyConsensusHeight) == nil {
			wb.Put(keyConsensusHeight, encoding.Marshal(uint64(0)))
		}
		if wb.Get(keyAuxiliarySeedFiles) == nil {
			wb.Put(keyAuxiliarySeedFiles, encoding.Marshal([]seedFile{}))
		}
		if wb.Get(keySpendableKeyFiles) == nil {
			wb.Put(keySpendableKeyFiles, encoding.Marshal([]spendableKeyFile{}))
		}
		if wb.Get(keyWatchedAddrs) == nil {
			wb.Put(keyWatchedAddrs, encoding.Marshal([]types.UnlockHash{}))
		}
		if wb.Get(keySeedsMinimumIndex) == nil {
			wb.Put(keySeedsMinimumIndex, encoding.Marshal([]uint64{}))
		}
		if wb.Get(keySeedsMaximumInternalIndex) == nil {
			wb.Put(keySeedsMaximumInternalIndex, encoding.Marshal([]uint64{}))
		}
		if wb.Get(keySeedsMaximumExternalIndex) == nil {
			wb.Put(keySeedsMaximumExternalIndex, encoding.Marshal([]uint64{}))
		}

		// build the bucketAddrTransactions bucket if necessary
		if buildAddrTxns {
			it := dbProcessedTransactionsIterator(tx)
			for it.next() {
				index, pt := it.key(), it.value()
				if err := dbAddProcessedTransactionAddrs(tx, pt, index); err != nil {
					return err
				}
			}
		}

		// check whether wallet is encrypted
		w.encrypted = tx.Bucket(bucketWallet).Get(keyEncryptionVerification) != nil
		return nil
	})
	return err
}

// initPersist loads all of the wallet's persistence files into memory,
// creating them if they do not exist.
func (w *Wallet) initPersist() error {
	// Create a directory for the wallet without overwriting an existing
	// directory.
	err := os.MkdirAll(w.persistDir, 0700)
	if err != nil {
		return err
	}

	// Start logging.
	w.log, err = persist.NewFileLogger(filepath.Join(w.persistDir, logFile))
	if err != nil {
		return err
	}

	// open/create the database
	err = w.openDB(filepath.Join(w.persistDir, dbFile))
	if err != nil {
		return err
	}

	// begin the initial transaction
	w.dbTx, err = w.db.Begin(true)
	if err != nil {
		w.log.Critical("ERROR: failed to start database update:", err)
	}

	// ensure that the final db transaction is committed when the wallet closes
	err = w.tg.AfterStop(func() error {
		var err error
		if w.dbRollback {
			// rollback txn if necessry.
			err = errors.New("database unable to sync - rollback requested")
			err = errors.Compose(err, w.dbTx.Rollback())
		} else {
			// else commit the transaction.
			err = w.dbTx.Commit()
		}
		if err != nil {
			w.log.Severe("ERROR: failed to apply database update:", err)
			return errors.AddContext(err, "unable to commit dbTx in syncDB")
		}
		return w.db.Close()
	})
	if err != nil {
		return err
	}

	// spawn a goroutine to commit the db transaction at regular intervals
	go w.threadedDBUpdate()
	return nil
}

// createBackup copies the wallet database to dst.
func (w *Wallet) createBackup(dst io.Writer) error {
	_, err := w.dbTx.WriteTo(dst)
	return err
}

// CreateBackup creates a backup file at the desired filepath.
func (w *Wallet) CreateBackup(backupFilepath string) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()
	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := os.Create(backupFilepath)
	if err != nil {
		return err
	}
	defer f.Close()
	return w.createBackup(f)
}

/*
// LoadBackup loads a backup file from the provided filepath. The backup file
// primary seed is loaded as an auxiliary seed.
func (w *Wallet) LoadBackup(masterKey, backupMasterKey crypto.TwofishKey, backupFilepath string) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	lockID := w.mu.Lock()
	defer w.mu.Unlock(lockID)

	// Load all of the seed files, check for duplicates, re-encrypt them (but
	// keep the UID), and add them to the walletPersist object)
	var backupPersist walletPersist
	err := persist.LoadFile(settingsMetadata, &backupPersist, backupFilepath)
	if err != nil {
		return err
	}
	backupSeeds := append(backupPersist.AuxiliarySeedFiles, backupPersist.PrimarySeedFile)
	TODO: more
}
*/

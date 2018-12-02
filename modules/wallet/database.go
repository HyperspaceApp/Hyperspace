package wallet

import (
	"encoding/binary"
	"fmt"
	"log"
	"reflect"
	"time"

	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/fastrand"

	"github.com/coreos/bbolt"
)

var (
	// bucketProcessedTransactions stores ProcessedTransactions in
	// chronological order. Only transactions relevant to the wallet are
	// stored. The key of this bucket is an autoincrementing integer.
	bucketProcessedTransactions = []byte("bucketProcessedTransactions")
	// bucketProcessedTxnIndex maps a ProcessedTransactions ID to it's
	// autoincremented index in bucketProcessedTransactions
	bucketProcessedTxnIndex = []byte("bucketProcessedTxnKey")
	// bucketAddrTransactions maps an UnlockHash to the
	// ProcessedTransactions that it appears in.
	bucketAddrTransactions = []byte("bucketAddrTransactions")
	// bucketSiacoinOutputs maps a SiacoinOutputID to its SiacoinOutput. Only
	// outputs that the wallet controls are stored. The wallet uses these
	// outputs to fund transactions.
	bucketSiacoinOutputs = []byte("bucketSiacoinOutputs")
	// bucketSpentOutputs maps an OutputID to the height at which it was
	// spent. Only outputs spent by the wallet are stored. The wallet tracks
	// these outputs so that it can reuse them if they are not confirmed on
	// the blockchain.
	bucketSpentOutputs = []byte("bucketSpentOutputs")
	// bucketUnlockConditions maps an UnlockHash to its UnlockConditions. It
	// is used to track UnlockConditions manually stored by the user,
	// typically with an offline wallet.
	bucketUnlockConditions = []byte("bucketUnlockConditions")
	// bucketWallet contains various fields needed by the wallet, such as its
	// UID, EncryptionVerification, and PrimarySeedFile.
	bucketWallet = []byte("bucketWallet")

	dbBuckets = [][]byte{
		bucketProcessedTransactions,
		bucketProcessedTxnIndex,
		bucketAddrTransactions,
		bucketSiacoinOutputs,
		bucketSpentOutputs,
		bucketUnlockConditions,
		bucketWallet,
	}

	errNoKey = errors.New("key does not exist")

	// these keys are used in bucketWallet
	keyAuxiliarySeedFiles        = []byte("keyAuxiliarySeedFiles")
	keyConsensusChange           = []byte("keyConsensusChange")
	keyConsensusHeight           = []byte("keyConsensusHeight")
	keyEncryptionVerification    = []byte("keyEncryptionVerification")
	keyPrimarySeedFile           = []byte("keyPrimarySeedFile")
	keyPrimarySeedProgress       = []byte("keyPrimarySeedProgress")
	keySpendableKeyFiles         = []byte("keySpendableKeyFiles")
	keyUID                       = []byte("keyUID")
	keyWatchedAddrs              = []byte("keyWatchedAddrs")
	keySeedsMaximumInternalIndex = []byte("keySeedsMaximumInternalIndex")
	keySeedsMaximumExternalIndex = []byte("keySeedsMaximumExternalIndex")
)

// threadedDBUpdate commits the active database transaction and starts a new
// transaction.
func (w *Wallet) threadedDBUpdate() {
	if err := w.tg.Add(); err != nil {
		return
	}
	defer w.tg.Done()

	for {
		select {
		case <-time.After(2 * time.Minute):
		case <-w.tg.StopChan():
			return
		}
		w.mu.Lock()
		err := w.syncDB()
		w.mu.Unlock()
		if err != nil {
			// If the database is having problems, we need to close it to
			// protect it. This will likely cause a panic somewhere when another
			// caller tries to access dbTx but it is nil.
			w.log.Severe("ERROR: syncDB encountered an error. Closing database to protect wallet. wallet may crash:", err)
			w.db.Close()
			return
		}
	}
}

// syncDB commits the current global transaction and immediately begins a
// new one. It must be called with a write-lock.
func (w *Wallet) syncDB() error {
	// If the rollback flag is set, it means that somewhere in the middle of an
	// atomic update there  was a failure, and that failure needs to be rolled
	// back. An error will be returned.
	if w.dbRollback {
		err := errors.New("database unable to sync - rollback requested")
		return errors.Compose(err, w.dbTx.Rollback())
	}

	// commit the current tx
	err := w.dbTx.Commit()
	if err != nil {
		log.Println("ERROR: failed to apply database update:", err)
		w.log.Severe("ERROR: failed to apply database update:", err)
		err = errors.Compose(err, w.dbTx.Rollback())
		return errors.AddContext(err, "unable to commit dbTx in syncDB")
	}
	// begin a new tx
	w.dbTx, err = w.db.Begin(true)
	if err != nil {
		w.log.Severe("ERROR: failed to start database update:", err)
		return errors.AddContext(err, "unable to begin new dbTx in syncDB")
	}
	return nil
}

// dbReset wipes and reinitializes a wallet database.
func dbReset(tx *bolt.Tx) error {
	for _, bucket := range dbBuckets {
		err := tx.DeleteBucket(bucket)
		if err != nil {
			return err
		}
		_, err = tx.CreateBucket(bucket)
		if err != nil {
			return err
		}
	}

	// reinitialize the database with default values
	wb := tx.Bucket(bucketWallet)
	wb.Put(keyUID, fastrand.Bytes(len(uniqueID{})))
	wb.Put(keyConsensusHeight, encoding.Marshal(uint64(0)))
	wb.Put(keyAuxiliarySeedFiles, encoding.Marshal([]seedFile{}))
	wb.Put(keySpendableKeyFiles, encoding.Marshal([]spendableKeyFile{}))
	wb.Put(keyWatchedAddrs, encoding.Marshal([]types.UnlockHash{}))
	wb.Put(keySeedsMaximumInternalIndex, encoding.Marshal([]uint64{0}))
	wb.Put(keySeedsMaximumExternalIndex, encoding.Marshal([]uint64{0}))
	dbPutConsensusHeight(tx, 0)
	dbPutConsensusChangeID(tx, modules.ConsensusChangeBeginning)

	return nil
}

// dbPut is a helper function for storing a marshalled key/value pair.
func dbPut(b *bolt.Bucket, key, val interface{}) error {
	return b.Put(encoding.Marshal(key), encoding.Marshal(val))
}

// dbGet is a helper function for retrieving a marshalled key/value pair. val
// must be a pointer.
func dbGet(b *bolt.Bucket, key, val interface{}) error {
	valBytes := b.Get(encoding.Marshal(key))
	if valBytes == nil {
		return errNoKey
	}
	return encoding.Unmarshal(valBytes, val)
}

// dbDelete is a helper function for deleting a marshalled key/value pair.
func dbDelete(b *bolt.Bucket, key interface{}) error {
	return b.Delete(encoding.Marshal(key))
}

// dbForEach is a helper function for iterating over a bucket and calling fn
// on each entry. fn must be a function with two parameters. The key/value
// bytes of each bucket entry will be unmarshalled into the types of fn's
// parameters.
func dbForEach(b *bolt.Bucket, fn interface{}) error {
	// check function type
	fnVal, fnTyp := reflect.ValueOf(fn), reflect.TypeOf(fn)
	if fnTyp.Kind() != reflect.Func || fnTyp.NumIn() != 2 {
		panic("bad fn type: needed func(key, val), got " + fnTyp.String())
	}

	return b.ForEach(func(keyBytes, valBytes []byte) error {
		key, val := reflect.New(fnTyp.In(0)), reflect.New(fnTyp.In(1))
		if err := encoding.Unmarshal(keyBytes, key.Interface()); err != nil {
			return err
		} else if err := encoding.Unmarshal(valBytes, val.Interface()); err != nil {
			return err
		}
		fnVal.Call([]reflect.Value{key.Elem(), val.Elem()})
		return nil
	})
}

// Type-safe wrappers around the db helpers

func dbPutSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID, output types.SiacoinOutput) error {
	return dbPut(tx.Bucket(bucketSiacoinOutputs), id, output)
}
func dbGetSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID) (output types.SiacoinOutput, err error) {
	err = dbGet(tx.Bucket(bucketSiacoinOutputs), id, &output)
	return
}
func dbDeleteSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID) error {
	return dbDelete(tx.Bucket(bucketSiacoinOutputs), id)
}
func dbForEachSiacoinOutput(tx *bolt.Tx, fn func(types.SiacoinOutputID, types.SiacoinOutput)) error {
	return dbForEach(tx.Bucket(bucketSiacoinOutputs), fn)
}

func dbPutSpentOutput(tx *bolt.Tx, id types.OutputID, height types.BlockHeight) error {
	return dbPut(tx.Bucket(bucketSpentOutputs), id, height)
}
func dbGetSpentOutput(tx *bolt.Tx, id types.OutputID) (height types.BlockHeight, err error) {
	err = dbGet(tx.Bucket(bucketSpentOutputs), id, &height)
	return
}
func dbDeleteSpentOutput(tx *bolt.Tx, id types.OutputID) error {
	return dbDelete(tx.Bucket(bucketSpentOutputs), id)
}

func dbPutAddrTransactions(tx *bolt.Tx, addr types.UnlockHash, txns []uint64) error {
	return dbPut(tx.Bucket(bucketAddrTransactions), addr, txns)
}
func dbGetAddrTransactions(tx *bolt.Tx, addr types.UnlockHash) (txns []uint64, err error) {
	err = dbGet(tx.Bucket(bucketAddrTransactions), addr, &txns)
	return
}

func dbPutUnlockConditions(tx *bolt.Tx, uc types.UnlockConditions) error {
	return dbPut(tx.Bucket(bucketUnlockConditions), uc.UnlockHash(), uc)
}
func dbGetUnlockConditions(tx *bolt.Tx, addr types.UnlockHash) (uc types.UnlockConditions, err error) {
	err = dbGet(tx.Bucket(bucketUnlockConditions), addr, &uc)
	return
}

// dbAddAddrTransaction appends a single transaction index to the set of
// transactions associated with addr. If the index is already in the set, it is
// not added again.
func dbAddAddrTransaction(tx *bolt.Tx, addr types.UnlockHash, txn uint64) error {
	txns, err := dbGetAddrTransactions(tx, addr)
	if err != nil && err != errNoKey {
		return err
	}
	for _, i := range txns {
		if i == txn {
			return nil
		}
	}
	return dbPutAddrTransactions(tx, addr, append(txns, txn))
}

// dbAddProcessedTransactionAddrs updates bucketAddrTransactions to associate
// every address in pt with txn, which is assumed to be pt's index in
// bucketProcessedTransactions.
func dbAddProcessedTransactionAddrs(tx *bolt.Tx, pt modules.ProcessedTransaction, txn uint64) error {
	addrs := make(map[types.UnlockHash]struct{})
	for _, input := range pt.Inputs {
		addrs[input.RelatedAddress] = struct{}{}
	}
	for _, output := range pt.Outputs {
		// miner fees don't have an address, so skip them
		if output.FundType == types.SpecifierMinerFee {
			continue
		}
		addrs[output.RelatedAddress] = struct{}{}
	}
	for addr := range addrs {
		if err := dbAddAddrTransaction(tx, addr, txn); err != nil {
			return errors.AddContext(err, fmt.Sprintf("failed to add txn %v to address %v",
				pt.TransactionID, addr))
		}
	}
	return nil
}

// bucketProcessedTransactions works a little differently: the key is
// meaningless, only used to order the transactions chronologically.

// decodeProcessedTransaction decodes a marshalled processedTransaction
func decodeProcessedTransaction(ptBytes []byte, pt *modules.ProcessedTransaction) error {
	err := encoding.Unmarshal(ptBytes, pt)
	return err
}

func dbDeleteTransactionIndex(tx *bolt.Tx, txid types.TransactionID) error {
	return dbDelete(tx.Bucket(bucketProcessedTxnIndex), txid)
}
func dbPutTransactionIndex(tx *bolt.Tx, txid types.TransactionID, key []byte) error {
	return dbPut(tx.Bucket(bucketProcessedTxnIndex), txid, key)
}

func dbGetTransactionIndex(tx *bolt.Tx, txid types.TransactionID) (key []byte, err error) {
	key = make([]byte, 8)
	err = dbGet(tx.Bucket(bucketProcessedTxnIndex), txid, &key)
	return
}

// initProcessedTxnIndex initializes the bucketProcessedTxnIndex with the
// elements from bucketProcessedTransactions
func initProcessedTxnIndex(tx *bolt.Tx) error {
	it := dbProcessedTransactionsIterator(tx)
	indexBytes := make([]byte, 8)
	for it.next() {
		index, pt := it.key(), it.value()
		binary.BigEndian.PutUint64(indexBytes, index)
		if err := dbPutTransactionIndex(tx, pt.TransactionID, indexBytes); err != nil {
			return err
		}
	}
	return nil
}

func dbAppendProcessedTransaction(tx *bolt.Tx, pt modules.ProcessedTransaction) error {
	b := tx.Bucket(bucketProcessedTransactions)
	key, err := b.NextSequence()
	if err != nil {
		return errors.AddContext(err, "failed to get next sequence from bucket")
	}
	// big-endian is used so that the keys are properly sorted
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, key)
	if err = b.Put(keyBytes, encoding.Marshal(pt)); err != nil {
		return errors.AddContext(err, "failed to store processed txn in database")
	}

	// add used index to bucketProcessedTxnIndex
	if err = dbPutTransactionIndex(tx, pt.TransactionID, keyBytes); err != nil {
		return errors.AddContext(err, "failed to store txn index in database")
	}

	// also add this txid to the bucketAddrTransactions
	if err = dbAddProcessedTransactionAddrs(tx, pt, key); err != nil {
		return errors.AddContext(err, "failed to add processed transaction to addresses in database")
	}
	return nil
}

func dbGetLastProcessedTransaction(tx *bolt.Tx) (pt modules.ProcessedTransaction, err error) {
	seq := tx.Bucket(bucketProcessedTransactions).Sequence()
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, seq)
	val := tx.Bucket(bucketProcessedTransactions).Get(keyBytes)
	err = decodeProcessedTransaction(val, &pt)
	return
}

func dbDeleteLastProcessedTransaction(tx *bolt.Tx) error {
	// Get the last processed txn.
	pt, err := dbGetLastProcessedTransaction(tx)
	if err != nil {
		return errors.New("can't delete from empty bucket")
	}
	// Delete its txid from the index bucket.
	if err := dbDeleteTransactionIndex(tx, pt.TransactionID); err != nil {
		return errors.AddContext(err, "couldn't delete txn index")
	}
	// Delete the last processed txn and decrement the sequence.
	b := tx.Bucket(bucketProcessedTransactions)
	seq := b.Sequence()
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, seq)
	return errors.Compose(b.SetSequence(seq-1), b.Delete(keyBytes))
}

func dbGetProcessedTransaction(tx *bolt.Tx, index uint64) (pt modules.ProcessedTransaction, err error) {
	// big-endian is used so that the keys are properly sorted
	indexBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(indexBytes, index)
	val := tx.Bucket(bucketProcessedTransactions).Get(indexBytes)
	err = decodeProcessedTransaction(val, &pt)
	return
}

// A processedTransactionsIter iterates through the ProcessedTransactions bucket.
type processedTransactionsIter struct {
	c   *bolt.Cursor
	seq uint64
	pt  modules.ProcessedTransaction
}

// next decodes the next ProcessedTransaction, returning false if the end of
// the bucket has been reached.
func (it *processedTransactionsIter) next() bool {
	var seqBytes, ptBytes []byte
	if it.pt.TransactionID == (types.TransactionID{}) {
		// this is the first time next has been called, so cursor is not
		// initialized yet
		seqBytes, ptBytes = it.c.First()
	} else {
		seqBytes, ptBytes = it.c.Next()
	}
	if seqBytes == nil {
		return false
	}
	it.seq = binary.BigEndian.Uint64(seqBytes)
	return decodeProcessedTransaction(ptBytes, &it.pt) == nil
}

// key returns the key for the most recently decoded ProcessedTransaction.
func (it *processedTransactionsIter) key() uint64 {
	return it.seq
}

// value returns the most recently decoded ProcessedTransaction.
func (it *processedTransactionsIter) value() modules.ProcessedTransaction {
	return it.pt
}

// dbProcessedTransactionsIterator creates a new processedTransactionsIter.
func dbProcessedTransactionsIterator(tx *bolt.Tx) *processedTransactionsIter {
	return &processedTransactionsIter{
		c: tx.Bucket(bucketProcessedTransactions).Cursor(),
	}
}

// dbGetWalletUID returns the UID assigned to the wallet's primary seed.
func dbGetWalletUID(tx *bolt.Tx) (uid uniqueID) {
	copy(uid[:], tx.Bucket(bucketWallet).Get(keyUID))
	return
}

// dbGetPrimarySeedProgress returns the number of keys generated from the
// primary seed.
func dbGetPrimarySeedProgress(tx *bolt.Tx) (progress uint64, err error) {
	err = encoding.Unmarshal(tx.Bucket(bucketWallet).Get(keyPrimarySeedProgress), &progress)
	return
}

// dbPutPrimarySeedProgress sets the primary seed progress counter.
func dbPutPrimarySeedProgress(tx *bolt.Tx, progress uint64) error {
	return tx.Bucket(bucketWallet).Put(keyPrimarySeedProgress, encoding.Marshal(progress))
}

// dbGetConsensusChangeID returns the ID of the last ConsensusChange processed by the wallet.
func dbGetConsensusChangeID(tx *bolt.Tx) (cc modules.ConsensusChangeID) {
	copy(cc[:], tx.Bucket(bucketWallet).Get(keyConsensusChange))
	return
}

// dbPutConsensusChangeID stores the ID of the last ConsensusChange processed by the wallet.
func dbPutConsensusChangeID(tx *bolt.Tx, cc modules.ConsensusChangeID) error {
	return tx.Bucket(bucketWallet).Put(keyConsensusChange, cc[:])
}

// dbGetConsensusHeight returns the height that the wallet has scanned to.
func dbGetConsensusHeight(tx *bolt.Tx) (height types.BlockHeight, err error) {
	err = encoding.Unmarshal(tx.Bucket(bucketWallet).Get(keyConsensusHeight), &height)
	return
}

// dbPutConsensusHeight stores the height that the wallet has scanned to.
func dbPutConsensusHeight(tx *bolt.Tx, height types.BlockHeight) error {
	return tx.Bucket(bucketWallet).Put(keyConsensusHeight, encoding.Marshal(height))
}

// dbGetSeedsMaximumInternalIndex returns the maximum internal address indices for all seeds.
func dbGetSeedsMaximumInternalIndex(tx *bolt.Tx) (indices []uint64, err error) {
	err = encoding.Unmarshal(tx.Bucket(bucketWallet).Get(keySeedsMaximumInternalIndex), &indices)
	if err != nil {
		return
	}
	return
}

// dbPutSeedsMaximumInternalIndex sets the maximum internal address address indices for all seeds.
func dbPutSeedsMaximumInternalIndex(tx *bolt.Tx, indices []uint64) (err error) {
	return tx.Bucket(bucketWallet).Put(keySeedsMaximumInternalIndex, encoding.Marshal(indices))
}

// dbGetSeedsMaximumInternalIndexForSeed returns the maximum internal address indices for a given
// seed number.
func dbGetSeedsMaximumInternalIndexForSeed(tx *bolt.Tx, seedIndex uint64) (index uint64, err error) {
	indices, err := dbGetSeedsMaximumInternalIndex(tx)
	if err != nil {
		return
	}
	index = indices[seedIndex]
	return
}

// dbPutWatchedAddresses stores the set of watched addresses.
func dbPutWatchedAddresses(tx *bolt.Tx, addrs []types.UnlockHash) error {
	return tx.Bucket(bucketWallet).Put(keyWatchedAddrs, encoding.Marshal(addrs))
}

// dbPutSeedsMaximumInternalIndexForSeed sets the maximum internal address index for a given seed
// number.
func dbPutSeedsMaximumInternalIndexForSeed(tx *bolt.Tx, seedIndex, index uint64) (err error) {
	indices, err := dbGetSeedsMaximumInternalIndex(tx)
	if err != nil {
		return
	}
	indices[seedIndex] = index
	err = dbPutSeedsMaximumInternalIndex(tx, indices)
	return
}

// dbGetPrimarySeedMaximumInternalIndex returns the maximum internal address index for the primary
// seed.
func dbGetPrimarySeedMaximumInternalIndex(tx *bolt.Tx) (index uint64, err error) {
	index, err = dbGetSeedsMaximumInternalIndexForSeed(tx, 0)
	return
}

// dbPutPrimarySeedMaximumInternalIndex sets the maximum internal address index for the primary
// seed.
func dbPutPrimarySeedMaximumInternalIndex(tx *bolt.Tx, index uint64) error {
	return dbPutSeedsMaximumInternalIndexForSeed(tx, 0, index)
}

// dbGetSeedsMaximumExternalIndex returns the maximum external address indices for all seeds.
func dbGetSeedsMaximumExternalIndex(tx *bolt.Tx) (indices []uint64, err error) {
	err = encoding.Unmarshal(tx.Bucket(bucketWallet).Get(keySeedsMaximumExternalIndex), &indices)
	if err != nil {
		return
	}
	return
}

// dbPutSeedsMaximumExternalIndex sets the maximum external address address indices for all seeds.
func dbPutSeedsMaximumExternalIndex(tx *bolt.Tx, indices []uint64) (err error) {
	return tx.Bucket(bucketWallet).Put(keySeedsMaximumExternalIndex, encoding.Marshal(indices))
}

// dbGetSeedsMaximumExternalIndexForSeed returns the maximum external address indices for a given
// seed number.
func dbGetSeedsMaximumExternalIndexForSeed(tx *bolt.Tx, seedIndex uint64) (index uint64, err error) {
	indices, err := dbGetSeedsMaximumExternalIndex(tx)
	if err != nil {
		return
	}
	index = indices[seedIndex]
	return
}

// dbPutSeedsMaximumExternalIndexForSeed sets the maximum external address index for a given seed
// number.
func dbPutSeedsMaximumExternalIndexForSeed(tx *bolt.Tx, seedIndex, index uint64) (err error) {
	indices, err := dbGetSeedsMaximumExternalIndex(tx)
	if err != nil {
		return
	}
	indices[seedIndex] = index
	err = dbPutSeedsMaximumExternalIndex(tx, indices)
	return
}

// dbGetPrimarySeedMaximumExternalIndex returns the maximum external address index for the primary
// seed.
func dbGetPrimarySeedMaximumExternalIndex(tx *bolt.Tx) (index uint64, err error) {
	index, err = dbGetSeedsMaximumExternalIndexForSeed(tx, 0)
	return
}

// dbPutPrimarySeedMaximumExternalIndex sets the maximum external address index for the primary
// seed.
func dbPutPrimarySeedMaximumExternalIndex(tx *bolt.Tx, index uint64) error {
	return dbPutSeedsMaximumExternalIndexForSeed(tx, 0, index)
}

package wallet

import (
	"errors"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
	"gitlab.com/NebulousLabs/fastrand"
)

const (
	// SiagFileExtension is the file extension to be used for siag files
	SiagFileExtension = ".siakey"

	// SiagFileHeader is the header for all siag files. Do not change. Because siag was created
	// early in development, compatibility with siag requires manually handling
	// the headers and version instead of using the persist package.
	SiagFileHeader = "siag"

	// SiagFileVersion is the version number to be used for siag files
	SiagFileVersion = "1.0"
)

var (
	errAllDuplicates         = errors.New("old wallet has no new seeds")
	errDuplicateSpendableKey = errors.New("key has already been loaded into the wallet")

	// ErrInconsistentKeys is the error when keyfiles provided are for different addresses
	ErrInconsistentKeys = errors.New("keyfiles provided that are for different addresses")
	// ErrInsufficientKeys is the error when there's not enough keys provided to spend the siafunds
	ErrInsufficientKeys = errors.New("not enough keys provided to spend the siafunds")
	// ErrNoKeyfile is the error when no keyfile has been presented
	ErrNoKeyfile = errors.New("no keyfile has been presented")
	// ErrUnknownHeader is the error when file contains wrong header
	ErrUnknownHeader = errors.New("file contains the wrong header")
	// ErrUnknownVersion is the error when the file has an unknown version number
	ErrUnknownVersion = errors.New("file has an unknown version number")
)

// A siagKeyPair is the struct representation of the bytes that get saved to
// disk by siag when a new keyfile is created.
type siagKeyPair struct {
	Header           string
	Version          string
	Index            int // should be uint64 - too late now
	SecretKey        crypto.SecretKey
	UnlockConditions types.UnlockConditions
}

// decryptSpendableKeyFile decrypts a spendableKeyFile, returning a
// spendableKey.
func decryptSpendableKeyFile(masterKey crypto.TwofishKey, uk spendableKeyFile) (sk spendableKey, err error) {
	// Verify that the decryption key is correct.
	decryptionKey := uidEncryptionKey(masterKey, uk.UID)
	err = verifyEncryption(decryptionKey, uk.EncryptionVerification)
	if err != nil {
		return
	}

	// Decrypt the spendable key and add it to the wallet.
	encodedKey, err := decryptionKey.DecryptBytes(uk.SpendableKey)
	if err != nil {
		return
	}
	err = encoding.Unmarshal(encodedKey, &sk)
	return
}

// integrateSpendableKey loads a spendableKey into the wallet.
func (w *Wallet) integrateSpendableKey(masterKey crypto.TwofishKey, sk spendableKey) {
	w.keys[sk.UnlockConditions.UnlockHash()] = sk
}

// loadSpendableKey loads a spendable key into the wallet database.
func (w *Wallet) loadSpendableKey(masterKey crypto.TwofishKey, sk spendableKey) error {
	// Duplication is detected by looking at the set of unlock conditions. If
	// the wallet is locked, correct deduplication is uncertain.
	if !w.unlocked {
		return modules.ErrLockedWallet
	}

	// Check for duplicates.
	_, exists := w.keys[sk.UnlockConditions.UnlockHash()]
	if exists {
		return errDuplicateSpendableKey
	}

	// TODO: Check that the key is actually spendable.

	// Create a UID and encryption verification.
	var skf spendableKeyFile
	fastrand.Read(skf.UID[:])
	encryptionKey := uidEncryptionKey(masterKey, skf.UID)
	skf.EncryptionVerification = encryptionKey.EncryptBytes(verificationPlaintext)

	// Encrypt and save the key.
	skf.SpendableKey = encryptionKey.EncryptBytes(encoding.Marshal(sk))

	err := checkMasterKey(w.dbTx, masterKey)
	if err != nil {
		return err
	}
	var current []spendableKeyFile
	err = encoding.Unmarshal(w.dbTx.Bucket(bucketWallet).Get(keySpendableKeyFiles), &current)
	if err != nil {
		return err
	}
	return w.dbTx.Bucket(bucketWallet).Put(keySpendableKeyFiles, encoding.Marshal(append(current, skf)))

	// w.keys[sk.UnlockConditions.UnlockHash()] = sk -> aids with duplicate
	// detection, but causes db inconsistency. Rescanning is probably the
	// solution.
}

// loadSiagKeys loads a set of siag keyfiles into the wallet, so that the
// wallet may spend the siafunds.
func (w *Wallet) loadSiagKeys(masterKey crypto.TwofishKey, keyfiles []string) error {
	// Load the keyfiles from disk.
	if len(keyfiles) < 1 {
		return ErrNoKeyfile
	}
	skps := make([]siagKeyPair, len(keyfiles))
	for i, keyfile := range keyfiles {
		err := encoding.ReadFile(keyfile, &skps[i])
		if err != nil {
			return err
		}

		if skps[i].Header != SiagFileHeader {
			return ErrUnknownHeader
		}
		if skps[i].Version != SiagFileVersion {
			return ErrUnknownVersion
		}
	}

	// Check that all of the loaded files have the same address, and that there
	// are enough to create the transaction.
	baseUnlockHash := skps[0].UnlockConditions.UnlockHash()
	for _, skp := range skps {
		if skp.UnlockConditions.UnlockHash() != baseUnlockHash {
			return ErrInconsistentKeys
		}
	}
	if uint64(len(skps)) < skps[0].UnlockConditions.SignaturesRequired {
		return ErrInsufficientKeys
	}
	// Drop all unneeded keys.
	skps = skps[0:skps[0].UnlockConditions.SignaturesRequired]

	// Merge the keys into a single spendableKey and save it to the wallet.
	var sk spendableKey
	sk.UnlockConditions = skps[0].UnlockConditions
	for _, skp := range skps {
		sk.SecretKeys = append(sk.SecretKeys, skp.SecretKey)
	}
	err := w.loadSpendableKey(masterKey, sk)
	if err != nil {
		return err
	}
	w.integrateSpendableKey(masterKey, sk)
	return nil
}

// LoadSiagKeys loads a set of siag-generated keys into the wallet.
func (w *Wallet) LoadSiagKeys(masterKey crypto.TwofishKey, keyfiles []string) error {
	if err := w.tg.Add(); err != nil {
		return err
	}
	defer w.tg.Done()

	// load the keys and reset the consensus change ID and height in preparation for rescan
	err := func() error {
		w.mu.Lock()
		defer w.mu.Unlock()
		err := w.loadSiagKeys(masterKey, keyfiles)
		if err != nil {
			return err
		}

		if err = w.dbTx.DeleteBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		if _, err = w.dbTx.CreateBucket(bucketProcessedTransactions); err != nil {
			return err
		}
		w.unconfirmedProcessedTransactions = nil
		err = dbPutConsensusChangeID(w.dbTx, modules.ConsensusChangeBeginning)
		if err != nil {
			return err
		}
		return dbPutConsensusHeight(w.dbTx, 0)
	}()
	if err != nil {
		return err
	}

	// rescan the blockchain
	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	done := make(chan struct{})
	go w.rescanMessage(done)
	defer close(done)

	err = w.cs.ConsensusSetSubscribe(w, modules.ConsensusChangeBeginning, w.tg.StopChan())
	if err != nil {
		return err
	}
	w.tpool.TransactionPoolSubscribe(w)
	return nil
}

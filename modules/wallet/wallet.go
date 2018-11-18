package wallet

// TODO: Theoretically, the transaction builder in this wallet supports
// multisig, but there are no automated tests to verify that.

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/coreos/bbolt"
	deadlock "github.com/sasha-s/go-deadlock"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/errors"
	"github.com/HyperspaceApp/threadgroup"
)

const (
	// RespendTimeout records the number of blocks that the wallet will wait
	// before spending an output that has been spent in the past. If the
	// transaction spending the output has not made it to the transaction pool
	// after the limit, the assumption is that it never will.
	RespendTimeout = 40
)

var (
	errNilConsensusSet = errors.New("wallet cannot initialize with a nil consensus set")
	errNilTpool        = errors.New("wallet cannot initialize with a nil transaction pool")
)

// spendableKey is a set of secret keys plus the corresponding unlock
// conditions.  The public key can be derived from the secret key and then
// matched to the corresponding public keys in the unlock conditions. All
// addresses that are to be used in 'FundSiacoins' in the
// transaction builder must conform to this form of spendable key.
type spendableKey struct {
	UnlockConditions types.UnlockConditions
	SecretKeys       []crypto.SecretKey
}

// Wallet is an object that tracks balances, creates keys and addresses,
// manages building and sending transactions.
type Wallet struct {
	// encrypted indicates whether the wallet has been encrypted (i.e.
	// initialized). unlocked indicates whether the wallet is currently
	// storing secret keys in memory. subscribed indicates whether the wallet
	// has subscribed to the consensus set yet - the wallet is unable to
	// subscribe to the consensus set until it has been unlocked for the first
	// time. The primary seed is used to generate new addresses for the
	// wallet.
	encrypted   bool
	unlocked    bool
	subscribed  bool
	primarySeed modules.Seed

	// The wallet's dependencies.
	cs    modules.ConsensusSet
	tpool modules.TransactionPool
	deps  modules.Dependencies

	// The following set of fields are responsible for tracking the confirmed
	// outputs, and for being able to spend them. The seeds are used to derive
	// the keys that are tracked on the blockchain. All keys are pregenerated
	// from the seeds, when checking new outputs or spending outputs, the seeds
	// are not referenced at all. The seeds are only stored so that the user
	// may access them.
	seeds        []modules.Seed
	keys         map[types.UnlockHash]spendableKey
	lookahead    lookahead
	watchedAddrs map[types.UnlockHash]struct{}
	// The minimum index should typically be zero, seeds that came over from
	// the Sia airdrop may have started with very high indices. So when we
	// import old seeds, we scan the airdrop blocks first and set a minimum
	// with the first value we find, or 0 if we don't find any matches.
	seedsMinimumIndex []uint64
	// The maximum internal index is the highest address index we're tracking
	// locally for a seed. Once the external blockchain has been scanned,
	// this value should be greater or equal to the maximum external index
	// that we've seen. If we are enforcing addressGapLimit, this value
	// should not exceed the maximum external index + addressGapLimit.
	seedsMaximumInternalIndex []uint64
	// The maximum external address is the highest address we've seen on the
	// external blockchain.
	seedsMaximumExternalIndex []uint64

	// unconfirmedProcessedTransactions tracks unconfirmed transactions.
	//
	// TODO: Replace this field with a linked list. Currently when a new
	// transaction set diff is provided, the entire array needs to be
	// reallocated. Since this can happen tens of times per second, and the
	// array can have tens of thousands of elements, it's a performance issue.
	unconfirmedSets                  map[modules.TransactionSetID][]types.TransactionID
	unconfirmedProcessedTransactions []modules.ProcessedTransaction

	// The wallet's database tracks its seeds, keys, outputs, and
	// transactions. A global db transaction is maintained in memory to avoid
	// excessive disk writes. Any operations involving dbTx must hold an
	// exclusive lock.
	//
	// If dbRollback is set, then when the database syncs it will perform a
	// rollback instead of a commit. For safety reasons, the db will close and
	// the wallet will close if a rollback is performed.
	db         *persist.BoltDatabase
	dbRollback bool
	dbTx       *bolt.Tx

	persistDir string
	log        *persist.Logger
	mu         deadlock.RWMutex

	// A separate TryMutex is used to protect against concurrent unlocking or
	// initialization.
	scanLock siasync.TryMutex

	// The wallet's ThreadGroup tells tracked functions to shut down and
	// blocks until they have all exited before returning from Close.
	tg threadgroup.ThreadGroup

	// addressGapLimit is by default set to 20. If the software hits 20 unused
	// addresses in a row, it expects there are no used addresses beyond this
	// point and stops searching the address chain. We scan just the external
	// chains, because internal chains receive only coins that come from the
	// associated external chains.
	//
	// For further information, read BIP 44.
	addressGapLimit uint64
	// scanAirdrop specifies whether or not we do a robust scan on legacy Sia
	// seeds against the initial 7 airdrop blocks
	scanAirdrop bool
}

// Height return the internal processed consensus height of the wallet
func (w *Wallet) Height() (types.BlockHeight, error) {
	if err := w.tg.Add(); err != nil {
		return types.BlockHeight(0), modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncDB()

	var height uint64
	err := w.db.View(func(tx *bolt.Tx) error {
		return encoding.Unmarshal(tx.Bucket(bucketWallet).Get(keyConsensusHeight), &height)
	})
	if err != nil {
		return types.BlockHeight(0), err
	}
	return types.BlockHeight(height), nil
}

// New creates a new wallet, loading any known addresses from the input file
// name and then using the file to save in the future. Keys and addresses are
// not loaded into the wallet during the call to 'new', but rather during the
// call to 'Unlock'.
func New(cs modules.ConsensusSet, tpool modules.TransactionPool, persistDir string, addressGapLimit int, scanAirdrop bool) (*Wallet, error) {
	return NewCustomWallet(cs, tpool, persistDir, addressGapLimit, scanAirdrop, modules.ProdDependencies)
}

// NewCustomWallet creates a new wallet using custom dependencies.
func NewCustomWallet(cs modules.ConsensusSet, tpool modules.TransactionPool, persistDir string, addressGapLimit int, scanAirdrop bool, deps modules.Dependencies) (*Wallet, error) {
	// Check for nil dependencies.
	if cs == nil {
		return nil, errNilConsensusSet
	}
	if tpool == nil {
		return nil, errNilTpool
	}
	lookahead := newLookahead(uint64(addressGapLimit))

	// Initialize the data structure.
	w := &Wallet{
		cs:    cs,
		tpool: tpool,

		keys:         make(map[types.UnlockHash]spendableKey),
		lookahead:    lookahead,
		watchedAddrs: make(map[types.UnlockHash]struct{}),

		unconfirmedSets: make(map[modules.TransactionSetID][]types.TransactionID),

		persistDir: persistDir,

		addressGapLimit: uint64(addressGapLimit),
		scanAirdrop:     scanAirdrop,

		deps: deps,
	}
	err := w.initPersist()
	if err != nil {
		return nil, err
	}

	cs.SetGetWalletKeysFunc(func() ([][]byte, error) {
		return w.allAddressesInByteArray()
	})
	tpool.SetGetWalletKeysFunc(func() (map[types.UnlockHash]bool, error) {
		return w.allAddressesInMap()
	})

	return w, nil
}

// Close terminates all ongoing processes involving the wallet, enabling
// garbage collection.
func (w *Wallet) Close() error {
	if err := w.tg.Stop(); err != nil {
		return err
	}
	var errs []error
	// Lock the wallet outside of mu.Lock because Lock uses its own mu.Lock.
	// Once the wallet is locked it cannot be unlocked except using the
	// unexported unlock method (w.Unlock returns an error if the wallet's
	// ThreadGroup is stopped).
	if w.managedUnlocked() {
		if err := w.managedLock(); err != nil {
			errs = append(errs, err)
		}
	}

	w.cs.Unsubscribe(w)
	w.tpool.Unsubscribe(w)

	if err := w.log.Close(); err != nil {
		errs = append(errs, fmt.Errorf("log.Close failed: %v", err))
	}
	return build.JoinErrors(errs, "; ")
}

func (w *Wallet) allAddressesInByteArray() ([][]byte, error) {
	if err := w.tg.Add(); err != nil {
		return [][]byte{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.RLock()
	defer w.mu.RUnlock()

	keyArray := make([][]byte, 0, len(w.keys))
	// TODO: maybe cache this somewhere
	for u := range w.keys {
		byteArray := make([]byte, len(u[:]))
		copy(byteArray, u[:])
		keyArray = append(keyArray, byteArray)
	}

	return keyArray, nil
}

func (w *Wallet) lookaheadAddressesInByteArray() ([][]byte, error) {
	if err := w.tg.Add(); err != nil {
		return [][]byte{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.RLock()
	defer w.mu.RUnlock()

	addresses := w.lookahead.Addresses()
	keyArray := make([][]byte, 0, len(addresses))
	// TODO: maybe cache this somewhere
	for _, u := range addresses {
		byteArray := make([]byte, len(u[:]))
		copy(byteArray, u[:])
		keyArray = append(keyArray, byteArray)
	}
	return keyArray, nil
}

func (w *Wallet) allAddressesInMap() (map[types.UnlockHash]bool, error) {
	if err := w.tg.Add(); err != nil {
		return nil, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.RLock()
	defer w.mu.RUnlock()

	keysMap := make(map[types.UnlockHash]bool)
	// TODO: maybe cache this somewhere
	for u := range w.keys {
		keysMap[u] = true
	}
	return keysMap, nil
}

// AllAddresses returns all addresses that the wallet is able to spend from,
// including unseeded addresses. Addresses are returned sorted in byte-order.
func (w *Wallet) AllAddresses() ([]types.UnlockHash, error) {
	if err := w.tg.Add(); err != nil {
		return []types.UnlockHash{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.RLock()
	defer w.mu.RUnlock()

	addrs := make([]types.UnlockHash, 0, len(w.keys))
	for addr := range w.keys {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	return addrs, nil
}

// Rescanning reports whether the wallet is currently rescanning the
// blockchain.
func (w *Wallet) Rescanning() (bool, error) {
	if err := w.tg.Add(); err != nil {
		return false, modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	rescanning := !w.scanLock.TryLock()
	if !rescanning {
		w.scanLock.Unlock()
	}
	return rescanning, nil
}

// Settings returns the wallet's current settings
func (w *Wallet) Settings() (modules.WalletSettings, error) {
	if err := w.tg.Add(); err != nil {
		return modules.WalletSettings{}, modules.ErrWalletShutdown
	}
	defer w.tg.Done()
	return modules.WalletSettings{}, nil
}

// SetSettings will update the settings for the wallet.
func (w *Wallet) SetSettings(s modules.WalletSettings) error {
	if err := w.tg.Add(); err != nil {
		return modules.ErrWalletShutdown
	}
	defer w.tg.Done()

	w.mu.Lock()
	w.mu.Unlock()
	return nil
}

// Package pool is an implementation of the pool module, and is responsible for
// creating a mining pool, accepting incoming potential block solutions and
// rewarding the submitters proportionally for their shares.
package pool

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/config"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/NebulousLabs/threadgroup"

	// blank to load the sql driver for mysql
	_ "github.com/go-sql-driver/mysql"
)

var (
	// persistMetadata is the header that gets written to the persist file, and is
	// used to recognize other persist files.
	persistMetadata = persist.Metadata{
		Header:  "Sia Pool",
		Version: "0.0.1",
	}

	// errPoolClosed gets returned when a call is rejected due to the pool
	// having been closed.
	errPoolClosed = errors.New("call is disabled because the pool is closed")

	// Nil dependency errors.
	errNilCS    = errors.New("pool cannot use a nil consensus state")
	errNilTpool = errors.New("pool cannot use a nil transaction pool")
	errNilGW    = errors.New("pool cannot use a nil gateway")
	//	errNilWallet = errors.New("pool cannot use a nil wallet")

	// Required settings to run pool
	errNoAddressSet = errors.New("pool operators address must be set")

	running  bool  // indicates if the mining pool is actually running
	hashRate int64 // indicates hashes per second
	// HeaderMemory is the number of previous calls to 'header'
	// that are remembered. Additionally, 'header' will only poll for a
	// new block every 'headerMemory / blockMemory' times it is
	// called. This reduces the amount of memory used, but comes at the cost of
	// not always having the most recent transactions.
	HeaderMemory = build.Select(build.Var{
		Standard: 10000,
		Dev:      500,
		Testing:  50,
	}).(int)

	// BlockMemory is the maximum number of blocks the miner will store
	// Blocks take up to 2 megabytes of memory, which is why this number is
	// limited.
	BlockMemory = build.Select(build.Var{
		Standard: 50,
		Dev:      10,
		Testing:  5,
	}).(int)

	// MaxSourceBlockAge is the maximum amount of time that is allowed to
	// elapse between generating source blocks.
	MaxSourceBlockAge = build.Select(build.Var{
		Standard: 30 * time.Second,
		Dev:      5 * time.Second,
		Testing:  1 * time.Second,
	}).(time.Duration)

	// ShiftDuration is how often we commit mining data to persistent
	// storage when a block hasn't been found.
	ShiftDuration = build.Select(build.Var{
		Standard: 20 * time.Second,
		Dev:      20 * time.Second,
		Testing:  1 * time.Second,
	}).(time.Duration)
)

// splitSet defines a transaction set that can be added componenet-wise to a
// block. It's split because it doesn't necessarily represent the full set
// prpovided by the transaction pool. Splits can be sorted so that the largest
// and most valuable sets can be selected when picking transactions.
type splitSet struct {
	averageFee   types.Currency
	size         uint64
	transactions []types.Transaction
}

type splitSetID int

// A Pool contains all the fields necessary for storing status for clients and
// performing the evaluation and rewarding on submitted shares
type Pool struct {
	// BlockManager variables. Becaues blocks are large, one block is used to
	// make many headers which can be used by miners. Headers include an
	// arbitrary data transaction (appended to the block) to make the merkle
	// roots unique (preventing miners from doing redundant work). Every N
	// requests or M seconds, a new block is used to create headers.
	//
	// Only 'blocksMemory' blocks are kept in memory at a time, which
	// keeps ram usage reasonable. Miners may request many headers in parallel,
	// and thus may be working on different blocks. When they submit the solved
	// header to the block manager, the rest of the block needs to be found in
	// a lookup.
	blockMem        map[types.BlockHeader]*types.Block             // Mappings from headers to the blocks they are derived from.
	blockTxns       *txnList                                       // list of transactions that are supposed to be solved in the next block
	arbDataMem      map[types.BlockHeader][crypto.EntropySize]byte // Mappings from the headers to their unique arb data.
	headerMem       []types.BlockHeader                            // A circular list of headers that have been given out from the api recently.
	sourceBlock     types.Block                                    // The block from which new headers for mining are created.
	sourceBlockTime time.Time                                      // How long headers have been using the same block (different from 'recent block').
	memProgress     int                                            // The index of the most recent header used in headerMem.

	// Transaction pool variables.
	fullSets        map[modules.TransactionSetID][]int
	blockMapHeap    *mapHeap
	overflowMapHeap *mapHeap
	setCounter      int
	splitSets       map[splitSetID]*splitSet

	// Dependencies.
	cs     modules.ConsensusSet
	tpool  modules.TransactionPool
	wallet modules.Wallet
	gw     modules.Gateway
	dependencies
	modules.StorageManager

	// Pool ACID fields - these fields need to be updated in serial, ACID
	// transactions.
	announceConfirmed bool
	secretKey         crypto.SecretKey
	// Pool transient fields - these fields are either determined at startup or
	// otherwise are not critical to always be correct.
	workingStatus        modules.PoolWorkingStatus
	connectabilityStatus modules.PoolConnectabilityStatus

	// Utilities.
	sqldb          *sql.DB
	listener       net.Listener
	log            *persist.Logger
	yiilog         *persist.Logger
	mu             sync.RWMutex
	persistDir     string
	port           string
	tg             threadgroup.ThreadGroup
	persist        persistence
	dispatcher     *Dispatcher
	stratumID      uint64
	shiftID        uint64
	shiftChan      chan bool
	shiftTimestamp time.Time
	blockCounter   uint64
	clients        map[string]*Client //client name to client pointer mapping

	clientSetupMutex sync.Mutex
	runningMutex     sync.RWMutex
	running          bool
}

// startupRescan will rescan the blockchain in the event that the pool
// persistence layer has become desynchronized from the consensus persistence
// layer. This might happen if a user replaces any of the folders with backups
// or deletes any of the folders.
func (p *Pool) startupRescan() error {
	// Reset all of the variables that have relevance to the consensus set. The
	// operations are wrapped by an anonymous function so that the locking can
	// be handled using a defer statement.
	err := func() error {
		//		p.log.Debugf("Waiting to lock pool\n")
		p.mu.Lock()
		defer func() {
			//			p.log.Debugf("Unlocking pool\n")
			p.mu.Unlock()
		}()

		p.log.Println("Performing a pool rescan.")
		p.persist.SetRecentChange(modules.ConsensusChangeBeginning)
		p.persist.SetBlockHeight(0)
		p.persist.SetTarget(types.Target{})
		return p.saveSync()
	}()
	if err != nil {
		return err
	}

	// Subscribe to the consensus set. This is a blocking call that will not
	// return until the pool has fully caught up to the current block.
	err = p.cs.ConsensusSetSubscribe(p, modules.ConsensusChangeBeginning, p.tg.StopChan())
	if err != nil {
		return err
	}
	p.tg.OnStop(func() error {
		p.cs.Unsubscribe(p)
		return nil
	})
	return nil
}

func (p *Pool) monitorShifts() {
	p.shiftChan = make(chan bool, 1)
	p.tg.Add()
	defer p.tg.Done()
	for {
		select {
		case <-p.shiftChan:
		case <-time.After(ShiftDuration):
		case <-p.tg.StopChan():
			return
		}
		p.log.Debugf("Shift change - end of shift %d\n", p.shiftID)
		atomic.AddUint64(&p.shiftID, 1)
		p.dispatcher.mu.RLock()
		// TODO: switch to batched insert
		for _, h := range p.dispatcher.handlers {
			h.mu.RLock()
			s := h.s.Shift()
			h.mu.RUnlock()
			if s != nil {
				s.UpdateOrSaveShift()
			}
			sh := p.newShift(h.s.CurrentWorker)
			h.s.addShift(sh)
		}
		p.dispatcher.mu.RUnlock()
	}
}

func (p *Pool) startServer() {
	p.log.Printf("      Waiting for consensus synchronization...\n")
	select {
	// if we've received the stop message before this can even be spun up, just exit
	case <-p.tg.StopChan():
		return
	default:
	}
	p.tg.Add()
	defer p.tg.Done()
	for {
		if p.cs.Synced() {
			// If we're immediately synced upon subscription AND we never got ProcessConsensusChange
			// calls (this happens when we start the server and our most recent change was the latest
			// block in a synced chain), we will need to look at the top of the chain to set up our
			// source block. So let's just always do that upon first sync.
			finalBlock := p.cs.CurrentBlock()
			parentID := finalBlock.ID()
			p.mu.Lock()
			p.newSourceBlock()
			p.sourceBlock.ParentID = parentID
			p.sourceBlock.Timestamp, _ = p.cs.MinimumValidChildTimestamp(parentID)
			p.mu.Unlock()

			p.log.Printf("      Starting Stratum Server\n")

			port := fmt.Sprintf("%d", p.InternalSettings().PoolNetworkPort)
			go p.dispatcher.ListenHandlers(port)
			p.tg.OnStop(func() error {
				if p.dispatcher.ln == nil {
					//panic(errors.New("network not opened yet"))
				} else {
					p.dispatcher.ln.Close()
				}
				return nil
			})
			return
		} else {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// newPool returns an initialized Pool, taking a set of dependencies as input.
// By making the dependencies an argument of the 'new' call, the pool can be
// mocked such that the dependencies can return unexpected errors or unique
// behaviors during testing, enabling easier testing of the failure modes of
// the Pool.
func newPool(dependencies dependencies, cs modules.ConsensusSet, tpool modules.TransactionPool, gw modules.Gateway, wallet modules.Wallet, persistDir string, initConfig config.MiningPoolConfig) (*Pool, error) {
	// Check that all the dependencies were provided.
	if cs == nil {
		return nil, errNilCS
	}
	if tpool == nil {
		return nil, errNilTpool
	}
	if gw == nil {
		return nil, errNilGW
	}

	// Create the pool object.
	p := &Pool{
		cs:           cs,
		tpool:        tpool,
		gw:           gw,
		wallet:       wallet,
		dependencies: dependencies,

		blockMem:   make(map[types.BlockHeader]*types.Block),
		blockTxns:  newTxnList(),
		arbDataMem: make(map[types.BlockHeader][crypto.EntropySize]byte),
		headerMem:  make([]types.BlockHeader, HeaderMemory),

		fullSets:  make(map[modules.TransactionSetID][]int),
		splitSets: make(map[splitSetID]*splitSet),
		blockMapHeap: &mapHeap{
			selectID: make(map[splitSetID]*mapElement),
			data:     nil,
			minHeap:  true,
		},
		overflowMapHeap: &mapHeap{
			selectID: make(map[splitSetID]*mapElement),
			data:     nil,
			minHeap:  false,
		},

		persistDir: persistDir,
		stratumID:  rand.Uint64(),
		clients:    make(map[string]*Client),
	}
	var err error

	// Create the perist directory if it does not yet exist.
	err = dependencies.mkdirAll(p.persistDir, 0700)
	if err != nil {
		return nil, err
	}

	// Initialize the logger, and set up the stop call that will close the
	// logger.
	p.log, err = dependencies.newLogger(filepath.Join(p.persistDir, logFile))
	if err != nil {
		return nil, err
	}
	p.yiilog, err = dependencies.newLogger(filepath.Join(p.persistDir, yiilogFile))
	if err != nil {
		return nil, err
	}
	p.tg.AfterStop(func() error {
		err = p.log.Close()
		if err != nil {
			// State of the logger is uncertain, a Println will have to
			// suffice.
			fmt.Println("Error when closing the logger:", err)
		}
		return err
	})

	// Load the prior persistence structures, and configure the pool to save
	// before shutting down.
	err = p.load()
	if err != nil {
		return nil, err
	}
	p.setPoolSettings(initConfig)

	p.tg.AfterStop(func() error {
		p.mu.Lock()
		err = p.saveSync()
		p.mu.Unlock()
		if err != nil {
			p.log.Println("Could not save pool upon shutdown:", err)
		}
		return err
	})
	dbc := p.InternalSettings().PoolDBConnection

	p.sqldb, err = sql.Open("mysql", dbc)
	if err != nil {
		return nil, errors.New("Failed to open database: " + err.Error())
	}
	err = p.sqldb.Ping()
	if err != nil {
		return nil, errors.New("Failed to ping database: " + err.Error())
	}

	// clean old worker records for this stratum server just in case we didn't
	// shutdown cleanly
	err = p.DeleteAllWorkerRecords()
	if err != nil {
		return nil, errors.New("Failed to clean database: " + err.Error())
	}

	p.tg.OnStop(func() error {
		p.DeleteAllWorkerRecords()
		p.sqldb.Close()
		return nil
	})

	err = p.setBlockCounterFromDB()
	if err != nil {
		return nil, errors.New("Failed to update block count: " + err.Error())
	}

	// grab our consensus set data
	err = p.cs.ConsensusSetSubscribe(p, p.persist.RecentChange, p.tg.StopChan())
	if err == modules.ErrInvalidConsensusChangeID {
		// Perform a rescan of the consensus set if the change id is not found.
		// The id will only be not found if there has been desynchronization
		// between the miner and the consensus package.
		err = p.startupRescan()
		if err != nil {
			return nil, errors.New("mining pool startup failed - rescanning failed: " + err.Error())
		}
	} else if err != nil {
		return nil, errors.New("mining pool subscription failed: " + err.Error())
	}

	// spin up a go routine to handle shift changes.
	go p.monitorShifts()

	p.tg.OnStop(func() error {
		p.cs.Unsubscribe(p)
		return nil
	})

	p.tpool.TransactionPoolSubscribe(p)
	p.tg.OnStop(func() error {
		p.tpool.Unsubscribe(p)
		return nil
	})

	p.runningMutex.Lock()
	p.dispatcher = &Dispatcher{handlers: make(map[string]*Handler), mu: sync.RWMutex{}, p: p}
	p.dispatcher.log, _ = dependencies.newLogger(filepath.Join(p.persistDir, "stratum.log"))
	p.running = true
	p.runningMutex.Unlock()

	go p.startServer()

	return p, nil
}

// New returns an initialized Pool.
func New(cs modules.ConsensusSet, tpool modules.TransactionPool, gw modules.Gateway, wallet modules.Wallet, persistDir string, initConfig config.MiningPoolConfig) (*Pool, error) {
	return newPool(productionDependencies{}, cs, tpool, gw, wallet, persistDir, initConfig)
}

// Close shuts down the pool.
func (p *Pool) Close() error {
	p.log.Println("Closing pool")
	//defer func () {}()
	return p.tg.Stop()
}

// TODO Start and Stop Pool are currently unused, maybe eliminate them
// StartPool starts the pool running
func (p *Pool) StartPool() {
	p.running = true
}

// StopPool stops the pool running
func (p *Pool) StopPool() {
	p.running = false
}

// GetRunning returns the running (or not) status of the pool
func (p *Pool) GetRunning() bool {
	p.runningMutex.Lock()
	defer p.runningMutex.Unlock()
	return p.running
}

// WorkingStatus returns the working state of the pool, where working is
// defined as having received more than workingStatusThreshold settings calls
// over the period of workingStatusFrequency.
func (p *Pool) WorkingStatus() modules.PoolWorkingStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.workingStatus
}

// ConnectabilityStatus returns the connectability state of the pool, whether
// the pool can connect to itself on its configured netaddress.
func (p *Pool) ConnectabilityStatus() modules.PoolConnectabilityStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.connectabilityStatus
}

// MiningMetrics returns information about the financial commitments,
// rewards, and activities of the pool.
func (p *Pool) MiningMetrics() modules.PoolMiningMetrics {
	err := p.tg.Add()
	if err != nil {
		build.Critical("Call to MiningMetrics after close")
	}
	defer p.tg.Done()
	return p.persist.GetMiningMetrics()
}

// SetInternalSettings updates the pool's internal PoolInternalSettings object.
func (p *Pool) SetInternalSettings(settings modules.PoolInternalSettings) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	err := p.tg.Add()
	if err != nil {
		return err
	}
	defer p.tg.Done()

	// The pool should not be open for business if it does not have an
	// unlock hash.
	err = p.checkAddress()
	if err != nil {
		return errors.New("internal settings not updated, no operator wallet set: " + err.Error())
	}

	p.persist.SetSettings(settings)
	p.persist.SetRevisionNumber(p.persist.GetRevisionNumber() + 1)

	err = p.saveSync()
	if err != nil {
		return errors.New("internal settings updated, but failed saving to disk: " + err.Error())
	}
	return nil
}

// InternalSettings returns the settings of a pool.
func (p *Pool) InternalSettings() modules.PoolInternalSettings {
	return p.persist.GetSettings()
}

// checkAddress checks that the miner has an address, fetching an address from
// the wallet if not.
func (p *Pool) checkAddress() error {
	if p.InternalSettings().PoolWallet == (types.UnlockHash{}) {
		return errNoAddressSet
	}
	return nil
}

func (p *Pool) Client(name string) *Client {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.clients[name]
}

func (p *Pool) AddClient(c *Client) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.clients[c.Name()] = c

}

// newStratumID returns a function pointer to a unique ID generator used
// for more the unique IDs within the Stratum protocol
func (p *Pool) newStratumID() (f func() uint64) {
	f = func() uint64 {
		// p.log.Debugf("Waiting to lock pool\n")
		p.mu.Lock()
		defer func() {
			// p.log.Debugf("Unlocking pool\n")
			p.mu.Unlock()
		}()
		atomic.AddUint64(&p.stratumID, 1)
		return p.stratumID
	}
	return
}

func (p *Pool) coinB1() types.Transaction {
	s := fmt.Sprintf("\000     Software: siad-miningpool-module v%d.%02d\nPool name: \"%s\"     \000", MajorVersion, MinorVersion, p.InternalSettings().PoolName)
	if ((len(modules.PrefixNonSia[:]) + len(s)) % 2) != 0 {
		// odd length, add extra null
		s = s + "\000"
	}
	cb := make([]byte, len(modules.PrefixNonSia[:])+len(s)) // represents the bytes appended later
	n := copy(cb, modules.PrefixNonSia[:])
	copy(cb[n:], s)
	return types.Transaction{
		ArbitraryData: [][]byte{cb},
	}
}

func (p *Pool) coinB1Txn() string {
	coinbaseTxn := p.coinB1()
	buf := new(bytes.Buffer)
	coinbaseTxn.MarshalSiaNoSignatures(buf)
	b := buf.Bytes()
	binary.LittleEndian.PutUint64(b[72:87], binary.LittleEndian.Uint64(b[72:87])+8)
	return hex.EncodeToString(b)
}

func (p *Pool) coinB2() string {
	return "0000000000000000"
}

func (p *Pool) NumConnections() int {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()
	if p.running {
		return p.dispatcher.NumConnections()
	} else {
		return 0
	}
}

func (p *Pool) NumConnectionsOpened() uint64 {
	p.runningMutex.RLock()
	defer p.runningMutex.RUnlock()
	if p.running {
		return p.dispatcher.NumConnectionsOpened()
	} else {
		return 0
	}
}

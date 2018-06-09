package index

import (
	"database/sql"
	"errors"
	// "fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/HyperspaceApp/Hyperspace/config"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"
)

const (
	// Names of the various persistent files in the pool.
	logFile = modules.IndexDir + ".log"

	// IndexDuration is the interval time of index scan
	IndexDuration = 10 * time.Minute
)

// Index is the main type of this module
type Index struct {
	currentHeight types.BlockHeight // The current running height

	// Dependencies.
	cs     modules.ConsensusSet
	tpool  modules.TransactionPool
	wallet modules.Wallet
	gw     modules.Gateway

	// Utilities.
	sqldb      *sql.DB
	log        *persist.Logger
	mu         sync.RWMutex
	persistDir string
	tg         siasync.ThreadGroup
}

var (
	// Nil dependency errors.
	errNilCS    = errors.New("index cannot use a nil consensus state")
	errNilTpool = errors.New("index cannot use a nil transaction pool")
	errNilGW    = errors.New("index cannot use a nil gateway")
)

func newIndex(cs modules.ConsensusSet, tpool modules.TransactionPool, gw modules.Gateway, wallet modules.Wallet, persistDir string, initConfig config.IndexConfig) (*Index, error) {
	// Check that all the dependencies were provided.
	if cs == nil {
		return nil, errNilCS
	}
	// if tpool == nil {
	// 	return nil, errNilTpool
	// }
	if gw == nil {
		return nil, errNilGW
	}

	// Create the index object.
	index := &Index{
		cs:            cs,
		tpool:         tpool,
		gw:            gw,
		wallet:        wallet,
		currentHeight: 0,

		persistDir: persistDir,
	}

	var err error
	err = os.MkdirAll(index.persistDir, 0700)
	if err != nil {
		return nil, err
	}

	index.log, err = persist.NewFileLogger(filepath.Join(index.persistDir, logFile))
	if err != nil {
		return nil, err
	}

	// get max height from mysql
	index.sqldb, err = sql.Open("mysql", initConfig.PoolDBConnection)
	if err != nil {
		return nil, errors.New("Failed to open database: " + err.Error())
	}

	err = index.sqldb.Ping()
	if err != nil {
		return nil, errors.New("Failed to ping database: " + err.Error())
	}

	go index.managedScan()

	return index, nil
}

// New returns an initialized Index.
func New(cs modules.ConsensusSet, tpool modules.TransactionPool, gw modules.Gateway, wallet modules.Wallet, persistDir string, initConfig config.IndexConfig) (*Index, error) {
	return newIndex(cs, tpool, gw, wallet, persistDir, initConfig)
}

func (index *Index) managedScan() error {
	index.tg.Add()
	defer index.tg.Done()

	err := index.setCurrentHeightFromDB()
	if err != nil {
		return err
	}

	for {
		select {
		case <-time.After(IndexDuration):
		case <-index.tg.StopChan():
			return nil
		}

		index.log.Debugln("index loop")
		err := index.Scan()
		if err != nil {
			index.log.Debugf("index error: %s", err)
			panic(err)
		}
	}
}

// Scan will go through every block and try to sync every block info
func (index *Index) Scan() error {
	index.log.Printf("Scaning \n")
	var err error

	// index.currentHeight = 0
	// max height index.cs.Height()
	for i := index.currentHeight; i <= index.cs.Height(); i++ {
		index.log.Printf("start to process block: %d\n", i)
		err = index.transBlock(i)
		if err != nil {
			return err
		}
	}

	//
	return nil
}

func (index *Index) setCurrentHeightFromDB() error {
	stmt, err := index.sqldb.Prepare(`SELECT MAX(height) FROM block_meta ORDER BY height DESC;`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	rows, err := stmt.Query()
	if err != nil {
		return err
	}
	defer rows.Close()
	var value types.BlockHeight
	for rows.Next() {
		err = rows.Scan(&value)
		if err != nil {
			value = 0
		}
		break
	}
	err = rows.Err()
	if err != nil {
		return err
	}
	index.mu.Lock()
	index.currentHeight = value
	if index.currentHeight <= 10 {
		index.currentHeight = 0
	} else {
		index.currentHeight = index.currentHeight - 10
	}

	index.mu.Unlock()
	index.log.Printf("setCurrentHeightFromDB, height = %d\n", value)
	return nil
}

func (index *Index) transBlock(h types.BlockHeight) error {
	block, exists := index.cs.BlockAtHeight(h)
	if !exists {
		index.log.Printf("no block found at height %d\n", h)
		return nil
	}
	index.log.Printf("fetched info of height %d, ID:%s\n", h, block.ID())

	err := index.insertBlockMeta(h, block)
	if err != nil {
		return err
	}

	for j, payout := range block.MinerPayouts {
		// scoid := block.MinerPayoutID(uint64(j)).String()
		err = index.insertOutputs(block.MinerPayoutID(uint64(j)), payout, h, "mined", types.TransactionID{})
		if err != nil {
			return err
		}
	}

	for _, txn := range block.Transactions {
		// Add the transaction to the list of active transactions.
		txid := txn.ID()
		err = index.insertTx(h, txid)
		if err != nil {
			return err
		}

		for _, sci := range txn.SiacoinInputs {
			err = index.insertInputs(sci.ParentID, h, txid)
			if err != nil {
				return err
			}
		}

		for j, sco := range txn.SiacoinOutputs {
			err = index.insertOutputs(txn.SiacoinOutputID(uint64(j)), sco, h, "normal", txid)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (index *Index) insertOutputs(scoid types.SiacoinOutputID, output types.SiacoinOutput, h types.BlockHeight, otype string, txid types.TransactionID) error {
	stmt, err := index.sqldb.Prepare("INSERT IGNORE INTO outputs(id,amount,unlockhash,txid,height,type) VALUES(?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(scoid.String(), output.Value.String(), output.UnlockHash.String(), txid.String(), h, otype)
	if err != nil {
		return err
	}

	return nil
}

func (index *Index) insertInputs(scoid types.SiacoinOutputID, h types.BlockHeight, txid types.TransactionID) error {
	stmt, err := index.sqldb.Prepare("INSERT IGNORE INTO inputs(output_id,height,txid) VALUES(?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(scoid.String(), h, txid.String())
	if err != nil {
		return err
	}

	return index.updateOutputSpent(scoid)
}

func (index *Index) updateOutputSpent(scoid types.SiacoinOutputID) error {
	stmt, err := index.sqldb.Prepare("UPDATE outputs SET spent=1 WHERE id=?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(scoid.String())
	if err != nil {
		return err
	}

	return nil
}

func (index *Index) insertTx(h types.BlockHeight, txid types.TransactionID) error {
	stmt, err := index.sqldb.Prepare("INSERT IGNORE INTO transactions(id,height) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(txid.String(), h)
	if err != nil {
		return err
	}

	return nil
}

func (index *Index) insertBlockMeta(h types.BlockHeight, block types.Block) error {
	stmt, err := index.sqldb.Prepare("INSERT IGNORE INTO block_meta(block_id,height) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(block.ID().String(), h)
	if err != nil {
		return err
	}
	return nil
}

// Close shuts down the index.
func (index *Index) Close() error {
	index.sqldb.Close()
	return index.tg.Stop()
}

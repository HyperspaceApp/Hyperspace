package modules

import (
	"time"

	"github.com/HyperspaceApp/Hyperspace/types"
)

const (
	// PoolDir names the directory that contains the pool persistence.
	PoolDir = "miningpool"
)

var (
// Whatever variables we need as we go
)

type (

	// PoolInternalSettings contains a list of settings that can be changed.
	PoolInternalSettings struct {
		PoolNetworkPort  int              `json:"networkport"`
		PoolName         string           `json:"name"`
		PoolID           uint64           `json:"poolid"`
		PoolDBConnection string           `json:"dbconnection"`
		PoolDBName       string           `json:"dbname"`
		PoolWallet       types.UnlockHash `json:"poolwallet"`
	}

	// PoolClient contains summary info for a mining client
	PoolClient struct {
		ClientName  string       `json:"clientname"`
		Balance     string       `json:"balance"`
		BlocksMined uint64       `json:"blocksminer"`
		Workers     []PoolWorker `json:"workers"`
	}

	// PoolClientTransaction represents a mining client transaction
	PoolClientTransaction struct {
		BalanceChange string    `json:"balancechange"`
		TxTime        time.Time `json:"txtime"`
		Memo          string    `json:"memo"`
	}

	// PoolWorker represents a mining client worker
	PoolWorker struct {
		WorkerName             string    `json:"workername"`
		LastShareTime          time.Time `json:"lastsharetime"`
		CurrentDifficulty      float64   `json:"currentdifficulty"`
		CumulativeDifficulty   float64   `json:"cumulativedifficulty"`
		SharesThisBlock        uint64    `json:"sharesthisblock"`
		InvalidSharesThisBlock uint64    `json:"invalidsharesthisblock"`
		StaleSharesThisBlock   uint64    `json:"stalesharesthisblock"`
		BlocksFound            uint64    `json:"blocksfound"`
	}

	// PoolBlock represents a block mined by the pool
	PoolBlock struct {
		BlockNumber uint64    `json:"blocknumber"`
		BlockHeight uint64    `json:"blockheight"`
		BlockReward string    `json:"blockreward"`
		BlockTime   time.Time `json:"blocktime"`
		BlockStatus string    `json:"blockstatus"`
	}

	// PoolBlockClient represents a block mined by the pool
	PoolBlockClient struct {
		ClientName       string  `json:"clientname"`
		ClientPercentage float64 `json:"clientpercentage"`
		ClientReward     string  `json:"clientreward"`
	}

	// PoolWorkingStatus reports the working state of a pool. Can be one of
	// "starting", "accepting", or "not accepting".
	PoolWorkingStatus string

	// PoolConnectabilityStatus reports the connectability state of a pool. Can be
	// one of "checking", "connectable", or "not connectable"
	PoolConnectabilityStatus string

	// A Pool accepts incoming target solutions, tracks the share (an attempted solution),
	// checks to see if we have a new block, and if so, pays all the share submitters,
	// proportionally based on their share of the solution (minus a percentage to the
	// pool operator )
	Pool interface {
		// InternalSettings returns the pool's internal settings, including
		// potentially private or sensitive information.
		InternalSettings() PoolInternalSettings

		// SetInternalSettings sets the parameters of the pool.
		SetInternalSettings(PoolInternalSettings) error

		// Close closes the Pool.
		Close() error

		// Returns the number of open tcp connections the pool currently is servicing
		NumConnections() int

		// Returns the number of open tcp connections the pool has opened since startup
		NumConnectionsOpened() uint64
	}
)

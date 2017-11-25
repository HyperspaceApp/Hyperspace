package modules

import (
	"time"

	"github.com/NebulousLabs/Sia/types"
)

const (
	// PoolDir names the directory that contains the pool persistence.
	PoolDir = "pool"
)

var (
// Whatever variables we need as we go
)

type (
	// PoolMiningMetrics stores the various stats of the pool.
	PoolMiningMetrics struct {
	}

	// PoolInternalSettings contains a list of settings that can be changed.
	PoolInternalSettings struct {
		AcceptingShares        bool             `json:"acceptingshares"`
		PoolOperatorPercentage float64          `json:"operatorpercentage"`
		PoolNetworkPort        uint16           `json:"networkport"`
		PoolName               string           `json:"name"`
		PoolID                 string           `json:"poolid"`
		PoolOperatorWallet     types.UnlockHash `json:"operatorwallet"`
		PoolWallet             types.UnlockHash `json:"poolwallet"`
	}

	PoolClients struct {
		ClientName  string        `json:"clientname"`
		Balance     string        `json:"balance"`
		BlocksMined uint64        `json:"blocksminer"`
		Workers     []PoolWorkers `json:"workers"`
	}

	PoolClientTransactions struct {
		BalanceChange string    `json:"balancechange"`
		TxTime        time.Time `json:"txtime"`
		Memo          string    `json:"memo"`
	}

	PoolWorkers struct {
		WorkerName             string    `json:"workername"`
		LastShareTime          time.Time `json:"lastsharetime"`
		CurrentDifficulty      float64   `json:"currentdifficulty"`
		CumulativeDifficulty   float64   `json:"cumulativedifficulty"`
		SharesThisBlock        uint64    `json:"sharesthisblock"`
		InvalidSharesThisBlock uint64    `json:"invalidsharesthisblock"`
		StaleSharesThisBlock   uint64    `json:"stalesharesthisblock"`
		BlocksFound            uint64    `json:"blocksfound"`
	}

	PoolBlocks struct {
		BlockNumber uint64    `json:"blocknumber"`
		BlockHeight uint64    `json:"blockheight"`
		BlockReward string    `json:"blockreward"`
		BlockTime   time.Time `json:"blocktime"`
		BlockStatus string    `json:"blockstatus"`
	}
	PoolBlock struct {
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
		// PoolMiningMetrics returns the mining statistics of the pool.
		MiningMetrics() PoolMiningMetrics

		// InternalSettings returns the pool's internal settings, including
		// potentially private or sensitive information.
		InternalSettings() PoolInternalSettings

		// SetInternalSettings sets the parameters of the pool.
		SetInternalSettings(PoolInternalSettings) error

		// ClientData returns a pointer to the client list
		ClientData() []PoolClients

		// FindClient returns a Client pointer or nil if the client name doesn't exist
		FindClient(name string) *PoolClients

		ClientTransactions(name string) []PoolClientTransactions

		// BlocksInfo returns a list of blocks information
		BlocksInfo() []PoolBlocks

		// BlockInfo returns a list of blocks information
		BlockInfo(block uint64) []PoolBlock

		// Close closes the Pool.
		Close() error

		// ConnectabilityStatus returns the connectability status of the pool, that
		// is, if it can connect to itself on the configured NetAddress.
		ConnectabilityStatus() PoolConnectabilityStatus

		// WorkingStatus returns the working state of the pool, determined by if
		// settings calls are increasing.
		WorkingStatus() PoolWorkingStatus

		// StartPool turns on the mining pool, which will endlessly work for new
		// blocks.
		StartPool()

		// StopPool turns off the mining pool
		StopPool()

		// GetRunning returns the running status (or not) of the pool
		GetRunning() bool
	}
)

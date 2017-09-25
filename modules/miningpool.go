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
		PoolOperatorPercentage float32          `json:"operatorpercentage"`
		PoolNetworkPort        uint16           `json:"networkport"`
		PoolName               string           `json:"name"`
		PoolOperatorWallet     types.UnlockHash `json:"operatorwallet"`
	}

	PoolClients struct {
		ClientName  string        `json:"clientname"`
		BlocksMined uint64        `json:"blocksminer"`
		Workers     []PoolWorkers `json:"workers"`
	}

	PoolWorkers struct {
		WorkerName               string    `json:"workername"`
		LastShareDuration        float64   `json:"lastshareduration"`
		LastShareTime            time.Time `json:"lastsharetime"`
		SharesThisSession        uint64    `json:"sharesthissession"`
		InvalidSharesThisSession uint64    `json:"invalidsharesthissession"`
		StaleSharesThisSession   uint64    `json:"stalesharesthissession"`
		SharesThisBlock          uint64    `json:"sharesthisblock"`
		InvalidSharesThisBlock   uint64    `json:"invalidsharesthisblock"`
		StaleSharesThisBlock     uint64    `json:"stalesharesthisblock"`
		BlocksFound              uint64    `json:"blocksfound"`
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

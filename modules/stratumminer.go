package modules

import (
	"io"
)

const (
	// StratumMinerDir is the name of the directory that is used to store the miner's
	// persistent data.
	StratumMinerDir = "stratumminer"
)

// StratumMiner provides access to a stratum miner.
type StratumMiner interface {
	// Hashrate returns the hashrate of the stratum miner in hashes per second.
	Hashrate() float64

	// Mining returns true if the stratum miner is enabled, and false otherwise.
	Mining() bool

	// Submissions returns the number of shares the miner has submitted
	Submissions() uint64

	// StartStratumMining turns on the miner, which will endlessly work for new
	// blocks.
	StartStratumMining(server, username string)

	// StopStratumMining turns off the miner
	StopStratumMining()

	io.Closer
}

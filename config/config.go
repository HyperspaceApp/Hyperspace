package config

import "fmt"

const (
	APIPort = 5580
	RPCPort = 5581
	HostPort = 5582
	TestnetAPIPort = 5590
	TestnetRPCPort = 5591
	TestnetHostPort = 5592
)

var (
	DefaultAPIAddr = fmt.Sprintf("localhost:%d", APIPort)
	DefaultRPCAddr = fmt.Sprintf(":%d", RPCPort)
	DefaultHostAddr = fmt.Sprintf(":%d", HostPort)
	TestnetAPIAddr = fmt.Sprintf("localhost:%d", TestnetAPIPort)
	TestnetRPCAddr = fmt.Sprintf(":%d", TestnetRPCPort)
	TestnetHostAddr = fmt.Sprintf(":%d", TestnetHostPort)
)

// MiningPoolConfig is config for miningpool
type MiningPoolConfig struct {
	PoolNetworkPort  int
	PoolName         string
	PoolID           uint64
	PoolDBConnection string
	PoolWallet       string
	Luck             bool
	Difficulty       map[string]string
}

// IndexConfig is config for index
type IndexConfig struct {
	PoolDBConnection string
}

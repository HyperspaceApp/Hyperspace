package config

const (
	APIPort = 5580
	RPCPort = 5581
	HostPort = 5582
	TestnetAPIPort = 5590
	TestnetRPCPort = 5591
	TestnetHostPort = 5592
)

// MiningPoolConfig is config for miningpool
type MiningPoolConfig struct {
	PoolNetworkPort  int
	PoolName         string
	PoolID           uint64
	PoolDBConnection string
	PoolWallet       string
	Luck             bool
}

// IndexConfig is config for index
type IndexConfig struct {
	PoolDBConnection string
}

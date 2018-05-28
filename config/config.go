package config

// MiningPoolConfig is config for miningpool
type MiningPoolConfig struct {
	PoolNetworkPort        int
	PoolName               string
	PoolID                 uint64
	PoolDBConnection       string
	PoolWallet             string
}

// IndexConfig is config for index
type IndexConfig struct {
	PoolDBConnection       string
}

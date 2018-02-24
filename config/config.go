package config

type MiningPoolConfig struct {
	AcceptingShares        bool
	PoolOperatorPercentage float64
	PoolNetworkPort        uint16
	PoolName               string
	PoolID                 string
	PoolDBConnection       string
	PoolDBName             string
	PoolOperatorWallet     string
	PoolWallet             string
}

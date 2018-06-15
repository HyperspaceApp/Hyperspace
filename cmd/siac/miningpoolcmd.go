package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	poolCmd = &cobra.Command{
		Use:   "pool",
		Short: "Perform pool actions",
		Long:  "Perform pool actions and view pool status.",
		Run:   wrap(poolcmd),
	}

	poolConfigCmd = &cobra.Command{
		Use:   "config [setting] [value]",
		Short: "Read/Modify pool settings",
		Long: `Read/Modify pool settings.

Available settings:
	name:               Name you select for your pool
	poolid              Unique string for this pool (needed when sharing database)
    acceptingshares:    Is your pool accepting shares
	networkport:        Stratum port for your pool
	dbconnection:       "internal" or connection string for shared database (pgsql only for now)
    operatorpercentage: What percentage of the block reward goes to the pool operator
	operatorwallet:     Pool operator sia wallet address <required if percentage is not 0>
	poolwallet:         Operating account for the pool <required>
 `,
		Run: wrap(poolconfigcmd),
	}
)

// poolcmd is the handler for the command `siac pool`.
// Prints the status of the pool.
func poolcmd() {
	config, err := httpClient.MiningPoolConfigGet()
	if err != nil {
		die("Could not get pool config:", err)
	}
	fmt.Printf(`Pool status:

Pool config:
Pool Name:              %s
Pool ID:                %d
Pool Stratum Port       %d
DB Connection           %s
Pool Wallet:            %s
`,
		config.Name, config.PoolID, config.NetworkPort,
		config.DBConnection, config.PoolWallet)
}

// poolconfigcmd is the handler for the command `siac pool config [parameter] [value]`
func poolconfigcmd(param, value string) {
	var err error
	switch param {
	case "operatorwallet":
	case "name":
	case "operatorpercentage":
	case "acceptingshares":
	case "networkport":
	case "dbconnection":
	case "poolid":
	case "poolwallet":
	default:
		die("Unknown pool config parameter: ", param)
	}
	err = httpClient.MiningPoolConfigPost(param, value)
	if err != nil {
		die("Could not update pool settings:", err)

	}
}

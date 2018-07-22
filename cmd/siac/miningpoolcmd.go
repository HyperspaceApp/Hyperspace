package main

import (
	"fmt"
	"sort"

	"github.com/NebulousLabs/Sia/node/api"
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
	poolClientsCmd = &cobra.Command{
		Use:   "clients",
		Short: "List clients",
		Long:  "List client overview, use pool client <clientname> for details",
		Run:   wrap(poolclientscmd),
	}

	/*
		poolClientCmd = &cobra.Command{
			Use:   "client <clientname>",
			Short: "Get client details",
			Long:  "Get client details by name",
			Run:   wrap(poolclientcmd),
		}

		poolBlocksCmd = &cobra.Command{
			Use:   "blocks",
			Short: "Get blocks info",
			Long:  "Get list of found blocks",
			Run:   wrap(poolblockscmd),
		}

		poolBlockCmd = &cobra.Command{
			Use:   "block <blocknum>",
			Short: "Get block details",
			Long:  "Get block specific details by block number",
			Run:   wrap(poolblockcmd),
		}
	*/
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

func poolclientscmd() {
	clients, err := httpClient.MiningPoolClientsGet()
	if err != nil {
		die("Could not get pool clients:", err)
	}
	fmt.Printf("Clients List:\n\n")
	fmt.Printf("Number of Clients: %d\nNumber of Workers: %d\n\n", clients.NumberOfClients, clients.NumberOfWorkers)
	fmt.Printf("Client Name                                                                  \n")
	fmt.Printf("---------------------------------------------------------------------------- \n")
	sort.Sort(ByClientName(clients.Clients))
	for _, c := range clients.Clients {
		fmt.Printf("% 76.76s \n", c.ClientName)
	}
}

/*
func poolclientcmd(name string) {
	client, err := httpClient.MiningPoolClientGet(name)
	if err != nil {
		die("Could not get pool client: ", err)
	}
	reward := big.NewInt(0)
	reward.SetString(client.Balance, 10)
	currency := types.NewCurrency(reward)
	fmt.Printf("\nClient Name: % 76.76s\nBlocks Mined: % -10d   Balance: %s\n\n", client.ClientName, client.BlocksMined, currencyUnits(currency))
	fmt.Printf("                    Per Current Block\n")
	fmt.Printf("Worker Name         Work Diff  Shares   Share*Diff   Stale(%%) Invalid(%%)   Blocks Found   Last Share Time\n")
	fmt.Printf("----------------    --------   -------   ---------   --------   --------       --------   ----------------\n")
	sort.Sort(ByWorkerName(client.Workers))
	for _, w := range client.Workers {
		var stale, invalid float64
		if w.SharesThisBlock == 0 {
			stale = 0.0
			invalid = 0.0
		} else {
			stale = float64(w.StaleSharesThisBlock) / float64(w.SharesThisBlock) * 100.0
			invalid = float64(w.InvalidSharesThisBlock) / float64(w.SharesThisBlock) * 100.0
		}
		fmt.Printf("% -16s    % 8.3f  % 8d    % 8d   % 8.3f   % 8.3f       % 8d  %v\n",
			w.WorkerName, w.CurrentDifficulty, w.SharesThisBlock, uint64(w.CumulativeDifficulty),
			stale, invalid, w.BlocksFound, shareTime(w.LastShareTime))
	}

	txs, err := httpClient.MiningPoolTransactionsGet(name)
	if err != nil {
		return
	}
	fmt.Printf("\nTransactions:\n")
	fmt.Printf("%-19s    %-10s   %s\n", "Timestamp", "Change", "Memo")
	fmt.Printf("-------------------    ----------   --------------------------------------------\n")
	for _, t := range txs {
		change := big.NewInt(0)
		change.SetString(t.BalanceChange, 10)
		currency := types.NewCurrency(change)
		fmt.Printf("%-19s    %-10s   %s\n", t.TxTime.Format(time.RFC822), currencyUnits(currency), t.Memo)
	}
}

func shareTime(t time.Time) string {
	if t.IsZero() {
		return " never"
	}

	if time.Now().Sub(t).Seconds() < 1 {
		return " now"
	}

	switch {
	case time.Now().Sub(t).Hours() > 1:
		return fmt.Sprintf(" %s", t.Format(time.RFC822))
	case time.Now().Sub(t).Minutes() > 1:
		return fmt.Sprintf(" %.2f minutes ago", time.Now().Sub(t).Minutes())
	default:
		return fmt.Sprintf(" %.2f seconds ago", time.Now().Sub(t).Seconds())
	}
}

func poolblockscmd() {
	blocks, err := httpClient.MiningPoolBlocksGet()
	if err != nil {
		die("Could not get pool blocks: ", err)
	}
	fmt.Printf("Blocks List:\n")
	fmt.Printf("%-10s %-10s   %-19s   %-10s   %s\n", "Blocks", "Height", "Timestamp", "Reward", "Status")
	fmt.Printf("---------- ----------   -------------------   ------   -------------------\n")
	for _, b := range blocks {
		reward := big.NewInt(0)
		reward.SetString(b.BlockReward, 10)
		currency := types.NewCurrency(reward)
		fmt.Printf("% 10d % 10d   %19s   %-10s   %s\n", b.BlockNumber, b.BlockHeight,
			b.BlockTime.Format(time.RFC822), currencyUnits(currency), b.BlockStatus)
	}
}

func poolblockcmd(name string) {
	blocks, err := httpClient.MiningPoolBlocksGet()
	if err != nil {
		die("Could not get pool blocks: ", err)
	}
	var blockID uint64
	var blocksInfo api.MiningPoolBlocksInfo
	match := false
	fmt.Sscanf(name, "%d", &blockID)
	for _, blocksInfo = range blocks {
		if blocksInfo.BlockNumber == blockID {
			match = true
			break
		}
	}
	block, err := httpClient.MiningPoolBlockGet(name)
	if err != nil {
		die("Could not get pool block:", err)
	}
	if match == false {
		fmt.Printf("Current Block\n\n")
	} else {
		reward := big.NewInt(0)
		reward.SetString(blocksInfo.BlockReward, 10)
		currency := types.NewCurrency(reward)
		fmt.Printf("%-10s %-10s   %-19s   %-10s   %s\n", "Blocks", "Height", "Timestamp", "Reward", "Status")
		fmt.Printf("%-10d %-10d   %-19s   %-10s   %s\n\n", blocksInfo.BlockNumber, blocksInfo.BlockHeight,
			blocksInfo.BlockTime.Format(time.RFC822), currencyUnits(currency), blocksInfo.BlockStatus)
	}

	fmt.Printf("Client Name                                                                   Reward %% Block Reward\n")
	fmt.Printf("----------------------------------------------------------------------------  -------- ------------\n")
	for _, b := range block {
		reward := big.NewInt(0)
		reward.SetString(b.ClientReward, 10)
		currency := types.NewCurrency(reward)
		fmt.Printf("%-76.76s %9.2f %12.12s\n", b.ClientName, b.ClientPercentage, currencyUnits(currency))
	}
}
*/

// ByClientName contains mining pool client info
type ByClientName []api.MiningPoolClientInfo

func (a ByClientName) Len() int           { return len(a) }
func (a ByClientName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByClientName) Less(i, j int) bool { return a[i].ClientName < a[j].ClientName }

// ByWorkerName contains mining pool worker info
type ByWorkerName []api.PoolWorkerInfo

func (a ByWorkerName) Len() int           { return len(a) }
func (a ByWorkerName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByWorkerName) Less(i, j int) bool { return a[i].WorkerName < a[j].WorkerName }

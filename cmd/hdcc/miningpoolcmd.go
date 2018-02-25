package main

import (
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/HyperspaceProject/Hyperspace/node/api"
	"github.com/HyperspaceProject/Hyperspace/types"

	"net/url"

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

	poolStartCmd = &cobra.Command{
		Use:   "start",
		Short: "Start mining pool",
		Long:  "Start mining pool, if the pool is already running, this command does nothing",
		Run:   wrap(poolstartcmd),
	}

	poolStopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop mining pool",
		Long:  "Stop mining pool (this may take a few moments).",
		Run:   wrap(poolstopcmd),
	}

	poolClientsCmd = &cobra.Command{
		Use:   "clients",
		Short: "List clients",
		Long:  "List client overview, use pool client <clientname> for details",
		Run:   wrap(poolclientscmd),
	}

	poolClientCmd = &cobra.Command{
		Use:   "client <clientname>",
		Short: "Get Client details",
		Long:  "Get Client details by name",
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
		Short: "Get Block details",
		Long:  "Get Block specific details by block number",
		Run:   wrap(poolblockcmd),
	}
)

// poolstartcmd is the handler for the command `hdcc pool start`.
// Starts the mining pool.
func poolstartcmd() {
	err := get("/pool/start")
	if err != nil {
		die("Could not start mining pool:", err)
	}
	fmt.Println("Mining pool is now running.")
}

// poolcmd is the handler for the command `hdcc pool`.
// Prints the status of the pool.
func poolcmd() {
	status := new(api.PoolGET)
	err := getAPI("/pool", status)
	if err != nil {
		die("Could not get pool status:", err)
	}
	config := new(api.PoolConfig)
	err = getAPI("/pool/config", config)
	if err != nil {
		die("Could not get pool config:", err)
	}
	poolStr := "off"
	if status.PoolRunning {
		poolStr = "on"
	}
	fmt.Printf(`Pool status:
Mining Pool:   %s
Pool Hashrate: %v GH/s
Blocks Mined: %d

Pool config:
Pool Name:              %s
Pool ID:                %s
Pool Accepting Shares   %t
Pool Stratum Port       %d
DB Connection           %s
Operator Percentage     %.02f %%
Operator Wallet:        %s
Pool Wallet:            %s
`,
		poolStr, status.PoolHashrate/1000000000, status.BlocksMined,
		config.Name, config.PoolID, config.AcceptingShares, config.NetworkPort,
		config.DBConnection,
		config.OperatorPercentage, config.OperatorWallet, config.PoolWallet)
}

// poolstopcmd is the handler for the command `hdcc pool stop`.
// Stops the CPU miner.
func poolstopcmd() {
	err := get("/pool/stop")
	if err != nil {
		die("Could not stop pool:", err)
	}
	fmt.Println("Stopped mining pool.")
}

// poolconfigcmd is the handler for the command `hdcc pool config [parameter] [value]`
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
	err = post("/pool/config", param+"="+url.PathEscape(value))
	if err != nil {
		die("Could not update pool settings:", err)

	}
}

func poolclientscmd() {
	clients := new(api.PoolClientsInfo)
	err := getAPI("/pool/clients", clients)
	if err != nil {
		die("Could not get pool clients:", err)
	}
	fmt.Printf("Clients List:\n\n")
	fmt.Printf("Number of Clients: %d\nNumber of Workers: %d\n\n", clients.NumberOfClients, clients.NumberOfWorkers)
	fmt.Printf("Client Name                                                                   Blocks Mined Last Share Submitted\n")
	fmt.Printf("----------------------------------------------------------------------------  ------------ --------------------\n")
	sort.Sort(ByClientName(clients.Clients))
	for _, c := range clients.Clients {
		var latest time.Time
		for _, w := range c.Workers {
			if w.LastShareTime.Unix() > latest.Unix() {
				latest = w.LastShareTime
			}
		}
		fmt.Printf("% 76.76s  %12d%v\n", c.ClientName, c.BlocksMined, shareTime(latest))
	}
}

func poolclientcmd(name string) {
	client := new(api.PoolClientInfo)
	err := getAPI("/pool/client?name="+name, client)
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

	txs := new([]api.PoolClientTransactions)
	err = getAPI("/pool/clienttx?name="+name, txs)
	if err != nil {
		return
	}
	fmt.Printf("\nTransactions:\n")
	fmt.Printf("%-19s    %-10s   %s\n", "Timestamp", "Change", "Memo")
	fmt.Printf("-------------------    ----------   --------------------------------------------\n")
	for _, t := range *txs {
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
	blocks := new([]api.PoolBlocksInfo)
	err := getAPI("/pool/blocks", blocks)
	if err != nil {
		die("Could not get pool blocks: ", err)
	}
	fmt.Printf("Blocks List:\n")
	fmt.Printf("%-10s %-10s   %-19s   %-10s   %s\n", "Blocks", "Height", "Timestamp", "Reward", "Status")
	fmt.Printf("---------- ----------   -------------------   ------   -------------------\n")
	for _, b := range *blocks {
		reward := big.NewInt(0)
		reward.SetString(b.BlockReward, 10)
		currency := types.NewCurrency(reward)
		fmt.Printf("% 10d % 10d   %19s   %-10s   %s\n", b.BlockNumber, b.BlockHeight,
			b.BlockTime.Format(time.RFC822), currencyUnits(currency), b.BlockStatus)
	}
}

func poolblockcmd(name string) {
	blocks := new([]api.PoolBlocksInfo)
	err := getAPI("/pool/blocks", blocks)
	if err != nil {
		die("Could not get pool blocks: ", err)
	}
	var blockID uint64
	var blocksInfo api.PoolBlocksInfo
	match := false
	fmt.Sscanf(name, "%d", &blockID)
	for _, blocksInfo = range *blocks {
		if blocksInfo.BlockNumber == blockID {
			match = true
			break
		}
	}
	block := new([]api.PoolBlockInfo)
	err = getAPI("/pool/block?block="+name, block)
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
	for _, b := range *block {
		reward := big.NewInt(0)
		reward.SetString(b.ClientReward, 10)
		currency := types.NewCurrency(reward)
		fmt.Printf("%-76.76s %9.2f %12.12s\n", b.ClientName, b.ClientPercentage, currencyUnits(currency))
	}
}

type ByClientName []api.PoolClientInfo

func (a ByClientName) Len() int           { return len(a) }
func (a ByClientName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByClientName) Less(i, j int) bool { return a[i].ClientName < a[j].ClientName }

type ByWorkerName []api.PoolWorkerInfo

func (a ByWorkerName) Len() int           { return len(a) }
func (a ByWorkerName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByWorkerName) Less(i, j int) bool { return a[i].WorkerName < a[j].WorkerName }

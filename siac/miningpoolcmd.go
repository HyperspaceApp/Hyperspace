package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/NebulousLabs/Sia/api"

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
    acceptingshares:    Is your pool accepting shares
    networkport:        Stratum port for your pool
    operatorpercentage: What percentage of the block reward goes to the pool operator
    operatorwallet:     Pool operator sia wallet address
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
)

// poolstartcmd is the handler for the command `siac pool start`.
// Starts the mining pool.
func poolstartcmd() {
	err := get("/pool/start")
	if err != nil {
		die("Could not start mining pool:", err)
	}
	fmt.Println("Mining pool is now running.")
}

// poolcmd is the handler for the command `siac pool`.
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
Pool Accepting Shares   %t
Pool Stratum Port       %d
Operator Percentage     %.02f %%
Operator Wallet:        %s
`,
		poolStr, status.PoolHashrate/1000000000, status.BlocksMined,
		config.Name, config.AcceptingShares, config.NetworkPort, config.OperatorPercentage, config.OperatorWallet)
}

// poolstopcmd is the handler for the command `siac pool stop`.
// Stops the CPU miner.
func poolstopcmd() {
	err := get("/pool/stop")
	if err != nil {
		die("Could not stop pool:", err)
	}
	fmt.Println("Stopped mining pool.")
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
	fmt.Printf("                         Client Name                                         Blocks Mined\n")
	sort.Sort(ByClientName(clients.Clients))
	for _, c := range clients.Clients {
		fmt.Printf("% 76.76s %d\n", c.ClientName, c.BlocksMined)
		fmt.Printf("     Worker Name      Last Share Time\n")
		for _, w := range c.Workers {
			fmt.Printf(" % -16s     %v\n", w.WorkerName, shareTime(w.LastShareTime))
		}
	}
}

func poolclientcmd(name string) {
	client := new(api.PoolClientInfo)
	err := getAPI("/pool/client?name="+name, client)
	if err != nil {
		die("Could not get pool client:", err)
	}
	fmt.Printf("\nClient Name: % 76.76s\nBlocks Mined: %d\n\n", client.ClientName, client.BlocksMined)
	fmt.Printf("                    Per Current Block\n")
	fmt.Println("Worker Name         Work Diff  Shares    Cumm Diff   Stale(%) Invalid(%)   Blocks Found   Last Share Time")
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
		return fmt.Sprintf(" %v", t)
	case time.Now().Sub(t).Minutes() > 1:
		return fmt.Sprintf(" %.2f minutes ago", time.Now().Sub(t).Minutes())
	default:
		return fmt.Sprintf(" %.2f seconds ago", time.Now().Sub(t).Seconds())
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

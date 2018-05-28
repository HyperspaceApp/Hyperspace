package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	stratumminerCmd = &cobra.Command{
		Use:   "stratum-miner",
		Short: "Perform stratum miner actions",
		Long:  "Perform stratum miner actions and view miner status.",
		Run:   wrap(stratumminercmd),
	}
	stratumminerStartCmd = &cobra.Command{
		Use:   "start [server] [username]",
		Short: "Start stratum mining to server using username",
		Long:  "Start stratum mining to server using username, if the miner is already running, this command does nothing",
		Run:   stratumminerstartcmd,
	}

	stratumminerStopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop stratum mining",
		Long:  "Stop stratum mining (this may take a few moments).",
		Run:   wrap(stratumminerstopcmd),
	}
)

// minerstartcmd is the handler for the command `siac stratum-miner start`.
// Starts the stratum miner.
func stratumminerstartcmd(cmd *cobra.Command, args []string) {
	var err error
	switch len(args) {
	case 2:
		err = httpClient.StratumMinerStartPost(args[0], args[1])
	default:
		cmd.UsageFunc()(cmd)
		os.Exit(exitCodeUsage)
	}
	if err != nil {
		die("Could not start stratum miner:", err)
	}
	fmt.Println("Stratum miner is now running.")
}

// stratumminerstopcmd is the handler for the command `siac stratum-miner stop`.
// Stops the stratum miner.
func stratumminerstopcmd() {
	err := httpClient.StratumMinerStopPost()
	if err != nil {
		die("Could not stop stratum miner:", err)
	}
	fmt.Println("Stopped mining.")
}

// stratumminercmd is the handler for the command `siac stratum-miner`.
// Prints the status of the stratum miner.
func stratumminercmd() {
	status, err := httpClient.StratumMinerGet()
	if err != nil {
		die("Could not get stratum miner status:", err)
	}

	miningStr := "off"
	if status.Mining {
		miningStr = "on"
	}
	fmt.Printf(`Miner status:
Mining:      %s
Hashrate:    %v KH/s
Submissions: %v
`, miningStr, status.Hashrate/1000, status.Submissions)
}

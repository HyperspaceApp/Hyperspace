package main

import (
	"fmt"

	"github.com/HyperspaceProject/Hyperspace/build"
	"github.com/spf13/cobra"
)

var (
	stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the Hard Drive Coin daemon",
		Long:  "Stop the Hard Drive Coin daemon.",
		Run:   wrap(stopcmd),
	}

	updateCheckCmd = &cobra.Command{
		Use:   "check",
		Short: "Check for available updates",
		Long:  "Check for available updates.",
		Run:   wrap(updatecheckcmd),
	}

	updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Update Hard Drive Coin",
		Long:  "Check for (and/or download) available updates for Hard Drive Coin.",
		Run:   wrap(updatecmd),
	}

	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print version information.",
		Run:   wrap(versioncmd),
	}
)

type updateInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

type daemonVersion struct {
	Version string
}

// version prints the version of hdcc and hdcd.
func versioncmd() {
	fmt.Println("Hard Drive Coin Client v" + build.Version)
	var versioninfo daemonVersion
	err := getAPI("/daemon/version", &versioninfo)
	if err != nil {
		fmt.Println("Could not get daemon version:", err)
		return
	}
	fmt.Println("Hard Drive Coin Daemon v" + versioninfo.Version)
}

// stopcmd is the handler for the command `hdcc stop`.
// Stops the daemon.
func stopcmd() {
	err := get("/daemon/stop")
	if err != nil {
		die("Could not stop daemon:", err)
	}
	fmt.Println("Hard Drive Coin daemon stopped.")
}

func updatecmd() {
	var update updateInfo
	err := getAPI("/daemon/update", &update)
	if err != nil {
		fmt.Println("Could not check for update:", err)
		return
	}
	if !update.Available {
		fmt.Println("Already up to date.")
		return
	}

	err = post("/daemon/update", "")
	if err != nil {
		fmt.Println("Could not apply update:", err)
		return
	}
	fmt.Printf("Updated to version %s! Restart hdcd now.\n", update.Version)
}

func updatecheckcmd() {
	var update updateInfo
	err := getAPI("/daemon/update", &update)
	if err != nil {
		fmt.Println("Could not check for update:", err)
		return
	}
	if update.Available {
		fmt.Printf("A new release (v%s) is available! Run 'hdcc update' to install it.\n", update.Version)
	} else {
		fmt.Println("Up to date.")
	}
}

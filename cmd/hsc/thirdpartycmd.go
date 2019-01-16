package main

// TODO: If you run siac from a non-existent directory, the abs() function does
// not handle this very gracefully.

import (
	"fmt"
	"math/big"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"
)

var (
	thirdpartyCmd = &cobra.Command{
		Use:   "thirdparty",
		Short: "Perform thirdparty actions",
		Long:  "contracts, contracts-view, hotdb, hostdb-view.",
		Run:   wrap(thirdpartycmd),
	}

	thirdpartyContractsCmd = &cobra.Command{
		Use:   "contracts",
		Short: "View the Thirdpary's contracts",
		Long:  "View the contracts that the Thirdpary has formed with hosts.",
		Run:   wrap(thirdpartycontractscmd),
	}

	thirdpartyContractsViewCmd = &cobra.Command{
		Use:   "view [contract-id]",
		Short: "View details of the specified contract",
		Long:  "View all details available of the specified contract.",
		Run:   wrap(rentercontractsviewcmd),
	}

	thirdpartyHostdbCmd = &cobra.Command{
		Use:   "hostdb",
		Short: "Interact with the renter's host database.",
		Long:  "View the list of active hosts, the list of all hosts, or query specific hosts.\nIf the '-v' flag is set, a list of recent scans will be provided, with the most\nrecent scan on the right. a '0' indicates that the host was offline, and a '1'\nindicates that the host was online.",
		Run:   wrap(thirdpartyhostdbcmd),
	}

	thirdpartyHostdbViewCmd = &cobra.Command{
		Use:   "view [pubkey]",
		Short: "View the full information for a host.",
		Long:  "View detailed information about a host, including things like a score breakdown.",
		Run:   wrap(thirdpartyhostdbviewcmd),
	}
)

// rentercmd displays the renter's financial metrics and lists the files it is
// tracking.
func thirdpartycmd() {
	fmt.Printf(`Thirdparty commands:
	contracts, contracts view, hotdb, hostdb view.`)
}

// rentercontractscmd is the handler for the comand `hsc renter contracts`.
// It lists the Renter's contracts.
func thirdpartycontractscmd() {
	rc, err := httpClient.ThirdpartyInactiveContractsGet()
	if err != nil {
		die("Could not get contracts:", err)
	}

	fmt.Println("Active Contracts:")
	if len(rc.ActiveContracts) == 0 {
		fmt.Println("  No active contracts.")
	} else {
		// Display Active Contracts
		sort.Sort(byValue(rc.ActiveContracts))
		var activeTotalStored uint64
		var activeTotalRemaining, activeTotalSpent, activeTotalFees types.Currency
		for _, c := range rc.ActiveContracts {
			activeTotalStored += c.Size
			activeTotalRemaining = activeTotalRemaining.Add(c.RenterFunds)
			activeTotalSpent = activeTotalSpent.Add(c.TotalCost.Sub(c.RenterFunds).Sub(c.Fees))
			activeTotalFees = activeTotalFees.Add(c.Fees)
		}
		fmt.Printf(`  Number of Contracts:  %v
  Total stored:         %s
  Total Remaining:      %v
  Total Spent:          %v
  Total Fees:           %v

`, len(rc.ActiveContracts), filesizeUnits(int64(activeTotalStored)),
			currencyUnits(activeTotalRemaining), currencyUnits(activeTotalSpent), currencyUnits(activeTotalFees))
		w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  Host\tRemaining Funds\tSpent Funds\tSpent Fees\tData\tEnd Height\tID\tGoodForUpload\tGoodForRenew")
		for _, c := range rc.ActiveContracts {
			address := c.NetAddress
			if address == "" {
				address = "Host Removed"
			}
			fmt.Fprintf(w, "  %v\t%8s\t%8s\t%8s\t%v\t%v\t%v\t%v\t%v\n",
				address,
				currencyUnits(c.RenterFunds),
				currencyUnits(c.TotalCost.Sub(c.RenterFunds).Sub(c.Fees)),
				currencyUnits(c.Fees),
				filesizeUnits(int64(c.Size)),
				c.EndHeight,
				c.ID,
				c.GoodForUpload,
				c.GoodForRenew)
		}
		w.Flush()
	}

	fmt.Println("\nInactive Contracts:")
	if len(rc.InactiveContracts) == 0 {
		fmt.Println("  No inactive contracts.")
	} else {
		// Display Inactive Contracts
		sort.Sort(byValue(rc.InactiveContracts))
		var inactiveTotalStored uint64
		var inactiveTotalRemaining, inactiveTotalSpent, inactiveTotalFees types.Currency
		for _, c := range rc.InactiveContracts {
			inactiveTotalStored += c.Size
			inactiveTotalRemaining = inactiveTotalRemaining.Add(c.RenterFunds)
			inactiveTotalSpent = inactiveTotalSpent.Add(c.TotalCost.Sub(c.RenterFunds).Sub(c.Fees))
			inactiveTotalFees = inactiveTotalFees.Add(c.Fees)
		}

		fmt.Printf(`
  Number of Contracts:  %v
  Total stored:         %s
  Total Remaining:      %v
  Total Spent:          %v
  Total Fees:           %v

`, len(rc.InactiveContracts), filesizeUnits(int64(inactiveTotalStored)), currencyUnits(inactiveTotalRemaining), currencyUnits(inactiveTotalSpent), currencyUnits(inactiveTotalFees))
		w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  Host\tRemaining Funds\tSpent Funds\tSpent Fees\tData\tEnd Height\tID\tGoodForUpload\tGoodForRenew")
		for _, c := range rc.InactiveContracts {
			address := c.NetAddress
			if address == "" {
				address = "Host Removed"
			}
			fmt.Fprintf(w, "  %v\t%8s\t%8s\t%8s\t%v\t%v\t%v\t%v\t%v\n",
				address,
				currencyUnits(c.RenterFunds),
				currencyUnits(c.TotalCost.Sub(c.RenterFunds).Sub(c.Fees)),
				currencyUnits(c.Fees),
				filesizeUnits(int64(c.Size)),
				c.EndHeight,
				c.ID,
				c.GoodForUpload,
				c.GoodForRenew)
		}
		w.Flush()
	}

	if renterAllContracts {
		fmt.Println("\nExpired Contracts:")
		rce, err := httpClient.RenterExpiredContractsGet()
		if err != nil {
			die("Could not get expired contracts:", err)
		}
		if len(rce.ExpiredContracts) == 0 {
			fmt.Println("  No expired contracts.")
		} else {
			sort.Sort(byValue(rce.ExpiredContracts))
			var expiredTotalStored uint64
			var expiredTotalWithheld, expiredTotalSpent, expiredTotalFees types.Currency
			for _, c := range rce.ExpiredContracts {
				expiredTotalStored += c.Size
				expiredTotalWithheld = expiredTotalWithheld.Add(c.RenterFunds)
				expiredTotalSpent = expiredTotalSpent.Add(c.TotalCost.Sub(c.RenterFunds).Sub(c.Fees))
				expiredTotalFees = expiredTotalFees.Add(c.Fees)
			}
			fmt.Printf(`
	Number of Contracts:  %v
	Total stored:         %9s
	Total Remaining:      %v
	Total Spent:          %v
	Total Fees:           %v

	`, len(rce.ExpiredContracts), filesizeUnits(int64(expiredTotalStored)), currencyUnits(expiredTotalWithheld), currencyUnits(expiredTotalSpent), currencyUnits(expiredTotalFees))
			w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
			fmt.Fprintln(w, "  Host\tWithheld Funds\tSpent Funds\tSpent Fees\tData\tEnd Height\tID\tGoodForUpload\tGoodForRenew")
			for _, c := range rce.ExpiredContracts {
				address := c.NetAddress
				if address == "" {
					address = "Host Removed"
				}
				fmt.Fprintf(w, "  %v\t%8s\t%8s\t%8s\t%v\t%v\t%v\t%v\t%v\n",
					address,
					currencyUnits(c.RenterFunds),
					currencyUnits(c.TotalCost.Sub(c.RenterFunds).Sub(c.Fees)),
					currencyUnits(c.Fees),
					filesizeUnits(int64(c.Size)),
					c.EndHeight,
					c.ID,
					c.GoodForUpload,
					c.GoodForRenew)
			}
			w.Flush()
		}
	}
}

// thirdpartycontractsviewcmd is the handler for the command `hsc thirdparty contracts <id>`.
// It lists details of a specific contract.
func thirdpartycontractsviewcmd(cid string) {
	rc, err := httpClient.ThirdpartyInactiveContractsGet()
	if err != nil {
		die("Could not get contract details: ", err)
	}
	rce, err := httpClient.ThirdpartyExpiredContractsGet()
	if err != nil {
		die("Could not get expired contract details: ", err)
	}

	contracts := append(rc.ActiveContracts, rc.InactiveContracts...)
	contracts = append(contracts, rce.ExpiredContracts...)

	for _, rc := range contracts {
		if rc.ID.String() == cid {
			hostInfo, err := httpClient.ThirdpartyHostDbHostsGet(rc.HostPublicKey)
			if err != nil {
				die("Could not fetch details of host: ", err)
			}
			fmt.Printf(`
Contract %v
  Host: %v (Public Key: %v)

  Start Height: %v
  End Height:   %v

  Total cost:        %v (Fees: %v)
  Funds Allocated:   %v
  Upload Spending:   %v
  Storage Spending:  %v
  Download Spending: %v
  Remaining Funds:   %v

  File Size: %v
`, rc.ID, rc.NetAddress, rc.HostPublicKey.String(), rc.StartHeight, rc.EndHeight,
				currencyUnits(rc.TotalCost),
				currencyUnits(rc.Fees),
				currencyUnits(rc.TotalCost.Sub(rc.Fees)),
				currencyUnits(rc.UploadSpending),
				currencyUnits(rc.StorageSpending),
				currencyUnits(rc.DownloadSpending),
				currencyUnits(rc.RenterFunds),
				filesizeUnits(int64(rc.Size)))

			printScoreBreakdown(&hostInfo)
			return
		}
	}

	fmt.Println("Contract not found")
}

func thirdpartyhostdbcmd() {
	if !hostdbVerbose {
		info, err := httpClient.ThirdpartyHostDbActiveGet()
		if err != nil {
			die("Could not fetch host list:", err)
		}
		if len(info.Hosts) == 0 {
			fmt.Println("No known active hosts")
			return
		}

		// Strip down to the number of requested hosts.
		if hostdbNumHosts != 0 && hostdbNumHosts < len(info.Hosts) {
			info.Hosts = info.Hosts[len(info.Hosts)-hostdbNumHosts:]
		}

		fmt.Println(len(info.Hosts), "Active Hosts:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\t\tAddress\tPrice (per TB per Mo)")
		for i, host := range info.Hosts {
			price := host.StoragePrice.Mul(modules.BlockBytesPerMonthTerabyte)
			fmt.Fprintf(w, "\t%v:\t%v\t%v\n", len(info.Hosts)-i, host.NetAddress, currencyUnits(price))
		}
		w.Flush()
	} else {
		info, err := httpClient.ThirdpartyHostDbAllGet()
		if err != nil {
			die("Could not fetch host list:", err)
		}
		if len(info.Hosts) == 0 {
			fmt.Println("No known hosts")
			return
		}

		// Iterate through the hosts and divide by category.
		var activeHosts, inactiveHosts, offlineHosts []api.ExtendedHostDBEntry
		for _, host := range info.Hosts {
			if host.AcceptingContracts && len(host.ScanHistory) > 0 && host.ScanHistory[len(host.ScanHistory)-1].Success {
				activeHosts = append(activeHosts, host)
				continue
			}
			if len(host.ScanHistory) > 0 && host.ScanHistory[len(host.ScanHistory)-1].Success {
				inactiveHosts = append(inactiveHosts, host)
				continue
			}
			offlineHosts = append(offlineHosts, host)
		}

		if hostdbNumHosts > 0 && len(offlineHosts) > hostdbNumHosts {
			offlineHosts = offlineHosts[len(offlineHosts)-hostdbNumHosts:]
		}
		if hostdbNumHosts > 0 && len(inactiveHosts) > hostdbNumHosts {
			inactiveHosts = inactiveHosts[len(inactiveHosts)-hostdbNumHosts:]
		}
		if hostdbNumHosts > 0 && len(activeHosts) > hostdbNumHosts {
			activeHosts = activeHosts[len(activeHosts)-hostdbNumHosts:]
		}

		fmt.Println()
		fmt.Println(len(offlineHosts), "Offline Hosts:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\t\tPubkey\tAddress\tPrice (/ TB / Month)\tDownload Price (/ TB)\tUptime\tRecent Scans")
		for i, host := range offlineHosts {
			// Compute the total measured uptime and total measured downtime for this
			// host.
			uptimeRatio := float64(0)
			if len(host.ScanHistory) > 1 {
				downtime := host.HistoricDowntime
				uptime := host.HistoricUptime
				recentTime := host.ScanHistory[0].Timestamp
				recentSuccess := host.ScanHistory[0].Success
				for _, scan := range host.ScanHistory[1:] {
					if recentSuccess {
						uptime += scan.Timestamp.Sub(recentTime)
					} else {
						downtime += scan.Timestamp.Sub(recentTime)
					}
					recentTime = scan.Timestamp
					recentSuccess = scan.Success
				}
				uptimeRatio = float64(uptime) / float64(uptime+downtime)
			}

			// Get the scan history string.
			scanHistStr := ""
			displayScans := host.ScanHistory
			if len(host.ScanHistory) > scanHistoryLen {
				displayScans = host.ScanHistory[len(host.ScanHistory)-scanHistoryLen:]
			}
			for _, scan := range displayScans {
				if scan.Success {
					scanHistStr += "1"
				} else {
					scanHistStr += "0"
				}
			}

			// Get a string representation of the historic outcomes of the most
			// recent scans.
			price := host.StoragePrice.Mul(modules.BlockBytesPerMonthTerabyte)
			downloadBWPrice := host.StoragePrice.Mul(modules.BytesPerTerabyte)
			fmt.Fprintf(w, "\t%v:\t%v\t%v\t%v\t%v\t%.3f\t%s\n", len(offlineHosts)-i, host.PublicKeyString,
				host.NetAddress, currencyUnits(price), currencyUnits(downloadBWPrice), uptimeRatio, scanHistStr)
		}
		w.Flush()

		fmt.Println()
		fmt.Println(len(inactiveHosts), "Inactive Hosts:")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\t\tPubkey\tAddress\tPrice (/ TB / Month)\tCollateral (/ TB / Month)\tDownload Price (/ TB)\tUptime\tRecent Scans")
		for i, host := range inactiveHosts {
			// Compute the total measured uptime and total measured downtime for this
			// host.
			uptimeRatio := float64(0)
			if len(host.ScanHistory) > 1 {
				downtime := host.HistoricDowntime
				uptime := host.HistoricUptime
				recentTime := host.ScanHistory[0].Timestamp
				recentSuccess := host.ScanHistory[0].Success
				for _, scan := range host.ScanHistory[1:] {
					if recentSuccess {
						uptime += scan.Timestamp.Sub(recentTime)
					} else {
						downtime += scan.Timestamp.Sub(recentTime)
					}
					recentTime = scan.Timestamp
					recentSuccess = scan.Success
				}
				uptimeRatio = float64(uptime) / float64(uptime+downtime)
			}

			// Get a string representation of the historic outcomes of the most
			// recent scans.
			scanHistStr := ""
			displayScans := host.ScanHistory
			if len(host.ScanHistory) > scanHistoryLen {
				displayScans = host.ScanHistory[len(host.ScanHistory)-scanHistoryLen:]
			}
			for _, scan := range displayScans {
				if scan.Success {
					scanHistStr += "1"
				} else {
					scanHistStr += "0"
				}
			}

			price := host.StoragePrice.Mul(modules.BlockBytesPerMonthTerabyte)
			collateral := host.Collateral.Mul(modules.BlockBytesPerMonthTerabyte)
			downloadBWPrice := host.DownloadBandwidthPrice.Mul(modules.BytesPerTerabyte)
			fmt.Fprintf(w, "\t%v:\t%v\t%v\t%v\t%v\t%v\t%.3f\t%s\n", len(inactiveHosts)-i, host.PublicKeyString, host.NetAddress, currencyUnits(price), currencyUnits(collateral), currencyUnits(downloadBWPrice), uptimeRatio, scanHistStr)
		}
		fmt.Fprintln(w, "\t\tPubkey\tAddress\tPrice (/ TB / Month)\tCollateral (/ TB / Month)\tDownload Price (/ TB)\tUptime\tRecent Scans")
		w.Flush()

		// Grab the host at the 3/5th point and use it as the reference. (it's
		// like using the median, except at the 3/5th point instead of the 1/2
		// point.)
		referenceScore := big.NewRat(1, 1)
		if len(activeHosts) > 0 {
			referenceIndex := len(activeHosts) * 3 / 5
			hostInfo, err := httpClient.ThirdpartyHostDbHostsGet(activeHosts[referenceIndex].PublicKey)
			if err != nil {
				die("Could not fetch provided host:", err)
			}
			if !hostInfo.ScoreBreakdown.Score.IsZero() {
				referenceScore = new(big.Rat).Inv(new(big.Rat).SetInt(hostInfo.ScoreBreakdown.Score.Big()))
			}
		}

		fmt.Println()
		fmt.Println(len(activeHosts), "Active Hosts:")
		w = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "\t\tPubkey\tAddress\tScore\tContract Fee\tPrice (/ TB / Month)\tCollateral (/ TB / Month)\tDownload Price (/TB)\tUptime\tRecent Scans")
		for i, host := range activeHosts {
			// Compute the total measured uptime and total measured downtime for this
			// host.
			uptimeRatio := float64(0)
			if len(host.ScanHistory) > 1 {
				downtime := host.HistoricDowntime
				uptime := host.HistoricUptime
				recentTime := host.ScanHistory[0].Timestamp
				recentSuccess := host.ScanHistory[0].Success
				for _, scan := range host.ScanHistory[1:] {
					if recentSuccess {
						uptime += scan.Timestamp.Sub(recentTime)
					} else {
						downtime += scan.Timestamp.Sub(recentTime)
					}
					recentTime = scan.Timestamp
					recentSuccess = scan.Success
				}
				uptimeRatio = float64(uptime) / float64(uptime+downtime)
			}

			// Get a string representation of the historic outcomes of the most
			// recent scans.
			scanHistStr := ""
			displayScans := host.ScanHistory
			if len(host.ScanHistory) > scanHistoryLen {
				displayScans = host.ScanHistory[len(host.ScanHistory)-scanHistoryLen:]
			}
			for _, scan := range displayScans {
				if scan.Success {
					scanHistStr += "1"
				} else {
					scanHistStr += "0"
				}
			}

			// Grab the score information for the active hosts.
			hostInfo, err := httpClient.ThirdpartyHostDbHostsGet(host.PublicKey)
			if err != nil {
				die("Could not fetch provided host:", err)
			}
			score, _ := new(big.Rat).Mul(referenceScore, new(big.Rat).SetInt(hostInfo.ScoreBreakdown.Score.Big())).Float64()

			price := host.StoragePrice.Mul(modules.BlockBytesPerMonthTerabyte)
			collateral := host.Collateral.Mul(modules.BlockBytesPerMonthTerabyte)
			downloadBWPrice := host.DownloadBandwidthPrice.Mul(modules.BytesPerTerabyte)
			fmt.Fprintf(w, "\t%v:\t%v\t%v\t%12.6g\t%v\t%v\t%v\t%v\t%.3f\t%s\n", len(activeHosts)-i, host.PublicKeyString, host.NetAddress, score, currencyUnits(host.ContractPrice), currencyUnits(price), currencyUnits(collateral), currencyUnits(downloadBWPrice), uptimeRatio, scanHistStr)
		}
		fmt.Fprintln(w, "\t\tPubkey\tAddress\tScore\tContract Fee\tPrice (/ TB / Month)\tCollateral (/ TB / Month)\tDownload Price (/TB)\tUptime\tRecent Scans")
		w.Flush()
	}
}

func thirdpartyhostdbviewcmd(pubkey string) {
	var publicKey types.SiaPublicKey
	publicKey.LoadString(pubkey)
	info, err := httpClient.ThirdpartyHostDbHostsGet(publicKey)
	if err != nil {
		die("Could not fetch provided host:", err)
	}

	fmt.Println("Host information:")

	fmt.Println("  Public Key:      ", info.Entry.PublicKeyString)
	fmt.Println("  Block First Seen:", info.Entry.FirstSeen)
	fmt.Println("  Absolute Score:  ", info.ScoreBreakdown.Score)

	fmt.Println("\n  Host Settings:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\t\tAccepting Contracts:\t", info.Entry.AcceptingContracts)
	fmt.Fprintln(w, "\t\tTotal Storage:\t", info.Entry.TotalStorage/1e9, "GB")
	fmt.Fprintln(w, "\t\tRemaining Storage:\t", info.Entry.RemainingStorage/1e9, "GB")
	fmt.Fprintln(w, "\t\tOffered Collateral (TB / Mo):\t", currencyUnits(info.Entry.Collateral.Mul(modules.BlockBytesPerMonthTerabyte)))
	fmt.Fprintln(w, "\n\t\tContract Price:\t", currencyUnits(info.Entry.ContractPrice))
	fmt.Fprintln(w, "\t\tStorage Price (TB / Mo):\t", currencyUnits(info.Entry.StoragePrice.Mul(modules.BlockBytesPerMonthTerabyte)))
	fmt.Fprintln(w, "\t\tDownload Price (1 TB):\t", currencyUnits(info.Entry.DownloadBandwidthPrice.Mul(modules.BytesPerTerabyte)))
	fmt.Fprintln(w, "\t\tUpload Price (1 TB):\t", currencyUnits(info.Entry.UploadBandwidthPrice.Mul(modules.BytesPerTerabyte)))
	fmt.Fprintln(w, "\t\tVersion:\t", info.Entry.Version)
	w.Flush()

	printScoreBreakdown(&info)

	// Compute the total measured uptime and total measured downtime for this
	// host.
	uptimeRatio := float64(0)
	if len(info.Entry.ScanHistory) > 1 {
		downtime := info.Entry.HistoricDowntime
		uptime := info.Entry.HistoricUptime
		recentTime := info.Entry.ScanHistory[0].Timestamp
		recentSuccess := info.Entry.ScanHistory[0].Success
		for _, scan := range info.Entry.ScanHistory[1:] {
			if recentSuccess {
				uptime += scan.Timestamp.Sub(recentTime)
			} else {
				downtime += scan.Timestamp.Sub(recentTime)
			}
			recentTime = scan.Timestamp
			recentSuccess = scan.Success
		}
		uptimeRatio = float64(uptime) / float64(uptime+downtime)
	}

	// Compute the uptime ratio, but shift by 0.02 to acknowledge fully that
	// 98% uptime and 100% uptime is valued the same.
	fmt.Println("\n  Scan History Length:", len(info.Entry.ScanHistory))
	fmt.Printf("  Overall Uptime:      %.3f\n", uptimeRatio)

	fmt.Println()
}

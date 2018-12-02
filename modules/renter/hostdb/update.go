package hostdb

import (
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// findHostAnnouncements returns a list of the host announcements found within
// a given block. No check is made to see that the ip address found in the
// announcement is actually a valid ip address.
func findHostAnnouncements(b types.Block) (announcements []modules.HostDBEntry) {
	for _, t := range b.Transactions {
		// the HostAnnouncement must be prefaced by the standard host
		// announcement string
		for _, arb := range t.ArbitraryData {
			addr, pubKey, err := modules.DecodeAnnouncement(arb)
			if err != nil {
				continue
			}

			// Add the announcement to the slice being returned.
			var host modules.HostDBEntry
			host.NetAddress = addr
			host.PublicKey = pubKey
			announcements = append(announcements, host)
		}
	}
	return
}

// insertBlockchainHost adds a host entry to the state. The host will be inserted
// into the set of all hosts, and if it is online and responding to requests it
// will be put into the list of active hosts.
func (hdb *HostDB) insertBlockchainHost(host modules.HostDBEntry) {
	// Remove garbage hosts and local hosts (but allow local hosts in testing).
	if err := host.NetAddress.IsValid(); err != nil {
		hdb.log.Debugf("WARN: host '%v' has an invalid NetAddress: %v", host.NetAddress, err)
		return
	}
	// Ignore all local hosts announced through the blockchain.
	if build.Release == "standard" && host.NetAddress.IsLocal() {
		return
	}

	// Make sure the host gets into the host tree so it does not get dropped if
	// shutdown occurs before a scan can be performed.
	oldEntry, exists := hdb.hostTree.Select(host.PublicKey)
	if exists {
		// Replace the netaddress with the most recently announced netaddress.
		// Also replace the FirstSeen value with the current block height if
		// the first seen value has been set to zero (no hosts actually have a
		// first seen height of zero, but due to rescans hosts can end up with
		// a zero-value FirstSeen field.
		oldEntry.NetAddress = host.NetAddress
		if oldEntry.FirstSeen == 0 {
			oldEntry.FirstSeen = hdb.blockHeight
		}
		// Resolve the host's used subnets and update the timestamp if they
		// changed. We only update the timestamp if resolving the ipNets was
		// successful.
		ipNets, err := hdb.managedLookupIPNets(oldEntry.NetAddress)
		if err == nil && !equalIPNets(ipNets, oldEntry.IPNets) {
			oldEntry.IPNets = ipNets
			oldEntry.LastIPNetChange = time.Now()
		}
		// Modify hosttree
		err = hdb.modify(oldEntry)
		if err != nil {
			hdb.log.Println("ERROR: unable to modify host entry of host tree after a blockchain scan:", err)
		}
	} else {
		host.FirstSeen = hdb.blockHeight
		// Insert into hosttree
		err := hdb.insert(host)
		if err != nil {
			hdb.log.Println("ERROR: unable to insert host entry into host tree after a blockchain scan:", err)
		}
	}

	// Add the host to the scan queue.
	hdb.queueScan(host)
}

// ProcessConsensusChange will be called by the consensus set every time there
// is a change in the blockchain. Updates will always be called in order.
func (hdb *HostDB) ProcessConsensusChange(cc modules.ConsensusChange) {
	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	// Update the hostdb's understanding of the block height.
	for _, block := range cc.RevertedBlocks {
		// Only doing the block check if the height is above zero saves hashing
		// and saves a nontrivial amount of time during IBD.
		if hdb.blockHeight > 0 || block.ID() != types.GenesisID {
			hdb.blockHeight--
		} else if hdb.blockHeight != 0 {
			// Sanity check - if the current block is the genesis block, the
			// hostdb height should be set to zero.
			hdb.log.Critical("Hostdb has detected a genesis block, but the height of the hostdb is set to ", hdb.blockHeight)
			hdb.blockHeight = 0
		}
	}
	for _, block := range cc.AppliedBlocks {
		// Only doing the block check if the height is above zero saves hashing
		// and saves a nontrivial amount of time during IBD.
		if hdb.blockHeight > 0 || block.ID() != types.GenesisID {
			hdb.blockHeight++
		} else if hdb.blockHeight != 0 {
			// Sanity check - if the current block is the genesis block, the
			// hostdb height should be set to zero.
			hdb.log.Critical("Hostdb has detected a genesis block, but the height of the hostdb is set to ", hdb.blockHeight)
			hdb.blockHeight = 0
		}
	}

	// Add hosts announced in blocks that were applied.
	for _, block := range cc.AppliedBlocks {
		for _, host := range findHostAnnouncements(block) {
			hdb.log.Debugln("Found a host in a host announcement:", host.NetAddress, host.PublicKey)
			hdb.insertBlockchainHost(host)
		}
	}

	hdb.lastChange = cc.ID
}

// ProcessHeaderConsensusChange will be called by the consensus set every time there
// is a change in the blockchain. Updates will always be called in order.
func (hdb *HostDB) ProcessHeaderConsensusChange(hcc modules.HeaderConsensusChange) {
	hdb.mu.Lock()
	defer hdb.mu.Unlock()

	// Update the hostdb's understanding of the block height.
	for _, header := range hcc.RevertedBlockHeaders {
		// Only doing the block check if the height is above zero saves hashing
		// and saves a nontrivial amount of time during IBD.
		if hdb.blockHeight > 0 || header.BlockHeader.ID() != types.GenesisID {
			hdb.blockHeight--
		} else if hdb.blockHeight != 0 {
			// Sanity check - if the current block is the genesis block, the
			// hostdb height should be set to zero.
			hdb.log.Critical("Hostdb has detected a genesis block, but the height of the hostdb is set to ", hdb.blockHeight)
			hdb.blockHeight = 0
		}
	}
	for _, header := range hcc.AppliedBlockHeaders {
		// Only doing the block check if the height is above zero saves hashing
		// and saves a nontrivial amount of time during IBD.
		if hdb.blockHeight > 0 || header.BlockHeader.ID() != types.GenesisID {
			hdb.blockHeight++
		} else if hdb.blockHeight != 0 {
			// Sanity check - if the current block is the genesis block, the
			// hostdb height should be set to zero.
			hdb.log.Critical("Hostdb has detected a genesis block, but the height of the hostdb is set to ", hdb.blockHeight)
			hdb.blockHeight = 0
		}
	}

	// Add hosts announced in blocks that were applied.
	for _, header := range hcc.AppliedBlockHeaders {
		for _, announcement := range header.Announcements {
			var entry modules.HostDBEntry
			entry.NetAddress = announcement.NetAddress
			entry.PublicKey = announcement.PublicKey
			hdb.log.Debugln("Found a host in a host announcement:", entry.NetAddress, entry.PublicKey)
			hdb.insertBlockchainHost(entry)
		}
	}

	hdb.lastChange = hcc.ID
}

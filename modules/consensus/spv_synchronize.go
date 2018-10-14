// This file contains synchronization code that is only relevant to SPV nodes.
// Code that is relevant to both full nodes and SPV nodes is left in synchronize.go

package consensus

import (
	"errors"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

var (
	errSendHeadersStalled = errors.New("SendHeaders RPC timed and never received any blocks")

	// sendHeaders is the timeout for the SendHeaders RPC.
	sendHeadersTimeout = build.Select(build.Var{
		Standard: 5 * time.Minute,
		Dev:      40 * time.Second,
		Testing:  5 * time.Second,
	}).(time.Duration)
)

// managedReceiveHeaders is the calling end of the SendHeaders RPC, without the
// threadgroup wrapping.
// This method will only be used by SPV clients.  It should not have any dependency on the
// BlockMap DB bucket
func (cs *ConsensusSet) managedReceiveHeaders(conn modules.PeerConn) (returnErr error) {
	// Set a deadline after which SendHeaders will timeout. During IHD, esepcially,
	// SendHeaders will timeout. This is by design so that IHD switches peers to
	// prevent any one peer from stalling IHD.
	err := conn.SetDeadline(time.Now().Add(sendHeadersTimeout))
	if err != nil {
		return err
	}
	finishedChan := make(chan struct{})
	defer close(finishedChan)
	go func() {
		select {
		case <-cs.tg.StopChan():
		case <-finishedChan:
		}
		conn.Close()
	}()
	// Check whether this RPC has timed out with the remote peer at the end of
	// the fuction, and if so, return a custom error to signal that a new peer
	// needs to be chosen.
	stalled := true
	defer func() {
		// TODO: we're not even using muxado anymore - can this be changed?
		//
		// TODO: Timeout errors returned by muxado do not conform to the net.Error
		// interface and therefore we cannot check if the error is a timeout using
		// the Timeout() method. Once muxado issue #14 is resolved change the below
		// condition to:
		//     if netErr, ok := returnErr.(net.Error); ok && netErr.Timeout() && stalled { ... }
		if stalled && returnErr != nil && (returnErr.Error() == "Read timeout" || returnErr.Error() == "Write timeout") {
			returnErr = errSendHeadersStalled
		}
	}()
	// Get blockIDs to send.
	var history [32]types.BlockID
	cs.mu.RLock()
	err = cs.db.View(func(tx *bolt.Tx) error {
		history = blockHistory(tx)
		return nil
	})
	cs.mu.RUnlock()
	if err != nil {
		return err
	}
	// Send the block ids.
	if err := encoding.WriteObject(conn, history); err != nil {
		return err
	}
	// Broadcast the last BlockID accepted. This functionality is in a defer to
	// ensure that a block is always broadcast if any blocks are accepted. This
	// is to stop an attacker from preventing block broadcasts.
	//
	// NOTE: SPV nodes don't broadcast headers for 2 reasons:
	// 1) We want to save bandwidth on mobile devices
	// 2) Currently, full nodes will ask for full blocks from the broadcasting
	// peer when they receive a header. If they receive a header from an SPV
	// node, the getblock request to the SPV node will fail. This could be worked
	// around by having the full node grab the block from someone else, but it's
	// unclear from whom the full node would know to grab the block.
	/*
	var initialBlock types.BlockID
	if build.DEBUG {
		// Prepare for a sanity check on 'chainExtended' - chain extended should
		// be set to true if an ony if the result of calling dbCurrentBlockID
		// changes.
		initialBlock = cs.dbCurrentBlockID()
	}
	chainExtended := false
	defer func() {
		cs.mu.RLock()
		synced := cs.synced
		cs.mu.RUnlock()
		if synced && chainExtended {
			if build.DEBUG && initialBlock == cs.dbCurrentBlockID() {
				panic("blockchain extension reporting is incorrect")
			}
			header := cs.managedCurrentHeader() // TODO: Add cacheing, replace this line by looking at the cache.
			cs.managedBroadcastBlock(header) // even broadcast, no block for fullblock remote
		}
	}()
	*/
	// Read headers off of the wire and add them to the consensus set until
	// there are no more blocks available.
	moreAvailable := true
	for moreAvailable {
		//Read a slice of headers from the wire.
		var newTransmittedHeaders []modules.TransmittedBlockHeader
		if err := encoding.ReadObject(conn, &newTransmittedHeaders, uint64(MaxCatchUpBlocks)*types.BlockSizeLimit); err != nil {
			return err
		}
		if err := encoding.ReadObject(conn, &moreAvailable, 1); err != nil {
			return err
		}
		if len(newTransmittedHeaders) == 0 {
			continue
		}
		stalled = false
		//extended, _, acceptErr := cs.managedAcceptHeaders(newTransmittedHeaders)
		_, _, acceptErr := cs.managedAcceptHeaders(newTransmittedHeaders)
		/*
		if extended {
			chainExtended = true
		}
		*/
		if acceptErr != nil && acceptErr != modules.ErrNonExtendingBlock && acceptErr != modules.ErrBlockKnown {
			return acceptErr
		}
	}
	return nil
}
// threadedReceiveHeaders is the calling end of the SendHeaders RPC.
func (cs *ConsensusSet) threadedReceiveHeaders(conn modules.PeerConn) error {
	err := conn.SetDeadline(time.Now().Add(sendHeadersTimeout))
	if err != nil {
		return err
	}
	finishedChan := make(chan struct{})
	defer close(finishedChan)
	go func() {
		select {
		case <-cs.tg.StopChan():
		case <-finishedChan:
		}
		conn.Close()
	}()
	err = cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()
	if remoteSupportsSPVHeader(conn.Version()) {
		return cs.managedReceiveHeaders(conn)
	}
	return nil
}

// rpcSendHeaders is the receiving end of the SendHeaders RPC. It returns a
// sequential set of block headers based on the 32 input block IDs. The most recent
// known ID is used as the starting point, and up to 'MaxCatchUpBlocks' from
// that BlockHeight onwards are returned. It also sends a boolean indicating
// whether more blocks are available.
// This should only be registered on full nodes.  SPV nodes will not have the full
// blockchain available to calculate this response.
func (cs *ConsensusSet) rpcSendHeaders(conn modules.PeerConn) error {
	err := conn.SetDeadline(time.Now().Add(sendHeadersTimeout))
	if err != nil {
		return err
	}
	finishedChan := make(chan struct{})
	defer close(finishedChan)
	go func() {
		select {
		case <-cs.tg.StopChan():
		case <-finishedChan:
		}
		conn.Close()
	}()
	err = cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()
	// Read a list of blocks known to the requester and find the most recent
	// block from the current path.
	var knownBlocks [32]types.BlockID
	err = encoding.ReadObject(conn, &knownBlocks, 32*crypto.HashSize)
	if err != nil {
		return err
	}
	// Find the most recent block from knownBlocks in the current path.
	found := false
	var start types.BlockHeight
	var csHeight types.BlockHeight
	cs.mu.RLock()
	//

	err = cs.db.View(func(tx *bolt.Tx) error {
		csHeight = blockHeight(tx)
		for _, id := range knownBlocks {
			pbh, exists := cs.processedBlockHeaders[id]
			if !exists {
				continue
			}
			pathID, err := getPath(tx, pbh.Height)
			if err != nil {
				continue
			}
			if pathID != pbh.BlockHeader.ID() {
				continue
			}
			if pbh.Height == csHeight {
				break
			}
			found = true
			// Start from the child of the common block.
			start = pbh.Height + 1
			break
		}
		return nil
	})
	cs.mu.RUnlock()
	if err != nil {
		return err
	}
	// If no matching block headers are found, or if the caller has all known block headers,
	// don't send anything.
	if !found {
		// Send 0 headers.
		if remoteSupportsSPVHeader(conn.Version()) {
			err = encoding.WriteObject(conn, []modules.TransmittedBlockHeader{})
			if err != nil {
				return err
			}
		} else {
			err = encoding.WriteObject(conn, []types.BlockHeader{})
			if err != nil {
				return err
			}
		}
		// Indicate that no more headers are available.
		return encoding.WriteObject(conn, false)
	}
	// Send the caller all of the headers that they are missing.
	moreAvailable := true
	for moreAvailable {
		// Get the set of block headers to send
		var blockHeaders []types.BlockHeader
		var transmittedBlockHeaders []modules.TransmittedBlockHeader
		cs.mu.RLock()
		err = cs.db.View(func(tx *bolt.Tx) error {
			height := blockHeight(tx)
			for i := start; i <= height && i < start+MaxCatchUpBlocks; i++ {
				id, err := getPath(tx, i)
				if err != nil {
					cs.log.Critical("Unable to get path: height", height, ":: request", i)
					return err
				}
				// TODO: read from mem
				pbh, exists := cs.processedBlockHeaders[id]
				if !exists {
					cs.log.Critical("Unable to get header from mem: height", height, ":: request", i, ":: id", id)
					return errHeaderNotExist
				}
				if pbh == nil {
					cs.log.Critical("header from mem yielded 'nil' header:", height, ":: request", i, ":: id", id)
					return errHeaderNotExist
				}
				if remoteSupportsSPVHeader(conn.Version()) {
					transmittedBlockHeaders = append(transmittedBlockHeaders, *pbh.ForSend())
				} else {
					blockHeaders = append(blockHeaders, pbh.BlockHeader)
				}
			}
			moreAvailable = start+MaxCatchUpBlocks <= height
			start += MaxCatchUpBlocks
			return nil
		})
		cs.mu.RUnlock()
		if err != nil {
			return err
		}
		// Send a set of blocks to the caller + a flag indicating whether more
		// are available.
		if remoteSupportsSPVHeader(conn.Version()) {
			if err = encoding.WriteObject(conn, transmittedBlockHeaders); err != nil {
				return err
			}
		} else {
			if err = encoding.WriteObject(conn, blockHeaders); err != nil {
				return err
			}
		}

		if err = encoding.WriteObject(conn, moreAvailable); err != nil {
			return err
		}
	}
	return nil
}

// managedReceiveSingleBlock takes a block id and returns an RPCFunc that requests that
// block and then calls AcceptBlockForSPV on it. The returned function should be used
// as the calling end of the SendBlk RPC.
func (cs *ConsensusSet) managedReceiveSingleBlock(id types.BlockID, changes []changeEntry) modules.RPCFunc {
	return func(conn modules.PeerConn) error {
		if err := encoding.WriteObject(conn, id); err != nil {
			return err
		}
		var block types.Block
		if err := encoding.ReadObject(conn, &block, types.BlockSizeLimit); err != nil {
			return err
		}
		_, err := cs.managedAcceptSingleBlockForSPV(block, changes)
		if err != nil {
			return err
		}
		return nil
	}
}

func (cs *ConsensusSet) threadedInitialHeadersDownload() error {
	// The consensus set will not recognize IHD as complete until it has enough
	// peers. After the deadline though, it will recognize the blockchain
	// download as complete even with only one peer. This deadline is helpful
	// to local-net setups, where a machine will frequently only have one peer
	// (and that peer will be another machine on the same local network, but
	// within the local network at least one peer is connected to the broad
	// network).
	deadline := time.Now().Add(minIBDWaitTime)
	numOutboundSynced := 0
	numOutboundNotSynced := 0
	for {
		numOutboundSynced = 0
		numOutboundNotSynced = 0
		for _, p := range cs.gateway.Peers() {
			// We can't sync from old peers - move on to the next peer if this peer
			// is too old.
			//
			// We also disconnect from the old peer because it's useless to us.
			if !remoteSupportsSPVHeader(p.Version) {
				err := cs.gateway.Disconnect(p.NetAddress)
				if err != nil {
					cs.log.Printf("WARN: disconnecting from peer %v failed: %v", p.NetAddress, err)
				}
				continue
			}

			// We only sync on outbound peers at first to make IHD less susceptible to
			// fast-mining and other attacks, as outbound peers are more difficult to
			// manipulate.
			if p.Inbound {
				continue
			}

			// Put the rest of the iteration inside of a thread group.
			err := func() error {
				err := cs.tg.Add()
				if err != nil {
					return err
				}
				defer cs.tg.Done()

				// Request headers from the peer. The error returned will only be
				// 'nil' if there are no more headers to receive.
				err = cs.gateway.RPC(p.NetAddress, modules.SendHeadersCmd, cs.managedReceiveHeaders)
				if err == nil {
					numOutboundSynced++
					// In this case, 'return nil' is equivalent to skipping to
					// the next iteration of the loop.
					return nil
				}
				numOutboundNotSynced++
				if !isTimeoutErr(err) {
					cs.log.Printf("WARN: disconnecting from peer %v because IHD failed: %v", p.NetAddress, err)
					// Disconnect if there is an unexpected error (not a timeout). This
					// includes errSendBlocksStalled.
					//
					// We disconnect so that these peers are removed from gateway.Peers() and
					// do not prevent us from marking ourselves as fully synced.
					err := cs.gateway.Disconnect(p.NetAddress)
					if err != nil {
						cs.log.Printf("WARN: disconnecting from peer %v failed: %v", p.NetAddress, err)
					}
				}
				return nil
			}()
			if err != nil {
				return err
			}
		}

		// The consensus set is not considered synced until a majority of
		// outbound peers say that we are synced. If less than 10 minutes have
		// passed, a minimum of 'minNumOutbound' peers must say that we are
		// synced, otherwise a 1 vs 0 majority is sufficient.
		//
		// This scheme is used to prevent malicious peers from being able to
		// barricade the sync'd status of the consensus set, and to make sure
		// that consensus sets behind a firewall with only one peer
		// (potentially a local peer) are still able to eventually conclude
		// that they have syncrhonized. Miners and hosts will often have setups
		// beind a firewall where there is a single node with many peers and
		// then the rest of the nodes only have a few peers.
		if numOutboundSynced > numOutboundNotSynced && (numOutboundSynced >= minNumOutbound || time.Now().After(deadline)) {
			break
		} else {
			// Sleep so we don't hammer the network with SendHeader requests.
			time.Sleep(ibdLoopDelay)
		}
	}

	cs.log.Printf("INFO: IHD done, synced with %v peers", numOutboundSynced)
	return nil
}

func (cs *ConsensusSet) downloadSingleBlock(id types.BlockID, pb *processedBlock) modules.RPCFunc {
	return func(conn modules.PeerConn) (err error) {
		if err = encoding.WriteObject(conn, id); err != nil {
			return
		}
		var block types.Block
		if err = encoding.ReadObject(conn, &block, types.BlockSizeLimit); err != nil {
			return
		}
		pb, err = cs.managedAcceptSingleBlockForSPV(block, nil)
		if err != nil {
			return
		}
		return nil
	}
}

func (cs *ConsensusSet) getOrDownloadBlock(tx *bolt.Tx, id types.BlockID) (*processedBlock, error) {
	pb, err := getBlockMap(tx, id)
	if err == errNilItem {
		//TODO: randomly pick a node
		for _, peer := range cs.gateway.Peers() {
			// how to get this peer connection
			err = cs.gateway.RPC(peer.NetAddress, modules.SendBlockCmd, cs.downloadSingleBlock(id, pb))
			if err != nil {
				return nil, err
			}
		}
	} else {
		if err == nil {
			return pb, nil
		}
		return nil, err
	}
	return pb, nil
}

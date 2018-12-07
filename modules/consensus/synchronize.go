package consensus

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	siasync "github.com/HyperspaceApp/Hyperspace/sync"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

const (
	// minNumOutbound is the minimum number of outbound peers required before ibd
	// is confident we are synced.
	minNumOutbound = 5
	// MaxDownloadSingleBlockDuration is the timeout time for each download go routine
	MaxDownloadSingleBlockDuration = 5 * time.Minute
)

var (
	errEarlyStop         = errors.New("initial blockchain download did not complete by the time shutdown was issued")
	errNilProcBlock      = errors.New("nil processed block was fetched from the database")
	errSendBlocksStalled = errors.New("SendBlocks RPC timed and never received any blocks")

	// ibdLoopDelay is the time that threadedInitialBlockchainDownload waits
	// between attempts to synchronize with the network if the last attempt
	// failed.
	ibdLoopDelay = build.Select(build.Var{
		Standard: 10 * time.Second,
		Dev:      1 * time.Second,
		Testing:  100 * time.Millisecond,
	}).(time.Duration)

	// MaxCatchUpBlocks is the maxiumum number of blocks that can be given to
	// the consensus set in a single iteration during the initial blockchain
	// download.
	MaxCatchUpBlocks = build.Select(build.Var{
		Standard: types.BlockHeight(10),
		Dev:      types.BlockHeight(50),
		Testing:  types.BlockHeight(3),
	}).(types.BlockHeight)

	// minIBDWaitTime is the time threadedInitialBlockchainDownload waits before
	// exiting if there are >= 1 and <= minNumOutbound peers synced. This timeout
	// will primarily affect miners who have multiple nodes daisy chained off each
	// other. Those nodes will likely have to wait minIBDWaitTime on every startup
	// before IBD is done.
	minIBDWaitTime = build.Select(build.Var{
		Standard: 90 * time.Minute,
		Dev:      80 * time.Second,
		Testing:  10 * time.Second,
	}).(time.Duration)

	// relayHeaderTimeout is the timeout for the RelayHeader RPC.
	relayHeaderTimeout = build.Select(build.Var{
		Standard: 60 * time.Second,
		Dev:      20 * time.Second,
		Testing:  3 * time.Second,
	}).(time.Duration)

	// sendBlkTimeout is the timeout for the SendBlk RPC.
	sendBlkTimeout = build.Select(build.Var{
		Standard: 90 * time.Second,
		Dev:      30 * time.Second,
		Testing:  4 * time.Second,
	}).(time.Duration)

	// sendBlocksTimeout is the timeout for the SendBlocks RPC.
	sendBlocksTimeout = build.Select(build.Var{
		Standard: 180 * time.Second,
		Dev:      40 * time.Second,
		Testing:  5 * time.Second,
	}).(time.Duration)

	// MaxDownloadSingleBlockRequest is the node count we concurrently try to fetch
	MaxDownloadSingleBlockRequest = build.Select(build.Var{
		Standard: int(3),
		Dev:      int(1),
		Testing:  int(1),
	}).(int)
)

// isTimeoutErr is a helper function that returns true if err was caused by a
// network timeout.
func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}
	return false
}

// blockHistory returns up to 32 block ids, starting with recent blocks and
// then proving exponentially increasingly less recent blocks. The genesis
// block is always included as the last block. This block history can be used
// to find a common parent that is reasonably recent, usually the most recent
// common parent is found, but always a common parent within a factor of 2 is
// found.
func blockHistory(tx *bolt.Tx) (blockIDs [32]types.BlockID) {
	height := blockHeight(tx)
	step := types.BlockHeight(1)
	// The final step is to include the genesis block, which is why the final
	// element is skipped during iteration.
	for i := 0; i < 31; i++ {
		// Include the next block.
		blockID, err := getPath(tx, height)
		if build.DEBUG && err != nil {
			panic(err)
		}
		blockIDs[i] = blockID

		// Determine the height of the next block to include and then increase
		// the step size. The height must be decreased first to prevent
		// underflow.
		//
		// `i >= 9` means that the first 10 blocks will be included, and then
		// skipping will start.
		if i >= 9 {
			step *= 2
		}
		if height <= step {
			break
		}
		height -= step
	}
	// Include the genesis block as the last element
	blockID, err := getPath(tx, 0)
	if build.DEBUG && err != nil {
		panic(err)
	}
	blockIDs[31] = blockID
	return blockIDs
}

// managedReceiveBlocks is the calling end of the SendBlocks RPC, without the
// threadgroup wrapping.
func (cs *ConsensusSet) managedReceiveBlocks(conn modules.PeerConn) (returnErr error) {
	// Set a deadline after which SendBlocks will timeout. During IBD, especially,
	// SendBlocks will timeout. This is by design so that IBD switches peers to
	// prevent any one peer from stalling IBD.
	err := conn.SetDeadline(time.Now().Add(sendBlocksTimeout))
	if err != nil {
		return err
	}
	// cs.log.Printf("managedReceiveBlocks: %s", conn.RemoteAddr().String())
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
		if isTimeoutErr(returnErr) && stalled {
			returnErr = errSendBlocksStalled
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

	// Broadcast the last block accepted. This functionality is in a defer to
	// ensure that a block is always broadcast if any blocks are accepted. This
	// is to stop an attacker from preventing block broadcasts.
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
			fullBlock := cs.managedCurrentBlock() // TODO: Add cacheing, replace this line by looking at the cache.
			cs.managedBroadcastBlock(fullBlock.Header())
		}
	}()

	// Read blocks off of the wire and add them to the consensus set until
	// there are no more blocks available.
	moreAvailable := true
	for moreAvailable {
		// Read a slice of blocks from the wire.
		var newBlocks []types.Block
		if err := encoding.ReadObject(conn, &newBlocks, uint64(MaxCatchUpBlocks)*types.BlockSizeLimit); err != nil {
			return err
		}
		if err := encoding.ReadObject(conn, &moreAvailable, 1); err != nil {
			return err
		}
		if len(newBlocks) == 0 {
			continue
		}
		stalled = false
		// log.Printf("newBlocks: %d %s", len(newBlocks), conn.RemoteAddr().String())

		// Call managedAcceptBlock instead of AcceptBlock so as not to broadcast
		// every block.
		extended, acceptErr := cs.managedAcceptBlocks(newBlocks)
		if extended {
			chainExtended = true
		}
		// ErrNonExtendingBlock must be ignored until headers-first block
		// sharing is implemented, block already in database should also be
		// ignored.
		if acceptErr != nil && acceptErr != modules.ErrNonExtendingBlock && acceptErr != modules.ErrBlockKnown {
			return acceptErr
		}
	}
	return nil
}

// threadedReceiveBlocks is the calling end of the SendBlocks RPC.
func (cs *ConsensusSet) threadedReceiveBlocks(conn modules.PeerConn) error {
	err := conn.SetDeadline(time.Now().Add(sendBlocksTimeout))
	if err != nil {
		return err
	}
	// log.Printf("threadedReceiveBlocks: %s", conn.RemoteAddr().String())
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
	return cs.managedReceiveBlocks(conn)
}

// rpcSendBlocks is the receiving end of the SendBlocks RPC. It returns a
// sequential set of blocks based on the 32 input block IDs. The most recent
// known ID is used as the starting point, and up to 'MaxCatchUpBlocks' from
// that BlockHeight onwards are returned. It also sends a boolean indicating
// whether more blocks are available.
func (cs *ConsensusSet) rpcSendBlocks(conn modules.PeerConn) error {
	err := conn.SetDeadline(time.Now().Add(sendBlocksTimeout))
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
	err = cs.db.View(func(tx *bolt.Tx) error {
		csHeight = blockHeight(tx)
		for _, id := range knownBlocks {
			pb, err := getBlockMap(tx, id)
			if err != nil {
				continue
			}
			pathID, err := getPath(tx, pb.Height)
			if err != nil {
				continue
			}
			if pathID != pb.Block.ID() {
				continue
			}
			if pb.Height == csHeight {
				break
			}
			found = true
			// Start from the child of the common block.
			start = pb.Height + 1
			break
		}
		return nil
	})
	cs.mu.RUnlock()
	if err != nil {
		return err
	}

	// If no matching blocks are found, or if the caller has all known blocks,
	// don't send any blocks.
	if !found {
		// Send 0 blocks.
		err = encoding.WriteObject(conn, []types.Block{})
		if err != nil {
			return err
		}
		// Indicate that no more blocks are available.
		return encoding.WriteObject(conn, false)
	}

	// Send the caller all of the blocks that they are missing.
	moreAvailable := true
	for moreAvailable {
		// Get the set of blocks to send.
		var blocks []types.Block
		cs.mu.RLock()
		err = cs.db.View(func(tx *bolt.Tx) error {
			height := blockHeight(tx)
			for i := start; i <= height && i < start+MaxCatchUpBlocks; i++ {
				id, err := getPath(tx, i)
				if err != nil {
					cs.log.Critical("Unable to get path: height", height, ":: request", i)
					return err
				}
				pb, err := getBlockMap(tx, id)
				if err != nil {
					cs.log.Critical("Unable to get block from block map: height", height, ":: request", i, ":: id", id)
					return err
				}
				if pb == nil {
					cs.log.Critical("getBlockMap yielded 'nil' block:", height, ":: request", i, ":: id", id)
					return errNilProcBlock
				}
				blocks = append(blocks, pb.Block)
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
		if err = encoding.WriteObject(conn, blocks); err != nil {
			return err
		}
		if err = encoding.WriteObject(conn, moreAvailable); err != nil {
			return err
		}
	}

	return nil
}

// threadedRPCRelayHeader is an RPC that accepts a block header from a peer.
func (cs *ConsensusSet) threadedRPCRelayHeader(conn modules.PeerConn) error {
	err := conn.SetDeadline(time.Now().Add(relayHeaderTimeout))
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
	wg := new(sync.WaitGroup)
	defer func() {
		go func() {
			wg.Wait()
			cs.tg.Done()
		}()
	}()

	// Decode the block header from the connection.
	var h types.BlockHeader
	var phfs modules.TransmittedBlockHeader
	// TODO: processed header's size is not fixed,but should not larger than block limit
	// log.Printf("remote version: %s", conn.Version())
	if remoteSupportsSPVHeader(conn.Version()) {
		err = encoding.ReadObject(conn, &phfs, types.BlockSizeLimit)
		if err != nil {
			return err
		}
		h = phfs.BlockHeader
	} else {
		err = encoding.ReadObject(conn, &h, types.BlockHeaderSize)
		if err != nil {
			return err
		}
	}
	// Start verification inside of a bolt View tx.
	cs.mu.RLock()
	err = cs.db.View(func(tx *bolt.Tx) error {
		// Do some relatively inexpensive checks to validate the header
		_, err := cs.validateHeader(boltTxWrapper{tx}, h)
		return err
	})
	cs.mu.RUnlock()
	// log.Printf("after validate header")
	// WARN: orphan multithreading logic (dangerous areas, see below)
	//
	// If the header is valid and extends the heaviest chain, fetch the
	// corresponding block. Call needs to be made in a separate goroutine
	// because an exported call to the gateway is used, which is a deadlock
	// risk given that rpcRelayHeader is called from the gateway.
	//
	// NOTE: In general this is bad design. Rather than recycling other
	// calls, the whole protocol should have been kept in a single RPC.
	// Because it is not, we have to do weird threading to prevent
	// deadlocks, and we also have to be concerned every time the code in
	// managedReceiveBlock is adjusted.
	if err == errOrphan { // WARN: orphan multithreading logic case #1
		if cs.spv { //spv dont want to fetch blocks from remote
			return nil
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// TODO: deal with orphan header case
			err := cs.gateway.RPC(conn.RPCAddr(), modules.SendBlocksCmd, cs.managedReceiveBlocks)
			if err != nil {
				cs.log.Debugln("WARN: failed to get parents of orphan header:", err)
			}
		}()
		return nil
	} else if err != nil {
		return err
	}

	// WARN: orphan multithreading logic case #2
	wg.Add(1)
	go func() {
		defer wg.Done()
		if cs.spv {
			if !remoteSupportsSPVHeader(conn.Version()) {
				return
			}
			_, _, err := cs.managedAcceptHeaders([]modules.TransmittedBlockHeader{phfs})
			if err != nil {
				cs.log.Debugln("WARN: failed to get header's corresponding block:", err)
			}
		} else {
			err = cs.gateway.RPC(conn.RPCAddr(), modules.SendBlockCmd, cs.managedReceiveBlock(h.ID()))
			if err != nil {
				cs.log.Debugln("WARN: failed to get header's corresponding block:", err)
			}
		}
	}()
	return nil
}

// rpcSendBlk is an RPC that sends the requested block to the requesting peer.
func (cs *ConsensusSet) rpcSendBlk(conn modules.PeerConn) error {
	err := conn.SetDeadline(time.Now().Add(sendBlkTimeout))
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

	// Decode the block id from the connection.
	var id types.BlockID
	err = encoding.ReadObject(conn, &id, crypto.HashSize)
	if err != nil {
		return err
	}
	// Lookup the corresponding block.
	var b types.Block
	cs.mu.RLock()
	err = cs.db.View(func(tx *bolt.Tx) error {
		pb, err := getBlockMap(tx, id)
		if err != nil {
			return err
		}
		b = pb.Block
		return nil
	})
	cs.mu.RUnlock()
	if err != nil {
		return err
	}
	// Encode and send the block to the caller.
	err = encoding.WriteObject(conn, b)
	if err != nil {
		return err
	}
	return nil
}

// managedReceiveBlock takes a block id and returns an RPCFunc that requests that
// block and then calls AcceptBlock on it. The returned function should be used
// as the calling end of the SendBlk RPC.
func (cs *ConsensusSet) managedReceiveBlock(id types.BlockID) modules.RPCFunc {
	return func(conn modules.PeerConn) error {
		if err := encoding.WriteObject(conn, id); err != nil {
			return err
		}
		var block types.Block
		if err := encoding.ReadObject(conn, &block, types.BlockSizeLimit); err != nil {
			return err
		}
		chainExtended, err := cs.managedAcceptBlocks([]types.Block{block})
		if chainExtended {
			cs.managedBroadcastBlock(block.Header())
		}
		if err != nil {
			return err
		}
		return nil
	}
}

// threadedInitialBlockchainDownload performs the IBD on outbound peers. Blocks
// are downloaded from one peer at a time in 5 minute intervals, so as to
// prevent any one peer from significantly slowing down IBD.
//
// NOTE: IBD will succeed right now when each peer has a different blockchain.
// The height and the block id of the remote peers' current blocks are not
// checked to be the same. This can cause issues if you are connected to
// outbound peers <= v0.5.1 that are stalled in IBD.
func (cs *ConsensusSet) threadedInitialBlockchainDownload() error {
	// The consensus set will not recognize IBD as complete until it has enough
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
			// We only sync on outbound peers at first to make IBD less susceptible to
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

				// Request blocks from the peer. The error returned will only be
				// 'nil' if there are no more blocks to receive.
				err = cs.gateway.RPC(p.NetAddress, modules.SendBlocksCmd, cs.managedReceiveBlocks)
				if err == nil {
					numOutboundSynced++
					// In this case, 'return nil' is equivalent to skipping to
					// the next iteration of the loop.
					return nil
				}
				numOutboundNotSynced++
				if !isTimeoutErr(err) {
					cs.log.Printf("WARN: disconnecting from peer %v because IBD failed: %v", p.NetAddress, err)
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
			// Sleep so we don't hammer the network with SendBlock requests.
			time.Sleep(ibdLoopDelay)
		}
	}

	cs.log.Printf("INFO: IBD done, synced with ", numOutboundSynced, "peers")
	return nil
}

func (cs *ConsensusSet) downloadSingleBlock(id types.BlockID, pbChan chan *processedBlock,
	acceptLockPtr *deadlock.Mutex, wg *sync.WaitGroup) modules.RPCFunc {
	return func(conn modules.PeerConn) (err error) {
		defer func() {
			// log.Printf("downloadSingleBlock done: %v", conn.RPCAddr())
			wg.Done()
		}()
		// log.Printf("downloadSingleBlock start: %v", conn.RPCAddr())
		if err = encoding.WriteObject(conn, id); err != nil {
			return
		}
		doneChan := make(chan struct{})
		var block types.Block
		go func() {
			defer close(doneChan)
			if err = encoding.ReadObject(conn, &block, types.BlockSizeLimit); err != nil {
				cs.log.Printf("err when download single block:ReadObject: %s", err)
			}
		}()
		select {
		case <-time.After(MaxDownloadSingleBlockDuration):
			return
		case <-pbChan: // block from other peer accepted, return to close this connection
			return
		case <-doneChan:
		}
		// all downloaded single accept by sequence,
		// 1. the first one accepted, second one will be reject by check
		// 2. the first one failed, second one try again
		acceptLockPtr.Lock()
		defer acceptLockPtr.Unlock()
		// log.Printf("before downloadSingleBlock: %s", id)
		var pb *processedBlock
		err = cs.db.Update(func(tx *bolt.Tx) error {
			var errAcceptSingleBlock error
			pb, errAcceptSingleBlock = cs.managedAcceptSingleBlock(tx, block)
			if errAcceptSingleBlock == nil {
				pbChan <- pb // pass the processed block and stop other connection
				close(pbChan)
			}
			return errAcceptSingleBlock
		})
		// log.Printf("after downloadSingleBlock: %s", id)
		if err != nil {
			cs.log.Printf("err when download single block: %s", err)
			return
		}
		return nil
	}
}

// dbGetBlockMap is a convenience function allowing getBlockMap to be called
// without a bolt.Tx.
func (cs *ConsensusSet) dbGetBlockMap(id types.BlockID) (pb *processedBlock, err error) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		pb, err = getBlockMap(tx, id)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return pb, err
}

func (cs *ConsensusSet) getOrDownloadBlock(id types.BlockID) (*processedBlock, error) {
	pb, err := cs.dbGetBlockMap(id)
	if err == errNilItem {
		// TODO: add retry download when fail to download from one peer (could be spv)
		pbChan := make(chan *processedBlock, 1)
		waitChan := make(chan bool, 1)
		var wg sync.WaitGroup
		// finishedChan := make(chan bool, MaxDownloadSingleBlockRequest)
		var count int
		var acceptLock deadlock.Mutex
		peerMap := make(map[modules.Peer]bool)
		for {
			peer, err := cs.gateway.RandomPeer()
			if err != nil {
				return nil, err
			}
			_, exists := peerMap[peer]
			if exists { // don't fetch from same server
				continue
			}
			peerMap[peer] = true
			count++
			wg.Add(1) // add this out of go routine to prevent wg.Wait get pass before add(1)
			go func() {
				err = cs.gateway.RPC(peer.NetAddress, modules.SendBlockCmd, cs.downloadSingleBlock(id, pbChan, &acceptLock, &wg))
				if err != nil {
					cs.log.Printf("cs.gateway.RPC err: %s", err)
				}
			}()
			if count >= MaxDownloadSingleBlockRequest {
				break
			}
		}
		go func() {
			wg.Wait()
			close(waitChan)
		}()
		select {
		case <-cs.tg.StopChan():
			return nil, siasync.ErrStopped
		case <-time.After(MaxDownloadSingleBlockDuration):
			return nil, errors.New("download block timeout")
		case <-waitChan: // all download fail
			return nil, errors.New("all download failed")
		case pb = <-pbChan:
			if pb == nil {
				return nil, fmt.Errorf("download failed, processed block nil")
			}
			cs.log.Printf("got block %d from channel", pb.Height)
		}
	} else {
		if err == nil {
			return pb, nil
		}
		return nil, err
	}
	return pb, nil
}

// Synced returns true if the consensus set is synced with the network.
func (cs *ConsensusSet) Synced() bool {
	err := cs.tg.Add()
	if err != nil {
		return false
	}
	defer cs.tg.Done()
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.synced
}

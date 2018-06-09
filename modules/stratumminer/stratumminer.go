// Package stratumminer is responsible for finding valid block headers and
// submitting them to a stratum server
package stratumminer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/persist"

	"github.com/NebulousLabs/threadgroup"
)

//miningWork is sent to the mining routines and defines what ranges should be searched for a matching nonce
type miningWork struct {
	Header []byte
	Offset uint64
	Target []byte
	Job    interface{}
}

//const maxUint32 = int64(^uint32(0))
const maxUint64 = ^uint64(0)

// StratumMiner does the actual stratum mining
type StratumMiner struct {
	HashRateReports   chan float64
	miningWorkChannel chan *miningWork
	Client            GenericClient

	// Miner variables.
	miningOn          bool    // indicates if the miner is supposed to be running
	mining            bool    // indicates if the miner is actually running
	hashRate          float64 // indicates hashes per second
	submissions       uint64  // indicates how many shares the miner has submitted
	workThreadRunning bool    // indicates if a workthread is still running

	// Utils
	log        *persist.Logger
	mu         sync.RWMutex
	persistDir string

	// tg signals the Miner's goroutines to shut down and blocks until all
	// goroutines have exited before returning from Close().
	tg threadgroup.ThreadGroup
}

func New(persistDir string) (*StratumMiner, error) {
	// Assemble the stratum miner.
	var hashRateReportsChannel = make(chan float64, 10)
	sm := &StratumMiner{
		HashRateReports: hashRateReportsChannel,
		persistDir:      persistDir,
	}
	err := sm.initPersist()
	if err != nil {
		return nil, errors.New("stratum miner persistence startup failed: " + err.Error())
	}
	return sm, nil
}

func (sm *StratumMiner) Hashrate() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.hashRate
}

func (sm *StratumMiner) Submissions() uint64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.submissions
}

func (sm *StratumMiner) Connected() bool {
	if err := sm.tg.Add(); err != nil {
		build.Critical(err)
	}
	defer sm.tg.Done()

	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.Client.Connected()
}

func (sm *StratumMiner) Mining() bool {
	if err := sm.tg.Add(); err != nil {
		build.Critical(err)
	}
	defer sm.tg.Done()

	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.mining
}

// Starting the stratum miner spins off two goroutines:
// One listens for work via a tcp connection and pushes it into a work channel
// The other waits for work from the channel and then grinds on it and submits solutions
func (sm *StratumMiner) StartStratumMining(server, username string) {
	if err := sm.tg.Add(); err != nil {
		build.Critical(err)
	}
	defer sm.tg.Done()

	sm.mu.Lock()
	defer sm.mu.Unlock()
	// if we're already mining, do nothing
	if sm.miningOn || sm.workThreadRunning {
		return
	}
	sm.miningOn = true
	sm.workThreadRunning = true
	sm.submissions = 0
	sm.log.Println("Starting stratum mining.")
	sm.miningWorkChannel = make(chan *miningWork, 1)
	sm.Client = NewClient(server, username)
	go sm.createWork()
	go sm.mine()
	sm.log.Println("Finished starting stratum mining.")
}

func (sm *StratumMiner) StopStratumMining() {
	//sm.log.Println("StopStratumMining called")
	if err := sm.tg.Add(); err != nil {
		build.Critical(err)
	}
	defer func() {
		sm.tg.Done()
		//sm.log.Println("Done stopping stratum mining.")
	}()
	sm.log.Println("Stopping stratum mining.")
	sm.mu.Lock()
	defer sm.mu.Unlock()
	// if mining is not on, we don't need to do anything
	if !sm.miningOn {
		return
	}
	sm.miningOn = false
	if sm.Client != nil {
		sm.Client.Stop()
		sm.Client = nil
	}
}

func (sm *StratumMiner) Close() error {
	sm.log.Println("StratumMiner.Close() called")
	if err := sm.tg.Stop(); err != nil {
		return err
	}

	sm.mu.Lock()
	sm.StopStratumMining()
	defer sm.mu.Unlock()

	return nil
}

func (sm *StratumMiner) createWork() {
	if err := sm.tg.Add(); err != nil {
		return
	}
	defer func() {
		// we're the only goroutine responsible for writing
		// to this channel, so lets make sure we're the one
		// who closes it
		sm.log.Println("Exiting createWork() thread")
		sm.mu.Lock()
		sm.workThreadRunning = false
		close(sm.miningWorkChannel)
		sm.mu.Unlock()
		sm.tg.Done()
		sm.log.Println("Exited createWork() thread")
	}()

	select {
	// if we've asked the thread to stop, don't proceed
	case <-sm.tg.StopChan():
		return
	default:
	}

	// if we told the miner to stop before we even got here, we need to bail out
	sm.mu.Lock()
	if !sm.miningOn {
		sm.mu.Unlock()
		return
	}
	// Register a function to clear the generated work if a job gets deprecated.
	// It does not matter if we clear too many, it is worse to work on a stale job.
	sm.Client.SetDeprecatedJobCall(func() {
		numberOfWorkItemsToRemove := len(sm.miningWorkChannel)
		for i := 0; i <= numberOfWorkItemsToRemove; i++ {
			<-sm.miningWorkChannel
		}
	})

	sm.mu.Unlock()
	sm.Client.Start()

	for {
		sm.log.Println("createWork() for loop start")
		sm.mu.Lock()
		select {
		case <-sm.tg.StopChan():
			sm.mu.Unlock()
			return
		default:
		}

		// if we've told the miner to stop, don't try to keep getting headers
		if !sm.miningOn {
			sm.mu.Unlock()
			return
		}

		sm.log.Println("waiting on GetHeaderForWork()")
		target, header, deprecationChannel, job, err := sm.Client.GetHeaderForWork()
		sm.log.Println("done waiting on GetHeaderForWork()")

		sm.mu.Unlock()
		if err != nil {
			sm.log.Println("ERROR fetching work -", err)
			time.Sleep(1000 * time.Millisecond)
			continue
		}

		//Fill the workchannel with work
		sm.log.Println("start filling workchannel")
	nonce64loop:
		// do we need to block here? I don't think so
		for i := uint64(0); i < maxUint64; i++ {
			//sm.mu.Lock()
			select {
			// Do not continue mining the 64 bit nonce space if the current job
			// is deprecated
			case <-deprecationChannel:
				//sm.mu.Unlock()
				break nonce64loop
			// Shut down the thread if we've been stopped the module
			case <-sm.tg.StopChan():
				sm.log.Println("stopchan() called exiting")
				//sm.mu.Unlock()
				return
			default:
			}
			if !sm.miningOn {
				sm.log.Println("mining turned off, exiting createWork()")
				return
			}

			sm.miningWorkChannel <- &miningWork{header, i, target, job}
			time.Sleep(time.Nanosecond)
		}
		sm.log.Println("done filling workchannel")
		sm.log.Println("createWork() for loop end")
	}
}

// header structure: 80 bytes
//       32 bytes             8 bytes      8 bytes           32 bytes
// ----------------------- | ---------- | ---------- | ----------------------- |
//       Parent ID             Nonce       Timestamp        Merkle Root
func (sm *StratumMiner) mine() {
	if err := sm.tg.Add(); err != nil {
		return
	}
	defer func() {
		sm.log.Println("Exited mine() thread")
		sm.tg.Done()
	}()

	sm.log.Println("Initializing mining")

	// There should not be another thread mining, and mining should be enabled.
	sm.mu.Lock()
	if sm.mining || !sm.miningOn {
		sm.mu.Unlock()
		return
	}
	sm.mining = true
	sm.mu.Unlock()

	// Solve blocks repeatedly, keeping track of how fast hashing is
	// occurring.
	cycleStart := time.Now()
	var cycleTime int64
	hashesComputed := 0
	nonce := make([]byte, 8, 8)
	for {
		var work *miningWork
		continueMining := true
		sm.mu.Lock()
		select {
		// Kill the thread if 'Stop' has been called
		case <-sm.tg.StopChan():
			sm.miningOn = false
			sm.mining = false
			sm.mu.Unlock()
			return
		// See if we have work available
		case work, continueMining = <-sm.miningWorkChannel:
		// If not, block until we have work or the miningWorkChannel is closed
		default:
			//sm.log.Println("No work ready")
			sm.mu.Unlock()
			//sm.log.Println("blocking on miningWorkChannel")
			work, continueMining = <-sm.miningWorkChannel
			//sm.log.Println("Done blocking on miningWorkChannel")
			sm.mu.Lock()
			//sm.log.Println("Continuing")
		}
		// If the miningWorkChannel has been closed, kill the thread
		if !continueMining {
			sm.log.Println("miningworkchannel has been shut - Halting miner")
			sm.mining = false
			sm.mu.Unlock()
			return
		}

		// Kill the thread if mining has been turned off
		if !sm.miningOn {
			sm.log.Println("Mining has been turned off - halting miner")
			sm.mining = false
			sm.mu.Unlock()
			return
		}
		sm.mu.Unlock()

		binary.BigEndian.PutUint64(nonce, work.Offset)
		for i := 0; i < 8; i++ {
			work.Header[i+32] = nonce[i]
		}
		id := crypto.HashBytes(work.Header)
		/*
			sm.log.Println("nonce: ", nonce[:])
			sm.log.Println("header: ", work.Header[:])
		*/
		//Check if match found
		if bytes.Compare(work.Target[:], id[:]) >= 0 {
			sm.log.Println("Yay, solution found!")

			// Copy nonce to a new header.
			header := append([]byte(nil), work.Header...)
			/*
				for i := 0; i < 8; i++ {
					header[i+32] = nonce[i]
				}
			*/
			go func() {
				if e := sm.Client.SubmitHeader(header, work.Job); e != nil {
					sm.log.Println("Error submitting solution -", e)
				} else {
					sm.submissions += 1
				}
			}()

		}
		hashesComputed += 1

		cycleTime = time.Since(cycleStart).Nanoseconds()
		cycleSeconds := float64(cycleTime / (1000 * 1000))
		// longer than 1 second
		if cycleSeconds > 1 {
			sm.hashRate = float64(hashesComputed) / cycleSeconds
			hashesComputed = 0
			cycleStart = time.Now()
		}

	}

}

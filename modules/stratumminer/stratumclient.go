package stratumminer

import (
	"errors"
	//"fmt"
	"log"
	"math/big"
	"reflect"
	//"strconv"
	"sync"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/types"

	siasync "github.com/HyperspaceApp/Hyperspace/sync"
)

type stratumJob struct {
	JobID        string
	PrevHash     []byte
	Coinbase1    []byte
	Coinbase2    []byte
	MerkleBranch [][]byte
	Version      string
	NBits        string
	NTime        []byte
	CleanJobs    bool
	ExtraNonce2  types.ExtraNonce2
}

//StratumClient is a sia client using the stratum protocol
type StratumClient struct {
	connectionstring string
	User             string

	mutex           sync.Mutex // protects following
	tcpclient       *TcpClient
	tcpinited       bool
	shouldstop      bool
	extranonce1     []byte
	extranonce2Size uint
	target          types.Target
	currentJob      stratumJob

	tg siasync.ThreadGroup
	BaseClient
}

// RestartOnError closes the old tcpclient and starts the process of opening
// a new tcp connection
func (sc *StratumClient) RestartOnError(err error) {
	if err := sc.tg.Add(); err != nil {
		//build.Critical(err)
		// this code is reachable if we stopped before we fully started up
		return
	}
	defer sc.tg.Done()
	log.Println("Error in connection to stratumserver:", err)
	select {
	case <-sc.tg.StopChan():
		log.Println("... tried restarting stratumclient from error, but stop has already been called. exiting")
		return
	default:
	}
	log.Println("blocking on mutex")
	sc.mutex.Lock()
	log.Println("done blocking on mutex")
	sc.tcpinited = false
	sc.mutex.Unlock()
	sc.DeprecateOutstandingJobs()
	log.Println("done deprecating outstanding jobs")
	sc.tcpclient.Close()

	log.Println("  Closed tcpclient")
	time.Sleep(1000 * time.Millisecond)
	// Make sure we haven't called Stop(). If we haven't, try again.
	log.Println("... Restarting stratumclient from error")
	sc.Start()
}

// SubscribeAndAuthorize first attempts to authorize and, if successful,
// subscribes
func (sc *StratumClient) SubscribeAndAuthorize() {
	log.Println("About to launch authorization goroutine")
	// Authorize the miner
	result, err := sc.tcpclient.Call("mining.authorize", []string{sc.User, ""})
	if err != nil {
		log.Println("Unable to authorize:", err)
		return
	}
	log.Println("Authorization of", sc.User, ":", result)

	// Subscribe for mining
	// Closing the connection on an error will cause the client to generate an error,
	// resulting in the errorhandler to be triggered
	result, err = sc.tcpclient.Call("mining.subscribe", []string{"gominer"})
	if err != nil {
		log.Println("subscribe not ok")
		log.Println("ERROR Error in response from stratum:", err)
		return
	}
	log.Println("subscribe ok")
	reply, ok := result.([]interface{})
	if !ok || len(reply) < 3 {
		log.Println("ERROR Invalid response from stratum:", result)
		sc.tcpclient.Close()
		return
	}

	// Keep the extranonce1 and extranonce2_size from the reply
	if sc.extranonce1, err = encoding.HexStringToBytes(reply[1]); err != nil {
		log.Println("ERROR Invalid extrannonce1 from stratum")
		sc.tcpclient.Close()
		return
	}

	extranonce2Size, ok := reply[2].(float64)
	if !ok {
		log.Println("ERROR Invalid extranonce2_size from stratum", reply[2], "type", reflect.TypeOf(reply[2]))
		sc.tcpclient.Close()
		return
	}
	sc.extranonce2Size = uint(extranonce2Size)
}

// Start connects to the stratumserver and processes the notifications
func (sc *StratumClient) Start() {
	log.Println("Starting StratumClient")
	sc.mutex.Lock()
	if sc.shouldstop {
		return
	}
	sc.mutex.Unlock()
	if err := sc.tg.Add(); err != nil {
		build.Critical(err)
	}
	/*
		else {
			// we can get here if we stop the client before we finished starting it,
			// so just return
			return
		}
	*/
	defer func() {
		sc.tg.Done()
		log.Println("Finished Starting StratumClient")
	}()

	select {
	// if we've asked the thread to stop, don't proceed
	case <-sc.tg.StopChan():
		log.Println("tried starting stratumclient, but stop has already been called. exiting")
		return
	default:
	}

	sc.mutex.Lock()

	sc.DeprecateOutstandingJobs()
	sc.tcpclient = &TcpClient{}
	sc.tcpinited = true
	sc.subscribeToStratumDifficultyChanges()
	sc.subscribeToStratumJobNotifications()

	sc.mutex.Unlock()

	// Connect to the stratum server
	for {
		select {
		case <-sc.tg.StopChan():
			return
		default:
		}
		log.Println("Connecting to", sc.connectionstring)
		err := sc.tcpclient.Dial(sc.connectionstring)
		if err != nil {
			//log.Println("TCP dialing failed")
			log.Println("ERROR Error in making tcp connection:", err)
			continue
		} else {
			break
		}
	}
	// In case of an error, drop the current tcpclient and restart
	sc.tcpclient.ErrorCallback = sc.RestartOnError
	sc.SubscribeAndAuthorize()

}

func (sc *StratumClient) subscribeToStratumDifficultyChanges() {
	sc.tcpclient.SetNotificationHandler("mining.set_difficulty", func(params []interface{}) {
		if params == nil || len(params) < 1 {
			log.Println("ERROR No difficulty parameter supplied by stratum server")
			return
		}
		diff, ok := params[0].(float64)
		//diff, err := strconv.ParseFloat(params[0].(string), 64)
		if !ok {
			log.Println("ERROR Invalid difficulty supplied by stratum server:", params[0])
			return
		}
		if diff == 0 {
			log.Println("ERROR Invalid difficulty supplied by stratum server:", diff)
			return
		}
		log.Println("Stratum server changed difficulty to", diff)
		sc.setDifficulty(diff)
	})
}

// Stop shuts down the stratumclient and closes the tcp connection
func (sc *StratumClient) Stop() {
	log.Println("Stopping StratumClient")
	if err := sc.tg.Stop(); err != nil {
		log.Println("Error stopping StratumClient")
	}
	sc.mutex.Lock()
	defer func() {
		sc.mutex.Unlock()
		log.Println("Finished Stopping StratumClient")
	}()
	sc.shouldstop = true
	// we need to make sure we've actually Start()ed before can Close() the tcpclient
	if sc.tcpinited {
		sc.tcpinited = false
		sc.DeprecateOutstandingJobs()
		sc.tcpclient.Close()
	}
}

// Connected returns whether or not the stratumclient has an open tcp
// connection
func (sc *StratumClient) Connected() bool {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	if sc.tcpinited {
		return sc.tcpclient.Connected()
	}
	return false
}

func (sc *StratumClient) subscribeToStratumJobNotifications() {
	sc.tcpclient.SetNotificationHandler("mining.notify", func(params []interface{}) {
		log.Println("New job received from stratum server")
		if params == nil || len(params) < 9 {
			log.Println("ERROR Wrong number of parameters supplied by stratum server")
			return
		}

		sj := stratumJob{}

		sj.ExtraNonce2.Size = sc.extranonce2Size

		var ok bool
		var err error
		if sj.JobID, ok = params[0].(string); !ok {
			log.Println("ERROR Wrong job_id parameter supplied by stratum server")
			return
		}
		if sj.PrevHash, err = encoding.HexStringToBytes(params[1]); err != nil {
			log.Println(params[1])
			log.Println(err)
			log.Println("ERROR Wrong prevhash parameter supplied by stratum server")
			return
		}
		if sj.Coinbase1, err = encoding.HexStringToBytes(params[2]); err != nil {
			log.Println("ERROR Wrong coinb1 parameter supplied by stratum server")
			return
		}
		if sj.Coinbase2, err = encoding.HexStringToBytes(params[3]); err != nil {
			log.Println("ERROR Wrong coinb2 parameter supplied by stratum server")
			return
		}

		//Convert the merklebranch parameter
		merklebranch, ok := params[4].([]interface{})
		if !ok {
			log.Println("ERROR Wrong merkle_branch parameter supplied by stratum server")
			return
		}
		sj.MerkleBranch = make([][]byte, len(merklebranch), len(merklebranch))
		for i, branch := range merklebranch {
			if sj.MerkleBranch[i], err = encoding.HexStringToBytes(branch); err != nil {
				log.Println("ERROR Wrong merkle_branch parameter supplied by stratum server")
				return
			}
		}

		if sj.Version, ok = params[5].(string); !ok {
			log.Println("ERROR Wrong version parameter supplied by stratum server")
			return
		}
		if sj.NBits, ok = params[6].(string); !ok {
			log.Println("ERROR Wrong nbits parameter supplied by stratum server")
			return
		}
		if sj.NTime, err = encoding.HexStringToBytes(params[7]); err != nil {
			log.Println("ERROR Wrong ntime parameter supplied by stratum server")
			return
		}
		if sj.CleanJobs, ok = params[8].(bool); !ok {
			log.Println("ERROR Wrong clean_jobs parameter supplied by stratum server")
			return
		}
		sc.addNewStratumJob(sj)
	})
}

func (sc *StratumClient) addNewStratumJob(sj stratumJob) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.currentJob = sj
	if sj.CleanJobs {
		sc.DeprecateOutstandingJobs()
	}
	sc.AddJobToDeprecate(sj.JobID)
}

// IntToTarget converts a big.Int to a Target.
func intToTarget(i *big.Int) (t types.Target, err error) {
	// Check for negatives.
	if i.Sign() < 0 {
		err = errors.New("Negative target")
		return
	}
	// In the event of overflow, return the maximum.
	if i.BitLen() > 256 {
		err = errors.New("Target is too high")
		return
	}
	b := i.Bytes()
	offset := len(t[:]) - len(b)
	copy(t[offset:], b)
	return
}

func difficultyToTarget(difficulty float64) (target types.Target, err error) {
	diffAsBig := big.NewFloat(difficulty)

	diffOneString := "0x00000000ffff0000000000000000000000000000000000000000000000000000"
	targetOneAsBigInt := &big.Int{}
	targetOneAsBigInt.SetString(diffOneString, 0)

	targetAsBigFloat := &big.Float{}
	targetAsBigFloat.SetInt(targetOneAsBigInt)
	targetAsBigFloat.Quo(targetAsBigFloat, diffAsBig)
	targetAsBigInt, _ := targetAsBigFloat.Int(nil)
	target, err = intToTarget(targetAsBigInt)
	return
}

func (sc *StratumClient) setDifficulty(difficulty float64) {
	target, err := difficultyToTarget(difficulty)
	if err != nil {
		log.Println("ERROR Error setting difficulty to ", difficulty)
	}
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.target = target
	//fmt.Printf("difficulty set to: %d\n", difficulty)
}

//GetHeaderForWork fetches new work from the SIA daemon
func (sc *StratumClient) GetHeaderForWork() (target, header []byte, deprecationChannel chan bool, job interface{}, err error) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()

	job = sc.currentJob
	if sc.currentJob.JobID == "" {
		err = errors.New("No job received from stratum server yet")
		return
	}

	deprecationChannel = sc.GetDeprecationChannel(sc.currentJob.JobID)

	target = sc.target[:]

	//Create the arbitrary transaction
	en2 := sc.currentJob.ExtraNonce2.Bytes()
	err = sc.currentJob.ExtraNonce2.Increment()

	arbtx := []byte{0}
	arbtx = append(arbtx, sc.currentJob.Coinbase1...)
	arbtx = append(arbtx, sc.extranonce1...)
	arbtx = append(arbtx, en2...)
	arbtx = append(arbtx, sc.currentJob.Coinbase2...)
	arbtxHash := crypto.HashBytes(arbtx)

	//Construct the merkleroot from the arbitrary transaction and the merklebranches
	merkleRoot := arbtxHash
	for _, h := range sc.currentJob.MerkleBranch {
		m := append([]byte{1}[:], h...)
		m = append(m, merkleRoot[:]...)
		merkleRoot = crypto.HashBytes(m)
	}

	//Construct the header
	header = make([]byte, 0, 80)
	header = append(header, sc.currentJob.PrevHash...)
	header = append(header, []byte{0, 0, 0, 0, 0, 0, 0, 0}[:]...) //empty nonce
	header = append(header, sc.currentJob.NTime...)
	header = append(header, merkleRoot[:]...)

	return
}

//SubmitHeader reports a solution to the stratum server
func (sc *StratumClient) SubmitHeader(header []byte, job interface{}) (err error) {
	sj, _ := job.(stratumJob)
	nonce := encoding.BytesToHexString(header[32:40])
	encodedExtraNonce2 := encoding.BytesToHexString(sj.ExtraNonce2.Bytes())
	nTime := encoding.BytesToHexString(sj.NTime)
	sc.mutex.Lock()
	c := sc.tcpclient
	sc.mutex.Unlock()
	stratumUser := sc.User
	_, err = c.Call("mining.submit", []string{stratumUser, sj.JobID, encodedExtraNonce2, nTime, nonce})
	if err != nil {
		return
	}
	return
}

package pool

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	// "math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/sasha-s/go-deadlock"
)

const (
	extraNonce2Size = 4
)

// Handler represents the status (open/closed) of each connection
type Handler struct {
	mu     deadlock.RWMutex
	conn   net.Conn
	closed chan bool
	notify chan bool
	p      *Pool
	log    *persist.Logger
	s      *Session
}

const (
	blockTimeout = 5 * time.Second
	// This should represent the max number of pending notifications (new blocks found) within blockTimeout seconds
	// better to have too many, than not enough (causes a deadlock otherwise)
	numPendingNotifies = 20
)

func (h *Handler) parseRequest() (*types.StratumRequest, error) {
	var m types.StratumRequest
	// fmt.Printf("%s: Setting timeout for %v\n", h.s.printID(), time.Now().Add(blockTimeout))
	h.conn.SetReadDeadline(time.Now().Add(blockTimeout))
	//dec := json.NewDecoder(h.conn)
	buf := bufio.NewReader(h.conn)
	select {
	case <-h.p.tg.StopChan():
		return nil, errors.New("Stopping handler")
	case <-h.notify:
		m.Method = "mining.notify"
	default:
		// try reading from the connection
		//err = dec.Decode(&m)
		str, err := buf.ReadString('\n')
		// if we timeout
		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			// h.log.Printf("%s: Harmless timeout occurred\n", h.s.printID())
			//h.conn.SetReadDeadline(time.Time{})
			// check last job time and if over 25 seconds, send a new job.
			if time.Now().Sub(h.s.lastJobTimestamp) > (time.Second * 25) {
				m.Method = "mining.notify"
				break
			}
			if h.s.DetectDisconnected() {
				h.log.Println("Non-responsive disconnect detected!")
				return nil, errors.New("Non-responsive disconnect detected")
			}

			// ratio := h.s.CurrentDifficulty() / h.s.HighestDifficulty()
			// h.log.Printf("Non-responsive disconnect ratio: %f\n", ratio)

			if h.s.checkDiffOnNewShare() {
				err = h.sendSetDifficulty(h.s.CurrentDifficulty())
				if err != nil {
					h.log.Println("Error sending SetDifficulty")
					return nil, err
				}
			}

			return nil, nil
			// if we don't timeout but have some other error
		} else if err != nil {
			if err == io.EOF {
				//fmt.Println("End connection")
				h.log.Println("End connection")
			} else {
				h.log.Println("Unusual error")
				h.log.Println(err)
			}
			return nil, err
		} else {
			// NOTE: we were getting weird cases where the buffer would read a full
			// string, then read part of the string again (like the last few chars).
			// Somehow it seemed like the socket data wasn't getting purged correctly
			// on reading.
			// Not seeing it lately, but that's why we have this debugging code,
			// and that's why we don't just read straight into the JSON decoder.
			// Reading the data into a string buffer lets us debug more easily.

			// If we encounter this issue again, we may want to drop down into
			// lower-level socket manipulation to try to troubleshoot it.
			// h.log.Println(str)
			dec := json.NewDecoder(strings.NewReader(str))
			err = dec.Decode(&m)
			if err != nil {
				h.log.Println("Decoding error")
				h.log.Println(err)
				h.log.Println(str)
				//return nil, err
				// don't disconnect here, just treat it like a harmless timeout
				return nil, nil
			}
		}
	}
	// h.log.Printf("%s: Clearing timeout\n", h.s.printID())
	return &m, nil
}

func (h *Handler) handleRequest(m *types.StratumRequest) error {
	h.s.SetHeartbeat()
	var err error
	switch m.Method {
	case "mining.subscribe":
		return h.handleStratumSubscribe(m)
	case "mining.authorize":
		err = h.handleStratumAuthorize(m)
		if err != nil {
			h.log.Printf("Failed to authorize client\n")
			h.log.Println(err)
			return err
		}
		return h.sendStratumNotify(true)
	case "mining.extranonce.subscribe":
		return h.handleStratumNonceSubscribe(m)
	case "mining.submit":
		return h.handleStratumSubmit(m)
	case "mining.notify":
		h.log.Printf("mining.notify:New block to mine on\n")
		return h.sendStratumNotify(true)
	default:
		h.log.Debugln("Unknown stratum method: ", m.Method)
	}
	return nil
}

// Listen listens on a connection for incoming data and acts on it
func (h *Handler) Listen() {
	defer func() {
		h.closed <- true // send to dispatcher, that connection is closed
		h.conn.Close()
		if h.s != nil && h.s.CurrentWorker != nil {
			// delete a worker record when a session disconnections
			h.s.CurrentWorker.deleteWorkerRecord()
			// when we shut down the pool we get an error here because the log
			// is already closed... TODO
			h.s.log.Printf("Closed worker: %d\n", h.s.CurrentWorker.wr.workerID)
		}
	}()
	err := h.p.tg.Add()
	if err != nil {
		// If this goroutine is not run before shutdown starts, this
		// codeblock is reachable.
		return
	}
	defer h.p.tg.Done()

	h.log.Println("New connection from " + h.conn.RemoteAddr().String())
	h.mu.Lock()
	h.s, _ = newSession(h.p, h.conn.RemoteAddr().String())
	h.mu.Unlock()
	h.log.Println("New session: " + sPrintID(h.s.SessionID))
	for {
		m, err := h.parseRequest()
		h.conn.SetReadDeadline(time.Time{})
		// if we timed out
		if m == nil && err == nil {
			continue
			// else if we got a request
		} else if m != nil {
			err = h.handleRequest(m)
			if err != nil {
				h.log.Println(err)
				return
			}
			// else if we got an error
		} else if err != nil {
			h.log.Println(err)
			return
		}
	}
}

func (h *Handler) sendResponse(r types.StratumResponse) error {
	b, err := json.Marshal(r)
	//fmt.Printf("SERVER: %s\n", b)
	if err != nil {
		h.log.Debugln("json marshal failed for id: ", r.ID, err)
		return err
	}
	b = append(b, '\n')
	_, err = h.conn.Write(b)
	if err != nil {
		h.log.Debugln("connection write failed for id: ", r.ID, err)
		return err
	}
	return nil
}

func (h *Handler) sendRequest(r types.StratumRequest) error {
	b, err := json.Marshal(r)
	if err != nil {
		h.log.Debugln("json marshal failed for id: ", r.ID, err)
		return err
	}
	b = append(b, '\n')
	_, err = h.conn.Write(b)
	h.log.Debugln("sending request: ", string(b))
	if err != nil {
		h.log.Debugln("connection write failed for id: ", r.ID, err)
		return err
	}
	return nil
}

// handleStratumSubscribe message is the first message received and allows the pool to tell the miner
// the difficulty as well as notify, extranonce1 and extranonce2
//
// TODO: Pull the appropriate data from either in memory or persistent store as required
func (h *Handler) handleStratumSubscribe(m *types.StratumRequest) error {
	if len(m.Params) > 0 {
		h.log.Printf("Client subscribe name:%s", m.Params[0].(string))
		h.s.SetClientVersion(m.Params[0].(string))
	}

	if len(m.Params) > 0 && m.Params[0].(string) == "sgminer/4.4.2" {
		h.s.SetHighestDifficulty(4812.8)
		h.s.SetCurrentDifficulty(4812.8)
		h.s.SetDisableVarDiff(true)
	}
	if len(m.Params) > 0 && m.Params[0].(string) == "cgminer/4.9.0" {
		h.s.SetHighestDifficulty(1024)
		h.s.SetCurrentDifficulty(1024)
		h.s.SetDisableVarDiff(true)
	}
	if len(m.Params) > 0 && m.Params[0].(string) == "gominer" {
		h.s.SetHighestDifficulty(0.1)
		h.s.SetCurrentDifficulty(0.1)
	}

	r := types.StratumResponse{ID: m.ID}
	r.Method = m.Method
	/*
		if !h.s.Authorized() {
			r.Result = false
			r.Error = interfaceify([]string{"Session not authorized - authorize before subscribe"})
			return h.sendResponse(r)
		}
	*/

	//	diff := "b4b6693b72a50c7116db18d6497cac52"
	t, _ := h.p.persist.Target.Difficulty().Uint64()
	h.log.Debugf("Block Difficulty: %x\n", t)
	tb := make([]byte, 8)
	binary.LittleEndian.PutUint64(tb, t)
	diff := hex.EncodeToString(tb)
	//notify := "ae6812eb4cd7735a302a8a9dd95cf71f"
	notify := h.s.printID()
	extranonce1 := h.s.printNonce()
	extranonce2 := extraNonce2Size
	raw := fmt.Sprintf(`[ [ ["mining.set_difficulty", "%s"], ["mining.notify", "%s"]], "%s", %d]`, diff, notify, extranonce1, extranonce2)
	r.Result = json.RawMessage(raw)
	r.Error = nil
	return h.sendResponse(r)
}

// this is thread-safe, when we're looking for and possibly creating a client,
// we need to make sure the action is atomic
func (h *Handler) setupClient(client, worker string) (*Client, error) {
	var err error
	h.p.clientSetupMutex.Lock()
	defer h.p.clientSetupMutex.Unlock()
	c := h.p.FindClientDB(client)
	if c == nil {
		//fmt.Printf("Unable to find client: %s\n", client)
		c, err = newClient(h.p, client)
		if err != nil {
			//fmt.Println("Failed to create a new Client")
			h.p.log.Printf("Failed to create a new Client: %s\n", err)
			return nil, err
		}
		err = h.p.AddClientDB(c)
		if err != nil {
			h.p.log.Printf("Failed to add client to DB: %s\n", err)
			return nil, err
		}
	} else {
		//fmt.Printf("Found client: %s\n", client)
	}
	if h.p.Client(client) == nil {
		h.p.log.Printf("Adding client in memory: %s\n", client)
		h.p.AddClient(c)
	}
	return c, nil
}

func (h *Handler) setupWorker(c *Client, workerName string) (*Worker, error) {
	w, err := newWorker(c, workerName, h.s)
	if err != nil {
		c.log.Printf("Failed to add worker: %s\n", err)
		return nil, err
	}

	err = c.addWorkerDB(w)
	if err != nil {
		c.log.Printf("Failed to add worker: %s\n", err)
		return nil, err
	}
	h.s.log = w.log
	c.log.Printf("Adding new worker: %s, %d\n", workerName, w.wr.workerID)
	w.log.Printf("Adding new worker: %s, %d\n", workerName, w.wr.workerID)
	h.log.Debugln("client = " + c.Name() + ", worker = " + workerName)
	return w, nil
}

// handleStratumAuthorize allows the pool to tie the miner connection to a particular user
func (h *Handler) handleStratumAuthorize(m *types.StratumRequest) error {
	var err error

	r := types.StratumResponse{ID: m.ID}

	r.Method = "mining.authorize"
	r.Result = true
	r.Error = nil
	clientName := m.Params[0].(string)
	workerName := ""
	if strings.Contains(clientName, ".") {
		s := strings.SplitN(clientName, ".", -1)
		clientName = s[0]
		workerName = s[1]
	}

	// load wallet and check validity
	var walletTester types.UnlockHash
	err = walletTester.LoadString(clientName)
	if err != nil {
		r.Result = false
		r.Error = interfaceify([]string{"Client Name must be valid wallet address"})
		h.log.Println("Client Name must be valid wallet address. Client name is: " + clientName)
		err = errors.New("Client name must be a valid wallet address")
		h.sendResponse(r)
		return err
	}

	c, err := h.setupClient(clientName, workerName)
	if err != nil {
		return err
	}
	w, err := h.setupWorker(c, workerName)
	if err != nil {
		return err
	}

	// if everything is fine, setup the client and send a response and difficulty
	h.s.addClient(c)
	h.s.addWorker(w)
	h.s.addShift(h.p.newShift(h.s.CurrentWorker))
	h.s.SetAuthorized(true)
	err = h.sendResponse(r)
	if err != nil {
		return err
	}
	err = h.sendSetDifficulty(h.s.CurrentDifficulty())
	if err != nil {
		return err
	}
	return nil
}

// handleStratumNonceSubscribe tells the pool that this client can handle the extranonce info
// TODO: Not sure we have to anything if all our clients support this.
func (h *Handler) handleStratumNonceSubscribe(m *types.StratumRequest) error {
	h.p.log.Debugln("ID = "+strconv.FormatUint(m.ID, 10)+", Method = "+m.Method+", params = ", m.Params)

	// not sure why 3 is right, but ccminer expects it to be 3
	r := types.StratumResponse{ID: 3}
	r.Result = true
	r.Error = nil

	return h.sendResponse(r)
}

// request is sent as [name, jobid, extranonce2, nTime, nonce]
func (h *Handler) handleStratumSubmit(m *types.StratumRequest) error {
	// fmt.Printf("%s: %s Handle submit\n", time.Now(), h.s.printID())
	r := types.StratumResponse{ID: m.ID}
	r.Method = "mining.submit"
	r.Result = true
	r.Error = nil
	/*
		err := json.Unmarshal(m.Params, &p)
		if err != nil {
			h.log.Printf("Unable to parse mining.submit params: %v\n", err)
			r.Result = false //json.RawMessage(`false`)
			r.Error = interfaceify([]string{"20","Parse Error"}) //json.RawMessage(`["20","Parse Error"]`)
		}
	*/
	// name := m.Params[0].(string)
	var jobID uint64
	fmt.Sscanf(m.Params[1].(string), "%x", &jobID)
	extraNonce2 := m.Params[2].(string)
	nTime := m.Params[3].(string)
	nonce := m.Params[4].(string)
	// h.s.log.Printf("Submit jobid:%d nonce: %s, extraNonce2: %s", jobID, nonce, extraNonce2)

	needNewJob := false
	defer func() {
		if needNewJob == true {
			h.sendStratumNotify(true)
		}
	}()

	if h.s.CurrentWorker == nil {
		// worker failed to authorize
		h.log.Printf("Worker failed to authorize - dropping\n")
		return errors.New("Worker failed to authorize - dropping")
	}

	h.s.SetLastShareTimestamp(time.Now())
	sessionPoolDifficulty := h.s.CurrentDifficulty()
	if h.s.checkDiffOnNewShare() {
		h.sendSetDifficulty(h.s.CurrentDifficulty())
		needNewJob = true
	}

	// h.log.Debugln("name = " + name + ", jobID = " + fmt.Sprintf("%X", jobID) + ", extraNonce2 = " + extraNonce2 + ", nTime = " + nTime + ", nonce = " + nonce)
	if h.s.log != nil {
		// h.s.log.Debugln("name = " + name + ", jobID = " + fmt.Sprintf("%X", jobID) + ", extraNonce2 = " + extraNonce2 + ", nTime = " + nTime + ", nonce = " + nonce)
	} else {
		h.log.Debugln("session log not ready")
	}

	var b types.Block
	j, err := h.s.getJob(jobID, nonce)
	if err != nil {
		r.Result = false
		r.Error = interfaceify([]string{"23", err.Error()}) //json.RawMessage(`["21","Stale - old/unknown job"]`)
		h.s.CurrentWorker.IncrementInvalidShares()
		return h.sendResponse(r)
	}
	if j != nil {
		encoding.Unmarshal(j.MarshalledBlock, &b)
	}

	if len(b.MinerPayouts) == 0 {
		r.Result = false
		r.Error = interfaceify([]string{"22", "Stale - old/unknown job"}) //json.RawMessage(`["21","Stale - old/unknown job"]`)
		h.s.CurrentWorker.log.Printf("Stale Share rejected - old/unknown job\n")
		h.s.CurrentWorker.IncrementInvalidShares()
		return h.sendResponse(r)
	}

	bhNonce, err := hex.DecodeString(nonce)
	for i := range b.Nonce { // there has to be a better way to do this in golang
		b.Nonce[i] = bhNonce[i]
	}
	tb, _ := hex.DecodeString(nTime)
	b.Timestamp = types.Timestamp(encoding.DecUint64(tb))

	cointxn := h.p.coinB1()
	ex1, _ := hex.DecodeString(h.s.printNonce())
	ex2, _ := hex.DecodeString(extraNonce2)
	cointxn.ArbitraryData[0] = append(cointxn.ArbitraryData[0], ex1...)
	cointxn.ArbitraryData[0] = append(cointxn.ArbitraryData[0], ex2...)

	b.Transactions = append(b.Transactions, []types.Transaction{cointxn}...)
	blockHash := b.ID()
	bh := new(big.Int).SetBytes(blockHash[:])

	sessionPoolTarget, _ := difficultyToTarget(sessionPoolDifficulty)

	// h.s.CurrentWorker.log.Printf("session diff: %f, block version diff: %s",
	// 	sessionPoolDifficulty, printWithSuffix(sessionPoolTarget.Difficulty()))

	// need to verify that the submission reaches the pool target
	if bytes.Compare(sessionPoolTarget[:], blockHash[:]) < 0 {
		r.Result = false
		r.Error = interfaceify([]string{"22", "Submitted nonce did not reach pool diff target"}) //json.RawMessage(`["21","Stale - old/unknown job"]`)
		h.s.CurrentWorker.log.Printf("Submitted nonce did not reach pool diff target\n")
		h.s.CurrentWorker.log.Printf("Submitted id: %064x\n", bh)
		h.s.CurrentWorker.log.Printf("Session target:   %064x\n", sessionPoolTarget.Int())
		h.s.CurrentWorker.IncrementInvalidShares()
		return h.sendResponse(r)
	}

	t := h.p.persist.GetTarget()
	// h.s.CurrentWorker.log.Printf("Submit block hash is   %064x\n", bh)
	// h.s.CurrentWorker.log.Printf("Chain block target is  %064x\n", t.Int())
	// h.s.CurrentWorker.log.Printf("Difficulty %s/%s\n",
	// 		printWithSuffix(types.IntToTarget(bh).Difficulty()), printWithSuffix(t.Difficulty()))
	if bytes.Compare(t[:], blockHash[:]) < 0 {
		// h.s.CurrentWorker.log.Printf("Block hash is greater than block target\n")
		h.s.CurrentWorker.log.Printf("Share Accepted\n")
		h.s.CurrentWorker.IncrementShares(h.s.CurrentDifficulty(), currencyToAmount(b.MinerPayouts[0].Value))
		h.s.CurrentWorker.SetLastShareTime(time.Now())
		return h.sendResponse(r)
	}
	err = h.p.managedSubmitBlock(b)
	if err != nil && err != modules.ErrBlockUnsolved {
		h.log.Printf("Failed to SubmitBlock(): %v\n", err)
		h.log.Printf(sPrintBlock(b))
		panic(fmt.Sprintf("Failed to SubmitBlock(): %v\n", err))
		r.Result = false //json.RawMessage(`false`)
		r.Error = interfaceify([]string{"20", "Stale share"})
		h.s.CurrentWorker.IncrementInvalidShares()
		return h.sendResponse(r)
	}

	h.s.CurrentWorker.log.Printf("Share Accepted\n")
	h.s.CurrentWorker.IncrementShares(h.s.CurrentDifficulty(), currencyToAmount(b.MinerPayouts[0].Value))
	h.s.CurrentWorker.SetLastShareTime(time.Now())

	// TODO: why not err == nil ?
	if err != modules.ErrBlockUnsolved {
		h.s.CurrentWorker.Parent().log.Printf("Yay!!! Solved a block!!\n")
		// h.s.CurrentWorker.log.Printf("Yay!!! Solved a block!!\n")
		h.s.clearJobs()
		err = h.s.CurrentWorker.addFoundBlock(&b)
		if err != nil {
			h.s.CurrentWorker.log.Printf("Failed to update block in database: %s\n", err)
		}
		h.p.shiftChan <- true
	}
	return h.sendResponse(r)
}

func (h *Handler) sendSetDifficulty(d float64) error {
	var r types.StratumRequest

	r.Method = "mining.set_difficulty"
	// assuming this ID is the response to the original subscribe which appears to be a 1
	r.ID = 0
	params := make([]interface{}, 1)
	r.Params = params
	r.Params[0] = d
	return h.sendRequest(r)
}

func (h *Handler) sendStratumNotify(cleanJobs bool) error {
	var r types.StratumRequest
	r.Method = "mining.notify"
	r.ID = 0 // gominer requires notify to use an id of 0
	job, _ := newJob(h.p)
	h.s.addJob(job)
	jobid := job.printID()
	var b types.Block
	h.p.persist.mu.Lock()
	// make a copy of the block and hold it until the solution is submitted
	job.MarshalledBlock = encoding.Marshal(&h.p.sourceBlock)
	h.p.persist.mu.Unlock()
	encoding.Unmarshal(job.MarshalledBlock, &b)
	job.MerkleRoot = b.MerkleRoot()
	mbj := b.MerkleBranches()
	h.log.Debugf("merkleBranch: %s\n", mbj)

	version := ""
	nbits := fmt.Sprintf("%08x", BigToCompact(h.p.persist.Target.Int()))

	var buf bytes.Buffer
	encoding.WriteUint64(&buf, uint64(b.Timestamp))
	ntime := hex.EncodeToString(buf.Bytes())

	params := make([]interface{}, 9)
	r.Params = params
	r.Params[0] = jobid
	r.Params[1] = b.ParentID.String()
	r.Params[2] = h.p.coinB1Txn()
	r.Params[3] = h.p.coinB2()
	r.Params[4] = mbj
	r.Params[5] = version
	r.Params[6] = nbits
	r.Params[7] = ntime
	r.Params[8] = cleanJobs
	//h.log.Debugf("send.notify: %s\n", raw)
	h.log.Debugf("send.notify: %s\n", r.Params)
	return h.sendRequest(r)
}

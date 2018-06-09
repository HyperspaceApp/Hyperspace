//Package stratum implements the basic stratum protocol.
// This is normal jsonrpc but the go standard library is insufficient since we need features like notifications.
package stratumminer

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	//"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/NebulousLabs/threadgroup"
)

//ErrorCallback is the type of function that be registered to be notified of errors requiring a client
// to be dropped and a new one to be created
type ErrorCallback func(err error)

//NotificationHandler is the signature for a function that handles notifications
type NotificationHandler func(args []interface{})

// TcpClient maintains a connection to the stratum server and (de)serializes requests/reponses/notifications
type TcpClient struct {
	socket    net.Conn
	mu        sync.Mutex // protects connected state
	connected bool

	seqmutex sync.Mutex // protects following
	seq      uint64

	callsMutex   sync.Mutex // protects following
	pendingCalls map[uint64]chan interface{}

	ErrorCallback        ErrorCallback
	notificationHandlers map[string]NotificationHandler

	tg threadgroup.ThreadGroup
}

// Dial connects to a stratum+tcp at the specified network address.
// This function is not threadsafe
// If an error occurs, it is both returned here and through the ErrorCallback of the TcpClient
func (c *TcpClient) Dial(host string) (err error) {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	select {
	case <-c.tg.StopChan():
		return
	default:
	}

	fmt.Println("TcpClient Dialing")
	c.mu.Lock()
	c.connected = false
	c.socket, err = net.Dial("tcp", host)
	c.mu.Unlock()
	select {
	case <-c.tg.StopChan():
		return
	default:
	}
	if err != nil {
		//fmt.Println(err)
		c.dispatchError(err)
		return err
	} else {
		c.tg.OnStop(func() error {
			fmt.Println("TCPClient: Closing c.socket")
			c.cancelAllRequests()
			c.socket.Close()
			return nil
		})
		c.connected = true
	}
	//fmt.Println("TcpClient Done Dialing")
	go c.Listen()
	return
}

// Close releases the tcp connection
func (c *TcpClient) Close() {
	fmt.Println("TcpClient Close() called")
	if err := c.tg.Stop(); err != nil {
		panic(err.Error())
	}
	fmt.Println("Closing TcpClient socket")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	//fmt.Println("Done closing TcpClient")
}

func (c *TcpClient) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Printf("Checking if connected: %t\n", c.connected)
	return c.connected
}

// SetNotificationHandler registers a function to handle notification for a specific method.
// This function is not threadsafe and all notificationhandlers should be set prior to calling the Dial function
func (c *TcpClient) SetNotificationHandler(method string, handler NotificationHandler) {
	if c.notificationHandlers == nil {
		c.notificationHandlers = make(map[string]NotificationHandler)
	}
	c.notificationHandlers[method] = handler
}

func (c *TcpClient) dispatchNotification(n types.StratumNotification) {
	if c.notificationHandlers == nil {
		return
	}
	if notificationHandler, exists := c.notificationHandlers[n.Method]; exists {
		notificationHandler(n.Params)
	}
}

func (c *TcpClient) dispatch(r types.StratumResponse) {
	if r.ID == 0 {
		fmt.Println("Dispatching notification")
		fmt.Println(r.StratumNotification)
		c.dispatchNotification(r.StratumNotification)
		return
	}
	c.callsMutex.Lock()
	defer c.callsMutex.Unlock()
	cb, found := c.pendingCalls[r.ID]
	var result interface{}
	//fmt.Printf("dispatch response: %s\n", r.Result)
	//fmt.Printf("dispatch error: %s\n", r.Error)
	if r.Error != nil {
		message := ""
		if len(r.Error) >= 2 {
			message, _ = r.Error[1].(string)
		}
		result = errors.New(message)
		/*
			var message []byte
			r.Error.UnmarshalJSON(message)
			result = errors.New(string(message[:]))
		*/
	} else {
		result = r.Result
	}
	if found {
		cb <- result
	}
}

func (c *TcpClient) dispatchError(err error) {
	fmt.Println("dispatching error")
	select {
	// don't dispatch any errors if we've been shutdown!
	case <-c.tg.StopChan():
		fmt.Println("stop called, not dispatching error")
		return
	default:
	}
	if c.ErrorCallback != nil {
		c.ErrorCallback(err)
	}
}

//Listen reads data from the open connection, deserializes it and dispatches the reponses and notifications
// This is a blocking function and will continue to listen until an error occurs (io or deserialization)
func (c *TcpClient) Listen() {
	/*
		if err := c.tg.Add(); err != nil {
			build.Critical(err)
		}
		defer c.tg.Done()
	*/
	reader := bufio.NewReader(c.socket)
	for {
		rawmessage, err := reader.ReadString('\n')
		// bail out if we've called stop
		select {
		case <-c.tg.StopChan():
			fmt.Println("TCPCLIENT StopChan called, done Listen()ing")
			return
		default:
		}
		if err != nil {
			fmt.Printf("TCPCLIENT ERR: %s\n", err)
			c.dispatchError(err)
			return
		}
		r := types.StratumResponse{}
		err = json.Unmarshal([]byte(rawmessage), &r)
		if err != nil {
			c.dispatchError(err)
			return
		}
		c.dispatch(r)
	}
}

func (c *TcpClient) registerRequest(requestID uint64) (cb chan interface{}) {
	c.callsMutex.Lock()
	defer c.callsMutex.Unlock()
	if c.pendingCalls == nil {
		c.pendingCalls = make(map[uint64]chan interface{})
	}
	cb = make(chan interface{})
	c.pendingCalls[requestID] = cb
	return
}

func (c *TcpClient) cancelRequest(requestID uint64) {
	c.callsMutex.Lock()
	defer c.callsMutex.Unlock()
	cb, found := c.pendingCalls[requestID]
	if found {
		close(cb)
		delete(c.pendingCalls, requestID)
	}
}

func (c *TcpClient) cancelAllRequests() {
	c.callsMutex.Lock()
	defer c.callsMutex.Unlock()
	for requestID, cb := range c.pendingCalls {
		close(cb)
		delete(c.pendingCalls, requestID)
	}
}

//Call invokes the named function, waits for it to complete, and returns its error status.
func (c *TcpClient) Call(serviceMethod string, args []string) (reply interface{}, err error) {
	//jsonargs, _ := json.Marshal(args)
	//rawargs := json.RawMessage(jsonargs)
	params := make([]interface{}, len(args))
	for i, v := range args {
		params[i] = v
	}
	r := types.StratumRequest{Method: serviceMethod, Params: params}
	//r := types.StratumRequest{Method: serviceMethod, Params: rawargs}

	c.seqmutex.Lock()
	c.seq++
	r.ID = c.seq
	c.seqmutex.Unlock()

	rawmsg, err := json.Marshal(r)
	if err != nil {
		err = fmt.Errorf("json.Marshal failed: %v", err)
		return
	}
	call := c.registerRequest(r.ID)
	defer c.cancelRequest(r.ID)

	rawmsg = append(rawmsg, []byte("\n")...)
	c.mu.Lock()
	if c.connected {
		_, err = c.socket.Write(rawmsg)
	} else {
		err = fmt.Errorf("Can't write to socket, socket has been closed")
		return nil, err
	}
	c.mu.Unlock()
	if err != nil {
		err = fmt.Errorf("socket.Write failed: %v", err)
		return
	}
	//Make sure the request is cancelled if no response is given
	go func() {
		// cancel after 10 seconds
		for timeElapsed := 0; timeElapsed < 10; timeElapsed += 1 {
			// cancel the request if we've called stop
			select {
			case <-c.tg.StopChan():
				return
			default:
				time.Sleep(1 * time.Second)
			}
		}
		c.cancelRequest(r.ID)
	}()
	reply = <-call

	if reply == nil {
		err = errors.New("Timeout")
		return
	}
	err, _ = reply.(error)
	return
}

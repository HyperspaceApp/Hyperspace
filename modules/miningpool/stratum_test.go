package pool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/types"
	"gitlab.com/NebulousLabs/errors"
)

// TestStratumServer would test before and after a connections is made to the server
func TestStratumServer(t *testing.T) {
	//t.Log("TestStratumServer")
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}

	if len(pt.mpool.dispatcher.handlers) != 0 {
		t.Fatal(errors.New(fmt.Sprintf("wrong handler number %d", len(pt.mpool.dispatcher.handlers))))
	}

	time.Sleep(time.Millisecond * 2)

	//t.Logf("listening on port: %v\n", pt.mpool.InternalSettings().PoolNetworkPort)
	socket, err := net.Dial("tcp", fmt.Sprintf(":%d", pt.mpool.InternalSettings().PoolNetworkPort))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond * 2)

	if len(pt.mpool.dispatcher.handlers) != 1 {
		t.Fatal(errors.New(fmt.Sprintf("wrong handler number %d", len(pt.mpool.dispatcher.handlers))))
	}

	socket.Close()

	time.Sleep(time.Millisecond * 2)
	// after connection close, handler should be deleted
	if len(pt.mpool.dispatcher.handlers) != 0 {
		t.Fatal(errors.New(fmt.Sprintf("wrong handler number %d", len(pt.mpool.dispatcher.handlers))))
	}
}

func createAuthorizeRequest(t *testing.T, port int, waitchan chan int, tID uint64, autoclose bool) net.Conn {
	//fmt.Printf("listening on port: %v\n", pt.mpool.InternalSettings().PoolNetworkPort)
	socket, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if autoclose {
			socket.Close()
		}
		//fmt.Println("Closed socket: ", tID)
	}()
	//fmt.Println("listening successfully")

	args := []string{fmt.Sprintf("%s.%s", tAddress, tUser), ""}
	params := make([]interface{}, len(args))
	for i, v := range args {
		params[i] = v
	}
	req := types.StratumRequest{Method: "mining.authorize", Params: params, ID: tID}
	rawmsg, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
		return nil
	}
	rawmsg = append(rawmsg, []byte("\n")...)
	socket.SetWriteDeadline(time.Now().Add(1 * time.Second))
	if waitchan != nil {
		waitchan <- 2
	}
	_, err = socket.Write(rawmsg)
	if err != nil {
		t.Fatal(err)
		return nil
	} else if waitchan != nil {
		waitchan <- 0
	}
	for {
		//fmt.Printf("Creating NewReader for socket\n")
		reader := bufio.NewReader(socket)

		//fmt.Printf("Reading from socket\n")
		rawmessage, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}

		r := types.StratumResponse{}
		err = json.Unmarshal([]byte(rawmessage), &r)
		if err != nil {
			//fmt.Println(string(rawmessage))
			t.Fatal(err)
		}

		if r.ID == tID {
			break
		} else {
			t.Fatal("got a wrong message: ", rawmessage)
		}
	}
	if waitchan != nil {
		waitchan <- 1
	}
	return socket
}

func TestStratumAuthorize(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond * 2)
	port := pt.mpool.InternalSettings().PoolNetworkPort
	createAuthorizeRequest(t, port, nil, 123, true)
}

func TestStratumAuthorizeHeavyLoad(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond * 2)
	port := pt.mpool.InternalSettings().PoolNetworkPort
	// we start getting socket issues when we get into
	// 1000+ - not sure why, but I think the socket buffers
	// may get filled up before we can read them
	num := 100
	numPrepWritten := 0
	numWritten := 0
	numAuthed := 0
	waitchan := make(chan int, num*3)
	for i := 1; i < num+1; i++ {
		go createAuthorizeRequest(t, port, waitchan, uint64(i), true)
		// If we don't sleep for a ms here, we get a deadlock.
		// I'm not sure why.
		time.Sleep(2 * time.Millisecond)
	}
	for {
		select {
		case info := <-waitchan:
			if info == 0 {
				numWritten += 1
			} else if info == 1 {
				numAuthed += 1
			} else if info == 2 {
				numPrepWritten += 1
			}
			//fmt.Printf("Prep Written: %d, Written: %d, Authed: %d, Remaining Connections:  %d, Total Connections Opened: %d\n", numPrepWritten, numWritten, numAuthed, pt.mpool.NumConnections(), pt.mpool.NumConnectionsOpened())
			if numAuthed == num {
				return
			}
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func createSubscribeRequest(t *testing.T, socket net.Conn, waitchan chan int, tID uint64, autoclose bool) net.Conn {
	defer func() {
		if autoclose {
			socket.Close()
		}
	}()

	args := []string{"testminer"}
	params := make([]interface{}, len(args))
	for i, v := range args {
		params[i] = v
	}
	req := types.StratumRequest{Method: "mining.subscribe", Params: params, ID: tID}
	rawmsg, err := json.Marshal(req)
	if err != nil {
		return nil
	}

	rawmsg = append(rawmsg, []byte("\n")...)
	_, err = socket.Write(rawmsg)
	//fmt.Println("Subscribe write completed: ", tID)
	if err != nil {
		return nil
	}

	for {
		reader := bufio.NewReader(socket)

		rawmessage, err := reader.ReadString('\n')
		// fmt.Println(rawmessage)
		if err != nil {
			t.Fatal(err)
		}

		r := types.StratumResponse{}
		err = json.Unmarshal([]byte(rawmessage), &r)
		if err != nil {
			t.Fatal(err)
		}
		if r.ID == tID {
			//fmt.Printf("MATCH %d: %s\n", r.ID, rawmessage)
			if r.Error != nil {
				rErr := r.Error
				t.Fatal(errors.New(rErr[0].(string)))
			}
			resp := r.Result.([]interface{})
			if len(resp) != 3 {
				t.Fatal(errors.New(fmt.Sprintf("wrong response number %d", len(resp))))
			}

			break
		} else {
			//fmt.Printf("%d: %s\n", r.ID, rawmessage)
		}
	}
	//fmt.Println("Subscribe completed: ", tID)
	if waitchan != nil {
		waitchan <- 1
	} else {
		//fmt.Println("NOT sending waitchan: ", tID)
	}
	//fmt.Println("waitchan sent: ", tID)
	return socket
	//fmt.Println("exited TestStratumSubscribe")
}

func TestStratumSubscribe(t *testing.T) {
	//fmt.Println("TestStratumSubscribe")
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond * 2)
	port := pt.mpool.InternalSettings().PoolNetworkPort
	socket := createAuthorizeRequest(t, port, nil, 123, false)
	createSubscribeRequest(t, socket, nil, 123, true)
}

func TestStratumSubscribeHeavyLoad(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond * 2)
	port := pt.mpool.InternalSettings().PoolNetworkPort
	num := 10
	//numPrepWritten := 0
	//numWritten := 0
	numSubscribed := 0
	waitchan := make(chan int, num*3)
	for i := 1; i < num+1; i++ {
		socket := createAuthorizeRequest(t, port, nil, uint64(i), false)
		go createSubscribeRequest(t, socket, waitchan, uint64(i), true)
		// If we don't sleep for a ms here, we get a deadlock.
		// I'm not sure why.
		time.Sleep(2 * time.Millisecond)
	}
	for {
		select {
		case info := <-waitchan:
			if info == 1 {
				numSubscribed += 1
			}
			//fmt.Printf("Subscribed: %d, Remaining Connections:  %d, Total Connections Opened: %d\n", numSubscribed, pt.mpool.NumConnections(), pt.mpool.NumConnectionsOpened())
			if numSubscribed == num {
				return
			}
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
}

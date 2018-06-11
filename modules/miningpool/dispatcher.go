package pool

import (
	"fmt"
	"net"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/NebulousLabs/Sia/persist"
)

// Dispatcher contains a map of ip addresses to handlers
type Dispatcher struct {
	handlers map[string]*Handler
	ln       net.Listener
	mu       deadlock.RWMutex
	p        *Pool
	log      *persist.Logger
	connectionsOpened uint64
}

func (d *Dispatcher) NumConnections() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.handlers)
}

func (d *Dispatcher) NumConnectionsOpened() uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.connectionsOpened
}

func (d *Dispatcher) IncrementConnectionsOpened() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.connectionsOpened += 1
}

//AddHandler connects the incoming connection to the handler which will handle it
func (d *Dispatcher) AddHandler(conn net.Conn) {
	addr := conn.RemoteAddr().String()
	handler := &Handler{
		conn:   conn,
		closed: make(chan bool, 2),
		notify: make(chan bool, numPendingNotifies),
		p:      d.p,
		log:    d.log,
	}
	d.mu.Lock()
	d.handlers[addr] = handler
	d.mu.Unlock()

	fmt.Println("AddHandler listen() called")
	handler.Listen()

	<-handler.closed // when connection closed, remove handler from handlers
	d.mu.Lock()
	delete(d.handlers, addr)
	//fmt.Printf("Exiting AddHandler, %d connections remaining\n", len(d.handlers))
	d.mu.Unlock()
}

// ListenHandlers listens on a passed port and upon accepting the incoming connection,
// adds the handler to deal with it
func (d *Dispatcher) ListenHandlers(port string) {
	var err error
	err = d.p.tg.Add()
	if err != nil {
		// If this goroutine is not run before shutdown starts, this
		// codeblock is reachable.
		return
	}

	d.ln, err = net.Listen("tcp", ":"+port)
	if err != nil {
		d.log.Println(err)
		panic(err)
		// TODO: add error chan to report this
		//return
	}
	fmt.Printf("Listening: %s\n", port)

	defer d.ln.Close()
	defer d.p.tg.Done()

	for {
		var conn net.Conn
		var err error
		select {
		case <-d.p.tg.StopChan():
			//fmt.Println("Closing listener")
			//d.ln.Close()
			//fmt.Println("Done closing listener")
			return
		default:
			conn, err = d.ln.Accept() // accept connection
			d.IncrementConnectionsOpened()
			if err != nil {
				d.log.Println(err)
				continue
			}
		}

		tcpconn := conn.(*net.TCPConn)
		tcpconn.SetKeepAlive(true)
		//tcpconn.SetKeepAlivePeriod(30 * time.Second)
		tcpconn.SetKeepAlivePeriod(15 * time.Second)
		tcpconn.SetNoDelay(true)
		// maybe this will help with our disconnection problems
		tcpconn.SetLinger(2)

		go d.AddHandler(conn)
	}
}

func (d *Dispatcher) NotifyClients() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log.Printf("Notifying %d clients\n", len(d.handlers))
	for _, h := range d.handlers {
		h.notify <- true
	}
}

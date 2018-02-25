package pool

import (
	"path/filepath"
	"sync"

	"github.com/HyperspaceProject/Hyperspace/persist"
	"github.com/HyperspaceProject/Hyperspace/types"
)

//
// A ClientRecord represents the persistent data portion of the Client record
//
type ClientRecord struct {
	clientID uint64
	name     string
	wallet   types.UnlockHash
}

//
// A Client represents a user and may have one or more workers associated with it.  It is primarily used for
// accounting and statistics.
//
type Client struct {
	cr      ClientRecord
	mu      sync.RWMutex
	pool    *Pool
	log     *persist.Logger
	workers map[string]*Worker //worker name to worker pointer mapping
}

// newClient creates a new Client record
func newClient(p *Pool, name string) (*Client, error) {
	var err error
	id := p.newStratumID()
	c := &Client{
		cr: ClientRecord{
			clientID: id(),
			name:     name,
		},
		pool:    p,
		workers: make(map[string]*Worker),
	}
	// check if this worker instance is an oiginal or copy
	if p.Client(name) != nil {
		return c, nil
	}

	// Create the perist directory if it does not yet exist.
	dirname := filepath.Join(p.persistDir, "clients", name)
	err = p.dependencies.mkdirAll(dirname, 0700)
	if err != nil {
		return nil, err
	}

	// Initialize the logger, and set up the stop call that will close the
	// logger.
	c.log, err = p.dependencies.newLogger(filepath.Join(dirname, "client.log"))

	return c, err
}

func (c *Client) Name() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.cr.name
}

func (c *Client) SetName(n string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cr.name = n

}

func (c *Client) Wallet() *types.UnlockHash {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &c.cr.wallet
}

func (c *Client) addWallet(w types.UnlockHash) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cr.wallet = w
}

func (c *Client) Pool() *Pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pool
}

func (c *Client) Worker(wn string) *Worker {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.workers[wn]
}

func (c *Client) AddWorker(w *Worker) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.workers[w.Name()] = w
}

//
// Workers returns a copy of the workers map.  Caution however since the map contains pointers to the actual live
// worker data
//
func (c *Client) Workers() map[string]*Worker {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.workers
}

func (c *Client) printID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return sPrintID(c.cr.clientID)
}

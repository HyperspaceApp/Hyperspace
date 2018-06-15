package pool

import (
	"path/filepath"

	"github.com/sasha-s/go-deadlock"

	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
)

//
// A ClientRecord represents the persistent data portion of the Client record
//
type ClientRecord struct {
	clientID int64
	name     string
	wallet   types.UnlockHash
}

//
// A Client represents a user and may have one or more workers associated with it.  It is primarily used for
// accounting and statistics.
//
type Client struct {
	cr      ClientRecord
	mu      deadlock.RWMutex
	pool    *Pool
	log     *persist.Logger
}

// newClient creates a new Client record
func newClient(p *Pool, name string) (*Client, error) {
	var err error
	// id := p.newStratumID()
	c := &Client{
		cr: ClientRecord{
			name:   name,
		},
		pool:    p,
	}
	c.cr.wallet.LoadString(name)
	// check if this worker instance is an original or copy
	// TODO why do we need to make a copy instead of the original?
	if p.Client(name) != nil {
		//return c, nil
		return p.Client(name), nil
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
	if err != nil {
		return nil, err
	}

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

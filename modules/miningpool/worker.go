package pool

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/NebulousLabs/Sia/persist"
)

//
// A Worker is an instance of one miner.  A Client often represents a user and the worker represents a single miner.  There
// is a one to many client worker relationship
//
type Worker struct {
	mu       sync.RWMutex
	workerID uint64
	name     string
	parent   *Client
	// stats
	sharesThisBlock        uint64
	invalidSharesThisBlock uint64
	staleSharesThisBlock   uint64
	cumulativeDifficulty   float64
	continuousStaleCount   uint64
	blocksFound            uint64
	lastShareTime          time.Time
	// utility
	log *persist.Logger
}

func newWorker(c *Client, name string) (*Worker, error) {
	p := c.Pool()
	id := p.newStratumID()
	w := &Worker{
		workerID:               id(),
		name:                   name,
		parent:                 c,
		sharesThisBlock:        0,
		invalidSharesThisBlock: 0,
		staleSharesThisBlock:   0,
		cumulativeDifficulty:   0.0,
		blocksFound:            0,
	}

	var err error
	// Create the perist directory if it does not yet exist.
	dirname := filepath.Join(p.persistDir, "clients", c.Name())
	err = p.dependencies.mkdirAll(dirname, 0700)
	if err != nil {
		return nil, err
	}

	// Initialize the logger, and set up the stop call that will close the
	// logger.
	w.log, err = p.dependencies.newLogger(filepath.Join(dirname, name+".log"))

	return w, nil
}

func (w *Worker) printID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return sPrintID(w.workerID)
}

func (w *Worker) Name() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.name
}

func (w *Worker) SetName(n string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.name = n
}

func (w *Worker) Parent() *Client {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.parent
}

func (w *Worker) SetParent(p *Client) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.parent = p
}

func (w *Worker) SharesThisBlock() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.sharesThisBlock
}

func (w *Worker) ClearSharesThisBlock() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sharesThisBlock = 0
}

func (w *Worker) IncrementSharesThisBlock(currentDifficulty float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sharesThisBlock++
	w.cumulativeDifficulty += currentDifficulty
}

func (w *Worker) InvalidSharesThisBlock() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.invalidSharesThisBlock
}

func (w *Worker) ClearInvalidSharesThisBlock() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.invalidSharesThisBlock = 0
}

func (w *Worker) IncrementInvalidSharesThisBlock() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.invalidSharesThisBlock++
}

func (w *Worker) StaleSharesThisBlock() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.staleSharesThisBlock
}

func (w *Worker) ClearStaleSharesThisBlock() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.staleSharesThisBlock = 0
}

func (w *Worker) IncrementStaleSharesThisBlock() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.staleSharesThisBlock++
}

func (w *Worker) ContinuousStaleCount() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.continuousStaleCount
}

func (w *Worker) ClearContinuousStaleCount() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.continuousStaleCount = 0
}

func (w *Worker) IncrementContinuousStaleCount() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.continuousStaleCount++
}

func (w *Worker) SetLastShareTime(t time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastShareTime = t
}

func (w *Worker) LastShareTime() time.Time {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastShareTime
}

func (w *Worker) BlocksFound() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.blocksFound
}

func (w *Worker) ClearBlocksFound() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.blocksFound = 0
}

func (w *Worker) IncrementBlocksFound() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.blocksFound++
}

func (w *Worker) CumulativeDifficulty() float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cumulativeDifficulty
}

func (w *Worker) ClearCumulativeDifficulty() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cumulativeDifficulty = 0.0
}

// CurrentDifficulty returns the average difficulty of all instances of this worker
func (w *Worker) CurrentDifficulty() float64 {
	var currentDiff float64
	var workerCount uint64
	pool := w.parent.Pool()
	d := pool.dispatcher
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, h := range d.handlers {
		if h.s.Client == w.Parent() && h.s.CurrentWorker == w {
			currentDiff += h.s.CurrentDifficulty()
			workerCount++
		}
	}

	return currentDiff / float64(workerCount)
}

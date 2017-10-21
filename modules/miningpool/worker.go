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
	// utility
	log *persist.Logger
}

func newWorker(c *Client, name string) (*Worker, error) {
	p := c.Pool()
	id := p.newStratumID()
	w := &Worker{
		workerID: id(),
		name:     name,
		parent:   c,
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

	return w, err
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
	return w.getUint64Field("SharesThisBlock")
}

func (w *Worker) ClearSharesThisBlock() {
	w.setFieldValue("SharesThisBlock", 0)
}

func (w *Worker) IncrementSharesThisBlock(currentDifficulty float64) {
	w.incrementFieldValue("SharesThisBlock")
	value := w.getFloatField("CumulativeDifficulty")
	value += currentDifficulty
	w.setFieldValue("CumulativeDifficulty", value)
}

func (w *Worker) InvalidSharesThisBlock() uint64 {
	return w.getUint64Field("InvalidSharesThisBlock")
}

func (w *Worker) ClearInvalidSharesThisBlock() {
	w.setFieldValue("InvalidSharesThisBlock", 0)
}

func (w *Worker) IncrementInvalidSharesThisBlock() {
	w.incrementFieldValue("InvalidSharesThisBlock")
}

func (w *Worker) StaleSharesThisBlock() uint64 {
	return w.getUint64Field("StaleSharesThisBlock")
}

func (w *Worker) ClearStaleSharesThisBlock() {
	w.setFieldValue("StaleSharesThisBlock", 0)
}

func (w *Worker) IncrementStaleSharesThisBlock() {
	w.incrementFieldValue("StaleSharesThisBlock")
}

func (w *Worker) SetLastShareTime(t time.Time) {
	w.setFieldValue("LastShareTime", t.Unix())
}

func (w *Worker) LastShareTime() time.Time {
	unixTime := w.getUint64Field("LastShareTime")
	return time.Unix(int64(unixTime), 0)
}

func (w *Worker) BlocksFound() uint64 {
	return w.getUint64Field("BlocksFound")
}

func (w *Worker) ClearBlocksFound() {
	w.setFieldValue("BlocksFound", 0)
}

func (w *Worker) IncrementBlocksFound() {
	w.incrementFieldValue("BlocksFound")
}

func (w *Worker) CumulativeDifficulty() float64 {
	return w.getFloatField("CumulativeDifficulty")
}

func (w *Worker) ClearCumulativeDifficulty() {
	w.setFieldValue("CumulativeDifficulty", 0.0)
}

// CurrentDifficulty returns the average difficulty of all instances of this worker
func (w *Worker) CurrentDifficulty() float64 {
	pool := w.parent.Pool()
	d := pool.dispatcher
	d.mu.Lock()
	defer d.mu.Unlock()
	workerCount := uint64(0)
	currentDiff := float64(0.0)
	for _, h := range d.handlers {
		if h.s.Client.Name() == w.Parent().Name() && h.s.CurrentWorker.Name() == w.Name() {
			currentDiff += h.s.CurrentDifficulty()
			workerCount++
		}
	}
	if workerCount == 0 {
		return 0.0
	}
	return currentDiff / float64(workerCount)
}

package pool

import (
	"sync/atomic"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// A Share is how we track each worker's submissions and their difficulty
type Share struct {
	userid          int64
	workerid        int64
	height          int64
	valid           bool
	difficulty      float64
	reward          float64
	blockDifficulty uint64
	shareReward     float64
	shareDifficulty float64
	time            time.Time
}

// A Shift is a period over which a worker submits shares. At the end of the
// period, we record those shares into the database.
type Shift struct {
	mu             deadlock.RWMutex
	shiftID        uint64
	pool           uint64
	worker         *Worker
	blockID        uint64
	shares         []Share
	lastShareTime  time.Time
	startShiftTime time.Time
}

func (p *Pool) newShift(w *Worker) *Shift {
	currentShiftID := atomic.LoadUint64(&p.shiftID)

	s := &Shift{
		shiftID:        currentShiftID,
		pool:           p.InternalSettings().PoolID,
		worker:         w,
		startShiftTime: time.Now(),
		shares:         make([]Share, 0),
	}
	// fmt.Printf("New Shift: %s, block %d, shift %d\n", w.Name(), currentBlock, currentShiftID)
	return s
}

// ShiftID returns the shift's unique ID
func (s *Shift) ShiftID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shiftID
}

// PoolID returns the pool's unique ID. Multiple stratum servers connecting to
// the same database should use unique ids so that workers can be tracked as
// belonging to which server.
func (s *Shift) PoolID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

// Shares returns the slice of shares submitted during the shift
func (s *Shift) Shares() []Share {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shares
}

// IncrementShares adds a new share to the slice of shares processed during
// the shift
func (s *Shift) IncrementShares(share *Share) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.shares = append(s.shares, *share)
}

// IncrementInvalid marks a share as having been invalid
func (s *Shift) IncrementInvalid() {
	s.mu.Lock()
	defer s.mu.Unlock()
	share := &Share{
		userid:   s.worker.Parent().cr.clientID,
		workerid: s.worker.wr.workerID,
		valid:    false,
		time:     time.Now(),
	}
	s.shares = append(s.shares, *share)
}

// LastShareTime returns the most recent time a share was submitted during this
// shift
func (s *Shift) LastShareTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastShareTime
}

// SetLastShareTime specifies the most recent time a share was submitted during
// this shift
func (s *Shift) SetLastShareTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastShareTime = t
}

// StartShiftTime returns the time this shift started
func (s *Shift) StartShiftTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startShiftTime
}

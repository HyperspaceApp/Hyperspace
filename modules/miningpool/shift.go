package pool

import (
	"sync"
	"sync/atomic"
	"time"
)

type Share struct {
	userid int64
	workerid int64
	valid bool
	difficulty float64
	reward float64
	block_difficulty float64
}

type Shift struct {
	mu                   sync.RWMutex
	shiftID              uint64
	pool                 uint64
	worker               *Worker
	blockID              uint64
	shares               []Share
	lastShareTime        time.Time
	startShiftTime       time.Time
}

func (p *Pool) newShift(w *Worker) *Shift {
	currentShiftID := atomic.LoadUint64(&p.shiftID)
	p.mu.RLock()
	defer p.mu.RUnlock()
	currentBlock := p.blockCounter
	s := &Shift{
		shiftID:        currentShiftID,
		pool:           p.InternalSettings().PoolID,
		worker:         w,
		blockID:        currentBlock,
		startShiftTime: time.Now(),
		shares:         make([]Share, 0),
	}
	// fmt.Printf("New Shift: %s, block %d, shift %d\n", w.Name(), currentBlock, currentShiftID)
	return s
}

func (s *Shift) ShiftID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shiftID
}

func (s *Shift) PoolID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

func (s *Shift) BlockID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blockID
}

func (s *Shift) Shares() []Share {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shares
}

func (s *Shift) IncrementShares(currentDifficulty float64, reward float64, block_difficulty float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	share := &Share{
		userid:           s.worker.Parent().cr.clientID,
		workerid:         s.worker.wr.workerID,
		valid:            true,
		difficulty:       currentDifficulty,
		reward:           reward,
		block_difficulty: block_difficulty,
	}
	s.shares = append(s.shares, *share)
}

func (s *Shift) IncrementInvalid() {
	s.mu.Lock()
	defer s.mu.Unlock()
	share := &Share{
		userid:     s.worker.Parent().cr.clientID,
		workerid:   s.worker.wr.workerID,
		valid:      false,
	}
	s.shares = append(s.shares, *share)
}

func (s *Shift) LastShareTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastShareTime
}

func (s *Shift) SetLastShareTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastShareTime = t
}

func (s *Shift) StartShiftTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.startShiftTime
}

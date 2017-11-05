package pool

import (
	"sync"
	"sync/atomic"
	"time"
)

type Shift struct {
	mu                   sync.RWMutex
	shiftID              uint64
	pool                 string
	worker               *Worker
	blockID              uint64
	shares               uint64
	invalidShares        uint64
	staleShares          uint64
	cumulativeDifficulty float64
	lastShareTime        time.Time
}

func (p *Pool) newShift(w *Worker) *Shift {
	currentShiftID := atomic.LoadUint64(&p.shiftID)
	p.mu.RLock()
	currentBlock := p.blockCounter
	p.mu.RUnlock()
	s := &Shift{
		shiftID: currentShiftID,
		pool:    p.id,
		worker:  w,
		blockID: currentBlock,
	}
	return s
}

func (s *Shift) ShiftID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shiftID
}

func (s *Shift) Pool() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pool
}

func (s *Shift) BlockID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blockID
}

func (s *Shift) Shares() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shares
}

func (s *Shift) IncrementShares() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shares++
}

func (s *Shift) Invalid() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.invalidShares
}

func (s *Shift) IncrementInvalid() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidShares++
}

func (s *Shift) Stale() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.staleShares
}

func (s *Shift) IncrementStale() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staleShares++
}

func (s *Shift) CumulativeDifficulty() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cumulativeDifficulty
}

func (s *Shift) IncrementCumulativeDifficulty(more float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cumulativeDifficulty += more
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

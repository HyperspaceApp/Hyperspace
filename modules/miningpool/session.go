package pool

import (
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"

	"github.com/HyperspaceProject/Hyperspace/persist"
)

const (
	numSharesToAverage = 20
	initialDifficulty  = 6.0
)

//
// A Session captures the interaction with a miner client from when they connect until the connection is
// closed.  A session is tied to a single client and has many jobs associated with it
//
type Session struct {
	mu               sync.RWMutex
	SessionID        uint64
	CurrentJobs      []*Job
	lastJobTimestamp time.Time
	Client           *Client
	CurrentWorker    *Worker
	CurrentShift     *Shift
	ExtraNonce1      uint32
	// vardiff
	currentDifficulty    float64
	vardiff              Vardiff
	lastShareSpot        uint64
	shareTimes           [numSharesToAverage]float64
	lastVardiffRetarget  time.Time
	lastVardiffTimestamp time.Time
	// utility
	log *persist.Logger
}

func newSession(p *Pool) (*Session, error) {
	id := p.newStratumID()
	s := &Session{
		SessionID:            id(),
		ExtraNonce1:          uint32(id() & 0xffffffff),
		currentDifficulty:    initialDifficulty,
		lastVardiffRetarget:  time.Now(),
		lastVardiffTimestamp: time.Now(),
	}

	s.vardiff = *s.newVardiff()

	return s, nil
}

func (s *Session) addClient(c *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Client = c
}

func (s *Session) addWorker(w *Worker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentWorker = w
	w.SetSession(s)
}

func (s *Session) addJob(j *Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentJobs = append(s.CurrentJobs, j)
	s.lastJobTimestamp = time.Now()
}

func (s *Session) addShift(shift *Shift) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentShift = shift
}

func (s *Session) Shift() *Shift {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentShift
}

func (s *Session) printID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sPrintID(s.SessionID)
}

func (s *Session) printNonce() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ex1 := make([]byte, 4)
	binary.LittleEndian.PutUint32(ex1, s.ExtraNonce1)
	return hex.EncodeToString(ex1)
}

func (s *Session) LastShareDuration() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shareTimes[s.lastShareSpot]
}

func (s *Session) SetLastShareDuration(seconds float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastShareSpot++
	if s.lastShareSpot == s.vardiff.bufSize {
		s.lastShareSpot = 0
	}
	//	fmt.Printf("Shares per minute: %.2f\n", w.ShareRate()*60)
	s.shareTimes[s.lastShareSpot] = seconds
}

func (s *Session) ShareDurationAverage() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var average float64
	for i := uint64(0); i < s.vardiff.bufSize; i++ {
		average += s.shareTimes[i]
	}
	return average / float64(s.vardiff.bufSize)
}

func (s *Session) CurrentDifficulty() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentDifficulty
}

func (s *Session) SetCurrentDifficulty(d float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentDifficulty = d
}

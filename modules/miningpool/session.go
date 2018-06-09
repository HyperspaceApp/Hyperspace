package pool

import (
	"encoding/binary"
	"encoding/hex"
	"sync"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/persist"
)

const (
	numSharesToAverage = 30
	// we can drop to 1% of the highest difficulty before we decide we're disconnected
	maxDifficultyDropRatio = 0.01
	// how long we allow the session to linger if we haven't heard from the worker
	heartbeatTimeout = 3 * time.Minute
)

var (
	initialDifficulty = build.Select(build.Var{
		Standard: 6400.0,
		Dev:      6400.0,
		Testing:  0.00001,
	}).(float64) // change from 6.0 to 1.0
)

//
// A Session captures the interaction with a miner client from when they connect until the connection is
// closed.  A session is tied to a single client and has many jobs associated with it
//
type Session struct {
	mu               sync.RWMutex
	authorized       bool
	SessionID        uint64
	CurrentJobs      []*Job
	lastJobTimestamp time.Time
	Client           *Client
	CurrentWorker    *Worker
	CurrentShift     *Shift
	ExtraNonce1      uint32
	// vardiff
	currentDifficulty     float64
	highestDifficulty     float64
	vardiff               Vardiff
	lastShareSpot         uint64
	shareTimes            [numSharesToAverage]time.Time
	lastVardiffRetarget   time.Time
	lastVardiffTimestamp  time.Time
	sessionStartTimestamp time.Time
	lastHeartbeat         time.Time
	// utility
	log *persist.Logger
}

func newSession(p *Pool) (*Session, error) {
	id := p.newStratumID()
	s := &Session{
		SessionID:            id(),
		ExtraNonce1:          uint32(id() & 0xffffffff),
		currentDifficulty:    initialDifficulty,
		highestDifficulty:    initialDifficulty,
		lastVardiffRetarget:  time.Now(),
		lastVardiffTimestamp: time.Now(),
	}

	s.vardiff = *s.newVardiff()

	s.sessionStartTimestamp = time.Now()
	s.SetHeartbeat()

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

// SetLastShareTimestamp add a new time stamp
func (s *Session) SetLastShareTimestamp(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.log != nil {
		s.log.Printf("Shares index: %d %s\n", s.lastShareSpot, t)
	}
	s.shareTimes[s.lastShareSpot] = t
	s.lastShareSpot++
	if s.lastShareSpot == s.vardiff.bufSize {
		s.lastShareSpot = 0
	}
}

// ShareDurationAverage caculate the average duration of the
func (s *Session) ShareDurationAverage() (float64, float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var minTime, maxTime time.Time
	var timestampCount int

	for i := uint64(0); i < s.vardiff.bufSize; i++ {
		// s.log.Printf("ShareDurationAverage: %d %s %t\n", i, s.shareTimes[i], s.shareTimes[i].IsZero())
		if s.shareTimes[i].IsZero() {
			continue
		}
		timestampCount++
		if minTime.IsZero() {
			minTime = s.shareTimes[i]
		}
		if maxTime.IsZero() {
			maxTime = s.shareTimes[i]
		}
		if s.shareTimes[i].Before(minTime) {
			minTime = s.shareTimes[i]
		}
		if s.shareTimes[i].After(maxTime) {
			maxTime = s.shareTimes[i]
		}
	}

	var unsubmitStart time.Time
	if maxTime.IsZero() {
		unsubmitStart = s.sessionStartTimestamp
	} else {
		unsubmitStart = maxTime
	}
	unsubmitDuration := time.Now().Sub(unsubmitStart).Seconds()
	if timestampCount < 2 { // less than 2 stamp
		return unsubmitDuration, 0
	}

	historyDuration := maxTime.Sub(minTime).Seconds() / float64(timestampCount-1)

	return unsubmitDuration, historyDuration
}

func (s *Session) CurrentDifficulty() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentDifficulty
}

func (s *Session) SetHighestDifficulty(d float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.highestDifficulty = d
}

func (s *Session) SetCurrentDifficulty(d float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d > s.highestDifficulty {
		s.highestDifficulty = d
	}
	s.currentDifficulty = d
}

func (s *Session) HighestDifficulty() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.highestDifficulty
}

func (s *Session) DetectDisconnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// disconnect if we haven't heard from the worker for a long time
	if time.Now().After(s.lastHeartbeat.Add(heartbeatTimeout)) {
		return true
	}
	// disconnect if the worker's difficulty has dropped too far from it's historical diff
	if (s.currentDifficulty / s.highestDifficulty) < maxDifficultyDropRatio {
		return true
	} else {
		return false
	}
}

func (s *Session) SetAuthorized(b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorized = b
}

func (s *Session) Authorized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authorized
}

func (s *Session) SetHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastHeartbeat = time.Now()
}

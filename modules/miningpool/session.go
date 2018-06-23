package pool

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"time"

	"github.com/sasha-s/go-deadlock"

	"github.com/NebulousLabs/Sia/build"
	"github.com/NebulousLabs/Sia/persist"
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
	mu               deadlock.RWMutex
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
	log            *persist.Logger
	disableVarDiff bool
	clientVersion  string
	remoteAddr     string
}

func newSession(p *Pool, ip string) (*Session, error) {
	id := p.newStratumID()
	s := &Session{
		SessionID:            id(),
		ExtraNonce1:          uint32(id() & 0xffffffff),
		currentDifficulty:    initialDifficulty,
		highestDifficulty:    initialDifficulty,
		lastVardiffRetarget:  time.Now(),
		lastVardiffTimestamp: time.Now(),
		disableVarDiff:       false,
		remoteAddr:           ip,
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
	if s.log != nil && s.CurrentJobs != nil && j != nil {
		s.log.Printf("after new job len:%d, (id: %d)\n", len(s.CurrentJobs), j.JobID)
	}
	// for i, j := range s.CurrentJobs {
	// 	s.log.Printf("i: %d, id: %d\n", i, j.JobID)
	// }
	s.lastJobTimestamp = time.Now()
}

func (s *Session) getJob(jobID uint64, nonce string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log.Printf("submit id:%d, before pop len:%d\n", jobID, len(s.CurrentJobs))
	for _, j := range s.CurrentJobs {
		// s.log.Printf("i: %d, array id: %d\n", i, j.JobID)
		if jobID == j.JobID {
			s.log.Printf("after pop len:%d\n", len(s.CurrentJobs))
			if _, ok := j.SubmitedNonce[nonce]; ok {
				return nil, errors.New("already submited nonce for this job")
			}
			return j, nil
		}
	}

	return nil, nil // for stale/unkonwn job response
}

func (s *Session) clearJobs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.log != nil && s.CurrentJobs != nil {
		s.log.Printf("Before job clear:%d\n-----------Job clear---------\n", len(s.CurrentJobs))
	}
	s.CurrentJobs = nil
}

func (s *Session) addShift(shift *Shift) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.CurrentShift = shift
}

// Shift returns the current Shift associated with a session
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

// IsStable checks if the session has been running long enough to fill up the
// vardiff buffer
func (s *Session) IsStable() bool {
	if s.shareTimes[s.vardiff.bufSize-1].IsZero() {
		return false
	}
	s.log.Printf("is stable!")
	return true
}

// CurrentDifficulty returns the session's current difficulty
func (s *Session) CurrentDifficulty() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currentDifficulty
}

// SetHighestDifficulty records the highest difficulty the session has seen
func (s *Session) SetHighestDifficulty(d float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.highestDifficulty = d
}

// SetCurrentDifficulty sets the current difficulty for the session
func (s *Session) SetCurrentDifficulty(d float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d > s.highestDifficulty {
		s.highestDifficulty = d
	}
	s.currentDifficulty = d
}

// SetClientVersion sets the current client version for the session
func (s *Session) SetClientVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientVersion = v
}

// SetDisableVarDiff sets the disable var diff flag for the session
func (s *Session) SetDisableVarDiff(flag bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disableVarDiff = flag
}

// HighestDifficulty returns the highest difficulty the session has seen
func (s *Session) HighestDifficulty() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.highestDifficulty
}

// DetectDisconnected checks to see if we haven't heard from a client for too
// long of a time. It does this via 2 mechanisms:
// 1) how long ago was the last share submitted? (the hearbeat)
// 2) how low has the difficulty dropped from the highest difficulty the client
//    ever faced?
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
	}
	return false
}

// SetAuthorized specifies whether or not the session has been authorized
func (s *Session) SetAuthorized(b bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authorized = b
}

// Authorized returns whether or not the session has been authorized
func (s *Session) Authorized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.authorized
}

// SetHeartbeat indicates that we just received a share submission
func (s *Session) SetHeartbeat() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastHeartbeat = time.Now()
}

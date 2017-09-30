package pool

import (
	"encoding/binary"
	"encoding/hex"
	"time"

	"github.com/NebulousLabs/Sia/persist"
)

const (
	numSharesToAverage = 20
)

//
// A Session captures the interaction with a miner client from whewn they connect until the connection is
// closed.  A session is tied to a single client and has many jobs associated with it
//
type Session struct {
	SessionID     uint64
	CurrentJobs   []*Job
	Client        *Client
	CurrentWorker *Worker
	ExtraNonce1   uint32
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
		currentDifficulty:    4.0,
		lastVardiffRetarget:  time.Now(),
		lastVardiffTimestamp: time.Now(),
	}

	s.vardiff = *s.newVardiff()

	return s, nil
}

func (s *Session) addClient(c *Client) {
	s.Client = c
}

func (s *Session) addWorker(w *Worker) {
	s.CurrentWorker = w
}

func (s *Session) addJob(j *Job) {
	s.CurrentJobs = append(s.CurrentJobs, j)
}

func (s *Session) printID() string {
	return sPrintID(s.SessionID)
}

func (s *Session) printNonce() string {
	ex1 := make([]byte, 4)
	binary.LittleEndian.PutUint32(ex1, s.ExtraNonce1)
	return hex.EncodeToString(ex1)
}

func (s *Session) LastShareDuration() float64 {
	return s.shareTimes[s.lastShareSpot]
}

func (s *Session) SetLastShareDuration(seconds float64) {
	s.lastShareSpot++
	if s.lastShareSpot == s.vardiff.bufSize {
		s.lastShareSpot = 0
	}
	//	fmt.Printf("Shares per minute: %.2f\n", w.ShareRate()*60)
	s.shareTimes[s.lastShareSpot] = seconds
}

func (s *Session) ShareDurationAverage() float64 {
	var average float64
	for i := uint64(0); i < s.vardiff.bufSize; i++ {
		average += s.shareTimes[i]
	}
	return average / float64(s.vardiff.bufSize)
}

func (s *Session) CurrentDifficulty() float64 {
	return s.currentDifficulty
}

func (s *Session) SetCurrentDifficulty(d float64) {
	s.currentDifficulty = d
}

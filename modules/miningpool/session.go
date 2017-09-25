package pool

import (
	"encoding/binary"
	"encoding/hex"
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
}

func newSession(p *Pool) (*Session, error) {
	id := p.newStratumID()
	s := &Session{
		SessionID:   id(),
		ExtraNonce1: uint32(id() & 0xffffffff),
	}
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

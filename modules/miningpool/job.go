package pool

import (
	"github.com/NebulousLabs/Sia/crypto"
	"github.com/NebulousLabs/Sia/types"
)

//
// A Job in the stratum server is a unit of work which is passed to the client miner to solve.  It is primarily
// identified by a Job ID and this is used to keep track of what work has been assigned to each client
//
type Job struct {
	JobID      uint64
	Block      types.Block
	MerkleRoot crypto.Hash
}

func newJob(p *Pool) (*Job, error) {
	id := p.newStratumID()
	j := &Job{
		JobID: id(),
	}
	return j, nil
}

func (j *Job) printID() string {
	return sPrintID(j.JobID)
}

package pool

import (
	"sync"
	"sync/atomic"

	"github.com/NebulousLabs/Sia/types"
)

//
// A Accounting in the stratum server represents the relationship between a SIA block and the client/worker that
// solved it.  It is primarily identified by the Block ID
//
type Accounting struct {
	mu           sync.RWMutex
	blockID      types.BlockID
	blockCounter uint64
	clientShares map[string]uint64
}

func newAccounting(p *Pool, bi types.BlockID) (*Accounting, error) {
	ba := &Accounting{
		blockID:      bi,
		clientShares: make(map[string]uint64),
		blockCounter: p.incrementBlockCount(),
	}
	return ba, nil
}

func (ba *Accounting) resetClient(name string) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	ba.clientShares[name] = 0
}

func (ba *Accounting) incrementClientShares(name string, shares uint64) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	if _, ok := ba.clientShares[name]; ok == false {
		ba.clientShares[name] = 0
	}
	ba.clientShares[name] += shares
}

func (p *Pool) incrementBlockCount() uint64 {
	p.log.Debugf("Waiting to lock pool\n")
	p.mu.Lock()
	defer func() {
		p.log.Debugf("Unlocking pool\n")
		p.mu.Unlock()
	}()
	atomic.AddUint64(&p.blockCounter, 1)
	return p.blockCounter

}

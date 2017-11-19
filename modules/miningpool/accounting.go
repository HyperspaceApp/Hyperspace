package pool

import (
	"sync"

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
	clientShares map[string]float64
}

func newAccounting(p *Pool, bi types.BlockID) (*Accounting, error) {
	ba := &Accounting{
		blockID:      bi,
		clientShares: make(map[string]float64),
		blockCounter: p.BlockCount(),
	}
	return ba, nil
}

func (ba *Accounting) resetClient(name string) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	ba.clientShares[name] = 0.0
}

func (ba *Accounting) incrementClientShares(name string, shares float64) {
	ba.mu.Lock()
	defer ba.mu.Unlock()
	if _, ok := ba.clientShares[name]; ok == false {
		ba.clientShares[name] = 0.0
	}
	ba.clientShares[name] += shares
}

func (p *Pool) BlockCount() uint64 {
	p.mu.RLock()
	defer func() {
		p.mu.RUnlock()
	}()
	return p.blockCounter

}

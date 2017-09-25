package pool

import (
	"errors"
	"time"

	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
)

var (
	errLateHeader = errors.New("header is old, block could not be recovered")
)

// newSourceBlock creates a new source block for the block manager so that new
// headers will use the updated source block.
func (p *Pool) newSourceBlock() {
	// To guarantee garbage collection of old blocks, delete all header entries
	// that have not been reached for the current block.
	for p.memProgress%(HeaderMemory/BlockMemory) != 0 {
		delete(p.blockMem, p.headerMem[p.memProgress])
		delete(p.arbDataMem, p.headerMem[p.memProgress])
		p.memProgress++
		if p.memProgress == HeaderMemory {
			p.memProgress = 0
		}
	}

	// Update the source block.
	block := p.persist.GetUnsolvedBlockPtr()

	// Update the timestamp.
	if block.Timestamp < types.CurrentTimestamp() {
		block.Timestamp = types.CurrentTimestamp()
	}

	block.MinerPayouts = []types.SiacoinOutput{{
		Value:      block.CalculateSubsidy(p.persist.BlockHeight + 1),
		UnlockHash: p.persist.GetSettings().PoolOperatorWallet,
	}}
	p.saveSync()
	p.sourceBlock = block
	p.sourceBlockTime = time.Now()
}

// managedSubmitBlock takes a solved block and submits it to the blockchain.
func (p *Pool) managedSubmitBlock(b types.Block) error {
	// Give the block to the consensus set.
	err := p.cs.AcceptBlock(b)
	// Add the miner to the blocks list if the only problem is that it's stale.
	if err == modules.ErrNonExtendingBlock {
		// p.log.Debugf("Waiting to lock pool\n")
		p.mu.Lock()

		// p.log.Debugf("Unlocking pool\n")
		p.mu.Unlock()
		p.log.Println("Mined a stale block - block appears valid but does not extend the blockchain")
		return err
	}
	if err == modules.ErrBlockUnsolved {
		// p.log.Println("Mined an unsolved block - header submission appears to be incorrect")
		return err
	}
	if err != nil {
		p.tpool.PurgeTransactionPool()
		p.log.Println("ERROR: an invalid block was submitted:", err)
		return err
	}
	// p.log.Debugf("Waiting to lock pool\n")
	p.mu.Lock()
	defer func() {
		// p.log.Debugf("Unlocking pool\n")
		p.mu.Unlock()
	}()

	// Grab a new address for the miner. Call may fail if the wallet is locked
	// or if the wallet addresses have been exhausted.
	p.persist.SetBlocksFound(append(p.persist.GetBlocksFound(), b.ID()))
	// var uc types.UnlockConditions
	// uc, err = p.wallet.NextAddress()
	// if err != nil {
	// 	return err
	// }
	// p.persist.Address = uc.UnlockHash()
	return p.saveSync()
}

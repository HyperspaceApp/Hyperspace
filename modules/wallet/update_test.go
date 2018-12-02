package wallet

import (
	"testing"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// TestUpdate tests that the wallet processes consensus updates properly.
func TestUpdate(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester("TestUpdate", modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a few more blocks to get past the hardfork height.
	for i := types.BlockHeight(0); i <= 2; i++ {
		b, _ := wt.miner.FindBlock()
		err := wt.cs.AcceptBlock(b)
		if err != nil {
			t.Fatal(err)
		}
	}

	// mine a block and add it to the consensus set
	b, err := wt.miner.FindBlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.cs.AcceptBlock(b); err != nil {
		t.Fatal(err)
	}
	// since the miner is mining into a wallet address, the wallet should have
	// added a new transaction
	_, ok, err := wt.wallet.Transaction(types.TransactionID(b.ID()))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no record of miner transaction")
	}

	// revert the block
	wt.wallet.ProcessConsensusChange(modules.ConsensusChange{
		RevertedBlocks: []types.Block{b},
	})
	// transaction should no longer be present
	_, ok, err = wt.wallet.Transaction(types.TransactionID(b.ID()))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("miner transaction was not removed after block was reverted")
	}

	// create a transaction
	addr, _ := wt.wallet.NextAddress()
	txnSet, err := wt.wallet.SendSiacoins(types.SiacoinPrecision.Mul64(10), addr.UnlockHash())
	if err != nil {
		t.Fatal(err)
	}

	// mine blocks until transaction is confirmed, while building up a cc that will revert all the blocks we add
	var revertCC modules.ConsensusChange
	for i := types.BlockHeight(0); i <= types.MaturityDelay; i++ {
		b, _ := wt.miner.FindBlock()
		if err := wt.cs.AcceptBlock(b); err != nil {
			t.Fatal(err)
		}
		revertCC.RevertedBlocks = append([]types.Block{b}, revertCC.RevertedBlocks...)
	}

	// transaction should be present
	_, ok, err = wt.wallet.Transaction(txnSet[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("no record of transaction")
	}

	// revert all the blocks
	wt.wallet.ProcessConsensusChange(revertCC)
	_, ok, err = wt.wallet.Transaction(txnSet[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("transaction was not removed")
	}
}

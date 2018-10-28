package wallet

import (
	"testing"
	"time"
	"unsafe"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// TestTransactionsFillWallet mines many blocks and checks that the wallet's
// outputs are built in a manner to fill the txset
func TestTransactionsFillWallet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	dustOutputValue := types.NewCurrency64(10000)
	noutputs := defragThreshold*15 + 1

	tbuilder, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}

	// Mine blocks and overload the txset
	for {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
		so, err := wt.wallet.getSortedOutputs()
		if err != nil {
			t.Fatal(err)
		}
		tbuilder.FundOutput(so.outputs[0],
			types.NewCurrency64(25).Mul(types.SiacoinPrecision))

		if (tbuilder.Size() >= modules.TransactionSizeLimit - 2e3) {
			break
		}
	}

	// Add another block to push the number of outputs over the threshold
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Wait some time, then mine another block
	time.Sleep(time.Second * 5)

	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	wt.wallet.mu.Lock()
	var dest types.UnlockHash
	for k := range wt.wallet.keys {
		dest = k
		break
	}
	wt.wallet.mu.Unlock()

	t.Log(unsafe.Sizeof(types.SiacoinOutput{
		Value:      dustOutputValue,
		UnlockHash: dest,
	}))

	for i := 0; i < noutputs; i++ {
		tbuilder.AddOutput(types.SiacoinOutput{
			Value:      dustOutputValue,
			UnlockHash: dest,
		})
	}

	txns, err := tbuilder.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	err = wt.tpool.AcceptTransactionSet(txns)
	if err != nil {
		t.Fatal(err)
	}

	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Second)

	wt.wallet.mu.Lock()
	// force a sync because bucket stats may not be reliable until commit
	wt.wallet.syncDB()
	wt.wallet.mu.Unlock()
}

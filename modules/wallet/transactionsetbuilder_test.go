package wallet

import (
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

func TestFundOutputTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), &modules.ProductionDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	b, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}
	uc1, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	amount1 := types.NewCurrency64(1000)
	output1 := types.SiacoinOutput{
		Value:      amount1,
		UnlockHash: uc1.UnlockHash(),
	}
	minerFee := types.NewCurrency64(750)

	// Wallet starts off with large inputs from mining blocks, larger than our
	// combined outputs and miner fees
	err = b.FundOutput(output1, minerFee)
	if err != nil {
		t.Fatal(err)
	}
	unfinishedTxn, _ := b.View()

	// Here we should have 3 outputs, the two specified plus a refund
	if len(unfinishedTxn.SiacoinOutputs) != 2 {
		t.Fatal("incorrect number of outputs generated")
	}
	if len(unfinishedTxn.MinerFees) != 1 {
		t.Fatal("miner fees were not generated but should have been")
	}
	if unfinishedTxn.MinerFees[0].Cmp(minerFee) != 0 {
		t.Fatal("miner fees were not generated but should have been")
	}

	// General construction seems ok, let's sign and submit it to the tpool
	txSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	// If the tpool accepts it, everything looks good
	err = wt.tpool.AcceptTransactionSet(txSet)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFundOutputsTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), &modules.ProductionDependencies{})
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	b, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}
	uc1, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	uc2, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	amount1 := types.NewCurrency64(1000)
	amount2 := types.NewCurrency64(2000)
	output1 := types.SiacoinOutput{
		Value:      amount1,
		UnlockHash: uc1.UnlockHash(),
	}
	output2 := types.SiacoinOutput{
		Value:      amount2,
		UnlockHash: uc2.UnlockHash(),
	}
	minerFee := types.NewCurrency64(750)

	// Wallet starts off with large inputs from mining blocks, larger than our
	// combined outputs and miner fees
	err = b.FundOutputs([]types.SiacoinOutput{output1, output2}, minerFee)
	if err != nil {
		t.Fatal(err)
	}
	unfinishedTxn, _ := b.View()

	// Here we should have 3 outputs, the two specified plus a refund
	if len(unfinishedTxn.SiacoinOutputs) != 3 {
		t.Fatal("incorrect number of outputs generated")
	}
	if len(unfinishedTxn.MinerFees) != 1 {
		t.Fatal("miner fees were not generated but should have been")
	}
	if unfinishedTxn.MinerFees[0].Cmp(minerFee) != 0 {
		t.Fatal("miner fees were not generated but should have been")
	}

	// General construction seems ok, let's sign and submit it to the tpool
	txSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	// If the tpool accepts it, everything looks good
	err = wt.tpool.AcceptTransactionSet(txSet)
	if err != nil {
		t.Fatal(err)
	}
}

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

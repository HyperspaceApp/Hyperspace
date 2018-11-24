package wallet

import (
	"sync"
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

	// Here we should have 2 outputs, the one specified plus a refund
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
func TestFundOutputsFillWalletTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	tbuilder, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}

	uc1, err := wt.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	numBlocks := 25
	var outputs []types.SiacoinOutput

	// Mine blocks and overload the txset
	for i := 0; i < numBlocks; i++ {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 5000; i++ {
		amount := types.NewCurrency64(100000000)
		output := types.SiacoinOutput{
			Value:      amount,
			UnlockHash: uc1.UnlockHash(),
		}
		outputs = append(outputs, output)
	}

	err = tbuilder.FundOutputs(outputs, types.NewCurrency64(10))
	if (err != nil) {
		t.Fatal(err)
	}

	// Add another block
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
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

// TestConcurrentBuildersTransactionSet checks that multiple transaction builders can safely
// be opened at the same time, and that they will make valid transactions when
// building concurrently.
func TestConcurrentBuildersTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	// Mine a few more blocks so that the wallet has lots of outputs to pick
	// from.
	for i := 0; i < 5; i++ {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Get a baseline balance for the wallet.
	startingSCConfirmed, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	startingOutgoing, startingIncoming, err := wt.wallet.UnconfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingOutgoing.IsZero() {
		t.Fatal(startingOutgoing)
	}
	if !startingIncoming.IsZero() {
		t.Fatal(startingIncoming)
	}

	// Create two builders at the same time, then add money to each.
	builder1, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}
	builder2, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}

	// Get a second reading on the wallet's balance.
	fundedSCConfirmed, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingSCConfirmed.Equals(fundedSCConfirmed) {
		t.Fatal("confirmed siacoin balance changed when no blocks have been mined", startingSCConfirmed, fundedSCConfirmed)
	}

	// Spend the transaction funds on miner fees and the void output.
	fee := types.NewCurrency64(25).Mul(types.SiacoinPrecision)
	// Send the money to the void.
	output := types.SiacoinOutput{Value: types.NewCurrency64(9975).Mul(types.SiacoinPrecision)}
	err = builder1.FundOutput(output, fee)
	if err != nil {
		t.Fatal(err)
	}
	err = builder2.FundOutput(output, fee)
	if err != nil {
		t.Fatal(err)
	}
	// Sign the transactions and verify that both are valid.
	tset1, err := builder1.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	tset2, err := builder2.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(tset1)
	if err != nil {
		t.Fatal(err)
	}
	err = wt.tpool.AcceptTransactionSet(tset2)
	if err != nil {
		t.Fatal(err)
	}

	// Mine a block to get the transaction sets into the blockchain.
	_, err = wt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
}

// TestDoubleSignError checks that an error is returned if there is a problem
// when trying to call 'Sign' on a transaction twice.
func TestDoubleSignErrorTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	// Create a transaction, add money to it, and then call sign twice.
	b, err := wt.wallet.StartTransactionSet()
	if err != nil {
		t.Fatal(err)
	}
	txnFund := types.NewCurrency64(100e9)
	// Send the money to the void.
	output := types.SiacoinOutput{Value: txnFund}
	err = b.FundOutput(output, txnFund)
	if err != nil {
		t.Fatal(err)
	}
	txnSet, err := b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}
	txnSet2, err := b.Sign(true)
	if err != errBuilderAlreadySigned {
		t.Error("the wrong error is being returned after a double call to sign")
	}
	if err != nil && txnSet2 != nil {
		t.Error("errored call to sign did not return a nil txn set")
	}
	err = wt.tpool.AcceptTransactionSet(txnSet)
	if err != nil {
		t.Fatal(err)
	}
}

// TestParallelBuilders checks that multiple transaction builders can safely be
// opened at the same time, and that they will make valid transactions when
// building concurrently, using multiple gothreads to manage the builders.
func TestParallelBuildersTransactionSet(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	wt, err := createWalletTester(t.Name(), modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.closeWt()

	// Mine a few more blocks so that the wallet has lots of outputs to pick
	// from.
	outputsDesired := 10
	for i := 0; i < outputsDesired; i++ {
		_, err := wt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Add MatruityDelay blocks with no payout to make tracking the balance
	// easier.
	for i := types.BlockHeight(0); i < types.MaturityDelay+1; i++ {
		err = wt.addBlockNoPayout()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Get a baseline balance for the wallet.
	startingSCConfirmed, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	startingOutgoing, startingIncoming, err := wt.wallet.UnconfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !startingOutgoing.IsZero() {
		t.Fatal(startingOutgoing)
	}
	if !startingIncoming.IsZero() {
		t.Fatal(startingIncoming)
	}

	// Create several builders in parallel.
	var wg sync.WaitGroup
	funding := types.NewCurrency64(10e3).Mul(types.SiacoinPrecision)
	for i := 0; i < outputsDesired; i++ {
		wg.Add(1)
		go func() {
			// Create the builder and fund the transaction.
			builder, err := wt.wallet.StartTransactionSet()
			if err != nil {
				t.Fatal(err)
			}
			output := types.SiacoinOutput{Value: types.NewCurrency64(9975).Mul(types.SiacoinPrecision)}
			err = builder.FundOutput(output, types.NewCurrency64(25).Mul(types.SiacoinPrecision))
			if err != nil {
				t.Fatal(err)
			}
			// Sign the transactions and verify that both are valid.
			tset, err := builder.Sign(true)
			if err != nil {
				t.Fatal(err)
			}
			err = wt.tpool.AcceptTransactionSet(tset)
			if err != nil {
				t.Fatal(err)
			}
			wg.Done()
		}()
	}
	wg.Wait()

	// Mine a block to get the transaction sets into the blockchain.
	err = wt.addBlockNoPayout()
	if err != nil {
		t.Fatal(err)
	}

	// Check the final balance.
	endingSCConfirmed, err := wt.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	expected := startingSCConfirmed.Sub(funding.Mul(types.NewCurrency64(uint64(outputsDesired))))
	if !expected.Equals(endingSCConfirmed) {
		t.Fatal("did not get the expected ending balance", expected, endingSCConfirmed, startingSCConfirmed)
	}
}

func TestDropTransactionSet(t *testing.T) {
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

	// Drop the transaction, then rebuild it and try to get it accepted
	b.Drop()

	// Wallet starts off with large inputs from mining blocks, larger than our
	// combined outputs and miner fees
	err = b.FundOutputs([]types.SiacoinOutput{output1, output2}, minerFee)
	if err != nil {
		t.Fatal(err)
	}
	unfinishedTxn, _ = b.View()

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
	txSet, err = b.Sign(true)
	if err != nil {
		t.Fatal(err)
	}

	// If the tpool accepts it, everything looks good
	err = wt.tpool.AcceptTransactionSet(txSet)
	if err != nil {
		t.Fatal(err)
	}
}

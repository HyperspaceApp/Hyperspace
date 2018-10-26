package consensus

import (
	"fmt"
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
	"github.com/HyperspaceApp/Hyperspace/types"
)

func spvConsensusSetTester(name string, deps modules.Dependencies) (*consensusSetTester, error) {
	testdir := build.TempDir(modules.ConsensusDir, name)
	log.Printf("path: %s", testdir)

	// Create modules.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir), false)
	if err != nil {
		return nil, err
	}
	cs, err := NewCustomConsensusSet(g, false, filepath.Join(testdir, modules.ConsensusDir), deps, true)
	if err != nil {
		return nil, err
	}
	tp, err := transactionpool.New(cs, g, filepath.Join(testdir, modules.ConsensusDir))
	if err != nil {
		return nil, err
	}
	w, err := wallet.New(cs, tp, filepath.Join(testdir, modules.WalletDir), modules.DefaultAddressGapLimit, false)
	if err != nil {
		return nil, err
	}
	key := crypto.GenerateSiaKey(crypto.TypeDefaultWallet)
	_, err = w.Encrypt(key)
	if err != nil {
		return nil, err
	}
	err = w.Unlock(key)
	if err != nil {
		return nil, err
	}

	// Assemble all objects into a consensusSetTester.
	cst := &consensusSetTester{
		gateway:   g,
		tpool:     tp,
		wallet:    w,
		walletKey: key,

		cs: cs,

		persistDir: testdir,
	}
	return cst, nil
}

func (cst *consensusSetTester) CloseSPV() error {
	errs := []error{
		cst.cs.Close(),
		cst.gateway.Close(),
	}
	if err := build.JoinErrors(errs, "; "); err != nil {
		panic(err)
	}
	return nil
}

func createSPVConsensusSetTester(name string) (*consensusSetTester, error) {
	cst, err := spvConsensusSetTester(name, modules.ProdDependencies)
	if err != nil {
		return nil, err
	}
	return cst, nil
}

func TestSPVConsensusSync(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst1, err := createSPVConsensusSetTester(t.Name() + "1")
	if err != nil {
		t.Fatal(err)
	}
	defer cst1.CloseSPV()
	cst2, err := createConsensusSetTester(t.Name() + "2")
	if err != nil {
		t.Fatal(err)
	}
	defer cst2.Close()

	// mine on cst2 until it is above cst1
	for cst1.cs.dbBlockHeight() >= cst2.cs.dbBlockHeight() {
		b, _ := cst2.miner.FindBlock()
		err = cst2.cs.AcceptBlock(b)
		if err != nil {
			t.Fatal(err)
		}
	}

	// connect gateways, triggering a Synchronize
	err = cst1.gateway.Connect(cst2.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}

	// blockchains should now match
	for i := 0; i < 50; i++ {
		if cst1.cs.dbCurrentBlockID() != cst2.cs.dbCurrentBlockID() {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if cst1.cs.dbCurrentBlockID() != cst2.cs.dbCurrentBlockID() {
		t.Fatal("Synchronize failed")
	}

	// Mine on cst2 until it is more than 'MaxCatchUpBlocks' ahead of cst1.
	// NOTE: we have to disconnect prior to this, otherwise cst2 will relay
	// blocks to cst1.
	cst1.gateway.Disconnect(cst2.gateway.Address())
	cst2.gateway.Disconnect(cst1.gateway.Address())
	for cst2.cs.dbBlockHeight() < cst1.cs.dbBlockHeight()+3+MaxCatchUpBlocks {
		_, err := cst2.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	// reconnect
	err = cst1.gateway.Connect(cst2.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}

	// block heights should now match
	for i := 0; i < 50; i++ {
		if cst1.cs.dbBlockHeight() != cst2.cs.dbBlockHeight() {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if cst1.cs.dbBlockHeight() != cst2.cs.dbBlockHeight() {
		t.Fatal("synchronize failed")
	}

	// extend cst2 with a "bad" (old) block, and synchronize. cst1 should
	// reject the bad block.
	cst2.cs.mu.Lock()
	id, err := cst2.cs.dbGetPath(0)
	if err != nil {
		t.Fatal(err)
	}
	cst2.cs.dbPushPath(id)
	cst2.cs.mu.Unlock()

	// Sleep for a few seconds to allow the network call between the two time
	// to occur.
	time.Sleep(5 * time.Second)
	if cst1.cs.dbBlockHeight() == cst2.cs.dbBlockHeight() {
		t.Fatal("cst1 did not reject bad block")
	}
}

// test miner payout detection (delayed diffs)

func waitTillSync(cst1, cst2 *consensusSetTester, t *testing.T) {
	// blockchains should now match
	for i := 0; i < 50; i++ {
		if cst1.cs.dbCurrentBlockID() != cst2.cs.dbCurrentBlockID() {
			time.Sleep(250 * time.Millisecond)
		}
	}

	if cst1.cs.dbCurrentBlockID() != cst2.cs.dbCurrentBlockID() {
		t.Fatal("Synchronize failed")
	}
}

// TestSPVBalance test txn detection
func TestSPVBalance(t *testing.T) {
	log.Printf("TestSPVBalance start!")
	if testing.Short() {
		t.SkipNow()
	}
	cst1, err := createSPVConsensusSetTester(t.Name() + "1")
	if err != nil {
		t.Fatal(err)
	}
	defer cst1.CloseSPV()

	uc, err := cst1.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	cst2, err := createConsensusSetTester(t.Name() + "2")
	if err != nil {
		t.Fatal(err)
	}
	defer cst2.Close()
	// 2 wallet with same seed

	cst2.wallet.SendSiacoins(types.SiacoinPrecision, uc.UnlockHash())
	cst2.mineSiacoins()
	// balance1
	// connect gateways, triggering a Synchronize
	err = cst1.gateway.Connect(cst2.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}
	waitTillSync(cst1, cst2, t)

	balance1, err := cst1.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !balance1.Equals(types.SiacoinPrecision) {
		t.Fatal(fmt.Printf("balance should be 1 XSC but is %s\n", balance1.String()))
	}

	_, err = cst2.wallet.SendSiacoins(types.SiacoinPrecision, uc.UnlockHash())
	if err != nil {
		t.Fatal(err)
	}
	cst2.mineSiacoins()

	waitTillSync(cst1, cst2, t)
	// balance2
	balance2, err := cst1.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !balance2.Equals(types.SiacoinPrecision.MulFloat(2.0)) {
		t.Fatal(fmt.Printf("balance should be 2 XSC but is %s\n", balance2.String()))
	}

	uc2, err := cst1.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	_, err = cst2.wallet.SendSiacoins(types.SiacoinPrecision, uc2.UnlockHash())
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)

	cst2.mineSiacoins()

	waitTillSync(cst1, cst2, t)
	time.Sleep(2 * time.Millisecond)
	// balance3
	balance3, err := cst1.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	if !balance3.Equals(types.SiacoinPrecision.MulFloat(3.0)) {
		t.Fatal(fmt.Printf("balance should be 3 XSC but is %s\n", balance3.String()))
	}

	testSendFromSPV(cst1, cst2, t)
}

func TestSPVDelayedOutputDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst1, err := createSPVConsensusSetTester(t.Name() + "1")
	if err != nil {
		t.Fatal(err)
	}
	defer cst1.CloseSPV()
	cst2, err := createConsensusSetTester(t.Name() + "2")
	if err != nil {
		t.Fatal(err)
	}
	defer cst2.Close()
	// 2 wallet with same seed

	uc, err := cst1.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	block, err := cst2.miner.AddBlockWithAddress(uc.UnlockHash())
	if err != nil {
		t.Fatal(err)
	}
	cst2.mineSiacoins()

	// balance1
	// connect gateways, triggering a Synchronize
	err = cst1.gateway.Connect(cst2.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}
	waitTillSync(cst1, cst2, t)

	balance1, err := cst1.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}

	if !balance1.Equals(block.MinerPayouts[0].Value) {
		t.Fatal(fmt.Printf("balance should be equal minerpayout, %s %s\n",
			balance1.String(), block.MinerPayouts[0].Value.String()))
	}

	testSendFromSPV(cst1, cst2, t)
}

func testSendFromSPV(cst1, cst2 *consensusSetTester, t *testing.T) {
	balanceBefore1, err := cst1.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	cst3, err := createSPVConsensusSetTester(t.Name() + "3")
	if err != nil {
		t.Fatal(err)
	}
	defer cst3.CloseSPV()

	err = cst3.gateway.Connect(cst2.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}
	waitTillSync(cst3, cst2, t)
	balanceBefore3, err := cst3.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}

	uc, err := cst3.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}

	txns, err := cst1.wallet.SendSiacoins(types.SiacoinPrecision, uc.UnlockHash())
	if err != nil {
		t.Fatal(err)
	}

	// _, err = cst2.miner.AddBlockWithAddress(types.UnlockHash{})
	// if err != nil {
	// 	t.Fatal(err)
	// }
	cst2.mineSiacoins()

	waitTillSync(cst1, cst2, t)
	waitTillSync(cst3, cst2, t)

	balanceAfter1, err := cst1.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	balanceAfter3, err := cst3.wallet.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	var totalFee types.Currency
	for _, txn := range txns {
		for _, fee := range txn.MinerFees {
			totalFee = totalFee.Add(fee)
		}
	}

	if !balanceAfter3.Equals(balanceBefore3.Add(types.SiacoinPrecision)) {
		t.Fatal(fmt.Printf("balanceAfter3 not match, %s = %s + %s\n",
			balanceAfter3, balanceBefore3, types.SiacoinPrecision))
	}
	if !balanceBefore1.Equals(balanceAfter1.Add(types.SiacoinPrecision).Add(totalFee)) {
		t.Fatal(fmt.Printf("balanceAfter1 not match, %s = %s + %s\n",
			balanceBefore1, balanceAfter1, types.SiacoinPrecision.Add(totalFee)))
	}
}

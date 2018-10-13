package consensus

import (
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
)

func spvConsensusSetTester(name string, deps modules.Dependencies) (*consensusSetTester, error) {
	testdir := build.TempDir(modules.ConsensusDir, name)
	log.Printf("path: %s", testdir)

	// Create modules.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir))
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

func createSPVConsensusSetTester(name string) (*consensusSetTester, error) {
	cst, err := spvConsensusSetTester(name, modules.ProdDependencies)
	if err != nil {
		return nil, err
	}
	return cst, nil
}

func TestSPVConsensusSync(t *testing.T) {
	// if testing.Short() {
	// 	t.SkipNow()
	// }
	cst1, err := createSPVConsensusSetTester(t.Name() + "1")
	if err != nil {
		t.Fatal(err)
	}
	defer cst1.Close()
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

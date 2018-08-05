package consensus

import (
	"path/filepath"
	"testing"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/miner"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
	"github.com/HyperspaceApp/Hyperspace/types"
	"gitlab.com/NebulousLabs/fastrand"
)

// A consensusSetTester is the helper object for consensus set testing,
// including helper modules and methods for controlling synchronization between
// the tester and the modules.
type consensusSetTester struct {
	gateway   modules.Gateway
	miner     modules.TestMiner
	tpool     modules.TransactionPool
	wallet    modules.Wallet
	walletKey crypto.TwofishKey

	cs *ConsensusSet

	persistDir string
}

// randAddress returns a random address that is not spendable.
func randAddress() (uh types.UnlockHash) {
	fastrand.Read(uh[:])
	return
}

// mineCoins mines blocks until there are siacoins in the wallet.
func (cst *consensusSetTester) mineSiacoins() {
	for i := types.BlockHeight(0); i <= types.MaturityDelay; i++ {
		b, _ := cst.miner.FindBlock()
		err := cst.cs.AcceptBlock(b)
		if err != nil {
			panic(err)
		}
	}
}

// blankConsensusSetTester creates a consensusSetTester that has only the
// genesis block.
func blankConsensusSetTester(name string, deps modules.Dependencies) (*consensusSetTester, error) {
	testdir := build.TempDir(modules.ConsensusDir, name)

	// Create modules.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir))
	if err != nil {
		return nil, err
	}
	cs, err := NewCustomConsensusSet(g, false, filepath.Join(testdir, modules.ConsensusDir), deps)
	if err != nil {
		return nil, err
	}
	tp, err := transactionpool.New(cs, g, filepath.Join(testdir, modules.ConsensusDir))
	if err != nil {
		return nil, err
	}
	w, err := wallet.New(cs, tp, filepath.Join(testdir, modules.WalletDir))
	if err != nil {
		return nil, err
	}
	key := crypto.GenerateTwofishKey()
	_, err = w.Encrypt(key)
	if err != nil {
		return nil, err
	}
	err = w.Unlock(key)
	if err != nil {
		return nil, err
	}
	m, err := miner.New(cs, tp, w, filepath.Join(testdir, modules.MinerDir))
	if err != nil {
		return nil, err
	}

	// Assemble all objects into a consensusSetTester.
	cst := &consensusSetTester{
		gateway:   g,
		miner:     m,
		tpool:     tp,
		wallet:    w,
		walletKey: key,

		cs: cs,

		persistDir: testdir,
	}
	return cst, nil
}

// createConsensusSetTester creates a consensusSetTester that's ready for use,
// including siacoins available in the wallet.
func createConsensusSetTester(name string) (*consensusSetTester, error) {
	cst, err := blankConsensusSetTester(name, modules.ProdDependencies)
	if err != nil {
		return nil, err
	}
	cst.mineSiacoins()
	return cst, nil
}

// Close safely closes the consensus set tester. Because there's not a good way
// to errcheck when deferring a close, a panic is called in the event of an
// error.
func (cst *consensusSetTester) Close() error {
	errs := []error{
		cst.cs.Close(),
		cst.gateway.Close(),
		cst.miner.Close(),
	}
	if err := build.JoinErrors(errs, "; "); err != nil {
		panic(err)
	}
	return nil
}

// TestNilInputs tries to create new consensus set modules using nil inputs.
func TestNilInputs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	testdir := build.TempDir(modules.ConsensusDir, t.Name())
	_, err := New(nil, false, testdir)
	if err != errNilGateway {
		t.Fatal(err)
	}
}

// TestClosing tries to close a consenuss set.
func TestDatabaseClosing(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	testdir := build.TempDir(modules.ConsensusDir, t.Name())

	// Create the gateway.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir))
	if err != nil {
		t.Fatal(err)
	}
	cs, err := New(g, false, testdir)
	if err != nil {
		t.Fatal(err)
	}
	err = cs.Close()
	if err != nil {
		t.Error(err)
	}
}

package api

import (
	"log"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/consensus"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/renter"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// assembleSPVTester creates a bunch of modules and assembles them into a
// server tester, without creating any directories or mining any blocks.
func assembleSPVServerTester(key crypto.CipherKey, testdir string) (*serverTester, error) {
	// assembleServerTester should not get called during short tests, as it
	// takes a long time to run.
	if testing.Short() {
		panic("assembleSPVServerTester called during short tests")
	}

	// Create the modules.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir), true)
	if err != nil {
		return nil, err
	}
	cs, err := consensus.New(g, false, filepath.Join(testdir, modules.ConsensusDir), true)
	if err != nil {
		return nil, err
	}
	tp, err := transactionpool.New(cs, g, filepath.Join(testdir, modules.TransactionPoolDir))
	if err != nil {
		return nil, err
	}
	w, err := wallet.New(cs, tp, filepath.Join(testdir, modules.WalletDir), modules.DefaultAddressGapLimit, false)
	if err != nil {
		return nil, err
	}
	encrypted, err := w.Encrypted()
	if err != nil {
		return nil, err
	}
	if !encrypted {
		_, err = w.Encrypt(key)
		if err != nil {
			return nil, err
		}
	}
	err = w.Unlock(key)
	if err != nil {
		return nil, err
	}
	// m, err := miner.New(cs, tp, w, filepath.Join(testdir, modules.MinerDir))
	// if err != nil {
	// 	return nil, err
	// }
	// h, err := host.New(cs, g, tp, w, "localhost:0", filepath.Join(testdir, modules.HostDir))
	// if err != nil {
	// 	return nil, err
	// }
	r, err := renter.New(g, cs, w, tp, filepath.Join(testdir, modules.RenterDir))
	if err != nil {
		return nil, err
	}
	srv, err := NewServer("localhost:0", "Hyperspace-Agent", "", cs, nil, g, nil, nil, r, tp, w, nil, nil, nil)
	if err != nil {
		return nil, err
	}

	// Assemble the serverTester.
	st := &serverTester{
		cs:        cs,
		gateway:   g,
		host:      nil,
		miner:     nil,
		renter:    r,
		tpool:     tp,
		wallet:    w,
		walletKey: key,

		server: srv,

		dir: testdir,
	}

	// TODO: A more reasonable way of listening for server errors.
	go func() {
		listenErr := srv.Serve()
		if listenErr != nil {
			panic(listenErr)
		}
	}()
	return st, nil
}

// createServerTester creates a server tester object that is ready for testing,
// including money in the wallet and all modules initialized.
func createSPVServerTester(name string) (*serverTester, error) {
	// createServerTester is expensive, and therefore should not be called
	// during short tests.
	if testing.Short() {
		panic("createServerTester called during short tests")
	}

	// Create the testing directory.
	testdir := build.TempDir("api", name)
	log.Println(testdir)

	key := crypto.GenerateSiaKey(crypto.TypeDefaultWallet)
	st, err := assembleSPVServerTester(key, testdir)
	if err != nil {
		return nil, err
	}

	return st, nil
}

// mineCoins mines blocks until there are siacoins in the wallet.
func (st *serverTester) mineSiacoins() {
	time.Sleep(20 * time.Millisecond) // wait for tpool sync
	for i := types.BlockHeight(0); i <= types.MaturityDelay; i++ {
		b, err := st.miner.FindBlock()
		if err != nil {
			panic(err)
		}
		// log.Printf("b:%d %s, %s", i+1, b.ID(), b.ParentID)
		err = st.cs.AcceptBlock(b)
		if err != nil {
			panic(err)
		}
	}
}

func (st *serverTester) fundRenter(t *testing.T, hostTester *serverTester) error {
	uc, err := st.wallet.NextAddress()
	if err != nil {
		t.Fatal(err)
	}
	_, err = hostTester.wallet.SendSiacoins(types.SiacoinPrecision.MulFloat(20000.0), uc.UnlockHash()) // 20k, double of default allowance
	if err != nil {
		t.Fatal(err)
	}
	hostTester.mineSiacoins()

	return nil
}

package pool

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	fileConfig "github.com/HyperspaceApp/Hyperspace/config"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/consensus"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
)

func newPoolTesterWitConfig(name string, port int, poolConfig fileConfig.MiningPoolConfig) (*poolTester, error) {
	testdir := build.TempDir(modules.PoolDir, name)

	// Create the modules.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir), false)
	if err != nil {
		return nil, err
	}
	cs, err := consensus.New(g, false, filepath.Join(testdir, modules.ConsensusDir), false)
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

	// sql to create user: CREATE USER 'miningpool_test'@'localhost' IDENTIFIED BY 'miningpool_test';GRANT ALL PRIVILEGES ON *.* TO 'miningpool_test'@'localhost' WITH GRANT OPTION;CREATE USER 'miningpool_test'@'%' IDENTIFIED BY 'miningpool_test';GRANT ALL PRIVILEGES ON *.* TO 'miningpool_test'@'%' WITH GRANT OPTION;flush privileges;
	//
	createPoolDBConnection := fmt.Sprintf("%s:%s@tcp(%s:%s)/", tdbUser, tdbPass, tdbAddress, tdbPort)
	err = createPoolDatabase(createPoolDBConnection, tdbName)
	if err != nil {
		return nil, err
	}

	if port == 0 {
		port, err = GetFreePort()
		if err != nil {
			return nil, err
		}
	}
	poolConfig.PoolNetworkPort = port

	mpool, err := New(cs, tp, g, w, filepath.Join(testdir, modules.PoolDir), poolConfig)

	if err != nil {
		return nil, err
	}

	// Assemble the poolTester.
	pt := &poolTester{
		gateway: g,
		cs:      cs,
		tpool:   tp,
		wallet:  w,

		mpool: mpool,

		persistDir: testdir,
	}

	return pt, nil
}

func TestStratumClientVersionDiffMap(t *testing.T) {
	if !build.POOL {
		return
	}
	configDiff := float64(1.0)
	poolConfig := fileConfig.MiningPoolConfig{
		PoolName:         "miningpool_test",
		PoolID:           98,
		PoolDBConnection: fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", tdbUser, tdbPass, tdbAddress, tdbPort, tdbName),
		PoolWallet:       tPoolWallet,
		Difficulty:       map[string]string{"testminer": fmt.Sprintf("%f", configDiff)},
	}

	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	pt2, err := newPoolTesterWitConfig(t.Name(), 0, poolConfig)
	defer pt2.Close()
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond * 2)
	port := pt.mpool.InternalSettings().PoolNetworkPort
	socket := createAuthorizeRequest(t, port, nil, 123, false)
	createSubscribeRequest(t, socket, nil, 123, false)
	for _, handler := range pt.mpool.dispatcher.handlers {
		if handler.s.currentDifficulty != initialDifficulty {
			t.Fatal(fmt.Errorf("diff should be %f, but is : %f", initialDifficulty, handler.s.currentDifficulty))
		}
	}
	socket.Close()

	port = pt2.mpool.InternalSettings().PoolNetworkPort
	socket = createAuthorizeRequest(t, port, nil, 123, false)
	createSubscribeRequest(t, socket, nil, 123, false)
	for _, handler := range pt2.mpool.dispatcher.handlers {
		if handler.s.currentDifficulty != configDiff {
			t.Fatal(fmt.Errorf("diff should be %f but is: %f", configDiff, handler.s.currentDifficulty))
		}
	}
	socket.Close()
}

package stratumminer

import (
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
)

type stratumminerTester struct {
	//mpool modules.Pool
	stratumminer *StratumMiner

	persistDir  string
}

func newStratumMinerTester(name string) (*stratumminerTester, error) {
	testdir := build.TempDir(modules.StratumMinerDir, name)
	sm, err := New(testdir)
	if err != nil {
		return nil, err
	}
	smTester := &stratumminerTester{
		stratumminer: sm,

		persistDir: testdir,
	}
	return smTester, nil
}

func TestIntegrationStratumMiner(t *testing.T) {
	_, err := newStratumMinerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
}

func TestBadServerStratumMinerStart(t *testing.T) {
	st, err := newStratumMinerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	poolAddress := "stratum+tcp://localhost:3333"
	walletAddress := "1234"
	st.stratumminer.StartStratumMining(poolAddress, walletAddress)
	// give the miner a bit to start running
	time.Sleep(10 * time.Millisecond)
	if st.stratumminer.Connected() {
		t.Fatal("stratum miner was started with a bad address but is reporting its status as connected")
	}
}

func TestStratumMinerStartAndStop(t *testing.T) {
	st, err := newStratumMinerTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	poolAddress := "stratum+tcp://localhost:3333"
	walletAddress := "1234"
	st.stratumminer.StartStratumMining(poolAddress, walletAddress)
	// give the miner a bit to start running
	time.Sleep(10 * time.Millisecond)
	if !st.stratumminer.Mining() {
		t.Fatal("stratum miner was started but is not reporting its status as mining")
	}
	st.stratumminer.StopStratumMining()
	// give the miner a bit to stop running
	time.Sleep(100 * time.Millisecond)
	if st.stratumminer.Mining() {
		t.Fatal("stratum miner was stopped but is reporting its status as mining")
	}
}

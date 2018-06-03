package pool

import (
	"testing"
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/NebulousLabs/errors"
)

func TestBlockForWork(t *testing.T) {
	//t.Log("TestBlockForWork")
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	if err != nil {
		t.Fatal(err)
	}

	b := pt.mpool.blockForWork()
	if(b.MinerPayouts[0].Value.String() != "299999000000000000000000000000") {
		t.Fatal(errors.New(fmt.Sprintf("wrong block payout value: %s", b.MinerPayouts[0].Value.String())))
	}

	if(b.MinerPayouts[0].UnlockHash.String() != tPoolWallet) {
		t.Fatal(errors.New(fmt.Sprintf("wrong block miner address: %s", b.MinerPayouts[0].UnlockHash.String())))
	}

	if(len(b.Transactions) != 0) {
		t.Fatal(errors.New(fmt.Sprintf("wrong tx number %d", len(b.Transactions))))
	}

}

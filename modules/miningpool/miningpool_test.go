package pool

import (
	"database/sql"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	fileConfig "github.com/HyperspaceApp/Hyperspace/config"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/consensus"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/stratumminer"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/NebulousLabs/fastrand"

	"github.com/NebulousLabs/errors"

	_ "github.com/go-sql-driver/mysql"
)

const (
	tdbUser     = "miningpool_test"
	tdbPass     = "miningpool_test"
	tdbAddress  = "127.0.0.1"
	tdbPort     = "3306"
	tdbName     = "miningpool_test"
	tPoolWallet = "6f24f36a94c052ea0f706e03914c693dc8c668bd1bf844690c221e98439b5e8b0a11711977da"

	tAddress = "b197ffe829605b03d6ba478fff68a7ac59788ca9174f8a8a5d13ba9ce0c251e9db553643b15a"
	tUser    = "tester"
	tID      = uint64(123)
)

type poolTester struct {
	gateway   modules.Gateway
	cs        modules.ConsensusSet
	tpool     modules.TransactionPool
	wallet    modules.Wallet
	walletKey crypto.TwofishKey

	mpool *Pool

	minedBlocks []types.Block
	persistDir  string
}

func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func newPoolTester(name string, port int) (*poolTester, error) {
	//fmt.Printf("newPoolTester: %s\n", name)
	testdir := build.TempDir(modules.PoolDir, name)

	// Create the modules.
	g, err := gateway.New("localhost:0", false, filepath.Join(testdir, modules.GatewayDir))
	if err != nil {
		return nil, err
	}
	cs, err := consensus.New(g, false, filepath.Join(testdir, modules.ConsensusDir))
	if err != nil {
		return nil, err
	}
	tp, err := transactionpool.New(cs, g, filepath.Join(testdir, modules.TransactionPoolDir))
	if err != nil {
		return nil, err
	}
	w, err := wallet.New(cs, tp, filepath.Join(testdir, modules.WalletDir))
	if err != nil {
		return nil, err
	}
	var key crypto.TwofishKey
	fastrand.Read(key[:])
	_, err = w.Encrypt(key)
	if err != nil {
		return nil, err
	}
	err = w.Unlock(key)
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

	poolConfig := fileConfig.MiningPoolConfig{
		PoolNetworkPort:  port,
		PoolName:         "miningpool_test",
		PoolID:           99,
		PoolDBConnection: fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", tdbUser, tdbPass, tdbAddress, tdbPort, tdbName),
		PoolWallet:       tPoolWallet,
	}

	mpool, err := New(cs, tp, g, w, filepath.Join(testdir, modules.PoolDir), poolConfig)

	if err != nil {
		return nil, err
	}

	// Assemble the poolTester.
	pt := &poolTester{
		gateway:   g,
		cs:        cs,
		tpool:     tp,
		wallet:    w,
		walletKey: key,

		mpool: mpool,

		persistDir: testdir,
	}

	return pt, nil
}

func createPoolDatabase(connection string, dbname string) error {
	sqldb, err := sql.Open("mysql", connection)
	if err != nil {
		return err
	}

	tx, err := sqldb.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(fmt.Sprintf("DROP DATABASE %s;", dbname))
	// it's ok if we can't drop it if it is not exists

	_, err = tx.Exec(fmt.Sprintf("CREATE DATABASE %s;", dbname))
	if err != nil {
		return err
	}
	_, err = tx.Exec(fmt.Sprintf("USE %s;", dbname))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		CREATE TABLE accounts (
			id int(255) NOT NULL AUTO_INCREMENT,
			coinid int(11) DEFAULT NULL,
			last_earning int(10) DEFAULT NULL,
			is_locked tinyint(1) DEFAULT '0',
			no_fees tinyint(1) DEFAULT NULL,
			donation tinyint(3) unsigned NOT NULL DEFAULT '0',
			logtraffic tinyint(1) DEFAULT NULL,
			balance double DEFAULT '0',
			username varchar(128) CHARACTER SET utf8 COLLATE utf8_bin NOT NULL,
			coinsymbol varchar(16) DEFAULT NULL,
			swap_time int(10) unsigned DEFAULT NULL,
			login varchar(45) DEFAULT NULL,
			hostaddr varchar(39) DEFAULT NULL,
			PRIMARY KEY (id),
			UNIQUE KEY username (username),
			KEY coin (coinid),
			KEY balance (balance),
			KEY earning (last_earning)
		  ) ENGINE=InnoDB DEFAULT CHARSET=utf8;
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		CREATE TABLE workers (
			id int(11) NOT NULL AUTO_INCREMENT,
			userid int(11) DEFAULT NULL,
			time int(11) DEFAULT NULL,
			pid int(11) DEFAULT NULL,
			subscribe tinyint(1) DEFAULT NULL,
			difficulty double DEFAULT NULL,
			ip varchar(32) DEFAULT NULL,
			dns varchar(1024) DEFAULT NULL,
			name varchar(128) DEFAULT NULL,
			nonce1 varchar(64) DEFAULT NULL,
			version varchar(64) DEFAULT NULL,
			password varchar(64) DEFAULT NULL,
			worker varchar(64) DEFAULT NULL,
			algo varchar(16) DEFAULT 'scrypt',
			PRIMARY KEY (id),
			KEY algo1 (algo),
			KEY name1 (name),
			KEY userid (userid),
			KEY pid (pid)
		  ) ENGINE=InnoDB DEFAULT CHARSET=utf8;
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		CREATE TABLE blocks (
			id int(11) unsigned NOT NULL AUTO_INCREMENT,
			coin_id int(11) DEFAULT NULL,
			height int(11) unsigned DEFAULT NULL,
			confirmations int(11) DEFAULT NULL,
			time int(11) DEFAULT NULL,
			userid int(11) DEFAULT NULL,
			workerid int(11) DEFAULT NULL,
			difficulty_user double DEFAULT NULL,
			price double DEFAULT NULL,
			amount double DEFAULT NULL,
			difficulty double DEFAULT NULL,
			category varchar(16) DEFAULT NULL,
			algo varchar(16) DEFAULT 'scrypt',
			blockhash varchar(128) DEFAULT NULL,
			txhash varchar(128) DEFAULT NULL,
			segwit tinyint(1) unsigned NOT NULL DEFAULT '0',
			PRIMARY KEY (id),
			KEY time (time),
			KEY algo1 (algo),
			KEY coin (coin_id),
			KEY category (category),
			KEY user1 (userid),
			KEY height1 (height)
		  ) ENGINE=InnoDB DEFAULT CHARSET=utf8;
	`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		CREATE TABLE shares (
			id bigint(30) NOT NULL AUTO_INCREMENT,
			userid int(11) DEFAULT NULL,
			workerid int(11) DEFAULT NULL,
			coinid int(11) DEFAULT NULL,
			jobid int(11) DEFAULT NULL,
			pid int(11) DEFAULT NULL,
			time int(11) DEFAULT NULL,
			error int(11) DEFAULT NULL,
			valid tinyint(1) DEFAULT NULL,
			extranonce1 tinyint(1) DEFAULT NULL,
			difficulty double NOT NULL DEFAULT '0',
			share_diff double NOT NULL DEFAULT '0',
			algo varchar(16) DEFAULT 'x11',
			reward double DEFAULT NULL,
			block_difficulty double DEFAULT NULL,
			status int(11) DEFAULT NULL,
			height int(11) DEFAULT NULL,
			share_reward double DEFAULT NULL,
			PRIMARY KEY (id),
			KEY time (time),
			KEY algo1 (algo),
			KEY valid1 (valid),
			KEY user1 (userid),
			KEY worker1 (workerid),
			KEY coin1 (coinid),
			KEY jobid (jobid)
		  ) ENGINE=InnoDB DEFAULT CHARSET=utf8;
	`)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

// Close safely closes the hostTester. It panics if err != nil because there
// isn't a good way to errcheck when deferring a close.
func (pt *poolTester) Close() error {
	// note: order of closing modules is important!
	// we have to unsubscribe the pool module from the tpool before we can close the tpool
	//fmt.Println("poolTester Close()")
	var errs []error
	//fmt.Println("closing mpool")
	errs = append(errs, pt.mpool.Close())
	//fmt.Println("closing tpool")
	errs = append(errs, pt.tpool.Close())
	//fmt.Println("closing cs")
	errs = append(errs, pt.cs.Close())
	//fmt.Println("closing gateway")
	errs = append(errs, pt.gateway.Close())
	if err := build.JoinErrors(errs, "; "); err != nil {
		panic(err)
	}
	//fmt.Println("finished poolTester Close()")
	return nil
}

func TestCreatingTestPool(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	if pt.cs.Height() != 0 {
		t.Fatal(errors.New("new consensus height wrong"))
	}
	time.Sleep(time.Millisecond * 2)
}

// test starting and stopping a miner rapidly on the pool using a bad address:
// good for catching race conditions
func TestStratumStartStopMiningBadAddress(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester("TestStratumStartStopMiningBadAddress", 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	testdir := build.TempDir(modules.PoolDir, "TestStratumStartStopMiningBadAddressStratumMiner")
	sm, err := stratumminer.New(testdir)
	if err != nil {
		t.Fatal(err)
	}
	settings := pt.mpool.InternalSettings()
	port := strconv.FormatInt(int64(settings.PoolNetworkPort), 10)
	username := "foo"
	sm.StartStratumMining(port, username)
	sm.StopStratumMining()
	sm.StartStratumMining(port, username)
	sm.StopStratumMining()
}

// test starting and stopping a miner rapidly on the pool using a valid address:
// good for catching race conditions
func TestStratumStartStopMiningGoodAddress(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester("TestStratumStartStopMiningGoodAddress", 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	testdir := build.TempDir(modules.PoolDir, "TestStratumStartStopMiningGoodAddressStratumMiner")
	sm, err := stratumminer.New(testdir)
	if err != nil {
		t.Fatal(err)
	}
	settings := pt.mpool.InternalSettings()
	port := strconv.FormatInt(int64(settings.PoolNetworkPort), 10)
	username := "06cc9f9196afb1a1efa21f72160d508f0cc192b581770fde57420cab795a2913fb4e1c85aa30"
	url := fmt.Sprintf("stratum+tcp://localhost:%s", port)
	sm.StartStratumMining(url, username)
	sm.StopStratumMining()
	sm.StartStratumMining(url, username)
	sm.StopStratumMining()
}

// func TestStratumMineBlocks(t *testing.T) {
// 	if !build.POOL {
// 		return
// 	}
// 	pt, err := newPoolTester("TestStratumMineBlocks", 0)
// 	defer pt.Close()
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	testdir := build.TempDir(modules.PoolDir, "TestStratumMineBlocksStratumMiner")
// 	sm, err := stratumminer.New(testdir)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	settings := pt.mpool.InternalSettings()
// 	port := strconv.FormatInt(int64(settings.PoolNetworkPort), 10)
// 	username := "06cc9f9196afb1a1efa21f72160d508f0cc192b581770fde57420cab795a2913fb4e1c85aa30"
// 	url := fmt.Sprintf("stratum+tcp://localhost:%s", port)
// 	sm.StartStratumMining(url, username)
// 	time.Sleep(time.Millisecond * 20)
// 	if !sm.Connected() {
// 		t.Fatal(errors.New("stratum server is running, but we are not connected"))
// 	}
// 	time.Sleep(time.Millisecond * 20)
// 	if !sm.Mining() {
// 		t.Fatal(errors.New("stratum server is running and we are connected, but we are not mining"))
// 	}
// 	time.Sleep(time.Millisecond * 10000)
// 	if sm.Hashrate() == 0 {
// 		t.Fatal(errors.New("we've been mining for a while but hashrate is 0"))
// 	}
// 	if sm.Submissions() == 0 {
// 		t.Fatal(errors.New("we've been mining for a while but have no submissions"))
// 	}
// 	sm.StopStratumMining()
// }

// NOTE: this is a long test
func TestStratumMineBlocksMiningUncleanShutdown(t *testing.T) {
	if !build.POOL {
		return
	}
	pt, err := newPoolTester("TestStratumMineBlocksMiningUncleanShutdown", 0)
	defer pt.Close()
	if err != nil {
		t.Fatal(err)
	}
	testdir := build.TempDir(modules.PoolDir, "TestStratumMineBlocksMiningUncleanShutdownMiner")
	sm, err := stratumminer.New(testdir)
	if err != nil {
		t.Fatal(err)
	}
	settings := pt.mpool.InternalSettings()
	port := strconv.FormatInt(int64(settings.PoolNetworkPort), 10)
	username := "06cc9f9196afb1a1efa21f72160d508f0cc192b581770fde57420cab795a2913fb4e1c85aa30"
	url := fmt.Sprintf("stratum+tcp://localhost:%s", port)
	sm.StartStratumMining(url, username)
	time.Sleep(time.Millisecond * 20)
	if !sm.Connected() {
		t.Fatal(errors.New("stratum server is running, but we are not connected"))
	}
	// don't stop mining, just let the server shutdown
}

func TestStratumMiningWhileRestart(t *testing.T) {
	return
	if !build.POOL {
		return
	}
	pt, err := newPoolTester(t.Name(), 0)
	time.Sleep(time.Millisecond * 2)
	settings := pt.mpool.InternalSettings()
	port := strconv.FormatInt(int64(settings.PoolNetworkPort), 10)
	username := "06cc9f9196afb1a1efa21f72160d508f0cc192b581770fde57420cab795a2913fb4e1c85aa30"
	url := fmt.Sprintf("stratum+tcp://localhost:%s", port)
	testdir := build.TempDir(modules.PoolDir, "TestStratumMineBlocksMiningUncleanShutdownMiner")
	sm, err := stratumminer.New(testdir)
	if err != nil {
		t.Fatal(err)
	}
	sm.StartStratumMining(url, username)
	time.Sleep(time.Millisecond * 20)
	if !sm.Connected() {
		t.Fatal(errors.New("stratum server is running, but we are not connected"))
	}
	time.Sleep(time.Millisecond * 20)
	if !sm.Mining() {
		t.Fatal(errors.New("stratum server is running and we are connected, but we are not mining"))
	}
	time.Sleep(time.Millisecond * 10000)
	if sm.Hashrate() == 0 {
		t.Fatal(errors.New("we've been mining for a while but hashrate is 0"))
	}
	if sm.Submissions() == 0 {
		t.Fatal(errors.New("we've been mining for a while but have no submissions"))
	}
	pt.Close()
	time.Sleep(time.Millisecond * 200)
	if sm.Connected() {
		t.Fatal(errors.New("stratum server is closed, but we are connected"))
	}
	pt, err = newPoolTester(t.Name(), settings.PoolNetworkPort)
	time.Sleep(time.Millisecond * 5000)
	if !sm.Connected() {
		t.Fatal(errors.New("stratum server is restarted and running, but we are not connected"))
	}
	sm.StopStratumMining()
}

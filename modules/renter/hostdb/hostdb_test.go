package hostdb

import (
	"bytes"
	"io/ioutil"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/consensus"
	"github.com/HyperspaceApp/Hyperspace/modules/gateway"
	"github.com/HyperspaceApp/Hyperspace/modules/miner"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/hostdb/hosttree"
	"github.com/HyperspaceApp/Hyperspace/modules/transactionpool"
	"github.com/HyperspaceApp/Hyperspace/modules/wallet"
	"github.com/HyperspaceApp/Hyperspace/persist"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// hdbTester contains a hostdb and all dependencies.
type hdbTester struct {
	cs        modules.ConsensusSet
	gateway   modules.Gateway
	miner     modules.TestMiner
	tpool     modules.TransactionPool
	wallet    modules.Wallet
	walletKey crypto.CipherKey

	hdb *HostDB

	persistDir string
}

// bareHostDB returns a HostDB with its fields initialized, but without any
// dependencies or scanning threads. It is only intended for use in unit tests.
func bareHostDB() *HostDB {
	hdb := &HostDB{
		log: persist.NewLogger(ioutil.Discard),
	}
	hdb.weightFunc = hdb.calculateHostWeightFn(modules.DefaultAllowance)
	hdb.hostTree = hosttree.New(hdb.weightFunc, &modules.ProductionResolver{})
	hdb.filteredTree = hosttree.New(hdb.weightFunc, &modules.ProductionResolver{})
	return hdb
}

// makeHostDBEntry makes a new host entry with a random public key
func makeHostDBEntry() modules.HostDBEntry {
	dbe := modules.HostDBEntry{}
	_, pk := crypto.GenerateKeyPair()

	dbe.AcceptingContracts = true
	dbe.PublicKey = types.Ed25519PublicKey(pk)
	dbe.ScanHistory = modules.HostDBScans{{
		Timestamp: time.Now(),
		Success:   true,
	}}
	dbe.Version = build.Version
	return dbe
}

// newHDBTester returns a tester object wrapping a HostDB and some extra
// information for testing.
func newHDBTester(name string) (*hdbTester, error) {
	return newHDBTesterDeps(name, modules.ProdDependencies)
}

// newHDBTesterDeps returns a tester object wrapping a HostDB and some extra
// information for testing, using the provided dependencies for the hostdb.
func newHDBTesterDeps(name string, deps modules.Dependencies) (*hdbTester, error) {
	if testing.Short() {
		panic("should not be calling newHDBTester during short tests")
	}
	testDir := build.TempDir("HostDB", name)

	g, err := gateway.New("localhost:0", false, filepath.Join(testDir, modules.GatewayDir), false)
	if err != nil {
		return nil, err
	}
	cs, err := consensus.New(g, false, filepath.Join(testDir, modules.ConsensusDir), false)
	if err != nil {
		return nil, err
	}
	tp, err := transactionpool.New(cs, g, filepath.Join(testDir, modules.TransactionPoolDir))
	if err != nil {
		return nil, err
	}
	w, err := wallet.New(cs, tp, filepath.Join(testDir, modules.WalletDir), modules.DefaultAddressGapLimit, false)
	if err != nil {
		return nil, err
	}
	m, err := miner.New(cs, tp, w, filepath.Join(testDir, modules.MinerDir))
	if err != nil {
		return nil, err
	}
	hdb, err := NewCustomHostDB(g, cs, filepath.Join(testDir, modules.RenterDir), deps)
	if err != nil {
		return nil, err
	}

	hdbt := &hdbTester{
		cs:      cs,
		gateway: g,
		miner:   m,
		tpool:   tp,
		wallet:  w,

		hdb: hdb,

		persistDir: testDir,
	}

	err = hdbt.initWallet()
	if err != nil {
		return nil, err
	}

	return hdbt, nil
}

// initWallet creates a wallet key, then initializes and unlocks the wallet.
func (hdbt *hdbTester) initWallet() error {
	hdbt.walletKey = crypto.GenerateSiaKey(crypto.TypeDefaultWallet)
	_, err := hdbt.wallet.Encrypt(hdbt.walletKey)
	if err != nil {
		return err
	}
	err = hdbt.wallet.Unlock(hdbt.walletKey)
	if err != nil {
		return err
	}
	return nil
}

// TestNew tests the New function.
func TestNew(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	testDir := build.TempDir("HostDB", t.Name())
	g, err := gateway.New("localhost:0", false, filepath.Join(testDir, modules.GatewayDir), false)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := consensus.New(g, false, filepath.Join(testDir, modules.ConsensusDir), false)
	if err != nil {
		t.Fatal(err)
	}

	// Vanilla HDB, nothing should go wrong.
	hdbName := filepath.Join(testDir, modules.RenterDir)
	_, err = New(g, cs, hdbName+"1")
	if err != nil {
		t.Fatal(err)
	}

	// Nil gateway.
	_, err = New(nil, cs, hdbName+"2")
	if err != errNilGateway {
		t.Fatalf("expected %v, got %v", errNilGateway, err)
	}
	// Nil consensus set.
	_, err = New(g, nil, hdbName+"3")
	if err != errNilCS {
		t.Fatalf("expected %v, got %v", errNilCS, err)
	}
	// Bad persistDir.
	_, err = New(g, cs, "")
	if !os.IsNotExist(err) {
		t.Fatalf("expected invalid directory, got %v", err)
	}
}

// quitAfterLoadDeps will quit startup in newHostDB
type disableScanLoopDeps struct {
	modules.ProductionDependencies
}

// Send a disrupt signal to the quitAfterLoad codebreak.
func (*disableScanLoopDeps) Disrupt(s string) bool {
	if s == "disableScanLoop" {
		return true
	}
	return false
}

// TestRandomHosts tests the hostdb's exported RandomHosts method.
func TestRandomHosts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	hdbt, err := newHDBTesterDeps(t.Name(), &disableScanLoopDeps{})
	if err != nil {
		t.Fatal(err)
	}

	entries := make(map[string]modules.HostDBEntry)
	nEntries := int(1e3)
	for i := 0; i < nEntries; i++ {
		entry := makeHostDBEntry()
		entries[string(entry.PublicKey.Key)] = entry
		err := hdbt.hdb.filteredTree.Insert(entry)
		if err != nil {
			t.Error(err)
		}
	}

	// Check that all hosts can be queried.
	for i := 0; i < 25; i++ {
		hosts, err := hdbt.hdb.RandomHosts(nEntries, nil, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(hosts) != nEntries {
			t.Errorf("RandomHosts returned few entries. got %v wanted %v\n", len(hosts), nEntries)
		}
		dupCheck := make(map[string]modules.HostDBEntry)
		for _, host := range hosts {
			_, exists := entries[string(host.PublicKey.Key)]
			if !exists {
				t.Error("hostdb returning host that doesn't exist.")
			}
			_, exists = dupCheck[string(host.PublicKey.Key)]
			if exists {
				t.Error("RandomHosts returning duplicates")
			}
			dupCheck[string(host.PublicKey.Key)] = host
		}
	}

	// Base case, fill out a map exposing hosts from a single RH query.
	dupCheck1 := make(map[string]modules.HostDBEntry)
	hosts, err := hdbt.hdb.RandomHosts(nEntries/2, nil, nil)
	if err != nil {
		t.Fatal("Failed to get hosts", err)
	}
	if len(hosts) != nEntries/2 {
		t.Fatalf("RandomHosts returned few entries. got %v wanted %v\n", len(hosts), nEntries/2)
	}
	for _, host := range hosts {
		_, exists := entries[string(host.PublicKey.Key)]
		if !exists {
			t.Error("hostdb returning host that doesn't exist.")
		}
		_, exists = dupCheck1[string(host.PublicKey.Key)]
		if exists {
			t.Error("RandomHosts returning duplicates")
		}
		dupCheck1[string(host.PublicKey.Key)] = host
	}

	// Iterative case. Check that every time you query for random hosts, you
	// get different responses.
	for i := 0; i < 10; i++ {
		dupCheck2 := make(map[string]modules.HostDBEntry)
		var overlap, disjoint bool
		hosts, err = hdbt.hdb.RandomHosts(nEntries/2, nil, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(hosts) != nEntries/2 {
			t.Fatalf("RandomHosts returned few entries. got %v wanted %v\n", len(hosts), nEntries/2)
		}
		for _, host := range hosts {
			_, exists := entries[string(host.PublicKey.Key)]
			if !exists {
				t.Error("hostdb returning host that doesn't exist.")
			}
			_, exists = dupCheck2[string(host.PublicKey.Key)]
			if exists {
				t.Error("RandomHosts returning duplicates")
			}
			_, exists = dupCheck1[string(host.PublicKey.Key)]
			if exists {
				overlap = true
			} else {
				disjoint = true
			}
			dupCheck2[string(host.PublicKey.Key)] = host

		}
		if !overlap || !disjoint {
			t.Error("Random hosts does not seem to be random")
		}
		dupCheck1 = dupCheck2
	}

	// Try exclude list by excluding every host except for the last one, and
	// doing a random select.
	for i := 0; i < 25; i++ {
		hosts, err := hdbt.hdb.RandomHosts(nEntries, nil, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		var exclude []types.SiaPublicKey
		for j := 1; j < len(hosts); j++ {
			exclude = append(exclude, hosts[j].PublicKey)
		}
		rand, err := hdbt.hdb.RandomHosts(1, exclude, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(rand) != 1 {
			t.Fatal("wrong number of hosts returned")
		}
		if string(rand[0].PublicKey.Key) != string(hosts[0].PublicKey.Key) {
			t.Error("exclude list seems to be excluding the wrong hosts.")
		}

		// Try again but request more hosts than are available.
		rand, err = hdbt.hdb.RandomHosts(5, exclude, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(rand) != 1 {
			t.Fatal("wrong number of hosts returned")
		}
		if string(rand[0].PublicKey.Key) != string(hosts[0].PublicKey.Key) {
			t.Error("exclude list seems to be excluding the wrong hosts.")
		}

		// Create an include map, and decrease the number of excluded hosts.
		// Make sure all hosts returned by rand function are in the include
		// map.
		includeMap := make(map[string]struct{})
		for j := 0; j < 50; j++ {
			includeMap[string(hosts[j].PublicKey.Key)] = struct{}{}
		}
		exclude = exclude[49:]

		// Select only 20 hosts.
		dupCheck := make(map[string]struct{})
		rand, err = hdbt.hdb.RandomHosts(20, exclude, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(rand) != 20 {
			t.Error("random hosts is returning the wrong number of hosts")
		}
		for _, host := range rand {
			_, exists := dupCheck[string(host.PublicKey.Key)]
			if exists {
				t.Error("RandomHosts is selecting duplicates")
			}
			dupCheck[string(host.PublicKey.Key)] = struct{}{}
			_, exists = includeMap[string(host.PublicKey.Key)]
			if !exists {
				t.Error("RandomHosts returning excluded hosts")
			}
		}

		// Select exactly 50 hosts.
		dupCheck = make(map[string]struct{})
		rand, err = hdbt.hdb.RandomHosts(50, exclude, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(rand) != 50 {
			t.Error("random hosts is returning the wrong number of hosts")
		}
		for _, host := range rand {
			_, exists := dupCheck[string(host.PublicKey.Key)]
			if exists {
				t.Error("RandomHosts is selecting duplicates")
			}
			dupCheck[string(host.PublicKey.Key)] = struct{}{}
			_, exists = includeMap[string(host.PublicKey.Key)]
			if !exists {
				t.Error("RandomHosts returning excluded hosts")
			}
		}

		// Select 100 hosts.
		dupCheck = make(map[string]struct{})
		rand, err = hdbt.hdb.RandomHosts(100, exclude, nil)
		if err != nil {
			t.Fatal("Failed to get hosts", err)
		}
		if len(rand) != 50 {
			t.Error("random hosts is returning the wrong number of hosts")
		}
		for _, host := range rand {
			_, exists := dupCheck[string(host.PublicKey.Key)]
			if exists {
				t.Error("RandomHosts is selecting duplicates")
			}
			dupCheck[string(host.PublicKey.Key)] = struct{}{}
			_, exists = includeMap[string(host.PublicKey.Key)]
			if !exists {
				t.Error("RandomHosts returning excluded hosts")
			}
		}
	}
}

// TestRemoveNonexistingHostFromHostTree checks that the host tree interface
// correctly responds to having a nonexisting host removed from the host tree.
func TestRemoveNonexistingHostFromHostTree(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	hdbt, err := newHDBTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Remove a host that doesn't exist from the tree.
	err = hdbt.hdb.hostTree.Remove(types.SiaPublicKey{})
	if err == nil {
		t.Fatal("There should be an error, but not a panic:", err)
	}
}

// TestUpdateHistoricInteractions is a simple check to ensure that incrementing
// the recent and historic host interactions works
func TestUpdateHistoricInteractions(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// create a HostDB tester without scanloop to be able to manually increment
	// the interactions without interference.
	hdbt, err := newHDBTesterDeps(t.Name(), &disableScanLoopDeps{})
	if err != nil {
		t.Fatal(err)
	}

	// create a HostDBEntry and add it to the tree
	host := makeHostDBEntry()
	err = hdbt.hdb.hostTree.Insert(host)
	if err != nil {
		t.Error(err)
	}

	// increment successful and failed interactions by 100
	interactions := 100.0
	for i := 0.0; i < interactions; i++ {
		hdbt.hdb.IncrementSuccessfulInteractions(host.PublicKey)
		hdbt.hdb.IncrementFailedInteractions(host.PublicKey)
	}

	// get updated host from hostdb
	host, ok := hdbt.hdb.Host(host.PublicKey)
	if !ok {
		t.Fatal("Modified host not found in hostdb")
	}

	// check that recent interactions are exactly 100 and historic interactions are 0
	if host.RecentFailedInteractions != interactions || host.RecentSuccessfulInteractions != interactions {
		t.Errorf("Interactions should be %v but were %v and %v", interactions,
			host.RecentFailedInteractions, host.RecentSuccessfulInteractions)
	}
	if host.HistoricFailedInteractions != 0 || host.HistoricSuccessfulInteractions != 0 {
		t.Errorf("Historic Interactions should be %v but were %v and %v", 0,
			host.HistoricFailedInteractions, host.HistoricSuccessfulInteractions)
	}

	// add single block to consensus
	_, err = hdbt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// increment interactions again by 100
	for i := 0.0; i < interactions; i++ {
		hdbt.hdb.IncrementSuccessfulInteractions(host.PublicKey)
		hdbt.hdb.IncrementFailedInteractions(host.PublicKey)
	}

	// get updated host from hostdb
	host, ok = hdbt.hdb.Host(host.PublicKey)
	if !ok {
		t.Fatal("Modified host not found in hostdb")
	}

	// historic actions should have incremented slightly, due to the clamp the
	// full interactions should not have made it into the historic group.
	if host.RecentFailedInteractions != interactions || host.RecentSuccessfulInteractions != interactions {
		t.Errorf("Interactions should be %v but were %v and %v", interactions,
			host.RecentFailedInteractions, host.RecentSuccessfulInteractions)
	}
	if host.HistoricFailedInteractions == 0 || host.HistoricSuccessfulInteractions == 0 {
		t.Error("historic actions should have updated")
	}

	// add 200 blocks to consensus, adding large numbers of historic actions
	// each time, so that the clamp does not need to be in effect anymore.
	for i := 0; i < 200; i++ {
		for j := uint64(0); j < 10; j++ {
			hdbt.hdb.IncrementSuccessfulInteractions(host.PublicKey)
			hdbt.hdb.IncrementFailedInteractions(host.PublicKey)
		}
		_, err = hdbt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Add five interactions
	for i := 0; i < 5; i++ {
		hdbt.hdb.IncrementSuccessfulInteractions(host.PublicKey)
		hdbt.hdb.IncrementFailedInteractions(host.PublicKey)
	}

	// get updated host from hostdb
	host, ok = hdbt.hdb.Host(host.PublicKey)
	if !ok {
		t.Fatal("Modified host not found in hostdb")
	}

	// check that recent interactions are exactly 5. Save the historic actions
	// to check that decay is being handled correctly, and that the recent
	// interactions are moved over correctly.
	if host.RecentFailedInteractions != 5 || host.RecentSuccessfulInteractions != 5 {
		t.Errorf("Interactions should be %v but were %v and %v", interactions,
			host.RecentFailedInteractions, host.RecentSuccessfulInteractions)
	}
	historicFailed := host.HistoricFailedInteractions
	if host.HistoricFailedInteractions != host.HistoricSuccessfulInteractions {
		t.Error("historic failed and successful should have the same values")
	}

	// Add a single block to apply one round of decay.
	_, err = hdbt.miner.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	host, ok = hdbt.hdb.Host(host.PublicKey)
	if !ok {
		t.Fatal("Modified host not found in hostdb")
	}

	// Get the historic successful and failed interactions, and see that they
	// are decaying properly.
	expected := historicFailed*math.Pow(historicInteractionDecay, 1) + 5
	if host.HistoricFailedInteractions != expected || host.HistoricSuccessfulInteractions != expected {
		t.Errorf("Historic Interactions should be %v but were %v and %v", expected,
			host.HistoricFailedInteractions, host.HistoricSuccessfulInteractions)
	}

	// Add 10 more blocks and check the decay again, make sure it's being
	// applied correctly.
	for i := 0; i < 10; i++ {
		_, err := hdbt.miner.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	host, ok = hdbt.hdb.Host(host.PublicKey)
	if !ok {
		t.Fatal("Modified host not found in hostdb")
	}
	expected = expected * math.Pow(historicInteractionDecay, 10)
	if host.HistoricFailedInteractions != expected || host.HistoricSuccessfulInteractions != expected {
		t.Errorf("Historic Interactions should be %v but were %v and %v", expected,
			host.HistoricFailedInteractions, host.HistoricSuccessfulInteractions)
	}
}

// testCheckForIPViolationsResolver is a resolver for the TestTwoAddresses test.
type testCheckForIPViolationsResolver struct{}

func (testCheckForIPViolationsResolver) LookupIP(host string) ([]net.IP, error) {
	switch host {
	case "host1":
		return []net.IP{{127, 0, 0, 1}}, nil
	case "host2":
		return []net.IP{{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}}, nil
	case "host3":
		return []net.IP{{127, 0, 0, 2}, {0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}}, nil
	default:
		panic("shouldn't happen")
	}
}

// testCheckForIPViolationsDeps is a custom dependency that overrides the
// Resolver method to return a testCheckForIPViolationsResolver.
type testCheckForIPViolationsDeps struct {
	disableScanLoopDeps
}

// Resolver returns a testCheckForIPViolationsResolver.
func (*testCheckForIPViolationsDeps) Resolver() modules.Resolver {
	return &testCheckForIPViolationsResolver{}
}

// TestCheckForIPViolations tests the hostdb's CheckForIPViolations method.
func TestCheckForIPViolations(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}

	// Prepare a few hosts for the test
	entry1 := makeHostDBEntry()
	entry1.NetAddress = "host1:1234"
	entry2 := makeHostDBEntry()
	entry2.NetAddress = "host2:1234"
	entry3 := makeHostDBEntry()
	entry3.NetAddress = "host3:1234"

	// create a HostDB tester without scanloop to be able to manually increment
	// the interactions without interference.
	hdbt, err := newHDBTesterDeps(t.Name(), &testCheckForIPViolationsDeps{})
	if err != nil {
		t.Fatal(err)
	}

	// Scan the entries. entry1 should be the 'oldest' and entry3 the
	// 'youngest'. This also inserts the entries into the hosttree.
	hdbt.hdb.managedScanHost(entry1)
	entry1, _ = hdbt.hdb.Host(entry1.PublicKey)
	time.Sleep(time.Millisecond)

	hdbt.hdb.managedScanHost(entry2)
	entry2, _ = hdbt.hdb.Host(entry2.PublicKey)
	time.Sleep(time.Millisecond)

	hdbt.hdb.managedScanHost(entry3)
	entry3, _ = hdbt.hdb.Host(entry3.PublicKey)
	time.Sleep(time.Millisecond)

	// Make sure that the timestamps are not zero and that they entries have
	// subnets associated with them.
	if len(entry1.IPNets) == 0 || entry1.LastIPNetChange.IsZero() {
		t.Fatal("entry1 wasn't updated correctly")
	}
	if len(entry2.IPNets) == 0 || entry2.LastIPNetChange.IsZero() {
		t.Fatal("entry2 wasn't updated correctly")
	}
	if len(entry3.IPNets) == 0 || entry3.LastIPNetChange.IsZero() {
		t.Fatal("entry3 wasn't updated correctly")
	}

	// Scan all the entries again in reversed order. This is a sanity check. If
	// the code works as expected this shouldn't do anything since the
	// hostnames didn't change. If it doesn't, it will update the timestamps
	// and the following checks will fail.
	time.Sleep(time.Millisecond)
	hdbt.hdb.managedScanHost(entry3)
	entry3, _ = hdbt.hdb.Host(entry3.PublicKey)

	time.Sleep(time.Millisecond)
	hdbt.hdb.managedScanHost(entry2)
	entry2, _ = hdbt.hdb.Host(entry2.PublicKey)

	time.Sleep(time.Millisecond)
	hdbt.hdb.managedScanHost(entry1)
	entry1, _ = hdbt.hdb.Host(entry1.PublicKey)

	// Add entry1 and entry2. There should be no violation.
	badHosts := hdbt.hdb.CheckForIPViolations([]types.SiaPublicKey{entry1.PublicKey, entry2.PublicKey})
	if len(badHosts) != 0 {
		t.Errorf("Got %v violations, should be 0", len(badHosts))
	}

	// Add entry3. It should cause a violation for entry 3.
	badHosts = hdbt.hdb.CheckForIPViolations([]types.SiaPublicKey{entry1.PublicKey, entry2.PublicKey, entry3.PublicKey})
	if len(badHosts) != 1 {
		t.Errorf("Got %v violations, should be 1", len(badHosts))
	}
	if len(badHosts) > 0 && !bytes.Equal(badHosts[0].Key, entry3.PublicKey.Key) {
		t.Error("Hdb returned violation for wrong host")
	}

	// Calling CheckForIPViolations with entry 2 as the first argument and
	// entry1 as the second should result in entry3 being the bad host again.
	badHosts = hdbt.hdb.CheckForIPViolations([]types.SiaPublicKey{entry2.PublicKey, entry1.PublicKey, entry3.PublicKey})
	if len(badHosts) != 1 {
		t.Errorf("Got %v violations, should be 1", len(badHosts))
	}
	if len(badHosts) > 0 && !bytes.Equal(badHosts[0].Key, entry3.PublicKey.Key) {
		t.Error("Hdb returned violation for wrong host")
	}

	// Calling CheckForIPViolations with entry 3 as the first argument should
	// result in 1 bad host, entry3. The reason being that entry3 is the
	// 'youngest' entry.
	badHosts = hdbt.hdb.CheckForIPViolations([]types.SiaPublicKey{entry3.PublicKey, entry1.PublicKey, entry2.PublicKey})
	if len(badHosts) != 1 {
		t.Errorf("Got %v violations, should be 1", len(badHosts))
	}
	if len(badHosts) > 1 || !bytes.Equal(badHosts[0].Key, entry3.PublicKey.Key) {
		t.Error("Hdb returned violation for wrong host")
	}
}

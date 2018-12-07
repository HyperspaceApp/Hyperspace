package contractor

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// newStub is used to test the New function. It implements all of the contractor's
// dependencies.
type newStub struct{}

// consensus set stubs
func (newStub) ConsensusSetSubscribe(modules.ConsensusSetSubscriber, modules.ConsensusChangeID, <-chan struct{}) error {
	return nil
}
func (newStub) HeaderConsensusSetSubscribe(modules.HeaderConsensusSetSubscriber, modules.ConsensusChangeID, <-chan struct{}) error {
	return nil
}
func (newStub) Synced() bool                               { return true }
func (newStub) Unsubscribe(modules.ConsensusSetSubscriber) { return }
func (newStub) SpvMode() bool                              { return false }

// wallet stubs
func (newStub) NextAddress() (uc types.UnlockConditions, err error)          { return }
func (newStub) GetAddress() (uc types.UnlockConditions, err error)           { return }
func (newStub) StartTransaction() (tb modules.TransactionBuilder, err error) { return }

// transaction pool stubs
func (newStub) AcceptTransactionSet([]types.Transaction) error      { return nil }
func (newStub) FeeEstimation() (a types.Currency, b types.Currency) { return }

// hdb stubs
func (newStub) AllHosts() []modules.HostDBEntry                                { return nil }
func (newStub) ActiveHosts() []modules.HostDBEntry                             { return nil }
func (newStub) CheckForIPViolations([]types.SiaPublicKey) []types.SiaPublicKey { return nil }
func (newStub) Filter() (modules.FilterMode, map[string]types.SiaPublicKey) {
	return 0, make(map[string]types.SiaPublicKey)
}
func (newStub) SetFilterMode(fm modules.FilterMode, hosts []types.SiaPublicKey) error { return nil }
func (newStub) Host(types.SiaPublicKey) (settings modules.HostDBEntry, ok bool)       { return }
func (newStub) IncrementSuccessfulInteractions(key types.SiaPublicKey)                { return }
func (newStub) IncrementFailedInteractions(key types.SiaPublicKey)                    { return }
func (newStub) RandomHosts(int, []types.SiaPublicKey, []types.SiaPublicKey) ([]modules.HostDBEntry, error) {
	return nil, nil
}
func (newStub) ScoreBreakdown(modules.HostDBEntry) modules.HostScoreBreakdown {
	return modules.HostScoreBreakdown{}
}
func (newStub) SetAllowance(allowance modules.Allowance) error { return nil }

// TestNew tests the New function.
func TestNew(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// Using a stub implementation of the dependencies is fine, as long as its
	// non-nil.
	var stub newStub
	dir := build.TempDir("contractor", t.Name())

	// Sane values.
	_, err := New(stub, stub, stub, stub, dir)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	// Nil consensus set.
	_, err = New(nil, stub, stub, stub, dir)
	if err != errNilCS {
		t.Fatalf("expected %v, got %v", errNilCS, err)
	}

	// Nil wallet.
	_, err = New(stub, nil, stub, stub, dir)
	if err != errNilWallet {
		t.Fatalf("expected %v, got %v", errNilWallet, err)
	}

	// Nil transaction pool.
	_, err = New(stub, stub, nil, stub, dir)
	if err != errNilTpool {
		t.Fatalf("expected %v, got %v", errNilTpool, err)
	}

	// Bad persistDir.
	_, err = New(stub, stub, stub, stub, "")
	if !os.IsNotExist(err) {
		t.Fatalf("expected invalid directory, got %v", err)
	}
}

// TestAllowance tests the Allowance method.
func TestAllowance(t *testing.T) {
	c := &Contractor{
		allowance: modules.Allowance{
			Funds:  types.NewCurrency64(1),
			Period: 2,
			Hosts:  3,
		},
	}
	a := c.Allowance()
	if a.Funds.Cmp(c.allowance.Funds) != 0 ||
		a.Period != c.allowance.Period ||
		a.Hosts != c.allowance.Hosts {
		t.Fatal("Allowance did not return correct allowance:", a, c.allowance)
	}
}

// stubHostDB mocks the hostDB dependency using zero-valued implementations of
// its methods.
type stubHostDB struct{}

func (stubHostDB) AllHosts() (hs []modules.HostDBEntry)                           { return }
func (stubHostDB) ActiveHosts() (hs []modules.HostDBEntry)                        { return }
func (stubHostDB) CheckForIPViolations([]types.SiaPublicKey) []types.SiaPublicKey { return nil }
func (stubHostDB) Filter() (modules.FilterMode, map[string]types.SiaPublicKey) {
	return 0, make(map[string]types.SiaPublicKey)
}
func (stubHostDB) SetFilterMode(fm modules.FilterMode, hosts []types.SiaPublicKey) error { return nil }
func (stubHostDB) Host(types.SiaPublicKey) (h modules.HostDBEntry, ok bool)              { return }
func (stubHostDB) IncrementSuccessfulInteractions(key types.SiaPublicKey)                { return }
func (stubHostDB) IncrementFailedInteractions(key types.SiaPublicKey)                    { return }
func (stubHostDB) PublicKey() (spk types.SiaPublicKey)                                   { return }
func (stubHostDB) RandomHosts(int, []types.SiaPublicKey, []types.SiaPublicKey) (hs []modules.HostDBEntry, _ error) {
	return
}
func (stubHostDB) ScoreBreakdown(modules.HostDBEntry) modules.HostScoreBreakdown {
	return modules.HostScoreBreakdown{}
}
func (stubHostDB) SetAllowance(allowance modules.Allowance) error { return nil }

// TestAllowanceSpending verifies that the contractor will not spend more or
// less than the allowance if uploading causes repeated early renewal, and that
// correct spending metrics are returned, even across renewals.
func TestAllowanceSpending(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create testing trio
	h, c, m, err := newTestingTrio(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// make the host's upload price very high so this test requires less
	// computation
	settings := h.InternalSettings()
	settings.MinUploadBandwidthPrice = types.SiacoinPrecision.Div64(10)
	err = h.SetInternalSettings(settings)
	if err != nil {
		t.Fatal(err)
	}
	err = h.Announce()
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.AddBlock()
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		hosts, err := c.hdb.RandomHosts(1, nil, nil)
		if err != nil {
			return err
		}
		if len(hosts) == 0 {
			return errors.New("host has not been scanned yet")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// set an allowance
	testAllowance := modules.Allowance{
		Funds:              types.SiacoinPrecision.Mul64(6000),
		RenewWindow:        100,
		Hosts:              1,
		Period:             200,
		ExpectedStorage:    modules.DefaultAllowance.ExpectedStorage,
		ExpectedUpload:     modules.DefaultAllowance.ExpectedUpload,
		ExpectedDownload:   modules.DefaultAllowance.ExpectedDownload,
		ExpectedRedundancy: modules.DefaultAllowance.ExpectedRedundancy,
	}
	err = c.SetAllowance(testAllowance)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 1 {
			return errors.New("allowance forming seems to have failed")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// exhaust a contract and add a block several times. Despite repeatedly
	// running out of funds, the contractor should not spend more than the
	// allowance.
	for i := 0; i < 15; i++ {
		for _, contract := range c.Contracts() {
			ed, err := c.Editor(contract.HostPublicKey, nil)
			if err != nil {
				continue
			}

			// upload 10 sectors to the contract
			for sec := 0; sec < 10; sec++ {
				ed.Upload(make([]byte, modules.SectorSize))
			}
			err = ed.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err := m.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	var minerRewards types.Currency
	w := c.wallet.(*WalletBridge).W.(modules.Wallet)
	txns, err := w.Transactions(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, txn := range txns {
		for _, so := range txn.Outputs {
			if so.FundType == types.SpecifierMinerPayout {
				minerRewards = minerRewards.Add(so.Value)
			}
		}
	}
	balance, err := w.ConfirmedBalance()
	if err != nil {
		t.Fatal(err)
	}
	spent := minerRewards.Sub(balance)
	if spent.Cmp(testAllowance.Funds) > 0 {
		t.Fatal("contractor spent too much money: spent", spent.HumanString(), "allowance funds:", testAllowance.Funds.HumanString())
	}

	// we should have spent at least the allowance minus the cost of one more refresh
	refreshCost := c.Contracts()[0].TotalCost.Mul64(2)
	expectedMinSpending := testAllowance.Funds.Sub(refreshCost)
	if spent.Cmp(expectedMinSpending) < 0 {
		t.Fatal("contractor spent to little money: spent", spent.HumanString(), "expected at least:", expectedMinSpending.HumanString())
	}

	// PeriodSpending should reflect the amount of spending accurately
	reportedSpending := c.PeriodSpending()
	if reportedSpending.TotalAllocated.Cmp(spent) != 0 {
		t.Fatal("reported incorrect spending for this billing cycle: got", reportedSpending.TotalAllocated.HumanString(), "wanted", spent.HumanString())
	}

	var expectedFees types.Currency
	for _, contract := range c.Contracts() {
		expectedFees = expectedFees.Add(contract.TxnFee)
		expectedFees = expectedFees.Add(contract.ContractFee)
	}
	if expectedFees.Cmp(reportedSpending.ContractFees) != 0 {
		t.Fatalf("expected %v reported fees but was %v",
			expectedFees.HumanString(), reportedSpending.ContractFees.HumanString())
	}
}

// TestIntegrationSetAllowance tests the SetAllowance method.
func TestIntegrationSetAllowance(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// create testing trio
	_, c, m, err := newTestingTrio(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// this test requires two hosts: create another one
	h, err := newTestingHost(build.TempDir("hostdata", ""), c.cs.(modules.ConsensusSet), c.tpool.(modules.TransactionPool))
	if err != nil {
		t.Fatal(err)
	}

	// announce the extra host
	err = h.Announce()
	if err != nil {
		t.Fatal(err)
	}

	// mine a block, processing the announcement
	_, err = m.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// wait for hostdb to scan
	hosts, err := c.hdb.RandomHosts(1, nil, nil)
	if err != nil {
		t.Fatal("failed to get hosts", err)
	}
	for i := 0; i < 100 && len(hosts) == 0; i++ {
		time.Sleep(time.Millisecond * 50)
	}

	// cancel allowance
	var a modules.Allowance
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}

	// bad args
	a.Hosts = 1
	err = c.SetAllowance(a)
	if err != errAllowanceZeroPeriod {
		t.Errorf("expected %q, got %q", errAllowanceZeroPeriod, err)
	}
	a.Period = 20
	err = c.SetAllowance(a)
	if err != ErrAllowanceZeroWindow {
		t.Errorf("expected %q, got %q", ErrAllowanceZeroWindow, err)
	}
	a.RenewWindow = 20
	err = c.SetAllowance(a)
	if err != errAllowanceWindowSize {
		t.Errorf("expected %q, got %q", errAllowanceWindowSize, err)
	}
	a.RenewWindow = 10
	err = c.SetAllowance(a)
	if err != errAllowanceZeroExpectedStorage {
		t.Errorf("expected %q, got %q", errAllowanceZeroExpectedStorage, err)
	}
	a.ExpectedStorage = modules.DefaultAllowance.ExpectedStorage
	err = c.SetAllowance(a)
	if err != errAllowanceZeroExpectedUpload {
		t.Errorf("expected %q, got %q", errAllowanceZeroExpectedUpload, err)
	}
	a.ExpectedUpload = modules.DefaultAllowance.ExpectedUpload
	err = c.SetAllowance(a)
	if err != errAllowanceZeroExpectedDownload {
		t.Errorf("expected %q, got %q", errAllowanceZeroExpectedDownload, err)
	}
	a.ExpectedDownload = modules.DefaultAllowance.ExpectedDownload
	err = c.SetAllowance(a)
	if err != errAllowanceZeroExpectedRedundancy {
		t.Errorf("expected %q, got %q", errAllowanceZeroExpectedRedundancy, err)
	}
	a.ExpectedRedundancy = modules.DefaultAllowance.ExpectedRedundancy

	// reasonable values; should succeed
	a.Funds = types.SiacoinPrecision.Mul64(100)
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 1 {
			return errors.New("allowance forming seems to have failed")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// set same allowance; should no-op
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	clen := c.staticContracts.Len()
	if clen != 1 {
		t.Fatal("expected 1 contract, got", clen)
	}

	_, err = m.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// set allowance with Hosts = 2; should only form one new contract
	a.Hosts = 2
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 2 {
			return errors.New("allowance forming seems to have failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// set allowance with Funds*2; should trigger renewal of both contracts
	a.Funds = a.Funds.Mul64(2)
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 2 {
			return errors.New("allowance forming seems to have failed")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// delete one of the contracts and set allowance with Funds*2; should
	// trigger 1 renewal and 1 new contract
	c.mu.Lock()
	ids := c.staticContracts.IDs()
	contract, _ := c.staticContracts.Acquire(ids[0])
	c.staticContracts.Delete(contract)
	c.mu.Unlock()
	a.Funds = a.Funds.Mul64(2)
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 2 {
			return errors.New("allowance forming seems to have failed")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestHostMaxDuration tests that a host will not be used if their max duration
// is not sufficient when renewing contracts
func TestHostMaxDuration(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create testing trio
	h, c, m, err := newTestingTrio(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Set host's MaxDuration to 5 to test if host will be skipped when contract
	// is formed
	settings := h.InternalSettings()
	settings.MaxDuration = types.BlockHeight(5)
	if err := h.SetInternalSettings(settings); err != nil {
		t.Fatal(err)
	}
	// Let host settings permeate
	err = build.Retry(50, 100*time.Millisecond, func() error {
		host, _ := c.hdb.Host(h.PublicKey())
		if settings.MaxDuration != host.MaxDuration {
			return fmt.Errorf("host max duration not set, expected %v, got %v", settings.MaxDuration, host.MaxDuration)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create allowance
	a := modules.Allowance{
		Funds:              types.SiacoinPrecision.Mul64(100),
		Hosts:              1,
		Period:             30,
		RenewWindow:        20,
		ExpectedStorage:    modules.DefaultAllowance.ExpectedStorage,
		ExpectedUpload:     modules.DefaultAllowance.ExpectedUpload,
		ExpectedDownload:   modules.DefaultAllowance.ExpectedDownload,
		ExpectedRedundancy: modules.DefaultAllowance.ExpectedRedundancy,
	}
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for and confirm no Contract creation
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.Contracts()) == 0 {
			return errors.New("no contract created")
		}
		return nil
	})
	if err == nil {
		t.Fatal("Contract should not have been created")
	}

	// Set host's MaxDuration to 50 to test if host will now form contract
	settings = h.InternalSettings()
	settings.MaxDuration = types.BlockHeight(50)
	if err := h.SetInternalSettings(settings); err != nil {
		t.Fatal(err)
	}
	// Let host settings permeate
	err = build.Retry(50, 100*time.Millisecond, func() error {
		host, _ := c.hdb.Host(h.PublicKey())
		if settings.MaxDuration != host.MaxDuration {
			return fmt.Errorf("host max duration not set, expected %v, got %v", settings.MaxDuration, host.MaxDuration)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.AddBlock()
	if err != nil {
		t.Fatal(err)
	}

	// Wait for Contract creation
	err = build.Retry(600, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 1 {
			return errors.New("no contract created")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Set host's MaxDuration to 5 to test if host will be skipped when contract
	// is renewed
	settings = h.InternalSettings()
	settings.MaxDuration = types.BlockHeight(5)
	if err := h.SetInternalSettings(settings); err != nil {
		t.Fatal(err)
	}
	// Let host settings permeate
	err = build.Retry(50, 100*time.Millisecond, func() error {
		host, _ := c.hdb.Host(h.PublicKey())
		if settings.MaxDuration != host.MaxDuration {
			return fmt.Errorf("host max duration not set, expected %v, got %v", settings.MaxDuration, host.MaxDuration)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mine blocks to renew contract
	for i := types.BlockHeight(0); i <= c.allowance.Period-c.allowance.RenewWindow; i++ {
		_, err = m.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Confirm Contract is not renewed
	err = build.Retry(50, 100*time.Millisecond, func() error {
		if len(c.OldContracts()) == 0 {
			return errors.New("no contract renewed")
		}
		return nil
	})
	if err == nil {
		t.Fatal("Contract should not have been renewed")
	}
}

// TestLinkedContracts tests that the contractors maps are updated correctly
// when renewing contracts
func TestLinkedContracts(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()

	// create testing trio
	h, c, m, err := newTestingTrio(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Create allowance
	a := modules.Allowance{
		Funds:              types.SiacoinPrecision.Mul64(100),
		Hosts:              1,
		Period:             20,
		RenewWindow:        10,
		ExpectedStorage:    modules.DefaultAllowance.ExpectedStorage,
		ExpectedUpload:     modules.DefaultAllowance.ExpectedUpload,
		ExpectedDownload:   modules.DefaultAllowance.ExpectedDownload,
		ExpectedRedundancy: modules.DefaultAllowance.ExpectedRedundancy,
	}
	err = c.SetAllowance(a)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for Contract creation
	err = build.Retry(200, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 1 {
			return errors.New("no contract created")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Confirm that maps are empty
	if len(c.renewedFrom) != 0 {
		t.Fatal("renewedFrom map should be empty")
	}
	if len(c.renewedTo) != 0 {
		t.Fatal("renewedTo map should be empty")
	}

	// Set host's uploadbandwidthprice to zero to test divide by zero check when
	// contracts are renewed
	settings := h.InternalSettings()
	settings.MinUploadBandwidthPrice = types.ZeroCurrency
	if err := h.SetInternalSettings(settings); err != nil {
		t.Fatal(err)
	}

	// Mine blocks to renew contract
	for i := types.BlockHeight(0); i < c.allowance.Period-c.allowance.RenewWindow; i++ {
		_, err = m.AddBlock()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Confirm Contracts got renewed
	err = build.Retry(200, 100*time.Millisecond, func() error {
		if len(c.Contracts()) != 1 {
			return errors.New("no contract")
		}
		if len(c.OldContracts()) != 1 {
			return errors.New("no old contract")
		}
		return nil
	})
	if err != nil {
		t.Error(err)
	}

	// Confirm maps are updated as expected
	if len(c.renewedFrom) != 1 {
		t.Fatalf("renewedFrom map should have 1 entry but has %v", len(c.renewedFrom))
	}
	if len(c.renewedTo) != 1 {
		t.Fatalf("renewedTo map should have 1 entry but has %v", len(c.renewedTo))
	}
	if c.renewedFrom[c.Contracts()[0].ID] != c.OldContracts()[0].ID {
		t.Fatalf(`Map assignment incorrect,
		expected:
		map[%v:%v]
		got:
		%v`, c.Contracts()[0].ID, c.OldContracts()[0].ID, c.renewedFrom)
	}
	if c.renewedTo[c.OldContracts()[0].ID] != c.Contracts()[0].ID {
		t.Fatalf(`Map assignment incorrect,
		expected:
		map[%v:%v]
		got:
		%v`, c.OldContracts()[0].ID, c.Contracts()[0].ID, c.renewedTo)
	}
}

// testWalletShim is used to test the walletBridge type.
type testWalletShim struct {
	nextAddressCalled bool
	startTxnCalled    bool
}

// These stub implementations for the walletShim interface set their respective
// booleans to true, allowing tests to verify that they have been called.
func (ws *testWalletShim) NextAddress() (types.UnlockConditions, error) {
	ws.nextAddressCalled = true
	return types.UnlockConditions{}, nil
}
func (ws *testWalletShim) GetAddress() (types.UnlockConditions, error) {
	return types.UnlockConditions{}, nil
}
func (ws *testWalletShim) StartTransaction() (modules.TransactionBuilder, error) {
	ws.startTxnCalled = true
	return nil, nil
}

// TestWalletBridge tests the walletBridge type.
func TestWalletBridge(t *testing.T) {
	shim := new(testWalletShim)
	bridge := WalletBridge{shim}
	bridge.NextAddress()
	if !shim.nextAddressCalled {
		t.Error("NextAddress was not called on the shim")
	}
	bridge.StartTransaction()
	if !shim.startTxnCalled {
		t.Error("StartTransaction was not called on the shim")
	}
}

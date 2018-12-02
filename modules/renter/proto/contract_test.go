package proto

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// TestContractUncommittedTxn tests that if a contract revision is left in an
// uncommitted state, either version of the contract can be recovered.
func TestContractUncommittedTxn(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// create contract set with one contract
	dir := build.TempDir(filepath.Join("proto", t.Name()))
	cs, err := NewContractSet(dir, modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	initialHeader := contractHeader{
		Transaction: types.Transaction{
			FileContractRevisions: []types.FileContractRevision{{
				NewRevisionNumber:    1,
				NewValidProofOutputs: []types.SiacoinOutput{{}, {}},
				UnlockConditions: types.UnlockConditions{
					PublicKeys: []types.SiaPublicKey{{}, {}},
				},
			}},
		},
	}
	initialRoots := []crypto.Hash{{1}}
	c, err := cs.managedInsertContract(initialHeader, initialRoots)
	if err != nil {
		t.Fatal(err)
	}

	// apply an update to the contract, but don't commit it
	sc := cs.mustAcquire(t, c.ID)
	revisedHeader := contractHeader{
		Transaction: types.Transaction{
			FileContractRevisions: []types.FileContractRevision{{
				NewRevisionNumber:    2,
				NewValidProofOutputs: []types.SiacoinOutput{{}, {}},
				UnlockConditions: types.UnlockConditions{
					PublicKeys: []types.SiaPublicKey{{}, {}},
				},
			}},
		},
		StorageSpending: types.NewCurrency64(7),
		UploadSpending:  types.NewCurrency64(17),
	}
	revisedRoots := []crypto.Hash{{1}, {2}}
	fcr := revisedHeader.Transaction.FileContractRevisions[0]
	newRoot := revisedRoots[1]
	storageCost := revisedHeader.StorageSpending.Sub(initialHeader.StorageSpending)
	bandwidthCost := revisedHeader.UploadSpending.Sub(initialHeader.UploadSpending)
	walTxn, err := sc.recordUploadIntent(fcr, newRoot, storageCost, bandwidthCost)
	if err != nil {
		t.Fatal(err)
	}

	// the state of the contract should match the initial state
	// NOTE: can't use reflect.DeepEqual for the header because it contains
	// types.Currency fields
	merkleRoots, err := sc.merkleRoots.merkleRoots()
	if err != nil {
		t.Fatal("failed to get merkle roots", err)
	}
	if !bytes.Equal(encoding.Marshal(sc.header), encoding.Marshal(initialHeader)) {
		t.Fatal("contractHeader should match initial contractHeader")
	} else if !reflect.DeepEqual(merkleRoots, initialRoots) {
		t.Fatal("Merkle roots should match initial Merkle roots")
	}

	// close and reopen the contract set.
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	cs, err = NewContractSet(dir, modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	// the uncommitted transaction should be stored in the contract
	sc = cs.mustAcquire(t, c.ID)
	if len(sc.unappliedTxns) != 1 {
		t.Fatal("expected 1 unappliedTxn, got", len(sc.unappliedTxns))
	} else if !bytes.Equal(sc.unappliedTxns[0].Updates[0].Instructions, walTxn.Updates[0].Instructions) {
		t.Fatal("WAL transaction changed")
	}
	// the state of the contract should match the initial state
	merkleRoots, err = sc.merkleRoots.merkleRoots()
	if err != nil {
		t.Fatal("failed to get merkle roots:", err)
	}
	if !bytes.Equal(encoding.Marshal(sc.header), encoding.Marshal(initialHeader)) {
		t.Fatal("contractHeader should match initial contractHeader", sc.header, initialHeader)
	} else if !reflect.DeepEqual(merkleRoots, initialRoots) {
		t.Fatal("Merkle roots should match initial Merkle roots")
	}

	// apply the uncommitted transaction
	err = sc.commitTxns()
	if err != nil {
		t.Fatal(err)
	}
	// the uncommitted transaction should be gone now
	if len(sc.unappliedTxns) != 0 {
		t.Fatal("expected 0 unappliedTxns, got", len(sc.unappliedTxns))
	}
	// the state of the contract should now match the revised state
	merkleRoots, err = sc.merkleRoots.merkleRoots()
	if err != nil {
		t.Fatal("failed to get merkle roots:", err)
	}
	if !bytes.Equal(encoding.Marshal(sc.header), encoding.Marshal(revisedHeader)) {
		t.Fatal("contractHeader should match revised contractHeader", sc.header, revisedHeader)
	} else if !reflect.DeepEqual(merkleRoots, revisedRoots) {
		t.Fatal("Merkle roots should match revised Merkle roots")
	}
}

// TestContractIncompleteWrite tests that if the merkle root section has the wrong
// length due to an incomplete write, it is truncated and the wal transactions
// are applied.
func TestContractIncompleteWrite(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	// create contract set with one contract
	dir := build.TempDir(filepath.Join("proto", t.Name()))
	cs, err := NewContractSet(dir, modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	initialHeader := contractHeader{
		Transaction: types.Transaction{
			FileContractRevisions: []types.FileContractRevision{{
				NewRevisionNumber:    1,
				NewValidProofOutputs: []types.SiacoinOutput{{}, {}},
				UnlockConditions: types.UnlockConditions{
					PublicKeys: []types.SiaPublicKey{{}, {}},
				},
			}},
		},
	}
	initialRoots := []crypto.Hash{{1}}
	c, err := cs.managedInsertContract(initialHeader, initialRoots)
	if err != nil {
		t.Fatal(err)
	}

	// apply an update to the contract, but don't commit it
	sc := cs.mustAcquire(t, c.ID)
	revisedHeader := contractHeader{
		Transaction: types.Transaction{
			FileContractRevisions: []types.FileContractRevision{{
				NewRevisionNumber:    2,
				NewValidProofOutputs: []types.SiacoinOutput{{}, {}},
				UnlockConditions: types.UnlockConditions{
					PublicKeys: []types.SiaPublicKey{{}, {}},
				},
			}},
		},
		StorageSpending: types.NewCurrency64(7),
		UploadSpending:  types.NewCurrency64(17),
	}
	revisedRoots := []crypto.Hash{{1}, {2}}
	fcr := revisedHeader.Transaction.FileContractRevisions[0]
	newRoot := revisedRoots[1]
	storageCost := revisedHeader.StorageSpending.Sub(initialHeader.StorageSpending)
	bandwidthCost := revisedHeader.UploadSpending.Sub(initialHeader.UploadSpending)
	_, err = sc.recordUploadIntent(fcr, newRoot, storageCost, bandwidthCost)
	if err != nil {
		t.Fatal(err)
	}

	// the state of the contract should match the initial state
	// NOTE: can't use reflect.DeepEqual for the header because it contains
	// types.Currency fields
	merkleRoots, err := sc.merkleRoots.merkleRoots()
	if err != nil {
		t.Fatal("failed to get merkle roots", err)
	}
	if !bytes.Equal(encoding.Marshal(sc.header), encoding.Marshal(initialHeader)) {
		t.Fatal("contractHeader should match initial contractHeader")
	} else if !reflect.DeepEqual(merkleRoots, initialRoots) {
		t.Fatal("Merkle roots should match initial Merkle roots")
	}

	// get the size of the merkle roots file.
	size, err := sc.merkleRoots.rootsFile.Size()
	if err != nil {
		t.Fatal(err)
	}
	// the size should be crypto.HashSize since we have exactly one root.
	if size != crypto.HashSize {
		t.Fatal("unexpected merkle root file size", size)
	}
	// truncate the rootsFile to simulate a corruption while writing the second
	// root.
	err = sc.merkleRoots.rootsFile.Truncate(size + crypto.HashSize/2)
	if err != nil {
		t.Fatal(err)
	}

	// close and reopen the contract set.
	if err := cs.Close(); err != nil {
		t.Fatal(err)
	}
	cs, err = NewContractSet(dir, modules.ProdDependencies)
	if err != nil {
		t.Fatal(err)
	}
	// the uncommitted txn should be gone.
	sc = cs.mustAcquire(t, c.ID)
	if len(sc.unappliedTxns) != 0 {
		t.Fatal("expected 0 unappliedTxn, got", len(sc.unappliedTxns))
	}
	if sc.merkleRoots.len() != 2 {
		t.Fatal("expected 2 roots, got", sc.merkleRoots.len())
	}
	cs.Return(sc)
	cs.Close()
}

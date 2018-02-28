package consensus

import (
	"testing"

	"github.com/HyperspaceProject/Hyperspace/modules"
	"github.com/HyperspaceProject/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// TestCommitDelayedSiacoinOutputDiffBadMaturity commits a delayed siacoin
// output that has a bad maturity height and triggers a panic.
func TestCommitDelayedSiacoinOutputDiffBadMaturity(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cst.Close()

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting error after corrupting database")
		}
	}()

	// Commit a delayed siacoin output with maturity height = cs.height()+1
	maturityHeight := cst.cs.dbBlockHeight() - 1
	id := types.SiacoinOutputID{'1'}
	dsco := types.SiacoinOutput{Value: types.NewCurrency64(1)}
	dscod := modules.DelayedSiacoinOutputDiff{
		Direction:      modules.DiffApply,
		ID:             id,
		SiacoinOutput:  dsco,
		MaturityHeight: maturityHeight,
	}
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
		return nil
	})
}

// TestCommitNodeDiffs probes the commitNodeDiffs method of the consensus set.
/*
func TestCommitNodeDiffs(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cst.Close()
	pb := cst.cs.dbCurrentProcessedBlock()
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitDiffSet(tx, pb, modules.DiffRevert) // pull the block node out of the consensus set.
		return nil
	})

	// For diffs that can be destroyed in the same block they are created,
	// create diffs that do just that. This has in the past caused issues upon
	// rewinding.
	scoid := types.SiacoinOutputID{'1'}
	scod0 := modules.SiacoinOutputDiff{
		Direction: modules.DiffApply,
		ID:        scoid,
	}
	scod1 := modules.SiacoinOutputDiff{
		Direction: modules.DiffRevert,
		ID:        scoid,
	}
	fcid := types.FileContractID{'2'}
	fcd0 := modules.FileContractDiff{
		Direction: modules.DiffApply,
		ID:        fcid,
	}
	fcd1 := modules.FileContractDiff{
		Direction: modules.DiffRevert,
		ID:        fcid,
	}
	dscoid := types.SiacoinOutputID{'4'}
	dscod := modules.DelayedSiacoinOutputDiff{
		Direction:      modules.DiffApply,
		ID:             dscoid,
		MaturityHeight: cst.cs.dbBlockHeight() + types.MaturityDelay,
	}
	pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod0)
	pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod1)
	pb.FileContractDiffs = append(pb.FileContractDiffs, fcd0)
	pb.FileContractDiffs = append(pb.FileContractDiffs, fcd1)
	pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		createUpcomingDelayedOutputMaps(tx, pb, modules.DiffApply)
		return nil
	})
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitNodeDiffs(tx, pb, modules.DiffApply)
		return nil
	})
	exists := cst.cs.db.inSiacoinOutputs(scoid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	exists = cst.cs.db.inFileContracts(fcid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	_ = cst.cs.db.Update(func(tx *bolt.Tx) error {
		commitNodeDiffs(tx, pb, modules.DiffRevert)
		return nil
	})
	exists = cst.cs.db.inSiacoinOutputs(scoid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
	exists = cst.cs.db.inFileContracts(fcid)
	if exists {
		t.Error("intradependent outputs not treated correctly")
	}
}
*/

/*
// TestSiacoinOutputDiff applies and reverts a siacoin output diff, then
// triggers an inconsistency panic.
func TestCommitSiacoinOutputDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cst.Close()

	// Commit a siacoin output diff.
	initialScosLen := cst.cs.db.lenSiacoinOutputs()
	id := types.SiacoinOutputID{'1'}
	sco := types.SiacoinOutput{Value: types.NewCurrency64(1)}
	scod := modules.SiacoinOutputDiff{
		Direction:     modules.DiffApply,
		ID:            id,
		SiacoinOutput: sco,
	}
	cst.cs.commitSiacoinOutputDiff(scod, modules.DiffApply)
	if cst.cs.db.lenSiacoinOutputs() != initialScosLen+1 {
		t.Error("siacoin output diff set did not increase in size")
	}
	if cst.cs.db.getSiacoinOutputs(id).Value.Cmp(sco.Value) != 0 {
		t.Error("wrong siacoin output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitSiacoinOutputDiff(scod, modules.DiffRevert)
	if cst.cs.db.lenSiacoinOutputs() != initialScosLen {
		t.Error("siacoin output diff set did not increase in size")
	}
	exists := cst.cs.db.inSiacoinOutputs(id)
	if exists {
		t.Error("siacoin output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitSiacoinOutputDiff(scod, modules.DiffApply)
	scod.Direction = modules.DiffRevert
	cst.cs.commitSiacoinOutputDiff(scod, modules.DiffApply)
	if cst.cs.db.lenSiacoinOutputs() != initialScosLen {
		t.Error("siacoin output diff set did not increase in size")
	}
	exists = cst.cs.db.inSiacoinOutputs(id)
	if exists {
		t.Error("siacoin output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitSiacoinOutputDiff(scod, modules.DiffRevert)
	if cst.cs.db.lenSiacoinOutputs() != initialScosLen+1 {
		t.Error("siacoin output diff set did not increase in size")
	}
	if cst.cs.db.getSiacoinOutputs(id).Value.Cmp(sco.Value) != 0 {
		t.Error("wrong siacoin output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitSiacoinOutputDiff {
			t.Error("expecting errBadCommitSiacoinOutputDiff, got", r)
		}
	}()
	// Try reverting a revert diff that was already reverted. (add an object
	// that already exists)
	cst.cs.commitSiacoinOutputDiff(scod, modules.DiffRevert)
}
*/

/*
// TestCommitFileContracttDiff applies and reverts a file contract diff, then
// triggers an inconsistency panic.
func TestCommitFileContractDiff(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Commit a file contract diff.
	initialFcsLen := cst.cs.db.lenFileContracts()
	id := types.FileContractID{'1'}
	fc := types.FileContract{Payout: types.NewCurrency64(1)}
	fcd := modules.FileContractDiff{
		Direction:    modules.DiffApply,
		ID:           id,
		FileContract: fc,
	}
	cst.cs.commitFileContractDiff(fcd, modules.DiffApply)
	if cst.cs.db.lenFileContracts() != initialFcsLen+1 {
		t.Error("siacoin output diff set did not increase in size")
	}
	if cst.cs.db.getFileContracts(id).Payout.Cmp(fc.Payout) != 0 {
		t.Error("wrong siacoin output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert)
	if cst.cs.db.lenFileContracts() != initialFcsLen {
		t.Error("siacoin output diff set did not increase in size")
	}
	exists := cst.cs.db.inFileContracts(id)
	if exists {
		t.Error("siacoin output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitFileContractDiff(fcd, modules.DiffApply)
	fcd.Direction = modules.DiffRevert
	cst.cs.commitFileContractDiff(fcd, modules.DiffApply)
	if cst.cs.db.lenFileContracts() != initialFcsLen {
		t.Error("siacoin output diff set did not increase in size")
	}
	exists = cst.cs.db.inFileContracts(id)
	if exists {
		t.Error("siacoin output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert)
	if cst.cs.db.lenFileContracts() != initialFcsLen+1 {
		t.Error("siacoin output diff set did not increase in size")
	}
	if cst.cs.db.getFileContracts(id).Payout.Cmp(fc.Payout) != 0 {
		t.Error("wrong siacoin output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitFileContractDiff {
			t.Error("expecting errBadCommitFileContractDiff, got", r)
		}
	}()
	// Try reverting an apply diff that was already reverted. (remove an object
	// that was already removed)
	fcd.Direction = modules.DiffApply                      // Object currently exists, but make the direction 'apply'.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert) // revert the application.
	cst.cs.commitFileContractDiff(fcd, modules.DiffRevert) // revert the application again, in error.
}
*/

// TestCommitDelayedSiacoinOutputDiff probes the commitDelayedSiacoinOutputDiff
// method of the consensus set.
/*
func TestCommitDelayedSiacoinOutputDiff(t *testing.T) {
	t.Skip("test isn't working, but checks the wrong code anyway")
	if testing.Short() {
		t.Skip()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Commit a delayed siacoin output with maturity height = cs.height()+1
	maturityHeight := cst.cs.height() + 1
	initialDscosLen := cst.cs.db.lenDelayedSiacoinOutputsHeight(maturityHeight)
	id := types.SiacoinOutputID{'1'}
	dsco := types.SiacoinOutput{Value: types.NewCurrency64(1)}
	dscod := modules.DelayedSiacoinOutputDiff{
		Direction:      modules.DiffApply,
		ID:             id,
		SiacoinOutput:  dsco,
		MaturityHeight: maturityHeight,
	}
	cst.cs.commitDelayedSiacoinOutputDiff(dscod, modules.DiffApply)
	if cst.cs.db.lenDelayedSiacoinOutputsHeight(maturityHeight) != initialDscosLen+1 {
		t.Fatal("delayed output diff set did not increase in size")
	}
	if cst.cs.db.getDelayedSiacoinOutputs(maturityHeight, id).Value.Cmp(dsco.Value) != 0 {
		t.Error("wrong delayed siacoin output value after committing a diff")
	}

	// Rewind the diff.
	cst.cs.commitDelayedSiacoinOutputDiff(dscod, modules.DiffRevert)
	if cst.cs.db.lenDelayedSiacoinOutputsHeight(maturityHeight) != initialDscosLen {
		t.Error("siacoin output diff set did not increase in size")
	}
	exists := cst.cs.db.inDelayedSiacoinOutputsHeight(maturityHeight, id)
	if exists {
		t.Error("siacoin output was not reverted")
	}

	// Restore the diff and then apply the inverse diff.
	cst.cs.commitDelayedSiacoinOutputDiff(dscod, modules.DiffApply)
	dscod.Direction = modules.DiffRevert
	cst.cs.commitDelayedSiacoinOutputDiff(dscod, modules.DiffApply)
	if cst.cs.db.lenDelayedSiacoinOutputsHeight(maturityHeight) != initialDscosLen {
		t.Error("siacoin output diff set did not increase in size")
	}
	exists = cst.cs.db.inDelayedSiacoinOutputsHeight(maturityHeight, id)
	if exists {
		t.Error("siacoin output was not reverted")
	}

	// Revert the inverse diff.
	cst.cs.commitDelayedSiacoinOutputDiff(dscod, modules.DiffRevert)
	if cst.cs.db.lenDelayedSiacoinOutputsHeight(maturityHeight) != initialDscosLen+1 {
		t.Error("siacoin output diff set did not increase in size")
	}
	if cst.cs.db.getDelayedSiacoinOutputs(maturityHeight, id).Value.Cmp(dsco.Value) != 0 {
		t.Error("wrong siacoin output value after committing a diff")
	}

	// Trigger an inconsistency check.
	defer func() {
		r := recover()
		if r != errBadCommitDelayedSiacoinOutputDiff {
			t.Error("expecting errBadCommitDelayedSiacoinOutputDiff, got", r)
		}
	}()
	// Try applying an apply diff that was already applied. (add an object
	// that already exists)
	dscod.Direction = modules.DiffApply                             // set the direction to apply
	cst.cs.commitDelayedSiacoinOutputDiff(dscod, modules.DiffApply) // apply an already existing delayed output.
}
*/

/*
// TestDeleteObsoleteDelayedOutputMapsSanity probes the sanity checks of the
// deleteObsoleteDelayedOutputMaps method of the consensus set.
func TestDeleteObsoleteDelayedOutputMapsSanity(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	pb := cst.cs.currentProcessedBlock()
	err = cst.cs.db.Update(func(tx *bolt.Tx) error {
		return commitDiffSet(tx, pb, modules.DiffRevert)
	})
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting an error after corrupting the database")
		}
	}()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expecting an error after corrupting the database")
		}

		// Trigger a panic by deleting a map with outputs in it during revert.
		err = cst.cs.db.Update(func(tx *bolt.Tx) error {
			return createUpcomingDelayedOutputMaps(tx, pb, modules.DiffApply)
		})
		if err != nil {
			t.Fatal(err)
		}
		err = cst.cs.db.Update(func(tx *bolt.Tx) error {
			return commitNodeDiffs(tx, pb, modules.DiffApply)
		})
		if err != nil {
			t.Fatal(err)
		}
		err = cst.cs.db.Update(func(tx *bolt.Tx) error {
			return deleteObsoleteDelayedOutputMaps(tx, pb, modules.DiffRevert)
		})
		if err != nil {
			t.Fatal(err)
		}
	}()

	// Trigger a panic by deleting a map with outputs in it during apply.
	err = cst.cs.db.Update(func(tx *bolt.Tx) error {
		return deleteObsoleteDelayedOutputMaps(tx, pb, modules.DiffApply)
	})
	if err != nil {
		t.Fatal(err)
	}
}
*/

/*
// TestGenerateAndApplyDiffSanity triggers the sanity checks in the
// generateAndApplyDiff method of the consensus set.
func TestGenerateAndApplyDiffSanity(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	pb := cst.cs.currentProcessedBlock()
	cst.cs.commitDiffSet(pb, modules.DiffRevert)

	defer func() {
		r := recover()
		if r != errRegenerateDiffs {
			t.Error("expected errRegenerateDiffs, got", r)
		}
	}()
	defer func() {
		r := recover()
		if r != errInvalidSuccessor {
			t.Error("expected errInvalidSuccessor, got", r)
		}

		// Trigger errRegenerteDiffs
		_ = cst.cs.generateAndApplyDiff(pb)
	}()

	// Trigger errInvalidSuccessor
	parent := cst.cs.db.getBlockMap(pb.Parent)
	parent.DiffsGenerated = false
	_ = cst.cs.generateAndApplyDiff(parent)
}
*/

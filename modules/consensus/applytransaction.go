package consensus

// applytransaction.go handles applying a transaction to the consensus set.
// There is an assumption that the transaction has already been verified.

import (
	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// applySiacoinInputs takes all of the siacoin inputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiacoinInputs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	// Remove all siacoin inputs from the unspent siacoin outputs list.
	for _, sci := range t.SiacoinInputs {
		sco, err := getSiacoinOutput(tx, sci.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sci.ParentID,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

// applySiacoinOutputs takes all of the siacoin outputs in a transaction and
// applies them to the state, updating the diffs in the processed block.
func applySiacoinOutputs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	// Add all siacoin outputs to the unspent siacoin outputs list.
	for i, sco := range t.SiacoinOutputs {
		scoid := t.SiacoinOutputID(uint64(i))
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffApply,
			ID:            scoid,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		// log.Printf("applySiacoinOutputs: %s %s %v %s", scod.SiacoinOutput.UnlockHash,
		// 	scod.SiacoinOutput.Value, scod.Direction, scod.ID)
		commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

// applyFileContracts iterates through all of the file contracts in a
// transaction and applies them to the state, updating the diffs in the proccesed
// block.
func applyFileContracts(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for i, fc := range t.FileContracts {
		fcid := t.FileContractID(uint64(i))
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcid,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)
	}
}

// applyTxFileContractRevisions iterates through all of the file contract
// revisions in a transaction and applies them to the state, updating the diffs
// in the processed block.
func applyFileContractRevisions(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for _, fcr := range t.FileContractRevisions {
		fc, err := getFileContract(tx, fcr.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}

		// Add the diff to delete the old file contract.
		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           fcr.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)

		// Add the diff to add the revised file contract.
		newFC := types.FileContract{
			FileSize:           fcr.NewFileSize,
			FileMerkleRoot:     fcr.NewFileMerkleRoot,
			WindowStart:        fcr.NewWindowStart,
			WindowEnd:          fcr.NewWindowEnd,
			Payout:             fc.Payout,
			ValidProofOutputs:  fcr.NewValidProofOutputs,
			MissedProofOutputs: fcr.NewMissedProofOutputs,
			UnlockHash:         fcr.NewUnlockHash,
			RevisionNumber:     fcr.NewRevisionNumber,
		}
		fcd = modules.FileContractDiff{
			Direction:    modules.DiffApply,
			ID:           fcr.ParentID,
			FileContract: newFC,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)
	}
}

// applyTxStorageProofs iterates through all of the storage proofs in a
// transaction and applies them to the state, updating the diffs in the processed
// block.
func applyStorageProofs(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for _, sp := range t.StorageProofs {
		fc, err := getFileContract(tx, sp.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}

		// Add all of the outputs in the ValidProofOutputs of the contract.
		for i, vpo := range fc.ValidProofOutputs {
			spoid := sp.ParentID.StorageProofOutputID(types.ProofValid, uint64(i))
			dscod := modules.DelayedSiacoinOutputDiff{
				Direction:      modules.DiffApply,
				ID:             spoid,
				SiacoinOutput:  vpo,
				MaturityHeight: pb.Height + types.MaturityDelay,
			}
			pb.DelayedSiacoinOutputDiffs = append(pb.DelayedSiacoinOutputDiffs, dscod)
			commitDelayedSiacoinOutputDiff(tx, dscod, modules.DiffApply)
		}

		fcd := modules.FileContractDiff{
			Direction:    modules.DiffRevert,
			ID:           sp.ParentID,
			FileContract: fc,
		}
		pb.FileContractDiffs = append(pb.FileContractDiffs, fcd)
		commitFileContractDiff(tx, fcd, modules.DiffApply)
	}
}

// applyTransaction applies the contents of a transaction to the ConsensusSet.
// This produces a set of diffs, which are stored in the blockNode containing
// the transaction. No verification is done by this function.
func applyTransaction(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	applySiacoinInputs(tx, pb, t)
	applySiacoinOutputs(tx, pb, t)
	applyFileContracts(tx, pb, t)
	applyFileContractRevisions(tx, pb, t)
	applyStorageProofs(tx, pb, t)
}

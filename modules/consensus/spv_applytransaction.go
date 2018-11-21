package consensus

// applytransaction.go handles applying a transaction to the consensus set.
// There is an assumption that the transaction has already been verified.

import (
	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

func applyFileContractRevisionsForSPV(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	for _, fcr := range t.FileContractRevisions {
		fc, err := getFileContract(tx, fcr.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		if err != nil {
			return // don't apply in spv mode when parent nil
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
func applyStorageProofsForSPV(tx *bolt.Tx, pb *processedBlock, pbh *modules.ProcessedBlockHeader, t types.Transaction) {
	for _, sp := range t.StorageProofs {
		fc, err := getFileContract(tx, sp.ParentID)
		if build.DEBUG && err != nil {
			panic(err)
		}
		if err != nil {
			return // don't apply in spv mode when parent nil
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
			pbh.DelayedSiacoinOutputDiffs = append(pbh.DelayedSiacoinOutputDiffs, dscod) // delayed siacoin output diffs stored in header
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

func applyTransactionForSPV(tx *bolt.Tx, pb *processedBlock, pbh *modules.ProcessedBlockHeader, t types.Transaction) {
	applySiacoinInputsForSPV(tx, pb, t)
	applySiacoinOutputs(tx, pb, t)
	applyFileContracts(tx, pb, t)
	applyFileContractRevisionsForSPV(tx, pb, t)
	applyStorageProofsForSPV(tx, pb, pbh, t)
}

func applySiacoinInputsForSPV(tx *bolt.Tx, pb *processedBlock, t types.Transaction) {
	// Remove all siacoin inputs from the unspent siacoin outputs list.
	for _, sci := range t.SiacoinInputs {
		sco, err := getSiacoinOutput(tx, sci.ParentID)
		if err == errNilItem {
			// not related inputs
			return
		}
		scod := modules.SiacoinOutputDiff{
			Direction:     modules.DiffRevert,
			ID:            sci.ParentID,
			SiacoinOutput: sco,
		}
		pb.SiacoinOutputDiffs = append(pb.SiacoinOutputDiffs, scod)
		// log.Printf("applySiacoinInputsForSPV: %s %s %v %s", scod.SiacoinOutput.UnlockHash,
		// 	scod.SiacoinOutput.Value, scod.Direction, scod.ID)
		commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
	}
}

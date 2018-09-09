package types

import (
	"github.com/HyperspaceApp/Hyperspace/crypto"
)

type (
	// A FileContractRevisionV0 revises an existing file contract. The ParentID
	// points to the file contract that is being revised. The UnlockConditions
	// are the conditions under which the revision is valid, and must match the
	// UnlockHash of the parent file contract. The Payout of the file contract
	// cannot be changed, but all other fields are allowed to be changed. The
	// sum of the outputs must match the original payout (taking into account
	// the fee for the proof payouts.) A revision number is included. When
	// getting accepted, the revision number of the revision must be higher
	// than any previously seen revision number for that file contract.
	//
	// FileContractRevisions enable trust-free modifications to existing file
	// contracts.
	FileContractRevisionV0 struct {
		ParentID          FileContractID     `json:"parentid"`
		UnlockConditions  UnlockConditionsV0 `json:"unlockconditions"`
		NewRevisionNumber uint64             `json:"newrevisionnumber"`

		NewFileSize           uint64          `json:"newfilesize"`
		NewFileMerkleRoot     crypto.Hash     `json:"newfilemerkleroot"`
		NewWindowStart        BlockHeight     `json:"newwindowstart"`
		NewWindowEnd          BlockHeight     `json:"newwindowend"`
		NewValidProofOutputs  []SiacoinOutput `json:"newvalidproofoutputs"`
		NewMissedProofOutputs []SiacoinOutput `json:"newmissedproofoutputs"`
		NewUnlockHash         UnlockHash      `json:"newunlockhash"`
	}
)

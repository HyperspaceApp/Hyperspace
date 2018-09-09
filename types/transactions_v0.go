package types

type (
	// A TransactionV0 is an atomic component of a block. Transactions can contain
	// inputs and outputs, file contracts, storage proofs, and even arbitrary
	// data. They can also contain signatures to prove that a given party has
	// approved the transaction, or at least a particular subset of it.
	//
	// Transactions can depend on other previous transactions in the same block,
	// but transactions cannot spend outputs that they create or otherwise be
	// self-dependent.
	TransactionV0 struct {
		SiacoinInputs         []SiacoinInputV0         `json:"siacoininputs"`
		SiacoinOutputs        []SiacoinOutput          `json:"siacoinoutputs"`
		FileContracts         []FileContract           `json:"filecontracts"`
		FileContractRevisions []FileContractRevisionV0 `json:"filecontractrevisions"`
		StorageProofs         []StorageProof           `json:"storageproofs"`
		MinerFees             []Currency               `json:"minerfees"`
		ArbitraryData         [][]byte                 `json:"arbitrarydata"`
		TransactionSignatures []TransactionSignature   `json:"transactionsignatures"`
	}

	// A SiacoinInputV0 consumes a SiacoinOutput and adds the siacoins to the set of
	// siacoins that can be spent in the transaction. The ParentID points to the
	// output that is getting consumed, and the UnlockConditions contain the rules
	// for spending the output. The UnlockConditions must match the UnlockHash of
	// the output.
	SiacoinInputV0 struct {
		ParentID         SiacoinOutputID  `json:"parentid"`
		UnlockConditions UnlockConditionsV0 `json:"unlockconditions"`
	}

)

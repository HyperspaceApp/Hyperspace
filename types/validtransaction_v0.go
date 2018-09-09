package types

// validtransaction.go has functions for checking whether a transaction is
// valid outside of the context of a consensus set. This means checking the
// size of the transaction, the content of the signatures, and a large set of
// other rules that are inherent to how a transaction should be constructed.

// correctFileContracts checks that the file contracts adhere to the file
// contract rules.
func (t TransactionV0) correctFileContracts(currentHeight BlockHeight) error {
	// Check that FileContract rules are being followed.
	for _, fc := range t.FileContracts {
		// Check that start and expiration are reasonable values.
		if fc.WindowStart <= currentHeight {
			return ErrFileContractWindowStartViolation
		}
		if fc.WindowEnd <= fc.WindowStart {
			return ErrFileContractWindowEndViolation
		}

		// Check that the proof outputs sum to the payout
		var validProofOutputSum, missedProofOutputSum Currency
		for _, output := range fc.ValidProofOutputs {
			// if output.Value.IsZero() {
			// 	return ErrZeroOutput
			// }
			validProofOutputSum = validProofOutputSum.Add(output.Value)
		}
		for _, output := range fc.MissedProofOutputs {
			// if output.Value.IsZero() {
			// 	return ErrZeroOutput
			// }
			missedProofOutputSum = missedProofOutputSum.Add(output.Value)
		}
		outputPortion := fc.Payout
		if validProofOutputSum.Cmp(outputPortion) != 0 {
			return ErrFileContractOutputSumViolation
		}
		if missedProofOutputSum.Cmp(outputPortion) != 0 {
			return ErrFileContractOutputSumViolation
		}
	}
	return nil
}

// correctFileContractRevisions checks that any file contract revisions adhere
// to the revision rules.
func (t TransactionV0) correctFileContractRevisions(currentHeight BlockHeight) error {
	for _, fcr := range t.FileContractRevisions {
		// Check that start and expiration are reasonable values.
		if fcr.NewWindowStart <= currentHeight {
			return ErrFileContractWindowStartViolation
		}
		if fcr.NewWindowEnd <= fcr.NewWindowStart {
			return ErrFileContractWindowEndViolation
		}

		// Check that the valid outputs and missed outputs sum to the same
		// value.
		var validProofOutputSum, missedProofOutputSum Currency
		for _, output := range fcr.NewValidProofOutputs {
			// if output.Value.IsZero() {
			// 	log.Println("fcr.NewValidProofOutputs")
			// 	return ErrZeroOutput
			// }
			validProofOutputSum = validProofOutputSum.Add(output.Value)
		}
		for _, output := range fcr.NewMissedProofOutputs {
			// if output.Value.IsZero() {
			// 	log.Printf("fcr.NewMissedProofOutputs:%d\n", i)
			// 	return ErrZeroOutput
			// }
			missedProofOutputSum = missedProofOutputSum.Add(output.Value)
		}
		if validProofOutputSum.Cmp(missedProofOutputSum) != 0 {
			return ErrFileContractOutputSumViolation
		}
	}
	return nil
}

// fitsInABlock checks if the transaction is likely to fit in a block.
// Transactions must be smaller than 64 KiB.
func (t TransactionV0) fitsInABlock(currentHeight BlockHeight) error {
	// Check that the transaction will fit inside of a block, leaving 5kb for
	// overhead.
	size := uint64(t.MarshalSiaSize())
	if size > BlockSizeLimit-5e3 {
		return ErrTransactionTooLarge
	}
	if size > OakTxnSizeLimit {
		return ErrTransactionTooLarge
	}
	return nil
}

// followsMinimumValues checks that all outputs adhere to the rules for the
// minimum allowed value (generally 1).
func (t TransactionV0) followsMinimumValues() error {
	for _, sco := range t.SiacoinOutputs {
		if sco.Value.IsZero() {
			return ErrZeroOutput
		}
	}
	for _, fc := range t.FileContracts {
		if fc.Payout.IsZero() {
			return ErrZeroOutput
		}
	}
	for _, fee := range t.MinerFees {
		if fee.IsZero() {
			return ErrZeroMinerFee
		}
	}
	return nil
}

// FollowsStorageProofRules checks that a transaction follows the limitations
// placed on transactions that have storage proofs.
func (t TransactionV0) followsStorageProofRules() error {
	// No storage proofs, no problems.
	if len(t.StorageProofs) == 0 {
		return nil
	}

	// If there are storage proofs, there can be no siacoin outputs,
	// new file contracts, or file contract terminations. These
	// restrictions are in place because a storage proof can be invalidated by
	// a simple reorg, which will also invalidate the rest of the transaction.
	// These restrictions minimize blockchain turbulence. These other types
	// cannot be invalidated by a simple reorg, and must instead by replaced by
	// a conflicting transaction.
	if len(t.SiacoinOutputs) != 0 {
		return ErrStorageProofWithOutputs
	}
	if len(t.FileContracts) != 0 {
		return ErrStorageProofWithOutputs
	}
	if len(t.FileContractRevisions) != 0 {
		return ErrStorageProofWithOutputs
	}

	return nil
}

// noRepeats checks that a transaction does not spend multiple outputs twice,
// submit two valid storage proofs for the same file contract, etc. We
// frivolously check that a file contract termination and storage proof don't
// act on the same file contract. There is very little overhead for doing so,
// and the check is only frivolous because of the current rule that file
// contract terminations are not valid after the proof window opens.
func (t TransactionV0) noRepeats() error {
	// Check that there are no repeat instances of siacoin outputs, storage
	// proofs, or contract terminations.
	siacoinInputs := make(map[SiacoinOutputID]struct{})
	for _, sci := range t.SiacoinInputs {
		_, exists := siacoinInputs[sci.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		siacoinInputs[sci.ParentID] = struct{}{}
	}
	doneFileContracts := make(map[FileContractID]struct{})
	for _, sp := range t.StorageProofs {
		_, exists := doneFileContracts[sp.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		doneFileContracts[sp.ParentID] = struct{}{}
	}
	for _, fcr := range t.FileContractRevisions {
		_, exists := doneFileContracts[fcr.ParentID]
		if exists {
			return ErrDoubleSpend
		}
		doneFileContracts[fcr.ParentID] = struct{}{}
	}
	return nil
}

// validUnlockConditionsV0 checks that the conditions of uc have been met. The
// height is taken as input so that modules who might be at a different height
// can do the verification without needing to use their own function.
// Additionally, it means that the function does not need to be a method of the
// consensus set.
func validUnlockConditionsV0(uc UnlockConditionsV0, currentHeight BlockHeight) (err error) {
	if uc.Timelock > currentHeight {
		return ErrTimelockNotSatisfied
	}
	return
}

// validUnlockConditions checks that all of the unlock conditions in the
// transaction are valid.
func (t TransactionV0) validUnlockConditions(currentHeight BlockHeight) (err error) {
	for _, sci := range t.SiacoinInputs {
		err = validUnlockConditionsV0(sci.UnlockConditions, currentHeight)
		if err != nil {
			return
		}
	}
	for _, fcr := range t.FileContractRevisions {
		err = validUnlockConditionsV0(fcr.UnlockConditions, currentHeight)
		if err != nil {
			return
		}
	}
	return
}

// StandaloneValid returns an error if a transaction is not valid in any
// context, for example if the same output is spent twice in the same
// transaction. StandaloneValid will not check that all outputs being spent are
// legal outputs, as it has no confirmed or unconfirmed set to look at.
func (t TransactionV0) StandaloneValid(currentHeight BlockHeight) (err error) {
	err = t.fitsInABlock(currentHeight)
	if err != nil {
		return
	}
	err = t.followsStorageProofRules()
	if err != nil {
		return
	}
	err = t.noRepeats()
	if err != nil {
		return
	}
	err = t.followsMinimumValues()
	if err != nil {
		return
	}
	err = t.correctFileContracts(currentHeight)
	if err != nil {
		return
	}
	err = t.correctFileContractRevisions(currentHeight)
	if err != nil {
		return
	}
	err = t.validUnlockConditions(currentHeight)
	if err != nil {
		return
	}
	err = t.validSignatures(currentHeight)
	if err != nil {
		return
	}
	return
}

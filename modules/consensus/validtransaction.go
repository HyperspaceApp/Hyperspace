package consensus

import (
	"errors"
	"math/big"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

var (
	errAlteredRevisionPayouts     = errors.New("file contract revision has altered payout volume")
	errInvalidStorageProof        = errors.New("provided storage proof is invalid")
	errLateRevision               = errors.New("file contract revision submitted after deadline")
	errLowRevisionNumber          = errors.New("transaction has a file contract with an outdated revision number")
	errMissingSiacoinOutput       = errors.New("transaction spends a nonexisting siacoin output")
	errSiacoinInputOutputMismatch = errors.New("siacoin inputs do not equal siacoin outputs for transaction")
	errUnfinishedFileContract     = errors.New("file contract window has not yet openend")
	errUnrecognizedFileContractID = errors.New("cannot fetch storage proof segment for unknown file contract")
	errWrongUnlockConditions      = errors.New("transaction contains incorrect unlock conditions")
)

// validSiacoins checks that the siacoin inputs and outputs are valid in the
// context of the current consensus set.
func validSiacoins(tx *bolt.Tx, t types.Transaction) error {
	scoBucket := tx.Bucket(SiacoinOutputs)
	var inputSum types.Currency
	for _, sci := range t.SiacoinInputs {
		// Check that the input spends an existing output.
		scoBytes := scoBucket.Get(sci.ParentID[:])
		//log.Printf("errMissingSiacoinOutput: %s", sci.ParentID.String())
		if scoBytes == nil {
			return errMissingSiacoinOutput
		}

		// Check that the unlock conditions match the required unlock hash.
		var sco types.SiacoinOutput
		err := encoding.Unmarshal(scoBytes, &sco)
		if build.DEBUG && err != nil {
			panic(err)
		}
		if sci.UnlockConditions.UnlockHash() != sco.UnlockHash {
			return errWrongUnlockConditions
		}

		inputSum = inputSum.Add(sco.Value)
	}
	if !inputSum.Equals(t.SiacoinOutputSum()) {
		return errSiacoinInputOutputMismatch
	}
	return nil
}

// storageProofSegment returns the index of the segment that needs to be proven
// exists in a file contract.
func storageProofSegment(tx *bolt.Tx, fcid types.FileContractID) (uint64, error) {
	// Check that the parent file contract exists.
	fcBucket := tx.Bucket(FileContracts)
	fcBytes := fcBucket.Get(fcid[:])
	if fcBytes == nil {
		return 0, errUnrecognizedFileContractID
	}

	// Decode the file contract.
	var fc types.FileContract
	err := encoding.Unmarshal(fcBytes, &fc)
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Get the trigger block id.
	blockPath := tx.Bucket(BlockPath)
	triggerHeight := fc.WindowStart - 1
	if triggerHeight > blockHeight(tx) {
		return 0, errUnfinishedFileContract
	}
	var triggerID types.BlockID
	copy(triggerID[:], blockPath.Get(encoding.EncUint64(uint64(triggerHeight))))

	// Get the index by appending the file contract ID to the trigger block and
	// taking the hash, then converting the hash to a numerical value and
	// modding it against the number of segments in the file. The result is a
	// random number in range [0, numSegments]. The probability is very
	// slightly weighted towards the beginning of the file, but because the
	// size difference between the number of segments and the random number
	// being modded, the difference is too small to make any practical
	// difference.
	seed := crypto.HashAll(triggerID, fcid)
	numSegments := int64(crypto.CalculateLeaves(fc.FileSize))
	seedInt := new(big.Int).SetBytes(seed[:])
	index := seedInt.Mod(seedInt, big.NewInt(numSegments)).Uint64()
	return index, nil
}

// validStorageProofs checks that the storage proofs are valid in the context
// of the consensus set.
func validStorageProofs(tx *bolt.Tx, t types.Transaction) error {
	for _, sp := range t.StorageProofs {
		// Check that the storage proof itself is valid.
		segmentIndex, err := storageProofSegment(tx, sp.ParentID)
		if err != nil {
			return err
		}

		fc, err := getFileContract(tx, sp.ParentID)
		if err != nil {
			return err
		}
		leaves := crypto.CalculateLeaves(fc.FileSize)
		segmentLen := uint64(crypto.SegmentSize)

		// If this segment chosen is the final segment, it should only be as
		// long as necessary to complete the filesize.
		if segmentIndex == leaves-1 {
			segmentLen = fc.FileSize % crypto.SegmentSize
		}
		if segmentLen == 0 {
			segmentLen = uint64(crypto.SegmentSize)
		}

		verified := crypto.VerifySegment(
			sp.Segment[:segmentLen],
			sp.HashSet,
			leaves,
			segmentIndex,
			fc.FileMerkleRoot,
		)
		if !verified && fc.FileSize > 0 {
			return errInvalidStorageProof
		}
	}

	return nil
}

// validFileContractRevision checks that each file contract revision is valid
// in the context of the current consensus set.
func validFileContractRevisions(tx *bolt.Tx, t types.Transaction) error {
	for _, fcr := range t.FileContractRevisions {
		fc, err := getFileContract(tx, fcr.ParentID)
		if err != nil {
			return err
		}

		// Check that the height is less than fc.WindowStart - revisions are
		// not allowed to be submitted once the storage proof window has
		// opened.  This reduces complexity for unconfirmed transactions.
		if blockHeight(tx) > fc.WindowStart {
			return errLateRevision
		}

		// Check that the revision number of the revision is greater than the
		// revision number of the existing file contract.
		if fc.RevisionNumber >= fcr.NewRevisionNumber {
			return errLowRevisionNumber
		}

		// Check that the unlock conditions match the unlock hash.
		if fcr.UnlockConditions.UnlockHash() != fc.UnlockHash {
			return errWrongUnlockConditions
		}

		// Check that the payout of the revision matches the payout of the
		// original, and that the payouts match each other.
		var validPayout, missedPayout, oldPayout types.Currency
		for _, output := range fcr.NewValidProofOutputs {
			validPayout = validPayout.Add(output.Value)
		}
		for _, output := range fcr.NewMissedProofOutputs {
			missedPayout = missedPayout.Add(output.Value)
		}
		for _, output := range fc.ValidProofOutputs {
			oldPayout = oldPayout.Add(output.Value)
		}
		if !validPayout.Equals(oldPayout) {
			return errAlteredRevisionPayouts
		}
		if !missedPayout.Equals(oldPayout) {
			return errAlteredRevisionPayouts
		}
	}
	return nil
}

// validTransaction checks that all fields are valid within the current
// consensus state. If not an error is returned.
func validTransaction(tx *bolt.Tx, t types.Transaction) error {
	// StandaloneValid will check things like signatures and properties that
	// should be inherent to the transaction. (storage proof rules, etc.)
	err := t.StandaloneValid(blockHeight(tx))
	if err != nil {
		return err
	}

	// Check that each portion of the transaction is legal given the current
	// consensus set.
	err = validSiacoins(tx, t)
	if err != nil {
		return err
	}
	err = validStorageProofs(tx, t)
	if err != nil {
		return err
	}
	err = validFileContractRevisions(tx, t)
	if err != nil {
		return err
	}
	return nil
}

func validTransactionForSPV(tx *bolt.Tx, t types.Transaction) error {
	// StandaloneValid will check things like signatures and properties that
	// should be inherent to the transaction. (storage proof rules, etc.)
	err := t.StandaloneValid(blockHeight(tx))
	if err != nil {
		return err
	}

	return nil
}

// tryTransactionSet applies the input transactions to the consensus set to
// determine if they are valid. An error is returned IFF they are not a valid
// set in the current consensus set. The size of the transactions and the set
// is not checked. After the transactions have been validated, a consensus
// change is returned detailing the diffs that the transactions set would have.
func (cs *ConsensusSet) tryTransactionSet(txns []types.Transaction) (modules.ConsensusChange, error) {
	// applyTransaction will apply the diffs from a transaction and store them
	// in a block node. diffHolder is the blockNode that tracks the temporary
	// changes. At the end of the function, all changes that were made to the
	// consensus set get reverted.
	diffHolder := new(processedBlock)

	// Boltdb will only roll back a tx if an error is returned. In the case of
	// TryTransactionSet, we want to roll back the tx even if there is no
	// error. So errSuccess is returned. An alternate method would be to
	// manually manage the tx instead of using 'Update', but that has safety
	// concerns and is more difficult to implement correctly.
	errSuccess := errors.New("success")
	err := cs.db.Update(func(tx *bolt.Tx) error {
		diffHolder.Height = blockHeight(tx)
		for _, txn := range txns {
			err := validTransaction(tx, txn)
			if err != nil {
				return err
			}
			applyTransaction(tx, diffHolder, txn)
		}
		return errSuccess
	})
	if err != errSuccess {
		return modules.ConsensusChange{}, err
	}
	cc := modules.ConsensusChange{
		SiacoinOutputDiffs:        diffHolder.SiacoinOutputDiffs,
		FileContractDiffs:         diffHolder.FileContractDiffs,
		DelayedSiacoinOutputDiffs: diffHolder.DelayedSiacoinOutputDiffs,
	}
	return cc, nil
}

// TryTransactionSet applies the input transactions to the consensus set to
// determine if they are valid. An error is returned IFF they are not a valid
// set in the current consensus set. The size of the transactions and the set
// is not checked. After the transactions have been validated, a consensus
// change is returned detailing the diffs that the transactions set would have.
func (cs *ConsensusSet) TryTransactionSet(txns []types.Transaction) (modules.ConsensusChange, error) {
	err := cs.tg.Add()
	if err != nil {
		return modules.ConsensusChange{}, err
	}
	defer cs.tg.Done()
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.tryTransactionSet(txns)
}

// LockedTryTransactionSet calls fn while under read-lock, passing it a
// version of TryTransactionSet that can be called under read-lock. This fixes
// an edge case in the transaction pool.
func (cs *ConsensusSet) LockedTryTransactionSet(fn func(func(txns []types.Transaction) (modules.ConsensusChange, error)) error) error {
	err := cs.tg.Add()
	if err != nil {
		return err
	}
	defer cs.tg.Done()
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return fn(cs.tryTransactionSet)
}

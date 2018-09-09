package types

import (
	"bytes"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
)

type (
	// A TransactionSignature is a signature that is included in the transaction.
	// The signature should correspond to a public key in one of the
	// UnlockConditions of the transaction. This key is specified first by
	// 'ParentID', which specifies the UnlockConditions, and then
	// 'PublicKeyIndex', which indicates the key in the UnlockConditions. There
	// are two types that use UnlockConditions: SiacoinInputs
	// and FileContractTerminations. Each of these types also references a
	// ParentID, and this is the hash that 'ParentID' must match. The 'Timelock'
	// prevents the signature from being used until a certain height.
	// 'CoveredFields' indicates which parts of the transaction are being signed;
	// see CoveredFields.
	TransactionSignatureV0 struct {
		ParentID       crypto.Hash   `json:"parentid"`
		PublicKeyIndex uint64        `json:"publickeyindex"`
		Timelock       BlockHeight   `json:"timelock"`
		CoveredFields  CoveredFields `json:"coveredfields"`
		Signature      []byte        `json:"signature"`
	}

	// UnlockConditionsV0 are a set of conditions which must be met to execute
	// certain actions, such as spending a SiacoinOutput or terminating a
	// FileContract.
	//
	// The simplest requirement is that the block containing the UnlockConditions
	// must have a height >= 'Timelock'.
	//
	// 'PublicKeys' specifies the set of keys that can be used to satisfy the
	// UnlockConditions; of these, at least 'SignaturesRequired' unique keys must sign
	// the transaction. The keys that do not need to use the same cryptographic
	// algorithm.
	//
	// If 'SignaturesRequired' == 0, the UnlockConditions are effectively "anyone can
	// unlock." If 'SignaturesRequired' > len('PublicKeys'), then the UnlockConditions
	// cannot be fulfilled under any circumstances.
	UnlockConditionsV0 struct {
		Timelock           BlockHeight    `json:"timelock"`
		PublicKeys         []SiaPublicKey `json:"publickeys"`
		SignaturesRequired uint64         `json:"signaturesrequired"`
	}

	// Each input has a list of public keys and a required number of signatures.
	// inputSignatures keeps track of which public keys have been used and how many
	// more signatures are needed.
	inputSignatures struct {
		remainingSignatures uint64
		possibleKeys        []SiaPublicKey
		usedKeys            map[uint64]struct{}
		index               int
	}
)

// SigHash returns the hash of the fields in a transaction covered by a given
// signature. See CoveredFields for more details.
func (t TransactionV0) SigHash(i int) (hash crypto.Hash) {
	cf := t.TransactionSignatures[i].CoveredFields
	h := crypto.NewHash()
	if cf.WholeTransaction {
		t.marshalSiaNoSignatures(h)
		h.Write(t.TransactionSignatures[i].ParentID[:])
		encoding.WriteUint64(h, t.TransactionSignatures[i].PublicKeyIndex)
		encoding.WriteUint64(h, uint64(t.TransactionSignatures[i].Timelock))
	} else {
		for _, input := range cf.SiacoinInputs {
			t.SiacoinInputs[input].MarshalSia(h)
		}
		for _, output := range cf.SiacoinOutputs {
			t.SiacoinOutputs[output].MarshalSia(h)
		}
		for _, contract := range cf.FileContracts {
			t.FileContracts[contract].MarshalSia(h)
		}
		for _, revision := range cf.FileContractRevisions {
			t.FileContractRevisions[revision].MarshalSia(h)
		}
		for _, storageProof := range cf.StorageProofs {
			t.StorageProofs[storageProof].MarshalSia(h)
		}
		for _, minerFee := range cf.MinerFees {
			t.MinerFees[minerFee].MarshalSia(h)
		}
		for _, arbData := range cf.ArbitraryData {
			encoding.WritePrefixedBytes(h, t.ArbitraryData[arbData])
		}
	}

	for _, sig := range cf.TransactionSignatures {
		t.TransactionSignatures[sig].MarshalSia(h)
	}

	h.Sum(hash[:0])
	return
}

// UnlockHash calculates the root hash of a Merkle tree of the
// UnlockConditions object. The leaves of this tree are formed by taking the
// hash of the timelock, the hash of the public keys (one leaf each), and the
// hash of the number of signatures. The keys are put in the middle because
// Timelock and SignaturesRequired are both low entropy fields; they can be
// protected by having random public keys next to them.
func (uc UnlockConditionsV0) UnlockHash() UnlockHash {
	var buf bytes.Buffer
	e := encoding.NewEncoder(&buf)
	tree := crypto.NewTree()
	e.WriteUint64(uint64(uc.Timelock))
	tree.Push(buf.Bytes())
	buf.Reset()
	for _, key := range uc.PublicKeys {
		key.MarshalSia(e)
		tree.Push(buf.Bytes())
		buf.Reset()
	}
	e.WriteUint64(uc.SignaturesRequired)
	tree.Push(buf.Bytes())
	return UnlockHash(tree.Root())
}

// validCoveredFields makes sure that all covered fields objects in the
// signatures follow the rules. This means that if 'WholeTransaction' is set to
// true, all fields except for 'Signatures' must be empty. All fields must be
// sorted numerically, and there can be no repeats.
func (t TransactionV0) validCoveredFields() error {
	for _, sig := range t.TransactionSignatures {
		// convenience variables
		cf := sig.CoveredFields
		fieldMaxs := []struct {
			field []uint64
			max   int
		}{
			{cf.SiacoinInputs, len(t.SiacoinInputs)},
			{cf.SiacoinOutputs, len(t.SiacoinOutputs)},
			{cf.FileContracts, len(t.FileContracts)},
			{cf.FileContractRevisions, len(t.FileContractRevisions)},
			{cf.StorageProofs, len(t.StorageProofs)},
			{cf.MinerFees, len(t.MinerFees)},
			{cf.ArbitraryData, len(t.ArbitraryData)},
			{cf.TransactionSignatures, len(t.TransactionSignatures)},
		}

		// Check that all fields are empty if 'WholeTransaction' is set, except
		// for the Signatures field which isn't affected.
		if cf.WholeTransaction {
			// 'WholeTransaction' does not check signatures.
			for _, fieldMax := range fieldMaxs[:len(fieldMaxs)-1] {
				if len(fieldMax.field) != 0 {
					return ErrWholeTransactionViolation
				}
			}
		}

		// Check that all fields are sorted, and without repeat values, and
		// that all elements point to objects that exists within the
		// transaction. If there are repeats, it means a transaction is trying
		// to sign the same object twice. This is unncecessary, and opens up a
		// DoS vector where the transaction asks the verifier to verify many GB
		// of data.
		for _, fieldMax := range fieldMaxs {
			if !sortedUnique(fieldMax.field, fieldMax.max) {
				return ErrSortedUniqueViolation
			}
		}
	}

	return nil
}

// validSignatures checks the validaty of all signatures in a transaction.
func (t *TransactionV0) validSignatures(currentHeight BlockHeight) error {
	// Check that all covered fields objects follow the rules.
	err := t.validCoveredFields()
	if err != nil {
		return err
	}

	// Create the inputSignatures object for each input.
	sigMap := make(map[crypto.Hash]*inputSignatures)
	for i, input := range t.SiacoinInputs {
		id := crypto.Hash(input.ParentID)
		_, exists := sigMap[id]
		if exists {
			return ErrDoubleSpend
		}

		sigMap[id] = &inputSignatures{
			remainingSignatures: input.UnlockConditions.SignaturesRequired,
			possibleKeys:        input.UnlockConditions.PublicKeys,
			usedKeys:            make(map[uint64]struct{}),
			index:               i,
		}
	}
	for i, revision := range t.FileContractRevisions {
		id := crypto.Hash(revision.ParentID)
		_, exists := sigMap[id]
		if exists {
			return ErrDoubleSpend
		}

		sigMap[id] = &inputSignatures{
			remainingSignatures: revision.UnlockConditions.SignaturesRequired,
			possibleKeys:        revision.UnlockConditions.PublicKeys,
			usedKeys:            make(map[uint64]struct{}),
			index:               i,
		}
	}

	// Check all of the signatures for validity.
	for i, sig := range t.TransactionSignatures {
		// Check that sig corresponds to an entry in sigMap.
		inSig, exists := sigMap[crypto.Hash(sig.ParentID)]
		if !exists || inSig.remainingSignatures == 0 {
			return ErrFrivolousSignature
		}
		// Check that sig's key hasn't already been used.
		_, exists = inSig.usedKeys[sig.PublicKeyIndex]
		if exists {
			return ErrPublicKeyOveruse
		}
		// Check that the public key index refers to an existing public key.
		if sig.PublicKeyIndex >= uint64(len(inSig.possibleKeys)) {
			return ErrInvalidPubKeyIndex
		}
		// Check that the timelock has expired.
		if sig.Timelock > currentHeight {
			return ErrPrematureSignature
		}

		// Check that the signature verifies. Multiple signature schemes are
		// supported.
		publicKey := inSig.possibleKeys[sig.PublicKeyIndex]
		switch publicKey.Algorithm {
		case SignatureEntropy:
			// Entropy cannot ever be used to sign a transaction.
			return ErrEntropyKey

		case SignatureEd25519:
			// Decode the public key and signature.
			var edPK crypto.PublicKey
			copy(edPK[:], publicKey.Key)
			var edSig crypto.Signature
			copy(edSig[:], sig.Signature)

			sigHash := t.SigHash(i)
			err = crypto.VerifyHash(sigHash, edPK, edSig)
			if err != nil {
				return err
			}

		default:
			// If the identifier is not recognized, assume that the signature
			// is valid. This allows more signature types to be added via soft
			// forking.
		}

		inSig.usedKeys[sig.PublicKeyIndex] = struct{}{}
		inSig.remainingSignatures--
	}

	// Check that all inputs have been sufficiently signed.
	for _, reqSigs := range sigMap {
		if reqSigs.remainingSignatures != 0 {
			return ErrMissingSignatures
		}
	}

	return nil
}

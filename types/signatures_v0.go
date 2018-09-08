package types

import (
	"github.com/HyperspaceApp/Hyperspace/crypto"
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

	// UnlockConditions are a set of conditions which must be met to execute
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

// validSignatures checks the validaty of all signatures in a transaction.
func (t *Transaction) validSignaturesV0(currentHeight BlockHeight) error {
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

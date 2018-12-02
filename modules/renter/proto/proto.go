package proto

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// Dependencies.
type (
	transactionBuilder interface {
		AddFileContract(types.FileContract) uint64
		AddMinerFee(types.Currency) uint64
		AddParents([]types.Transaction)
		AddSiacoinInput(types.SiacoinInput) uint64
		AddSiacoinOutput(types.SiacoinOutput) uint64
		AddTransactionSignature(types.TransactionSignature) uint64
		FundSiacoins(types.Currency) error
		FundContract(types.Currency) ([]types.SiacoinOutput, error)
		Sign(bool) ([]types.Transaction, error)
		UnconfirmedParents() ([]types.Transaction, error)
		View() (types.Transaction, []types.Transaction)
		ViewAdded() (parents, coins, signatures []int)
	}

	transactionPool interface {
		AcceptTransactionSet([]types.Transaction) error
		FeeEstimation() (min types.Currency, max types.Currency)
	}

	hostDB interface {
		IncrementSuccessfulInteractions(key types.SiaPublicKey)
		IncrementFailedInteractions(key types.SiaPublicKey)
	}
)

// ContractParams are supplied as an argument to FormContract.
type ContractParams struct {
	Allowance     modules.Allowance
	Host          modules.HostDBEntry
	Funding       types.Currency
	StartHeight   types.BlockHeight
	EndHeight     types.BlockHeight
	RefundAddress types.UnlockHash
	// TODO: add optional keypair
}

// A revisionSaver is called just before we send our revision signature to the host; this
// allows the revision and Merkle roots to be reloaded later if we desync from the host.
type revisionSaver func(types.FileContractRevision, []crypto.Hash) error

// A recentRevisionError occurs if the host reports a different revision
// number than expected.
type recentRevisionError struct {
	ours, theirs uint64
}

func (e *recentRevisionError) Error() string {
	return fmt.Sprintf("our revision number (%v) does not match the host's (%v)", e.ours, e.theirs)
}

// IsRevisionMismatch returns true if err was caused by the host reporting a
// different revision number than expected.
func IsRevisionMismatch(err error) bool {
	_, ok := err.(*recentRevisionError)
	return ok
}

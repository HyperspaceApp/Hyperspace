package modules

import (
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/types"
)

type (
	// ThirdpartyRenterContract represents a contract formed by the renter.
	ThirdpartyRenterContract struct {
		ID            types.FileContractID `json:"id"`
		HostPublicKey types.SiaPublicKey   `json:"hostpublickey"`
		Transaction   types.Transaction    `json:"transaction"`

		StartHeight types.BlockHeight `json:"startheight"`
		EndHeight   types.BlockHeight `json:"endheight"`

		// RenterFunds is the amount remaining in the contract that the renter can
		// spend.
		RenterFunds types.Currency `json:"reterfunds"`

		// The FileContract does not indicate what funds were spent on, so we have
		// to track the various costs manually.
		DownloadSpending types.Currency `json:"downloadspending"`
		StorageSpending  types.Currency `json:"storagespending"`
		UploadSpending   types.Currency `json:"uploadspending"`

		// TotalCost indicates the amount of money that the renter spent and/or
		// locked up while forming a contract. This includes fees, and includes
		// funds which were allocated (but not necessarily committed) to spend on
		// uploads/downloads/storage.
		TotalCost types.Currency `json:"totalcost"`

		// ContractFee is the amount of money paid to the host to cover potential
		// future transaction fees that the host may incur, and to cover any other
		// overheads the host may have.
		//
		// TxnFee is the amount of money spent on the transaction fee when putting
		// the renter contract on the blockchain.
		ContractFee types.Currency `json:"contractfee"`

		TxnFee types.Currency `json:"txnfee"`

		Roots []crypto.Hash `json:"roots"`

		// SecretKey crypto.SecretKey `json:"secretkey"`
	}

	// ThirdpartyRenterContracts is a holder of contracts info
	ThirdpartyRenterContracts struct {
		Contracts []ThirdpartyRenterContract `json:"contracts"`
		Height    types.BlockHeight          `json:"height"`
	}

	// ThirdpartyRenterRevisionUpdator help update thirdparty revision
	ThirdpartyRenterRevisionUpdator struct {
		ID               types.FileContractID `json:"id"`
		Action           types.Specifier      `json:"action"`
		Transaction      types.Transaction    `json:"transaction"`
		DownloadSpending types.Currency       `json:"downloadspending"`
		StorageSpending  types.Currency       `json:"storagespending"`
		UploadSpending   types.Currency       `json:"uploadspending"`
		SectorRoot       crypto.Hash          `json:"sectorroot"`
		SectorRoots      []crypto.Hash        `json:"sectorroots"`
	}

	// ThirdpartySignPOSTParams contains the unsigned transaction
	ThirdpartySignPOSTParams struct {
		ID          types.FileContractID `json:"id"`
		Transaction types.Transaction    `json:"transaction"`
	}

	// ThirdpartySignPOSTResp contains the signed transaction.
	ThirdpartySignPOSTResp struct {
		Transaction types.Transaction `json:"transaction"`
	}

	// ThirdpartySignChallengePOSTParams is the param for challenge sign
	ThirdpartySignChallengePOSTParams struct {
		ID        types.FileContractID `json:"id"`
		Challenge crypto.Hash          `json:"challenge"`
	}

	// ThirdpartySignChallengePOSTResp is the signed challenge
	ThirdpartySignChallengePOSTResp struct {
		Signature crypto.Signature `json:"signature"`
	}
)

const (
	// ThirdpartyDir is the name of the directory that is used to store the
	// third party's persistent data.
	ThirdpartyDir = "thirdparty"
)

var (
	// ThirdpartySync is the specifier for syncing
	ThirdpartySync = types.Specifier{'T', 'h', 'i', 'r', 'd', 'p', 'a', 'r', 't', 'y', 'S', 'y', 'n', 'c'}
)

// A Thirdparty uploads, tracks, repairs, and downloads a set of files for the
// user.
type Thirdparty interface {
	// ActiveHosts provides the list of hosts that the renter is selecting,
	// sorted by preference.
	ActiveHosts() []HostDBEntry

	// AllHosts returns the full list of hosts known to the renter.
	AllHosts() []HostDBEntry

	// Close closes the Renter.
	Close() error

	// CancelContract cancels a specific contract of the renter.
	// CancelContract(id types.FileContractID) error

	// Contracts returns the staticContracts of the renter's hostContractor.
	Contracts() []RenterContract

	// ThirdpartyContracts returns the staticContracts of the renter's hostContractor.
	ThirdpartyContracts() []ThirdpartyRenterContract

	// OldContracts returns an array of host contractor's oldContracts
	OldContracts() []RenterContract

	// RecoverableContracts returns the contracts that the contractor deems
	// recoverable. That means they are not expired yet and also not part of the
	// active contracts. Usually this should return an empty slice unless the host
	// isn't available for recovery or something went wrong.
	RecoverableContracts() []RecoverableContract

	// hostdb
	Settings() RenterSettings

	// SetSettings sets the Renter's settings.
	SetSettings(RenterSettings) error

	// Host provides the DB entry and score breakdown for the requested host.
	Host(pk types.SiaPublicKey) (HostDBEntry, bool)

	// SetFilterMode set the filter's mode
	SetFilterMode(lm FilterMode, hosts []types.SiaPublicKey) error

	// InitialScanComplete returns a boolean indicating if the initial scan of the
	// hostdb is completed.
	InitialScanComplete() (bool, error)

	// EstimateHostScore will return the score for a host with the provided
	// settings, assuming perfect age and uptime adjustments
	EstimateHostScore(entry HostDBEntry, allowance Allowance) HostScoreBreakdown

	// ScoreBreakdown will return the score for a host db entry using the
	// hostdb's weighting algorithm.
	ScoreBreakdown(entry HostDBEntry) HostScoreBreakdown

	// UpdateContracatRevision help update thirdparty contract
	UpdateContractRevision(ThirdpartyRenterRevisionUpdator) error

	// Sign help thirdparty renter to sign upload/download action
	Sign(id types.FileContractID, txn *types.Transaction) error

	// SignChallenge help sign contract challenge
	SignChallenge(id types.FileContractID, challenge crypto.Hash) (crypto.Signature, error)
}

package modules

import (
	"github.com/HyperspaceApp/Hyperspace/types"
)

const (
	// ThirdpartyDir is the name of the directory that is used to store the
	// third party's persistent data.
	ThirdpartyDir = "thirdparty"
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
}

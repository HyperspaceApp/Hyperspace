package modules

const (
	// ThirdpartyDir is the name of the directory that is used to store the
	// third party's persistent data.
	ThirdpartyRenterDir = "thirdpartyrenter"
)

// A ThirdpartyRenter uploads, tracks, and downloads a set of files for the
// user.
type ThirdpartyRenter interface {

	// Close closes the Renter.
	Close() error
}

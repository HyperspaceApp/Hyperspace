package modules

import "regexp"

const (
	// ThirdpartyRenterDir is the name of the directory that is used to store the
	// third party renter's persistent data.
	ThirdpartyRenterDir = "thirdpartyrenter"
)

// A ThirdpartyRenter uploads, tracks, and downloads a set of files for the
// user.
type ThirdpartyRenter interface {

	// Close closes the Renter.
	Close() error

	// Download performs a download according to the parameters passed, including
	// downloads of `offset` and `length` type.
	Download(params RenterDownloadParameters) error

	// Download performs a download according to the parameters passed without
	// blocking, including downloads of `offset` and `length` type.
	// DownloadAsync(params RenterDownloadParameters) error

	// Upload uploads a file using the input parameters.
	Upload(FileUploadParams) error

	// CreateDir creates a directory for the renter
	CreateDir(siaPath string) error

	// DeleteDir deletes a directory from the renter
	DeleteDir(siaPath string) error

	// DirList lists the directories and the files stored in a siadir
	DirList(siaPath string) ([]DirectoryInfo, []FileInfo, error)

	// File returns information on specific file queried by user
	File(siaPath string) (FileInfo, error)

	// FileList returns information on all of the files stored by the renter.
	FileList(filter ...*regexp.Regexp) []FileInfo
}

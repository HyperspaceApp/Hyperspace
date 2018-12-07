package siatest

type (
	// RemoteDir is a helper struct that represents a directory on the Sia
	// network.
	RemoteDir struct {
		hyperspacepath string
	}
)

// HyperspacePath returns the hyperspacepath of a remote directory.
func (rd *RemoteDir) HyperspacePath() string {
	return rd.hyperspacepath
}

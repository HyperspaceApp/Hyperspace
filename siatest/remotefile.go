package siatest

import (
	"github.com/HyperspaceApp/Hyperspace/crypto"
)

type (
	// RemoteFile is a helper struct that represents a file uploaded to the Sia
	// network.
	RemoteFile struct {
		checksum crypto.Hash
		siaPath  string
	}
)

// HyperspacePath returns the siaPath of a remote file.
func (rf RemoteFile) HyperspacePath() string {
	return rf.siaPath
}

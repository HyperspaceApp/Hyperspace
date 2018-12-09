package siatest

import (
	"sync"

	"github.com/HyperspaceApp/Hyperspace/crypto"
)

type (
	// RemoteFile is a helper struct that represents a file uploaded to the Sia
	// network.
	RemoteFile struct {
		checksum crypto.Hash
		siaPath  string

		mu sync.Mutex
	}
)

// Checksum returns the checksum of a remote file.
func (rf *RemoteFile) Checksum() crypto.Hash {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.checksum
}

// HyperspacePath returns the hyperspacePath of a remote file.
func (rf *RemoteFile) HyperspacePath() string {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.siaPath
}

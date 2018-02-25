package siatest

import (
	"github.com/HyperspaceProject/Hyperspace/crypto"
)

type (
	// RemoteFile is a helper struct that represents a file uploaded to the Sia
	// network.
	RemoteFile struct {
		checksum crypto.Hash
		siaPath  string
	}
)

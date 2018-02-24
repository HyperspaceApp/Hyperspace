package siatest

import (
	"github.com/HardDriveCoin/HardDriveCoin/crypto"
)

type (
	// RemoteFile is a helper struct that represents a file uploaded to the Sia
	// network.
	RemoteFile struct {
		checksum crypto.Hash
		siaPath  string
	}
)

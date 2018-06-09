package modules

import (
// "github.com/NebulousLabs/Sia/types"
)

const (
	// IndexDir names the directory that contains the index persistence.
	IndexDir = "index"
)

var (
// Whatever variables we need as we go
)

type (
	// Index is a module help import info to RDB like Mysql and caculat all address coin info
	Index interface {
		// Scan will go through every block and try to sync every block info
		Scan() error

		// Close closes the Index.
		Close() error
	}
)

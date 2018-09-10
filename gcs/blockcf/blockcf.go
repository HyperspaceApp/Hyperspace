// Copyright (c) 2017 The btcsuite developers
// Copyright (c) 2017 The Lightning Network Developers
// Copyright (c) 2018 The Decred developers
// Copyright (c) 2018 The Hyperspace developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

/*
Package blockcf provides functions for building committed filters for blocks
using Golomb-coded sets in a way that is useful for light clients such as SPV
wallets.

Committed filters are a reversal of how bloom filters are typically used by a
light client: a consensus-validating full node commits to filters for every
block with a predetermined collision probability and light clients match against
the filters locally rather than uploading personal data to other nodes.  If a
filter matches, the light client should fetch the entire block and further
inspect it for relevant transactions.
*/
package blockcf

import (
	"github.com/HyperspaceApp/Hyperspace/gcs"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// P is the collision probability used for block committed filters (2^-20)
const P = 20

// Entries describes all of the filter entries used to create a GCS filter and
// provides methods for appending data structures found in blocks.
type Entries [][]byte

// AddUnlockhash adds a unlock hash to the entries
func (e *Entries) AddUnlockhash(unlockhash types.UnlockHash) {
	*e = append(*e, unlockhash[:])
}

// Key creates a block committed filter key by truncating a block hash to the
// key size.
func Key(hash *types.BlockID) [gcs.KeySize]byte {
	var key [gcs.KeySize]byte
	copy(key[:], hash[:])
	return key
}

// BuildFilter builds a GCS filter from a block.  A GCS filter will
// contain all the previous output unlockhash spent within a block, as well as
// the data pushes within all the outputs unlockhash created within a block
// which can be spent by regular transactions.
func BuildFilter(block *types.Block) (*gcs.Filter, error) {
	var data Entries
	for _, t := range block.Transactions {
		for _, sco := range t.SiacoinOutputs {
			data.AddUnlockhash(sco.UnlockHash)
		}
		for _, sci := range t.SiacoinInputs {
			data.AddUnlockhash(sci.UnlockConditions.UnlockHash())
		}
	}

	// Create the key by truncating the block hash.
	blockHash := block.ID()
	key := Key(&blockHash)

	return gcs.NewFilter(P, key, data)
}

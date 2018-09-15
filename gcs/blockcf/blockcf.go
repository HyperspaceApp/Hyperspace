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

// AddUnlockHash adds a unlock hash to the entries
func (e *Entries) AddUnlockHash(unlockhash types.UnlockHash) {
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
//
// unlockHashes is a list of unlock hashes to be added to this filter, in addition
// to those directly visible form this block. These are expected to be hashes
// from file contracts that are associated with storage proofs in this block.
// TODO please see the note by FileContractUnlockHashMap
func BuildFilter(block *types.Block, unlockHashes []types.UnlockHash) (*types.GCSFilter, error) {
	var data Entries
	for _, t := range block.Transactions {
		for _, sco := range t.SiacoinOutputs {
			data.AddUnlockHash(sco.UnlockHash)
		}
		for _, sci := range t.SiacoinInputs {
			data.AddUnlockHash(sci.UnlockConditions.UnlockHash())
		}
		for _, fc := range t.FileContracts {
			for _, hash := range fc.OutputUnlockHashes() {
				data.AddUnlockHash(hash)
			}
		}
		for _, fcr := range t.FileContractRevisions {
			for _, hash := range fcr.OutputUnlockHashes() {
				data.AddUnlockHash(hash)
			}
		}
	}
	for _, minerPayout := range block.MinerPayouts {
		data.AddUnlockHash(minerPayout.UnlockHash)
	}
	for _, unlockHash := range unlockHashes {
		data.AddUnlockHash(unlockHash)
	}

	// Create the key by truncating the block hash.
	blockHash := block.ID()
	key := Key(&blockHash)

	newFilter, err := gcs.NewFilter(P, key, data)
	if err != nil {
		return nil, err
	}
	gcsFilter := types.NewGCSFilter(newFilter)
	return &gcsFilter, nil
}

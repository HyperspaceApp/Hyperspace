package siafile

import (
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/writeaheadlog"
)

const (
	// pageSize is the size of a physical page on disk.
	pageSize = 4096

	// defaultReservedMDPages is the number of pages we reserve for the
	// metadata when we create a new siaFile. Should the metadata ever grow
	// larger than that, new pages are added on demand.
	defaultReservedMDPages = 1

	// updateInsertName is the name of a siaFile update that inserts data at a specific index.
	updateInsertName = "SiaFileInsert"

	// updateDeleteName is the name of a siaFile update that deletes the
	// specified file.
	updateDeleteName = "SiaFileDelete"

	// marshaledPieceSize is the size of a piece on disk. It consists of a 4
	// byte pieceIndex, a 4 byte table offset and a hash.
	marshaledPieceSize = 4 + 4 + crypto.HashSize

	// marshaledChunkOverhead is the size of a marshaled chunk on disk minus
	// the encoded pieces. It consists of the 16 byte extension info and a 2
	// byte length prefix for the pieces.
	marshaledChunkOverhead = 16 + 2

	// pubKeyTablePruneThreshold is the number of unused hosts a SiaFile can
	// store in its host key table before it is pruned.
	pubKeyTablePruneThreshold = 50

	// threadDepth is how deep the ThreadType will track calling files and
	// calling lines
	threadDepth = 3
)

var (
	// ecReedSolomon is the marshaled type of the reed solomon coder.
	ecReedSolomon = modules.ErasureCoderType{0, 0, 0, 1}

	// ecReedSolomonSubShards64 is the marshaled type of the reed solomon coder
	// for files where every 64 bytes of an encoded piece can be decoded
	// separately.
	ecReedSolomonSubShards64 = modules.ErasureCoderType{0, 0, 0, 2}
)

// marshaledChunkSize is a helper method that returns the size of a chunk on
// disk given the number of pieces the chunk contains.
func marshaledChunkSize(numPieces int) int64 {
	return marshaledChunkOverhead + marshaledPieceSize*int64(numPieces)
}

// IsSiaFileUpdate is a helper method that makes sure that a wal update belongs
// to the SiaFile package.
func IsSiaFileUpdate(update writeaheadlog.Update) bool {
	switch update.Name {
	case updateInsertName, updateDeleteName:
		return true
	default:
		return false
	}
}

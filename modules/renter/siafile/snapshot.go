package siafile

import (
	"os"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
)

type (
	// Snapshot is a snapshot of a SiaFile. A snapshot is a deep-copy and
	// can be accessed without locking at the cost of being a frozen readonly
	// representation of a siafile which only exists in memory.
	Snapshot struct {
		staticChunks         []Chunk
		staticFileSize       int64
		staticPieceSize      uint64
		staticErasureCode    modules.ErasureCoder
		staticMasterKey      crypto.CipherKey
		staticMode           os.FileMode
		staticPubKeyTable    []HostPublicKey
		staticHyperspacePath string
	}
)

// ChunkIndexByOffset will return the chunkIndex that contains the provided
// offset of a file and also the relative offset within the chunk. If the
// offset is out of bounds, chunkIndex will be equal to NumChunk().
func (s *Snapshot) ChunkIndexByOffset(offset uint64) (chunkIndex uint64, off uint64) {
	chunkIndex = offset / s.ChunkSize()
	off = offset % s.ChunkSize()
	return
}

// ChunkSize returns the size of a single chunk of the file.
func (s *Snapshot) ChunkSize() uint64 {
	return s.staticPieceSize * uint64(s.staticErasureCode.MinPieces())
}

// ErasureCode returns the erasure coder used by the file.
func (s *Snapshot) ErasureCode() modules.ErasureCoder {
	return s.staticErasureCode
}

// MasterKey returns the masterkey used to encrypt the file.
func (s *Snapshot) MasterKey() crypto.CipherKey {
	return s.staticMasterKey
}

// Mode returns the FileMode of the file.
func (s *Snapshot) Mode() os.FileMode {
	return s.staticMode
}

// NumChunks returns the number of chunks the file consists of. This will
// return the number of chunks the file consists of even if the file is not
// fully uploaded yet.
func (s *Snapshot) NumChunks() uint64 {
	return uint64(len(s.staticChunks))
}

// Pieces returns all the pieces for a chunk in a slice of slices that contains
// all the pieces for a certain index.
func (s *Snapshot) Pieces(chunkIndex uint64) ([][]Piece, error) {
	// Return the pieces. Since the snapshot is meant to be used read-only, we
	// don't have to return a deep-copy here.
	return s.staticChunks[chunkIndex].Pieces, nil
}

// PieceSize returns the size of a single piece of the file.
func (s *Snapshot) PieceSize() uint64 {
	return s.staticPieceSize
}

// HyperspacePath returns the HyperspacePath of the file.
func (s *Snapshot) HyperspacePath() string {
	return s.staticHyperspacePath
}

// Size returns the size of the file.
func (s *Snapshot) Size() uint64 {
	return uint64(s.staticFileSize)
}

// Snapshot creates a snapshot of the SiaFile.
func (sf *SiaFile) Snapshot() *Snapshot {
	mk := sf.MasterKey()
	sf.mu.RLock()
	defer sf.mu.RUnlock()

	// Copy PubKeyTable.
	pkt := make([]HostPublicKey, len(sf.pubKeyTable))
	copy(pkt, sf.pubKeyTable)

	// Copy chunks.
	chunks := make([]Chunk, 0, len(sf.staticChunks))
	for chunkIndex := range sf.staticChunks {
		pieces := make([][]Piece, len(sf.staticChunks[chunkIndex].Pieces))
		for pieceIndex := range pieces {
			pieces[pieceIndex] = make([]Piece, len(sf.staticChunks[chunkIndex].Pieces[pieceIndex]))
			for i, piece := range sf.staticChunks[chunkIndex].Pieces[pieceIndex] {
				pieces[pieceIndex][i] = Piece{
					HostPubKey: sf.pubKeyTable[piece.HostTableOffset].PublicKey,
					MerkleRoot: piece.MerkleRoot,
				}
			}
		}
		chunks = append(chunks, Chunk{
			Pieces: pieces,
		})
	}

	return &Snapshot{
		staticChunks:         chunks,
		staticFileSize:       sf.staticMetadata.StaticFileSize,
		staticPieceSize:      sf.staticMetadata.StaticPieceSize,
		staticErasureCode:    sf.staticMetadata.staticErasureCode,
		staticMasterKey:      mk,
		staticMode:           sf.staticMetadata.Mode,
		staticPubKeyTable:    pkt,
		staticHyperspacePath: sf.staticMetadata.HyperspacePath,
	}
}

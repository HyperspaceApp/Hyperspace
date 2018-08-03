package types

// block.go defines the Block type for Sia, and provides some helper functions
// for working with blocks.

import (
	"bytes"
	"encoding/hex"
	"hash"
	"unsafe"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
)

const (
	// BlockHeaderSize is the size, in bytes, of a block header.
	// 32 (ParentID) + 8 (Nonce) + 8 (Timestamp) + 32 (MerkleRoot)
	BlockHeaderSize = 80
)

type (
	// A Block is a summary of changes to the state that have occurred since the
	// previous block. Blocks reference the ID of the previous block (their
	// "parent"), creating the linked-list commonly known as the blockchain. Their
	// primary function is to bundle together transactions on the network. Blocks
	// are created by "miners," who collect transactions from other nodes, and
	// then try to pick a Nonce that results in a block whose BlockID is below a
	// given Target.
	Block struct {
		ParentID     BlockID         `json:"parentid"`
		Nonce        BlockNonce      `json:"nonce"`
		Timestamp    Timestamp       `json:"timestamp"`
		MinerPayouts []SiacoinOutput `json:"minerpayouts"`
		Transactions []Transaction   `json:"transactions"`
	}

	// A BlockHeader contains the data that, when hashed, produces the Block's ID.
	BlockHeader struct {
		ParentID   BlockID     `json:"parentid"`
		Nonce      BlockNonce  `json:"nonce"`
		Timestamp  Timestamp   `json:"timestamp"`
		MerkleRoot crypto.Hash `json:"merkleroot"`
	}

	// BlockHeight is the number of blocks that exist after the genesis block.
	BlockHeight uint64
	// A BlockID is the hash of a BlockHeader. A BlockID uniquely
	// identifies a Block, and indicates the amount of work performed
	// to mine that Block. The more leading zeros in the BlockID, the
	// more work was performed.
	BlockID crypto.Hash
	// The BlockNonce is a "scratch space" that miners can freely alter to produce
	// a BlockID that satisfies a given Target.
	BlockNonce [8]byte
)

// CalculateCoinbase calculates the coinbase for a given height. The coinbase
// equation is:
//
//     coinbase := max(InitialCoinbase - height, MinimumCoinbase) * SiacoinPrecision
func CalculateCoinbase(height BlockHeight) Currency {
	blockHeight := uint64(height)
	if blockHeight == 0 {
		return NewCurrency64(0)
	} else if blockHeight == 1 {
		return NewCurrency64(FirstCoinbase).Mul(SiacoinPrecision)
	} else if blockHeight == 2 {
		return NewCurrency64(SecondCoinbase).Mul(SiacoinPrecision)
	}
	// after the first 2 blocks, start at 60k
	base := NewCurrency64(InitialCoinbase).Mul(SiacoinPrecision)
	// reduce by 0.2 Space Cash every block
	deducted := NewCurrency64((blockHeight - 3) * 2).Mul(SiacoinPrecision).Div(NewCurrency64(10))
	minimum := NewCurrency64(MinimumCoinbase).Mul(SiacoinPrecision)
	// we can't have a negative value
	if base.Cmp(deducted) <= 0 {
		return minimum
	}
	base = base.Sub(deducted)
	//if uint64(height) > InitialCoinbase || base < MinimumCoinbase {
	//if base < minimum {
	// if we're not negative but smaller than the min, return the min
	if base.Cmp(minimum) <= 0 {
		base = minimum
	}
	//return NewCurrency64(base).Mul(SiacoinPrecision)
	return base
}

// CalculateNumSiacoins calculates the number of siacoins in circulation at a
// given height.
func CalculateNumSiacoins(height BlockHeight) Currency {
	initial := AirdropValue.Add(DeveloperAirdropValue).Add(ContributorAirdropValue).Add(PoolAirdropValue)
	if height == 0 {
		return initial
	} else if height == 1 {
		return initial.Add(NewCurrency64(FirstCoinbase).Mul(SiacoinPrecision))
	} else if height == 2 {
		return initial.Add(NewCurrency64(FirstCoinbase + SecondCoinbase).Mul(SiacoinPrecision))
	}
	founderSiacoins := NewCurrency64(FirstCoinbase + SecondCoinbase).Mul(SiacoinPrecision)
	// each block decrements by 0.2 SPACE, so we multiply by 5 to calculate the number of
	// deflation blocks
	deflationBlocks := BlockHeight(InitialCoinbase-MinimumCoinbase) * 5
	avgDeflationSiacoins := CalculateCoinbase(3).Add(CalculateCoinbase(height)).Div(NewCurrency64(2))
	// the first 3 blocks are special, then we deflate for deflationBlocks
	if (height - 3) <= deflationBlocks {
		deflationSiacoins := avgDeflationSiacoins.Mul(NewCurrency64(uint64((height - 3) + 1)))
		return initial.Add(founderSiacoins).Add(deflationSiacoins)
	}
	deflationSiacoins := avgDeflationSiacoins.Mul(NewCurrency64(uint64(deflationBlocks + 1)))
	trailingSiacoins := NewCurrency64(uint64(height - 3 - deflationBlocks)).Mul(CalculateCoinbase(height))
	return initial.Add(founderSiacoins).Add(deflationSiacoins).Add(trailingSiacoins)
}

// ID returns the ID of a Block, which is calculated by hashing the header.
func (h BlockHeader) ID() BlockID {
	return BlockID(crypto.HashObject(h))
}

// CalculateMinerFees calculates the sum of a block's miner transaction fees
func (b Block) CalculateMinerFees() Currency {
	fees := NewCurrency64(0)
	for _, txn := range b.Transactions {
		for _, fee := range txn.MinerFees {
			fees = fees.Add(fee)
		}
	}
	return fees
}

// CalculateSubsidies takes a block and a height and determines the block
// subsidies for miners and the dev fund.
func (b Block) CalculateSubsidies(height BlockHeight) (Currency, Currency) {
	if uint64(height) == 0 {
		return NewCurrency64(0), NewCurrency64(0)
	}
	if uint64(height) == 1 {
		return NewCurrency64(FirstCoinbase).Mul(SiacoinPrecision), NewCurrency64(0)
	}
	if uint64(height) == 2 {
		return NewCurrency64(SecondCoinbase).Mul(SiacoinPrecision), NewCurrency64(0)
	}
	coinbase := CalculateCoinbase(height)
	devSubsidy := coinbase.Div(DevFundDenom)
	minerSubsidy := coinbase.Sub(devSubsidy)
	minerSubsidy = minerSubsidy.Add(b.CalculateMinerFees())
	return minerSubsidy, devSubsidy
}

// Header returns the header of a block.
func (b Block) Header() BlockHeader {
	return BlockHeader{
		ParentID:   b.ParentID,
		Nonce:      b.Nonce,
		Timestamp:  b.Timestamp,
		MerkleRoot: b.MerkleRoot(),
	}
}

// ID returns the ID of a Block, which is calculated by hashing the
// concatenation of the block's parent's ID, nonce, and the result of the
// b.MerkleRoot(). It is equivalent to calling block.Header().ID()
func (b Block) ID() BlockID {
	return b.Header().ID()
}

// MerkleTree return the MerkleTree of the block
func (b Block) MerkleTree() *crypto.MerkleTree {
	tree := crypto.NewTree()
	var buf bytes.Buffer
	e := encoding.NewEncoder(&buf)
	for _, payout := range b.MinerPayouts {
		payout.MarshalSia(e)
		tree.Push(buf.Bytes())
		buf.Reset()
	}
	for _, txn := range b.Transactions {
		txn.MarshalSia(e)
		tree.Push(buf.Bytes())
		buf.Reset()
	}

	// Sanity check - verify that this root is the same as the root provided in
	// the old implementation.
	if build.DEBUG {
		verifyTree := crypto.NewTree()
		for _, payout := range b.MinerPayouts {
			verifyTree.PushObject(payout)
		}
		for _, txn := range b.Transactions {
			verifyTree.PushObject(txn)
		}
		if tree.Root() != verifyTree.Root() {
			panic("Block MerkleRoot implementation is broken")
		}
	}
	return tree
}

// MerkleRoot calculates the Merkle root of a Block. The leaves of the Merkle
// tree are composed of the miner outputs (one leaf per payout), and the
// transactions (one leaf per transaction).
func (b Block) MerkleRoot() crypto.Hash {
	return b.MerkleTree().Root()
}

// MinerPayoutID returns the ID of the miner payout at the given index, which
// is calculated by hashing the concatenation of the BlockID and the payout
// index.
func (b Block) MinerPayoutID(i uint64) SiacoinOutputID {
	return SiacoinOutputID(crypto.HashAll(
		b.ID(),
		i,
	))
}

// MerkleBranches returns the merkle branches of a block, as used in stratum
// mining.
func (b Block) MerkleBranches() []string {
	mbranch := crypto.NewTree()
	var buf bytes.Buffer
	for _, payout := range b.MinerPayouts {
		payout.MarshalSia(&buf)
		mbranch.Push(buf.Bytes())
		buf.Reset()
	}

	for _, txn := range b.Transactions {
		txn.MarshalSia(&buf)
		mbranch.Push(buf.Bytes())
		buf.Reset()
	}
	//
	// This whole approach needs to be revisited.  I basically am cheating to look
	// inside the merkle tree struct to determine if the head is a leaf or not
	//
	type SubTree struct {
		next   *SubTree
		height int // Int is okay because a height over 300 is physically unachievable.
		sum    []byte
	}

	type Tree struct {
		head         *SubTree
		hash         hash.Hash
		currentIndex uint64
		proofIndex   uint64
		proofSet     [][]byte
		cachedTree   bool
	}
	tr := *(*Tree)(unsafe.Pointer(mbranch))

	var merkle []string
	//	h.log.Debugf("mBranch Hash %s\n", mbranch.Root().String())
	for st := tr.head; st != nil; st = st.next {
		//		h.log.Debugf("Height %d Hash %x\n", st.height, st.sum)
		merkle = append(merkle, hex.EncodeToString(st.sum))
	}
	return merkle
}

package consensus

import (
	"math/big"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

// SurpassThreshold is a percentage that dictates how much heavier a competing
// chain has to be before the node will switch to mining on that chain. This is
// not a consensus rule. This percentage is only applied to the most recent
// block, not the entire chain; see blockNode.heavierThan.
//
// If no threshold were in place, it would be possible to manipulate a block's
// timestamp to produce a sufficiently heavier block.
var SurpassThreshold = big.NewRat(20, 100)

// processedBlock is a copy/rename of blockNode, with the pointers to
// other blockNodes replaced with block ID's, and all the fields
// exported, so that a block node can be marshalled
type processedBlock struct {
	Block       types.Block
	Height      types.BlockHeight
	Depth       types.Target
	ChildTarget types.Target

	DiffsGenerated            bool
	SiacoinOutputDiffs        []modules.SiacoinOutputDiff
	FileContractDiffs         []modules.FileContractDiff
	DelayedSiacoinOutputDiffs []modules.DelayedSiacoinOutputDiff

	ConsensusChecksum crypto.Hash
}

type processedHeader struct {
	BlockHeader types.BlockHeader
	Height      types.BlockHeight
	Depth       types.Target
	ChildTarget types.Target
}

// heavierThan returns true if the blockNode is sufficiently heavier than
// 'cmp'. 'cmp' is expected to be the current block node. "Sufficient" means
// that the weight of 'bn' exceeds the weight of 'cmp' by:
//		(the target of 'cmp' * 'Surpass Threshold')
func (pb *processedBlock) heavierThan(cmp *processedBlock) bool {
	requirement := cmp.Depth.AddDifficulties(cmp.ChildTarget.MulDifficulty(SurpassThreshold))
	return requirement.Cmp(pb.Depth) > 0 // Inversed, because the smaller target is actually heavier.
}

func (ph *processedHeader) heavierThan(cmp *processedHeader) bool {
	requirement := cmp.Depth.AddDifficulties(cmp.ChildTarget.MulDifficulty(SurpassThreshold))
	return requirement.Cmp(ph.Depth) > 0
}

// childDepth returns the depth of a blockNode's child nodes. The depth is the
// "sum" of the current depth and current difficulty. See target.Add for more
// detailed information.
func (pb *processedBlock) childDepth() types.Target {
	return pb.Depth.AddDifficulties(pb.ChildTarget)
}

// childDepth returns the depth of a headerNode's child nodes. The depth is the
// "sum" of the current depth and current difficulty. See target.Add for more
// detailed information.
func (ph *processedHeader) childDepth() types.Target {
	return ph.Depth.AddDifficulties(ph.ChildTarget)
}

// targetAdjustmentBase returns the magnitude that the target should be
// adjusted by before a clamp is applied.
func (cs *ConsensusSet) targetAdjustmentBase(blockMap *bolt.Bucket, parentID types.BlockID, currentID types.BlockID, currentTimestamp types.Timestamp) *big.Rat {
	// Grab the block that was generated 'TargetWindow' blocks prior to the
	// parent. If there are not 'TargetWindow' blocks yet, stop at the genesis
	// block.
	var windowSize types.BlockHeight
	parent := parentID
	current := currentID
	for windowSize = 0; windowSize < types.TargetWindow && parent != (types.BlockID{}); windowSize++ {
		current = parent
		copy(parent[:], blockMap.Get(parent[:])[:32])
	}
	timestamp := types.Timestamp(encoding.DecUint64(blockMap.Get(current[:])[40:48]))

	// The target of a child is determined by the amount of time that has
	// passed between the generation of its immediate parent and its
	// TargetWindow'th parent. The expected amount of seconds to have passed is
	// TargetWindow*BlockFrequency. The target is adjusted in proportion to how
	// time has passed vs. the expected amount of time to have passed.
	//
	// The target is converted to a big.Rat to provide infinite precision
	// during the calculation. The big.Rat is just the int representation of a
	// target.
	timePassed := currentTimestamp - timestamp
	expectedTimePassed := types.BlockFrequency * windowSize
	return big.NewRat(int64(timePassed), int64(expectedTimePassed))
}

// clampTargetAdjustment returns a clamped version of the base adjustment
// value. The clamp keeps the maximum adjustment to ~7x every 2000 blocks. This
// ensures that raising and lowering the difficulty requires a minimum amount
// of total work, which prevents certain classes of difficulty adjusting
// attacks.
func clampTargetAdjustment(base *big.Rat) *big.Rat {
	if base.Cmp(types.MaxTargetAdjustmentUp) > 0 {
		return types.MaxTargetAdjustmentUp
	} else if base.Cmp(types.MaxTargetAdjustmentDown) < 0 {
		return types.MaxTargetAdjustmentDown
	}
	return base
}

// setChildTarget computes the target of a blockNode's child. All children of a node
// have the same target.
func (cs *ConsensusSet) setChildTarget(blockMap *bolt.Bucket, pb *processedBlock) {
	// Fetch the parent block.
	var parent processedBlock
	parentBytes := blockMap.Get(pb.Block.ParentID[:])
	err := encoding.Unmarshal(parentBytes, &parent)
	if build.DEBUG && err != nil {
		panic(err)
	}

	if pb.Height%(types.TargetWindow/2) != 0 {
		pb.ChildTarget = parent.ChildTarget
		return
	}
	adjustment := clampTargetAdjustment(cs.targetAdjustmentBase(blockMap, pb.Block.ParentID, pb.Block.ID(), pb.Block.Timestamp))
	adjustedRatTarget := new(big.Rat).Mul(parent.ChildTarget.Rat(), adjustment)
	pb.ChildTarget = types.RatToTarget(adjustedRatTarget)
}

// newChild creates a blockNode from a block and adds it to the parent's set of
// children. The new node is also returned. It necessarily modifies the database
func (cs *ConsensusSet) newChild(tx *bolt.Tx, pb *processedBlock, b types.Block) *processedBlock {
	// Create the child node.
	childID := b.ID()
	child := &processedBlock{
		Block:  b,
		Height: pb.Height + 1,
		Depth:  pb.childDepth(),
	}

	// Push the total values for this block into the oak difficulty adjustment
	// bucket. The previous totals are required to compute the new totals.
	prevTotalTime, prevTotalTarget := cs.getBlockTotals(tx, b.ParentID)
	_, _, err := cs.storeBlockTotals(tx, child.Height, childID, prevTotalTime, pb.Block.Timestamp, b.Timestamp, prevTotalTarget, pb.ChildTarget)
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Use the difficulty adjustment algorithm to set the target of the child
	// block and put the new processed block into the database.
	blockMap := tx.Bucket(BlockMap)
	child.ChildTarget = cs.childTargetOak(prevTotalTime, prevTotalTarget, pb.ChildTarget, pb.Height, pb.Block.Timestamp)
	err = blockMap.Put(childID[:], encoding.Marshal(*child))
	if build.DEBUG && err != nil {
		panic(err)
	}
	return child
}

// setHeaderChildTarget computes the target of a headerNode's child. All children of a node
// have the same target.
func (cs *ConsensusSet) setHeaderChildTarget(headerMap *bolt.Bucket, ph *processedHeader) {
	// Fetch the parent header.
	var parent processedHeader
	parentBytes := headerMap.Get(ph.BlockHeader.ParentID[:])
	err := encoding.Unmarshal(parentBytes, &parent)
	if build.DEBUG && err != nil {
		panic(err)
	}
	if ph.Height%(types.TargetWindow/2) != 0 {
		ph.ChildTarget = parent.ChildTarget
		return
	}
	adjustment := clampTargetAdjustment(cs.targetAdjustmentBase(headerMap, ph.BlockHeader.ParentID, ph.BlockHeader.ID(), ph.BlockHeader.Timestamp))
	adjustedRatTarget := new(big.Rat).Mul(parent.ChildTarget.Rat(), adjustment)
	ph.ChildTarget = types.RatToTarget(adjustedRatTarget)
}

// newHeader creates a new child headerNode from a header and adds it to the parent's set of
// children. The new node is also returned. It necessarily modifies the HeaderMap bucket
func (cs *ConsensusSet) newHeader(tx *bolt.Tx, parentHeader *processedHeader, header types.BlockHeader) *processedHeader {
	// Create the child node.
	childID := header.ID()
	child := &processedHeader{
		BlockHeader: header,
		Height:      parentHeader.Height + 1,
		Depth:       parentHeader.childDepth(),
	}
	prevTotalTime, prevTotalTarget := cs.getBlockTotals(tx, header.ParentID)
	_, _, err := cs.storeBlockTotals(tx, child.Height, childID, prevTotalTime, parentHeader.BlockHeader.Timestamp, header.Timestamp, prevTotalTarget, parentHeader.ChildTarget)
	if build.DEBUG && err != nil {
		panic(err)
	}
	// Use the difficulty adjustment algorithm to set the target of the child
	// header and put the new processed header into the database.
	headerMap := tx.Bucket(HeaderMap)
	child.ChildTarget = cs.childTargetOak(prevTotalTime, prevTotalTarget, parentHeader.ChildTarget, parentHeader.Height, parentHeader.BlockHeader.Timestamp)
	err = headerMap.Put(childID[:], encoding.Marshal(*child))
	if build.DEBUG && err != nil {
		panic(err)
	}
	return child
}

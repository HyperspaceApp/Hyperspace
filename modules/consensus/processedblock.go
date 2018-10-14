package consensus

import (
	"math/big"

	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/gcs/blockcf"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

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

// heavierThan returns true if the blockNode is sufficiently heavier than
// 'cmp'. 'cmp' is expected to be the current block node. "Sufficient" means
// that the weight of 'bn' exceeds the weight of 'cmp' by:
//		(the target of 'cmp' * 'Surpass Threshold')
func (pb *processedBlock) heavierThan(cmp *processedBlock) bool {
	requirement := cmp.Depth.AddDifficulties(cmp.ChildTarget.MulDifficulty(modules.SurpassThreshold))
	return requirement.Cmp(pb.Depth) > 0 // Inversed, because the smaller target is actually heavier.
}

// childDepth returns the depth of a blockNode's child nodes. The depth is the
// "sum" of the current depth and current difficulty. See target.Add for more
// detailed information.
func (pb *processedBlock) childDepth() types.Target {
	return pb.Depth.AddDifficulties(pb.ChildTarget)
}

// targetAdjustmentBase returns the magnitude that the target should be
// adjusted by before a clamp is applied.
func (cs *ConsensusSet) targetAdjustmentBase(blockMap *bolt.Bucket, pb *processedBlock) *big.Rat {
	// Grab the block that was generated 'TargetWindow' blocks prior to the
	// parent. If there are not 'TargetWindow' blocks yet, stop at the genesis
	// block.
	var windowSize types.BlockHeight
	parent := pb.Block.ParentID
	current := pb.Block.ID()
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
	timePassed := pb.Block.Timestamp - timestamp
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
	adjustment := clampTargetAdjustment(cs.targetAdjustmentBase(blockMap, pb))
	adjustedRatTarget := new(big.Rat).Mul(parent.ChildTarget.Rat(), adjustment)
	pb.ChildTarget = types.RatToTarget(adjustedRatTarget)
}

// newChild creates a blockNode from a block and adds it to the parent's set of
// children. The new node is also returned. It necessarily modifies the block
// and block header buckets.
func (cs *ConsensusSet) newChild(tx *bolt.Tx, pb *processedBlock, b types.Block) (*processedBlock, *modules.ProcessedBlockHeader) {
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
	var hashes []types.UnlockHash
	fileContracts := getRelatedFileContracts(tx, &b)
	for _, fc := range fileContracts {
		contractHashes := fc.OutputUnlockHashes()
		hashes = append(hashes, contractHashes...)
	}
	filter, err := blockcf.BuildFilter(&b, hashes)
	if build.DEBUG && err != nil {
		panic(err)
	}
	childHeader := &modules.ProcessedBlockHeader{
		BlockHeader:   b.Header(),
		Height:        child.Height,
		Depth:         child.Depth,
		ChildTarget:   child.ChildTarget,
		GCSFilter:     *filter,
		Announcements: modules.FindHostAnnouncementsFromBlock(child.Block),
	}

	blockHeaderMap := tx.Bucket(BlockHeaderMap)
	err = blockHeaderMap.Put(childID[:], encoding.Marshal(*childHeader))
	if build.DEBUG && err != nil {
		panic(err)
	}
	cs.processedBlockHeaders[childID] = childHeader
	return child, childHeader
}

// newHeaderChild creates a new child headerNode from a header and adds it to the parent's set of
// children. The new node is also returned. It necessarily modifies the BlockHeaderMap bucket
func (cs *ConsensusSet) newHeaderChild(tx *bolt.Tx, parentHeader *modules.ProcessedBlockHeader, header modules.TransmittedBlockHeader) *modules.ProcessedBlockHeader {
	// Create the child node.
	childID := header.BlockHeader.ID()
	childHeader := &modules.ProcessedBlockHeader{
		BlockHeader:   header.BlockHeader,
		Height:        parentHeader.Height + 1,
		Depth:         parentHeader.ChildDepth(),
		GCSFilter:     header.GCSFilter,
		Announcements: header.Announcements,
	}
	prevTotalTime, prevTotalTarget := cs.getBlockTotals(tx, header.BlockHeader.ParentID)
	_, _, err := cs.storeBlockTotals(tx, childHeader.Height, childID, prevTotalTime, parentHeader.BlockHeader.Timestamp,
		header.BlockHeader.Timestamp, prevTotalTarget, parentHeader.ChildTarget)
	if build.DEBUG && err != nil {
		panic(err)
	}
	// Use the difficulty adjustment algorithm to set the target of the child
	// header and put the new processed header into the database.
	headerMap := tx.Bucket(BlockHeaderMap)
	childHeader.ChildTarget = cs.childTargetOak(prevTotalTime, prevTotalTarget, parentHeader.ChildTarget, parentHeader.Height, parentHeader.BlockHeader.Timestamp)
	err = headerMap.Put(childID[:], encoding.Marshal(*childHeader))
	if build.DEBUG && err != nil {
		panic(err)
	}
	cs.processedBlockHeaders[childID] = childHeader
	return childHeader
}

func (cs *ConsensusSet) newSingleChild(tx *bolt.Tx, pbh *modules.ProcessedBlockHeader, b types.Block) (*processedBlock, *modules.ProcessedBlockHeader) {
	// Create the child node.
	childID := b.ID()

	child := &processedBlock{
		Block:  b,
		Height: pbh.Height + 1,
		Depth:  pbh.ChildDepth(),
	}

	// Push the total values for this block into the oak difficulty adjustment
	// bucket. The previous totals are required to compute the new totals.
	prevTotalTime, prevTotalTarget := cs.getBlockTotals(tx, b.ParentID)
	_, _, err := cs.storeBlockTotals(tx, child.Height, childID, prevTotalTime, pbh.BlockHeader.Timestamp, b.Timestamp, prevTotalTarget, pbh.ChildTarget)
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Use the difficulty adjustment algorithm to set the target of the child
	// block and put the new processed block into the database.
	blockMap := tx.Bucket(BlockMap)
	child.ChildTarget = cs.childTargetOak(prevTotalTime, prevTotalTarget, pbh.ChildTarget, pbh.Height, pbh.BlockHeader.Timestamp)
	err = blockMap.Put(childID[:], encoding.Marshal(*child))
	if build.DEBUG && err != nil {
		panic(err)
	}

	childHeader, exist := cs.processedBlockHeaders[childID]
	if build.DEBUG && !exist {
		panic("received a block without header in mem")
	}

	return child, childHeader
}

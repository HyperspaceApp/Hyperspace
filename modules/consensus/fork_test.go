package consensus

import (
	"log"
	"testing"

	"github.com/HyperspaceApp/Hyperspace/modules"
)

// TestBacktrackToCurrentPath probes the backtrackToCurrentPath method of the
// consensus set.
func TestBacktrackToCurrentPath(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cst.Close()
	pb := cst.cs.dbCurrentProcessedBlock()

	// Backtrack from the current node to the blockchain.
	nodes := cst.cs.dbBacktrackToCurrentPath(pb)
	if len(nodes) != 1 {
		t.Fatal("backtracking to the current node gave incorrect result")
	}
	if nodes[0].Block.ID() != pb.Block.ID() {
		t.Error("backtrack returned the wrong node")
	}

	// Backtrack from a node that has diverted from the current blockchain.
	child0, _ := cst.miner.FindBlock()
	child1, _ := cst.miner.FindBlock() // Is the block not on hte current path.
	err = cst.cs.AcceptBlock(child0)
	if err != nil {
		t.Fatal(err)
	}
	err = cst.cs.AcceptBlock(child1)
	if err != modules.ErrNonExtendingBlock {
		t.Fatal(err)
	}
	pb, err = cst.cs.dbGetBlockMap(child1.ID())
	if err != nil {
		t.Fatal(err)
	}
	nodes = cst.cs.dbBacktrackToCurrentPath(pb)
	if len(nodes) != 2 {
		t.Error("backtracking grabbed wrong number of nodes")
	}
	parent, err := cst.cs.dbGetBlockMap(pb.Block.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].Block.ID() != parent.Block.ID() {
		t.Error("grabbed the wrong block as the common block")
	}
	if nodes[1].Block.ID() != pb.Block.ID() {
		t.Error("backtracked from the wrong node")
	}
}

func TestHeaderBacktrackToCurrentPath(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cst.Close()
	pbh := cst.cs.dbCurrentProcessedHeader()

	// Backtrack from the current node to the blockchain.
	nodes := cst.cs.dbHeaderBacktrackToCurrentPath(pbh)
	if len(nodes) != 1 {
		t.Fatal("backtracking to the current node gave incorrect result")
	}
	if nodes[0].BlockHeader.ID() != pbh.BlockHeader.ID() {
		t.Error("backtrack header returned the wrong node")
	}

	// Backtrack from a node that has diverted from the current blockchain.
	child0, _ := cst.miner.FindBlock()
	child1, _ := cst.miner.FindBlock() // Is the block not on hte current path.
	err = cst.cs.AcceptBlock(child0)
	if err != nil {
		t.Fatal(err)
	}
	err = cst.cs.AcceptBlock(child1)
	if err != modules.ErrNonExtendingBlock {
		t.Fatal(err)
	}
	pbh, err = cst.cs.dbGetBlockHeaderMap(child1.ID())
	if err != nil {
		t.Fatal(err)
	}
	nodes = cst.cs.dbHeaderBacktrackToCurrentPath(pbh)
	if len(nodes) != 2 {
		t.Error("backtracking grabbed wrong number of nodes")
	}
	parent, err := cst.cs.dbGetBlockHeaderMap(pbh.BlockHeader.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	if nodes[0].BlockHeader.ID() != parent.BlockHeader.ID() {
		t.Error("grabbed the wrong block header as the common block header")
	}
	if nodes[1].BlockHeader.ID() != pbh.BlockHeader.ID() {
		t.Error("backtracked header from the wrong node")
	}
}

// TestRevertToNode probes the revertToBlock method of the consensus set.
func TestRevertToNode(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst, err := createConsensusSetTester(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer cst.Close()
	pb := cst.cs.dbCurrentProcessedBlock()

	// Revert to a grandparent and verify the returned array is correct.
	parent, err := cst.cs.dbGetBlockMap(pb.Block.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	grandParent, err := cst.cs.dbGetBlockMap(parent.Block.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("--------------------------------start dbRevertToNode----------------------------------------------------------------")
	revertedNodes := cst.cs.dbRevertToNode(grandParent)
	if len(revertedNodes) != 2 {
		t.Error("wrong number of nodes reverted")
	}
	if revertedNodes[0].Block.ID() != pb.Block.ID() {
		t.Error("wrong composition of reverted nodes")
	}
	if revertedNodes[1].Block.ID() != parent.Block.ID() {
		t.Error("wrong composition of reverted nodes")
	}

	// Trigger a panic by trying to revert to a node outside of the current
	// path.
	defer func() {
		r := recover()
		if r != errExternalRevert {
			t.Error(r)
		}
	}()
	cst.cs.dbRevertToNode(pb)
}

func TestRevertToHeaderNode(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	t.Parallel()
	cst1, err := createSPVConsensusSetTester(t.Name() + "1")
	if err != nil {
		t.Fatal(err)
	}
	defer cst1.CloseSPV()
	cst2, err := createConsensusSetTester(t.Name() + "2")
	if err != nil {
		t.Fatal(err)
	}
	defer cst2.Close()
	// log.Printf("--------------------------------before connect----------------------------------------------------------------")

	// connect gateways, triggering a Synchronize
	err = cst1.gateway.Connect(cst2.gateway.Address())
	if err != nil {
		t.Fatal(err)
	}

	waitTillSync(cst1, cst2, t)
	cst := cst1
	pbh := cst.cs.dbCurrentProcessedHeader()

	// Revert to a grandparent and verify the returned array is correct.
	parent, err := cst.cs.dbGetBlockHeaderMap(pbh.BlockHeader.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	grandParent, err := cst.cs.dbGetBlockHeaderMap(parent.BlockHeader.ParentID)
	if err != nil {
		t.Fatal(err)
	}
	// log.Printf("--------------------------------start dbRevertToHeaderNode----------------------------------------------------------------")
	revertedHeaderNodes := cst.cs.dbRevertToHeaderNode(grandParent)
	if len(revertedHeaderNodes) != 2 {
		t.Error("wrong number of nodes reverted")
	}
	if revertedHeaderNodes[0].BlockHeader.ID() != pbh.BlockHeader.ID() {
		t.Error("wrong composition of reverted nodes")
	}
	if revertedHeaderNodes[1].BlockHeader.ID() != parent.BlockHeader.ID() {
		t.Error("wrong composition of reverted nodes")
	}

	// Trigger a panic by trying to revert to a node outside of the current
	// path.
	defer func() {
		r := recover()
		if r != errExternalRevert {
			t.Error(r)
		}
	}()
	cst.cs.dbRevertToHeaderNode(pbh)
}

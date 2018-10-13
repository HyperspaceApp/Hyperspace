package consensus

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/coreos/bbolt"
)

// dbBacktrackToCurrentPath is a convenience function to call
// backtrackToCurrentPath without a bolt.Tx.
func (cs *ConsensusSet) dbBacktrackToCurrentPath(pb *processedBlock) (pbs []*processedBlock) {
	_ = cs.db.Update(func(tx *bolt.Tx) error {
		pbs = backtrackToCurrentPath(tx, pb)
		return nil
	})
	return pbs
}

func (cs *ConsensusSet) dbHeaderBacktrackToCurrentPath(pbh *modules.ProcessedBlockHeader) (pbhs []*modules.ProcessedBlockHeader) {
	_ = cs.db.Update(func(tx *bolt.Tx) error {
		pbhs = backtrackHeadersToCurrentPath(tx, pbh)
		return nil
	})
	return pbhs
}

// dbRevertToNode is a convenience function to call revertToBlock without a
// bolt.Tx.
func (cs *ConsensusSet) dbRevertToNode(pb *processedBlock) (pbs []*processedBlock) {
	_ = cs.db.Update(func(tx *bolt.Tx) error {
		pbs = cs.revertToBlock(tx, pb)
		return nil
	})
	return pbs
}

func (cs *ConsensusSet) dbRevertToHeaderNode(pbh *modules.ProcessedBlockHeader) (pbhs []*modules.ProcessedBlockHeader) {
	_ = cs.db.Update(func(tx *bolt.Tx) error {
		pbhs = cs.revertToHeader(tx, pbh)
		return nil
	})
	return pbhs
}

// dbForkBlockchain is a convenience function to call forkBlockchain without a
// bolt.Tx.
func (cs *ConsensusSet) dbForkBlockchain(pb *processedBlock) (revertedBlocks, appliedBlocks []*processedBlock, err error) {
	updateErr := cs.db.Update(func(tx *bolt.Tx) error {
		revertedBlocks, appliedBlocks, err = cs.forkBlockchain(tx, pb, nil)
		return nil
	})
	if updateErr != nil {
		panic(updateErr)
	}
	return revertedBlocks, appliedBlocks, err
}

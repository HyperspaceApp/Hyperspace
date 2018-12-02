package consensus

// consensusdb.go contains all of the functions related to performing consensus
// related actions on the database, including initializing the consensus
// portions of the database. Many errors cause panics instead of being handled
// gracefully, but only when the debug flag is set. The errors are silently
// ignored otherwise, which is suboptimal.

import (
	"github.com/HyperspaceApp/Hyperspace/build"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/gcs/blockcf"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"

	"github.com/coreos/bbolt"
)

var (
	prefixDSCO = []byte("dsco_")
	prefixFCEX = []byte("fcex_")
)

var (
	// BlockHeight is a bucket that stores the current block height.
	//
	// Generally we would just look at BlockPath.Stats(), but there is an error
	// in boltdb that prevents the bucket stats from updating until a tx is
	// committed. Wasn't a problem until we started doing the entire block as
	// one tx.
	//
	// DEPRECATED - block.Stats() should be sufficient to determine the block
	// height, but currently stats are only computed after committing a
	// transaction, therefore cannot be assumed reliable.
	BlockHeight = []byte("BlockHeight")

	// BlockMap is a database bucket containing all of the processed blocks,
	// keyed by their id. This includes blocks that are not currently in the
	// consensus set, and blocks that may not have been fully validated yet.
	BlockMap = []byte("BlockMap")

	// BlockHeaderMap is a database bucket containing all of the processed
	// block headers, keyed by their id. This includes block headers that are
	// not currently in the consensus set, and blocks that may not have been
	// fully validated yet.
	BlockHeaderMap = []byte("BlockHeaderMap")

	// BlockPath is a database bucket containing a mapping from the height of a
	// block to the id of the block at that height. BlockPath only includes
	// blocks in the current path.
	BlockPath = []byte("BlockPath")

	// BucketOak is the database bucket that contains all of the fields related
	// to the oak difficulty adjustment algorithm. The cumulative difficulty and
	// time values are stored for each block id, and then the key "OakInit"
	// contains the value "true" if the oak fields have been properly
	// initialized.
	BucketOak = []byte("Oak")

	// Consistency is a database bucket with a flag indicating whether
	// inconsistencies within the database have been detected.
	Consistency = []byte("Consistency")

	// FileContractUnlockHashMap is a database bucket that contains a mapping
	// from all contract ids to a map of blockheights with relevant unlock
	// hashes as values. This is used by full nodes when building filters for
	// SPV clients. When a storage proof or file contract revision is posted,
	// the host needs to specify the filter to include all previously related
	// UnlockHashes (which can change between revisions, but probably in
	// practice don't).
	//
	// TODO: this is currently unimplemented and so light nodes would break
	// under this weird case.
	FileContractUnlockHashMap = []byte("FileContractUnlockHashMap")

	// FileContracts is a database bucket that contains all of the open file
	// contracts.
	FileContracts = []byte("FileContracts")

	// SiacoinOutputs is a database bucket that contains all of the unspent
	// siacoin outputs.
	SiacoinOutputs = []byte("SiacoinOutputs")
)

var (
	// FieldOakInit is a field in BucketOak that gets set to "true" after the
	// oak initialiation process has completed.
	FieldOakInit = []byte("OakInit")
)

var (
	// ValueOakInit is the value that the oak init field is set to if the oak
	// difficulty adjustment fields have been correctly intialized.
	ValueOakInit = []byte("true")
)

// createConsensusObjects initialzes the consensus portions of the database.
func (cs *ConsensusSet) createHeaderConsensusDB(tx *bolt.Tx) error {
	// Enumerate and create the database buckets.
	buckets := [][]byte{
		BlockHeaderMap,
	}
	for _, bucket := range buckets {
		_, err := tx.CreateBucket(bucket)
		if err != nil {
			return err
		}
	}

	var unlockHashes []types.UnlockHash
	filter, err := blockcf.BuildFilter(&cs.blockRoot.Block, unlockHashes)
	if err != nil {
		return err
	}

	addBlockHeaderMap(tx, &modules.ProcessedBlockHeader{
		BlockHeader: cs.blockRoot.Block.Header(),
		Height:      types.BlockHeight(0),
		Depth:       types.RootDepth,
		ChildTarget: types.RootTarget,
		GCSFilter:   *filter,
	})
	return nil
}

// createConsensusObjects initialzes the consensus portions of the database.
func (cs *ConsensusSet) createConsensusDB(tx *bolt.Tx) error {
	// Enumerate and create the database buckets.
	buckets := [][]byte{
		BlockHeight,
		BlockMap,
		BlockPath,
		Consistency,
		SiacoinOutputs,
		FileContracts,
	}
	for _, bucket := range buckets {
		_, err := tx.CreateBucket(bucket)
		if err != nil {
			return err
		}
	}

	// Set the block height to -1, so the genesis block is at height 0.
	blockHeight := tx.Bucket(BlockHeight)
	underflow := types.BlockHeight(0)
	err := blockHeight.Put(BlockHeight, encoding.Marshal(underflow-1))
	if err != nil {
		return err
	}

	// Update the siacoin output diffs map for the genesis block on disk. This
	// needs to happen between the database being opened/initilized and the
	// consensus set hash being calculated
	for _, scod := range cs.blockRoot.SiacoinOutputDiffs {
		commitSiacoinOutputDiff(tx, scod, modules.DiffApply)
	}

	// Add the miner payout from the genesis block to the delayed siacoin
	// outputs - unspendable, as the unlock hash is blank.
	createDSCOBucket(tx, types.MaturityDelay)
	addDSCO(tx, types.MaturityDelay, cs.blockRoot.Block.MinerPayoutID(0), types.SiacoinOutput{
		Value:      types.CalculateCoinbase(0),
		UnlockHash: types.UnlockHash{},
	})

	// Add the genesis block to the block structures - checksum must be taken
	// after pushing the genesis block into the path.
	pushPath(tx, cs.blockRoot.Block.ID())
	if build.DEBUG {
		cs.blockRoot.ConsensusChecksum = consensusChecksum(tx)
	}
	addBlockMap(tx, &cs.blockRoot)
	return nil
}

// blockHeight returns the height of the blockchain.
func blockHeight(tx *bolt.Tx) types.BlockHeight {
	var height types.BlockHeight
	bh := tx.Bucket(BlockHeight)
	err := encoding.Unmarshal(bh.Get(BlockHeight), &height)
	if build.DEBUG && err != nil {
		panic(err)
	}
	return height
}

// currentBlockID returns the id of the most recent block in the consensus set.
func currentBlockID(tx *bolt.Tx) types.BlockID {
	id, err := getPath(tx, blockHeight(tx))
	if build.DEBUG && err != nil {
		panic(err)
	}
	return id
}

// dbCurrentBlockID is a convenience function allowing currentBlockID to be
// called without a bolt.Tx.
func (cs *ConsensusSet) dbCurrentBlockID() (id types.BlockID) {
	dbErr := cs.db.View(func(tx *bolt.Tx) error {
		id = currentBlockID(tx)
		return nil
	})
	if dbErr != nil {
		panic(dbErr)
	}
	return id
}

// currentProcessedBlock returns the most recent block in the consensus set.
func currentProcessedBlock(tx *bolt.Tx) *processedBlock {
	pb, err := getBlockMap(tx, currentBlockID(tx))
	if build.DEBUG && err != nil {
		panic(err)
	}
	return pb
}

// currentProcessedHeader returns the most recent header in the consensus set
func currentProcessedHeader(tx *bolt.Tx) *modules.ProcessedBlockHeader {
	ph, err := getBlockHeaderMap(tx, currentBlockID(tx))
	if build.DEBUG && err != nil {
		panic(err)
	}
	return ph
}

// getBlockHeaderMap returns a processed block header with the input id.
func getBlockHeaderMap(tx *bolt.Tx, id types.BlockID) (*modules.ProcessedBlockHeader, error) {
	// Look up the encoded block.
	bucket := tx.Bucket(BlockHeaderMap)
	if bucket == nil {
		return nil, errNilItem
	}
	pbhBytes := bucket.Get(id[:])
	if pbhBytes == nil {
		return nil, errNilItem
	}

	// Decode the block - should never fail.
	var pbh modules.ProcessedBlockHeader
	err := encoding.Unmarshal(pbhBytes, &pbh)
	if build.DEBUG && err != nil {
		panic(err)
	}
	return &pbh, nil
}

// getBlockMap returns a processed block with the input id.
func getBlockMap(tx *bolt.Tx, id types.BlockID) (*processedBlock, error) {
	// Look up the encoded block.
	pbBytes := tx.Bucket(BlockMap).Get(id[:])
	if pbBytes == nil {
		return nil, errNilItem
	}

	// Decode the block - should never fail.
	var pb processedBlock
	err := encoding.Unmarshal(pbBytes, &pb)
	if build.DEBUG && err != nil {
		panic(err)
	}
	return &pb, nil
}

// addBlockHeaderMap adds a processed block header to the block map.
func addBlockHeaderMap(tx *bolt.Tx, pbh *modules.ProcessedBlockHeader) {
	id := pbh.BlockHeader.ID()
	err := tx.Bucket(BlockHeaderMap).Put(id[:], encoding.Marshal(*pbh))
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// addBlockMap adds a processed block to the block map.
func addBlockMap(tx *bolt.Tx, pb *processedBlock) {
	id := pb.Block.ID()
	err := tx.Bucket(BlockMap).Put(id[:], encoding.Marshal(*pb))
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// getPath returns the block id at 'height' in the block path.
func getPath(tx *bolt.Tx, height types.BlockHeight) (id types.BlockID, err error) {
	idBytes := tx.Bucket(BlockPath).Get(encoding.Marshal(height))
	if idBytes == nil {
		return types.BlockID{}, errNilItem
	}

	err = encoding.Unmarshal(idBytes, &id)
	if build.DEBUG && err != nil {
		panic(err)
	}
	return id, nil
}

// pushPath adds a block to the BlockPath at current height + 1.
func pushPath(tx *bolt.Tx, bid types.BlockID) {
	// Fetch and update the block height.
	bh := tx.Bucket(BlockHeight)
	heightBytes := bh.Get(BlockHeight)
	var oldHeight types.BlockHeight
	err := encoding.Unmarshal(heightBytes, &oldHeight)
	if build.DEBUG && err != nil {
		panic(err)
	}
	newHeightBytes := encoding.Marshal(oldHeight + 1)
	err = bh.Put(BlockHeight, newHeightBytes)
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Add the block to the block path.
	bp := tx.Bucket(BlockPath)
	err = bp.Put(newHeightBytes, bid[:])
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// popPath removes a block from the "end" of the chain, i.e. the block
// with the largest height.
func popPath(tx *bolt.Tx) {
	// Fetch and update the block height.
	bh := tx.Bucket(BlockHeight)
	oldHeightBytes := bh.Get(BlockHeight)
	var oldHeight types.BlockHeight
	err := encoding.Unmarshal(oldHeightBytes, &oldHeight)
	if build.DEBUG && err != nil {
		panic(err)
	}
	newHeightBytes := encoding.Marshal(oldHeight - 1)
	err = bh.Put(BlockHeight, newHeightBytes)
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Remove the block from the path - make sure to remove the block at
	// oldHeight.
	bp := tx.Bucket(BlockPath)
	err = bp.Delete(oldHeightBytes)
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// isSiacoinOutput returns true if there is a siacoin output of that id in the
// database.
func isSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID) bool {
	bucket := tx.Bucket(SiacoinOutputs)
	sco := bucket.Get(id[:])
	return sco != nil
}

// getSiacoinOutput fetches a siacoin output from the database. An error is
// returned if the siacoin output does not exist.
func getSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID) (types.SiacoinOutput, error) {
	scoBytes := tx.Bucket(SiacoinOutputs).Get(id[:])
	if scoBytes == nil {
		return types.SiacoinOutput{}, errNilItem
	}
	var sco types.SiacoinOutput
	err := encoding.Unmarshal(scoBytes, &sco)
	if err != nil {
		return types.SiacoinOutput{}, err
	}
	return sco, nil
}

// addSiacoinOutput adds a siacoin output to the database. An error is returned
// if the siacoin output is already in the database.
func addSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID, sco types.SiacoinOutput) {
	// While this is not supposed to be allowed, there's a bug in the consensus
	// code which means that earlier versions have accetped 0-value outputs
	// onto the blockchain. A hardfork to remove 0-value outputs will fix this,
	// and that hardfork is planned, but not yet.
	/*
		if build.DEBUG && sco.Value.IsZero() {
			panic("discovered a zero value siacoin output")
		}
	*/
	siacoinOutputs := tx.Bucket(SiacoinOutputs)
	// Sanity check - should not be adding an item that exists.
	if build.DEBUG && siacoinOutputs.Get(id[:]) != nil {
		panic("repeat siacoin output")
	}
	err := siacoinOutputs.Put(id[:], encoding.Marshal(sco))
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// removeSiacoinOutput removes a siacoin output from the database. An error is
// returned if the siacoin output is not in the database prior to removal.
func removeSiacoinOutput(tx *bolt.Tx, id types.SiacoinOutputID) {
	scoBucket := tx.Bucket(SiacoinOutputs)
	// Sanity check - should not be removing an item that is not in the db.
	if build.DEBUG && scoBucket.Get(id[:]) == nil {
		panic("nil siacoin output")
	}
	err := scoBucket.Delete(id[:])
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// getFileContract fetches a file contract from the database, returning an
// error if it is not there.
func getFileContract(tx *bolt.Tx, id types.FileContractID) (fc types.FileContract, err error) {
	fcBytes := tx.Bucket(FileContracts).Get(id[:])
	if fcBytes == nil {
		return types.FileContract{}, errNilItem
	}
	err = encoding.Unmarshal(fcBytes, &fc)
	if err != nil {
		return types.FileContract{}, err
	}
	return fc, nil
}

// addFileContract adds a file contract to the database. An error is returned
// if the file contract is already in the database.
func addFileContract(tx *bolt.Tx, id types.FileContractID, fc types.FileContract) {
	// Add the file contract to the database.
	fcBucket := tx.Bucket(FileContracts)
	// Sanity check - should not be adding a zero-payout file contract.
	if build.DEBUG && fc.Payout.IsZero() {
		panic("adding zero-payout file contract")
	}
	// Sanity check - should not be adding a file contract already in the db.
	if build.DEBUG && fcBucket.Get(id[:]) != nil {
		panic("repeat file contract")
	}
	err := fcBucket.Put(id[:], encoding.Marshal(fc))
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Add an entry for when the file contract expires.
	expirationBucketID := append(prefixFCEX, encoding.Marshal(fc.WindowEnd)...)
	expirationBucket, err := tx.CreateBucketIfNotExists(expirationBucketID)
	if build.DEBUG && err != nil {
		panic(err)
	}
	err = expirationBucket.Put(id[:], []byte{})
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// removeFileContract removes a file contract from the database.
func removeFileContract(tx *bolt.Tx, id types.FileContractID) {
	// Delete the file contract entry.
	fcBucket := tx.Bucket(FileContracts)
	fcBytes := fcBucket.Get(id[:])
	// Sanity check - should not be removing a file contract not in the db.
	if build.DEBUG && fcBytes == nil {
		panic("nil file contract")
	}
	err := fcBucket.Delete(id[:])
	if build.DEBUG && err != nil {
		panic(err)
	}

	// Delete the entry for the file contract's expiration. The portion of
	// 'fcBytes' used to determine the expiration bucket id is the
	// byte-representation of the file contract window end, which always
	// appears at bytes 48-56.
	expirationBucketID := append(prefixFCEX, fcBytes[48:56]...)
	expirationBucket := tx.Bucket(expirationBucketID)
	expirationBytes := expirationBucket.Get(id[:])
	if expirationBytes == nil {
		panic(errNilItem)
	}
	err = expirationBucket.Delete(id[:])
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// addDSCO adds a delayed siacoin output to the consnesus set.
func addDSCO(tx *bolt.Tx, bh types.BlockHeight, id types.SiacoinOutputID, sco types.SiacoinOutput) {
	// Sanity check - dsco should never have a value of zero.
	// An error in the consensus code means sometimes there are 0-value dscos
	// in the blockchain. A hardfork will fix this.
	/*
		if build.DEBUG && sco.Value.IsZero() {
			panic("zero-value dsco being added")
		}
	*/
	// Sanity check - output should not already be in the full set of outputs.
	if build.DEBUG && tx.Bucket(SiacoinOutputs).Get(id[:]) != nil {
		panic("dsco already in output set")
	}
	dscoBucketID := append(prefixDSCO, encoding.EncUint64(uint64(bh))...)
	dscoBucket := tx.Bucket(dscoBucketID)
	// Sanity check - should not be adding an item already in the db.
	if build.DEBUG && dscoBucket.Get(id[:]) != nil {
		panic(errRepeatInsert)
	}
	err := dscoBucket.Put(id[:], encoding.Marshal(sco))
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// removeDSCO removes a delayed siacoin output from the consensus set.
func removeDSCO(tx *bolt.Tx, bh types.BlockHeight, id types.SiacoinOutputID) {
	bucketID := append(prefixDSCO, encoding.Marshal(bh)...)
	// Sanity check - should not remove an item not in the db.
	dscoBucket := tx.Bucket(bucketID)
	if build.DEBUG && dscoBucket.Get(id[:]) == nil {
		panic("nil dsco")
	}
	err := dscoBucket.Delete(id[:])
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// createDSCOBucket creates a bucket for the delayed siacoin outputs at the
// input height.
func createDSCOBucket(tx *bolt.Tx, bh types.BlockHeight) {
	bucketID := append(prefixDSCO, encoding.Marshal(bh)...)
	_, err := tx.CreateBucket(bucketID)
	if build.DEBUG && err != nil {
		panic(err)
	}
}

// deleteDSCOBucket deletes the bucket that held a set of delayed siacoin
// outputs.
func deleteDSCOBucket(tx *bolt.Tx, bh types.BlockHeight) {
	// Delete the bucket.
	bucketID := append(prefixDSCO, encoding.Marshal(bh)...)
	bucket := tx.Bucket(bucketID)
	if build.DEBUG && bucket == nil {
		panic(errNilBucket)
	}

	// TODO: Check that the bucket is empty. Using Stats() does not work at the
	// moment, as there is an error in the boltdb code.

	err := tx.DeleteBucket(bucketID)
	if build.DEBUG && err != nil {
		panic(err)
	}
}

func createDSCOBucketIfNotExist(tx *bolt.Tx, bh types.BlockHeight) {
	bucketID := append(prefixDSCO, encoding.Marshal(bh)...)
	bucket := tx.Bucket(bucketID)
	if bucket == nil {
		_, err := tx.CreateBucket(bucketID)
		if build.DEBUG && err != nil {
			panic(err)
		}
	}
}

// getRelatedFileContracts finds contracts related to the storage proofs in a block
// TODO: this is currently used in lieu of a fuller-featured FileContractUnlockHashMap
func getRelatedFileContracts(tx *bolt.Tx, block *types.Block) []types.FileContract {
	var fileContracts []types.FileContract
	for _, txn := range block.Transactions {
		for _, proof := range txn.StorageProofs {
			fc, err := getFileContract(tx, proof.ParentID)
			if build.DEBUG && err != nil {
				panic(err)
			}
			fileContracts = append(fileContracts, fc)
		}
	}
	return fileContracts
}

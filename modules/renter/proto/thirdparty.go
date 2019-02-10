package proto

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
)

// UpdateContractRevision update contract revision
func (c *SafeContract) UpdateContractRevision(updator modules.ThirdpartyRenterRevisionUpdator) error {
	// construct new header
	if updator.Transaction.FileContractRevisions[0].NewRevisionNumber == c.header.LastRevision().NewRevisionNumber+1 {
		// need to be next revision to be accept
		return nil
	}

	c.headerMu.Lock()
	newHeader := c.header
	c.headerMu.Unlock()
	newHeader.Transaction = updator.Transaction
	if updator.Action == modules.RPCReviseContract {
		newHeader.StorageSpending = updator.StorageSpending
		newHeader.UploadSpending = updator.UploadSpending
		if err := c.applySetRoot(updator.SectorRoot, c.merkleRoots.len()); err != nil {
			return err
		}
	}
	if updator.Action == modules.RPCDownload {
		newHeader.StorageSpending = updator.StorageSpending
		newHeader.DownloadSpending = updator.DownloadSpending
	}

	if err := c.applySetHeader(newHeader); err != nil {
		return err
	}
	if err := c.headerFile.Sync(); err != nil {
		return err
	}

	return nil
}

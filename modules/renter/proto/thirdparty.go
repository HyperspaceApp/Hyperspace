package proto

import (
	"fmt"
	"log"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// UpdateContractRevision update contract revision
func (c *SafeContract) UpdateContractRevision(updator modules.ThirdpartyRenterRevisionUpdator) error {
	if updator.Action == modules.RPCReviseContract {
		// log.Printf("UpdateContractRevision for upload :%s", updator.ID)
	} else if updator.Action == modules.RPCDownload {
		log.Printf("UpdateContractRevision for download :%s", updator.ID)
	} else {
		log.Printf("UpdateContractRevision for other :%s", updator.ID)
	}

	// construct new header
	if updator.Transaction.FileContractRevisions[0].NewRevisionNumber != c.header.LastRevision().NewRevisionNumber+1 {
		// need to be next revision to be accept
		return fmt.Errorf("UpdateContractRevision not next revision: %s %d %d", updator.ID, updator.Transaction.FileContractRevisions[0].NewRevisionNumber, c.header.LastRevision().NewRevisionNumber)
	}
	// log.Printf("UpdateContractRevision for contract updating :%s", updator.ID)

	c.headerMu.Lock()
	newHeader := c.header
	c.headerMu.Unlock()
	newHeader.Transaction = updator.Transaction
	if updator.Action == modules.RPCReviseContract {
		newHeader.StorageSpending = updator.StorageSpending
		newHeader.UploadSpending = updator.UploadSpending
		newHeader.Transaction = updator.Transaction
		if err := c.applySetRoot(updator.SectorRoot, c.merkleRoots.len()); err != nil {
			return err
		}
	}
	if updator.Action == modules.RPCDownload {
		newHeader.StorageSpending = updator.StorageSpending
		newHeader.DownloadSpending = updator.DownloadSpending
		newHeader.Transaction = updator.Transaction
	}

	// if updator.Action == modules.ThirdpartySync {
	// }

	if err := c.applySetHeader(newHeader); err != nil {
		return err
	}
	if err := c.headerFile.Sync(); err != nil {
		return err
	}

	return nil
}

// Sign help thirdparty renter to sign upload/download action
func (c *SafeContract) Sign(id types.FileContractID, txn *types.Transaction) error {
	log.Printf("Sign for contract :%s", id)
	encodedSig := crypto.SignHash(txn.SigHash(0), c.header.SecretKey)
	txn.TransactionSignatures[0].Signature = encodedSig[:]
	return nil
}

// SignChallenge help sign contract challenge
func (c *SafeContract) SignChallenge(id types.FileContractID, challenge crypto.Hash) (crypto.Signature, error) {
	log.Printf("Sign challenge for contract :%s", id)
	encodedSig := crypto.SignHash(challenge, c.header.SecretKey)
	return encodedSig, nil
}

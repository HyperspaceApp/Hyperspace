package contractor

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/modules/renter/proto"
	"github.com/HyperspaceApp/Hyperspace/types"
	"github.com/HyperspaceApp/fastrand"
)

// findRecoverableContracts scans the block for contracts that could
// potentially be recovered. We are not going to recover them right away though
// since many of them could already be expired. Recovery happens periodically
// in threadedContractMaintenance.
func (c *Contractor) findRecoverableContracts(walletSeed modules.Seed, b types.Block) {
	for _, txn := range b.Transactions {
		// Check if the arbitrary data starts with the correct prefix.
		csi, encryptedHostKey, hasIdentifier := hasFCIdentifier(txn)
		if !hasIdentifier {
			continue
		}
		// Check if any contract should be recovered.
		for i, fc := range txn.FileContracts {
			// Create the RenterSeed for this contract and wipe it afterwards.
			rs := proto.EphemeralRenterSeed(walletSeed, fc.WindowStart)
			defer fastrand.Read(rs[:])
			// Validate it.
			hostKey, valid := csi.IsValid(rs, txn, encryptedHostKey)
			if !valid {
				continue
			}
			// Make sure we don't know about that contract already.
			fcid := txn.FileContractID(uint64(i))
			_, known := c.staticContracts.View(fcid)
			if known {
				continue
			}
			// Mark the contract for recovery.
			c.recoverableContracts = append(c.recoverableContracts, modules.RecoverableContract{
				FileContract:  fc,
				ID:            fcid,
				HostPublicKey: hostKey,
				InputParentID: txn.SiacoinInputs[0].ParentID,
			})
		}
	}
}

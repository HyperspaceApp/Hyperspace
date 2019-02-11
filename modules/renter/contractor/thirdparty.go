package contractor

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// ThirdpartyContracts will return contracts for thirdparty clients
func (c *Contractor) ThirdpartyContracts() []modules.ThirdpartyRenterContract {
	return c.staticContracts.ThirdpartyViewAll()
}

// UpdateContractRevision help update the contract revision upload/download
func (c *Contractor) UpdateContractRevision(updator modules.ThirdpartyRenterRevisionUpdator) error {
	sc, exists := c.staticContracts.Acquire(updator.ID)
	defer c.staticContracts.Return(sc)
	// TODO: compare and update
	if !exists {
		return fmt.Errorf("there is no such contract: %s", updator.ID)
	}
	return sc.UpdateContractRevision(updator)
}

// Sign help thirdparty renter to sign upload/download action
func (c *Contractor) Sign(id types.FileContractID, txn *types.Transaction) error {
	sc, exists := c.staticContracts.Acquire(id)
	defer c.staticContracts.Return(sc)
	// TODO: compare and update
	if !exists {
		return fmt.Errorf("there is no such contract: %s", id)
	}
	return sc.Sign(id, txn)
}

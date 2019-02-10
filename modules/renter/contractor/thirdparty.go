package contractor

import (
	"fmt"

	"github.com/HyperspaceApp/Hyperspace/modules"
)

// ThirdpartyContracts will return contracts for thirdparty clients
func (c *Contractor) ThirdpartyContracts() []modules.ThirdpartyRenterContract {
	return c.staticContracts.ThirdpartyViewAll()
}

// UpdateContractRevision help update the contract revision upload/download
func (c *Contractor) UpdateContractRevision(updator modules.ThirdpartyRenterRevisionUpdator) error {
	sc, exists := c.staticContracts.Acquire(updator.ID)
	// TODO: compare and update
	if !exists {
		return fmt.Errorf("there is no such contract: %s", updator.ID)
	}
	return sc.UpdateContractRevision(updator)
}

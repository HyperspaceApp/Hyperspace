package client

import (
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/node/api"
)

// ThirdpartyServerContractsGet get the contracts
func (c *Client) ThirdpartyServerContractsGet() (trc modules.ThirdpartyRenterContracts, err error) {
	err = c.get("/contracts", &trc)
	return
}

// ThirdpartyServerHostDbAllGet requests the /hostdb/all endpoint's resources.
func (c *Client) ThirdpartyServerHostDbAllGet() (hdag api.HostdbAllGET, err error) {
	err = c.get("/hostdb/all", &hdag)
	return
}

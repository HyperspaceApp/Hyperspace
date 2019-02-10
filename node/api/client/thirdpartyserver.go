package client

import (
	"encoding/base64"
	"net/url"

	"github.com/HyperspaceApp/Hyperspace/encoding"
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

// ThirdpartyServerContractRevisionPost uses the /contract/revision to update the transaction in server
func (c *Client) ThirdpartyServerContractRevisionPost(trru modules.ThirdpartyRenterRevisionUpdator) (err error) {
	values := url.Values{}
	values.Set("updator", base64.StdEncoding.EncodeToString(encoding.Marshal(trru)))

	err = c.post("/contract/revision", values.Encode(), nil)
	return
}

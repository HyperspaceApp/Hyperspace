package client

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// ThirdpartyContractsGet requests the /thirdparty/contracts resource and returns
// Contracts and ActiveContracts
func (c *Client) ThirdpartyContractsGet() (rc api.RenterContracts, err error) {
	err = c.get("/thirdparty/contracts", &rc)
	return
}

// ThirdpartyInactiveContractsGet requests the /thirdparty/contracts resource with the
// inactive flag set to true
func (c *Client) ThirdpartyInactiveContractsGet() (rc api.RenterContracts, err error) {
	values := url.Values{}
	values.Set("inactive", fmt.Sprint(true))
	err = c.get("/thirdparty/contracts?"+values.Encode(), &rc)
	return
}

// ThirdpartyExpiredContractsGet requests the /renter/contracts resource with the
// expired flag set to true
func (c *Client) ThirdpartyExpiredContractsGet() (rc api.RenterContracts, err error) {
	values := url.Values{}
	values.Set("expired", fmt.Sprint(true))
	err = c.get("/renter/contracts?"+values.Encode(), &rc)
	return
}

// ThirdpartyHostDbGet requests the /hostdb endpoint's resources.
func (c *Client) ThirdpartyHostDbGet() (hdg api.HostdbGet, err error) {
	err = c.get("/thirdparty/hostdb", &hdg)
	return
}

// ThirdpartyHostDbActiveGet requests the /hostdb/active endpoint's resources.
func (c *Client) ThirdpartyHostDbActiveGet() (hdag api.HostdbActiveGET, err error) {
	err = c.get("/thirdparty/hostdb/active", &hdag)
	return
}

// ThirdpartyHostDbAllGet requests the /hostdb/all endpoint's resources.
func (c *Client) ThirdpartyHostDbAllGet() (hdag api.HostdbAllGET, err error) {
	err = c.get("/thirdparty/hostdb/all", &hdag)
	return
}

// ThirdpartyHostDbFilterModePost requests the /hostdb/filtermode endpoint
func (c *Client) ThirdpartyHostDbFilterModePost(fm modules.FilterMode, hosts []types.SiaPublicKey) (err error) {
	filterMode := fm.String()
	hdblp := api.HostdbFilterModePOST{
		FilterMode: filterMode,
		Hosts:      hosts,
	}

	data, err := json.Marshal(hdblp)
	if err != nil {
		return err
	}
	err = c.post("/thirdparty/hostdb/FilterMode", string(data), nil)
	return
}

// ThirdpartyHostDbHostsGet request the /hostdb/hosts/:pubkey endpoint's resources.
func (c *Client) ThirdpartyHostDbHostsGet(pk types.SiaPublicKey) (hhg api.HostdbHostsGET, err error) {
	err = c.get("/thirdparty/hostdb/hosts/"+pk.String(), &hhg)
	return
}

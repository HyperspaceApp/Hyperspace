package client

import (
	"encoding/json"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// HostDbGet requests the /hostdb endpoint's resources.
func (c *Client) HostDbGet() (hdg api.HostdbGet, err error) {
	err = c.get("/hostdb", &hdg)
	return
}

// HostDbActiveGet requests the /hostdb/active endpoint's resources.
func (c *Client) HostDbActiveGet() (hdag api.HostdbActiveGET, err error) {
	err = c.get("/hostdb/active", &hdag)
	return
}

// HostDbAllGet requests the /hostdb/all endpoint's resources.
func (c *Client) HostDbAllGet() (hdag api.HostdbAllGET, err error) {
	err = c.get("/hostdb/all", &hdag)
	return
}

// HostDbFilterModePost requests the /hostdb/filtermode endpoint
func (c *Client) HostDbFilterModePost(fm modules.FilterMode, hosts []types.SiaPublicKey) (err error) {
	filterMode := fm.String()
	hdblp := api.HostdbFilterModePOST{
		FilterMode: filterMode,
		Hosts:      hosts,
	}

	data, err := json.Marshal(hdblp)
	if err != nil {
		return err
	}
	err = c.post("/hostdb/FilterMode", string(data), nil)
	return
}

// HostDbHostsGet request the /hostdb/hosts/:pubkey endpoint's resources.
func (c *Client) HostDbHostsGet(pk types.SiaPublicKey) (hhg api.HostdbHostsGET, err error) {
	err = c.get("/hostdb/hosts/"+pk.String(), &hhg)
	return
}

package client

import (
	"net/url"

	"github.com/NebulousLabs/Sia/node/api"
)

// MiningPoolConfigGet requests the /pool/config configuration info.
func (c *Client) MiningPoolConfigGet() (config api.MiningPoolConfig, err error) {
	err = c.get("/pool/config", &config)
	return
}

// MiningPoolConfigPost uses the /pool/config endpoint to tell mining pool
// to use a new configuration value
func (c *Client) MiningPoolConfigPost(key string, val string) (err error) {
	values := url.Values{}
	values.Set(key, val)
	err = c.post("/pool/config", values.Encode(), nil)
	return
}

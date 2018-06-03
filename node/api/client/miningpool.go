package client

import (
	"net/url"

	"github.com/HyperspaceApp/Hyperspace/node/api"
)

// MiningPoolGet requests the /pool endpoint's resources.
func (c *Client) MiningPoolGet() (mg api.MiningPoolGET, err error) {
	err = c.get("/pool", &mg)
	return
}

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

// MiningPoolClientsGet requests the /pool/clients client info list.
func (c *Client) MiningPoolClientsGet() (clientInfos api.MiningPoolClientsInfo, err error) {
	err = c.get("/pool/clients", &clientInfos)
	return
}

// MiningPoolClientGet requests /pool/client?name=foo to retrieve info about one client.
func (c *Client) MiningPoolClientGet(name string) (clientInfo api.MiningPoolClientInfo, err error) {
	err = c.get("/pool/client?name="+name, &clientInfo)
	return
}

// MiningPoolTransactionsGet requests /pool/clienttx?name=foo to retrieve transaction info about one client.
func (c *Client) MiningPoolTransactionsGet(name string) (txs []api.MiningPoolClientTransactions, err error) {
        err = c.get("/pool/clienttx?name="+name, &txs)
	return
}

// MiningPoolBlocksGet requests the /pool/blocks block info list.
func (c *Client) MiningPoolBlocksGet() (blockInfos []api.MiningPoolBlocksInfo, err error) {
	err = c.get("/pool/blocks", &blockInfos)
	return
}

// MiningPoolBlockGet requests the /pool/block?block=foo block info for a given client..
// TODO this API seems poorly named
func (c *Client) MiningPoolBlockGet(name string) (blockInfo []api.MiningPoolBlockInfo, err error) {
	err = c.get("/pool/block?block="+name, &blockInfo)
	return
}

// MiningPoolStartGet uses the /pool/start endpoint to start the cpu miner.
func (c *Client) MiningPoolStartGet() (err error) {
	err = c.get("/pool/start", nil)
	return
}

// MiningPoolStopGet uses the /pool/stop endpoint to stop the cpu miner.
func (c *Client) MiningPoolStopGet() (err error) {
	err = c.get("/pool/stop", nil)
	return
}

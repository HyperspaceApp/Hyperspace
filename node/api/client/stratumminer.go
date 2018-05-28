package client

import (
	"net/url"

	"github.com/NebulousLabs/Sia/node/api"
)

// StratumMinerGet requests the /stratumminer endpoint's resources.
func (c *Client) StratumMinerGet() (mg api.StratumMinerGET, err error) {
	err = c.get("/stratumminer", &mg)
	return
}

// StratumMinerStartPost uses the /stratumminer/start endpoint to start the stratum miner.
func (c *Client) StratumMinerStartPost(server, username string) (err error) {
	values := url.Values{}
	values.Set("server", server)
	values.Set("username", username)
	err = c.post("/stratumminer/start", values.Encode(), nil)
	return
}

// StratumMinerStopGet uses the /stratumminer/stop endpoint to stop the stratum miner.
func (c *Client) StratumMinerStopPost() (err error) {
	err = c.post("/stratumminer/stop", "", nil)
	return
}

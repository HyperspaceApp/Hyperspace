package client

import "github.com/HardDriveCoin/HardDriveCoin/node/api"

// ConsensusGet requests the /consensus api resource
func (c *Client) ConsensusGet() (cg api.ConsensusGET, err error) {
	err = c.Get("/consensus", &cg)
	return
}

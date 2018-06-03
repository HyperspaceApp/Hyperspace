package client

import (
	"net/url"

	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// TransactionPoolFeeGet uses the /tpool/fee endpoint to get a fee estimation.
func (c *Client) TransactionPoolFeeGet() (tfg api.TpoolFeeGET, err error) {
	err = c.get("/tpool/fee", &tfg)
	return
}

// TransactionPoolRawPost uses the /tpool/raw endpoint to send a raw
// transaction to the transaction pool.
func (c *Client) TransactionPoolRawPost(txn types.Transaction, parents types.Transaction) (err error) {
	values := url.Values{}
	values.Set("transaction", string(encoding.Marshal(txn)))
	values.Set("parents", string(encoding.Marshal(parents)))
	err = c.post("/tpool/raw", values.Encode(), nil)
	return
}

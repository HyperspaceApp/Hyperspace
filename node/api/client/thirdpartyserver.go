package client

import (
	"encoding/base64"
	"encoding/json"
	"net/url"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/encoding"
	"github.com/HyperspaceApp/Hyperspace/modules"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"
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

// ThirdpartyServerSignPost will remotely sign the transaction
func (c *Client) ThirdpartyServerSignPost(id types.FileContractID, txn types.Transaction) (encodedSig crypto.Signature, err error) {
	var tspr modules.ThirdpartySignPOSTResp
	json, err := json.Marshal(modules.ThirdpartySignPOSTParams{
		Transaction: txn,
		ID:          id,
	})
	if err != nil {
		return crypto.Signature{}, err
	}
	err = c.post("/sign", string(json), &tspr)
	if err != nil {
		return crypto.Signature{}, err
	}
	copy(encodedSig[:], tspr.Transaction.TransactionSignatures[0].Signature[:])
	return
}

// ThirdpartyServerSignChallengePost will remotely sign the transaction
func (c *Client) ThirdpartyServerSignChallengePost(id types.FileContractID, challenge crypto.Hash) (encodedSig crypto.Signature, err error) {
	var tscpr modules.ThirdpartySignChallengePOSTResp
	json, err := json.Marshal(modules.ThirdpartySignChallengePOSTParams{
		Challenge: challenge,
		ID:        id,
	})
	if err != nil {
		return crypto.Signature{}, err
	}
	err = c.post("/sign/challenge", string(json), &tscpr)
	if err != nil {
		return crypto.Signature{}, err
	}
	copy(encodedSig[:], tscpr.Signature[:])
	return
}

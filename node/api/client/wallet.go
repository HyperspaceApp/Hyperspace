package client

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/HyperspaceApp/Hyperspace/crypto"
	"github.com/HyperspaceApp/Hyperspace/node/api"
	"github.com/HyperspaceApp/Hyperspace/types"
)

// WalletAddressGet requests an unused address from the /wallet/address endpoint
func (c *Client) WalletAddressGet() (wag api.WalletAddressGET, err error) {
	err = c.get("/wallet/address", &wag)
	return
}

// WalletAddressPost requests a new address from the /wallet/address endpoint
func (c *Client) WalletAddressPost() (wap api.WalletAddressPOST, err error) {
	values := url.Values{}
	err = c.post("/wallet/address", values.Encode(), &wap)
	return
}

// WalletAddressesGet requests the wallets known addresses from the
// /wallet/addresses endpoint.
func (c *Client) WalletAddressesGet() (wag api.WalletAddressesGET, err error) {
	err = c.get("/wallet/addresses", &wag)
	return
}

// WalletChangePasswordPost uses the /wallet/changepassword endpoint to change
// the wallet's password.
func (c *Client) WalletChangePasswordPost(currentPassword, newPassword string) (err error) {
	values := url.Values{}
	values.Set("newpassword", newPassword)
	values.Set("encryptionpassword", currentPassword)
	err = c.post("/wallet/changepassword", values.Encode(), nil)
	return
}

// WalletInitPost uses the /wallet/init endpoint to initialize and encrypt a
// wallet
func (c *Client) WalletInitPost(password string, force bool) (wip api.WalletInitPOST, err error) {
	values := url.Values{}
	values.Set("encryptionpassword", password)
	values.Set("force", strconv.FormatBool(force))
	err = c.post("/wallet/init", values.Encode(), &wip)
	return
}

// WalletInitSeedPost uses the /wallet/init/seed endpoint to initialize and
// encrypt a wallet using a given seed.
func (c *Client) WalletInitSeedPost(seed, password string, force bool) (err error) {
	values := url.Values{}
	values.Set("seed", seed)
	values.Set("encryptionpassword", password)
	values.Set("force", strconv.FormatBool(force))
	err = c.post("/wallet/init/seed", values.Encode(), nil)
	return
}

// WalletGet requests the /wallet api resource
func (c *Client) WalletGet() (wg api.WalletGET, err error) {
	err = c.get("/wallet", &wg)
	return
}

// WalletLockPost uses the /wallet/lock endpoint to lock the wallet.
func (c *Client) WalletLockPost() (err error) {
	err = c.post("/wallet/lock", "", nil)
	return
}

// WalletSeedPost uses the /wallet/seed endpoint to add a seed to the wallet's list
// of seeds.
func (c *Client) WalletSeedPost(seed, password string) (err error) {
	values := url.Values{}
	values.Set("seed", seed)
	values.Set("encryptionpassword", password)
	err = c.post("/wallet/seed", values.Encode(), nil)
	return
}

// WalletSeedsGet uses the /wallet/seeds endpoint to return the wallet's
// current seeds.
func (c *Client) WalletSeedsGet() (wsg api.WalletSeedsGET, err error) {
	err = c.get("/wallet/seeds", &wsg)
	return
}

// WalletSiacoinsMultiPost uses the /wallet/siacoin api endpoint to send money
// to multiple addresses at once
func (c *Client) WalletSiacoinsMultiPost(outputs []types.SiacoinOutput) (wsp api.WalletSiacoinsPOST, err error) {
	values := url.Values{}
	marshaledOutputs, err := json.Marshal(outputs)
	if err != nil {
		return api.WalletSiacoinsPOST{}, err
	}
	values.Set("outputs", string(marshaledOutputs))
	err = c.post("/wallet/spacecash", values.Encode(), &wsp)
	return
}

// WalletSiacoinsPost uses the /wallet/spacecash api endpoint to send money to a
// single address
func (c *Client) WalletSiacoinsPost(amount types.Currency, destination types.UnlockHash) (wsp api.WalletSiacoinsPOST, err error) {
	values := url.Values{}
	values.Set("amount", amount.String())
	values.Set("destination", destination.String())
	err = c.post("/wallet/spacecash", values.Encode(), &wsp)
	return
}

// WalletSignPost uses the /wallet/sign api endpoint to sign a transaction.
func (c *Client) WalletSignPost(txn types.Transaction, toSign []crypto.Hash) (wspr api.WalletSignPOSTResp, err error) {
	json, err := json.Marshal(api.WalletSignPOSTParams{
		Transaction: txn,
		ToSign:      toSign,
	})
	if err != nil {
		return
	}
	err = c.post("/wallet/sign", string(json), &wspr)
	return
}

// WalletSiagKeyPost uses the /wallet/siagkey endpoint to load a siag key into
// the wallet.
func (c *Client) WalletSiagKeyPost(keyfiles, password string) (err error) {
	values := url.Values{}
	values.Set("keyfiles", keyfiles)
	values.Set("encryptionpassword", password)
	err = c.post("/wallet/siagkey", values.Encode(), nil)
	return
}

// WalletSweepPost uses the /wallet/sweep/seed endpoint to sweep a seed into
// the current wallet.
func (c *Client) WalletSweepPost(seed string) (wsp api.WalletSweepPOST, err error) {
	values := url.Values{}
	values.Set("seed", seed)
	err = c.post("/wallet/sweep/seed", values.Encode(), &wsp)
	return
}

// WalletTransactionsGet requests the/wallet/transactions api resource for a
// certain startheight and endheight
func (c *Client) WalletTransactionsGet(startHeight types.BlockHeight, endHeight types.BlockHeight) (wtg api.WalletTransactionsGET, err error) {
	err = c.get(fmt.Sprintf("/wallet/transactions?startheight=%v&endheight=%v",
		startHeight, endHeight), &wtg)
	return
}

// WalletBuildTransactionGet requests the /wallet/transactions/build api resource for a
// certain destiniation and amount
func (c *Client) WalletBuildTransactionGet(destination types.UnlockHash, amount types.Currency) (wbtg api.WalletBuildTransactionGET, err error) {
	err = c.get(fmt.Sprintf("/wallet/build/transaction?destination=%v&amount=%v",
		destination, amount), &wbtg)
	return
}

// WalletTransactionGet requests the /wallet/transaction/:id api resource for a
// certain TransactionID.
func (c *Client) WalletTransactionGet(id types.TransactionID) (wtg api.WalletTransactionGETid, err error) {
	err = c.get("/wallet/transaction/"+id.String(), wtg)
	return
}

// WalletUnlockPost uses the /wallet/unlock endpoint to unlock the wallet with
// a given encryption key. Per default this key is the seed.
func (c *Client) WalletUnlockPost(password string) (err error) {
	values := url.Values{}
	values.Set("encryptionpassword", password)
	err = c.post("/wallet/unlock", values.Encode(), nil)
	return
}

// WalletUnlockConditionsGet requests the /wallet/unlockconditions endpoint
// and returns the UnlockConditions of addr.
func (c *Client) WalletUnlockConditionsGet(addr types.UnlockHash) (wucg api.WalletUnlockConditionsGET, err error) {
	err = c.get("/wallet/unlockconditions/"+addr.String(), &wucg)
	return
}

// WalletUnspentGet requests the /wallet/unspent endpoint and returns all of
// the unspent outputs related to the wallet.
func (c *Client) WalletUnspentGet() (wug api.WalletUnspentGET, err error) {
	err = c.get("/wallet/unspent", &wug)
	return
}

// WalletWatchGet requests the /wallet/watch endpoint and returns the set of
// currently watched addresses.
func (c *Client) WalletWatchGet() (wwg api.WalletWatchGET, err error) {
	err = c.get("/wallet/watch", &wwg)
	return
}

// WalletWatchPost uses the /wallet/watch endpoint to add a set of addresses
// to the watch set. The unused flag should be set to true if the addresses
// have never appeared in the blockchain.
func (c *Client) WalletWatchAddPost(addrs []types.UnlockHash, unused bool) error {
	json, err := json.Marshal(api.WalletWatchPOST{
		Addresses: addrs,
		Remove:    false,
		Unused:    unused,
	})
	if err != nil {
		return err
	}
	return c.post("/wallet/watch", string(json), nil)
}

// WalletWatchPost uses the /wallet/watch endpoint to remove a set of
// addresses from the watch set. The unused flag should be set to true if the
// addresses have never appeared in the blockchain.
func (c *Client) WalletWatchRemovePost(addrs []types.UnlockHash, unused bool) error {
	json, err := json.Marshal(api.WalletWatchPOST{
		Addresses: addrs,
		Remove:    true,
		Unused:    unused,
	})
	if err != nil {
		return err
	}
	return c.post("/wallet/watch", string(json), nil)
}

Wallet Information for Exchanges
=======

Hyperspace's wallet differs a bit from Bitcoin's. This guide is to point any exchanges to relevant functions and features of the wallet that may be of unique interest to them.

### Address Management

The Hyperspace wallet follows the concept of an address gap limit [as specified in BIP 44](https://github.com/bitcoin/bips/blob/master/bip-0044.mediawiki#Address_gap_limit). The default gap limit is 50. This means the wallet will not allow creation of more than 50 consecutive addresses if none of those 50 addresses have been used on the blockchain.

An exchange may want to create more than 50 addresses before using them. The address gap limit can be modified by launching hsd with the `--address-gap-limit` flag. For example, if an exchange wants to start off with 10,000 addresses, they might want to set the address gap limit to be a little larger (just so they never bump into it): `hsd --address-gap-limit 15000`.

New addresses can be created via a POST request to the `/wallet/address` endpoint. Do not confuse this with a GET request to `/wallet/address`, which will always return the first address in the wallet that has not yet been seen on the blockchain. New addresses can also be created with the `hsc wallet new-address` command.

### Useful API Endpoints

#### /wallet/watch [POST]

###### Parameters
The parameters should be encoded as JSON.

```
// Array of addresses to be watched
addresses

// Boolean indicating whether we want to remove these address from our watch-list instead of add them
remove

// Boolean indicating whether or not these addresses have been used on the blockchain before. If unused is false, the wallet will rescan the blockchain after this request.
unused

```

###### Response

standard success or error response. See
[#standard-responses](#standard-responses).

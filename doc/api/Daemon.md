Daemon API
===========

There may be functional API calls which are not documented. These are not
guaranteed to be supported beyond the current release, and should not be used
in production.

Overview
--------

The daemon is responsible for starting and stopping the modules which make up
the rest of Hyperspace. It also provides endpoints for viewing build constants.

Index
-----

| Route                                     | HTTP verb |
| ----------------------------------------- | --------- |
| [/daemon/constants](#daemonconstants-get) | GET       |
| [/daemon/stop](#daemonstop-get)           | GET       |
| [/daemon/version](#daemonversion-get)     | GET       |

#### /daemon/constants [GET]

returns the set of constants in use.

###### JSON Response
```javascript
{
  // Timestamp of the genesis block.
  "genesistimestamp": 1532510521, // Unix time

  // Maximum size, in bytes, of a block. Blocks larger than this will be
  // rejected by peers.
  "blocksizelimit": 2000000, // bytes

  // Target for how frequently new blocks should be mined.
  "blockfrequency": 600, // seconds per block

  // Farthest a block's timestamp can be in the future before the block is
  // rejected outright.
  "extremefuturethreshold": 18000, // seconds

  // Height of the window used to adjust the difficulty.
  "targetwindow": 1000, // blocks

  // Duration of the window used to adjust the difficulty.
  "mediantimestampwindow": 11, // blocks

  // How far in the future a block can be without being rejected. A block
  // further into the future will not be accepted immediately, but the daemon
  // will attempt to accept the block as soon as it is valid.
  "futurethreshold": 10800, // seconds

  // Number of children a block must have before it is considered "mature."
  "maturitydelay": 144, // blocks

  // Number of coins given to the miner of the first block. Note that elsewhere
  // in the API currency is typically returned in hastings and as a bignum.
  // This is not the case here.
  "initialcoinbase": 60000, // SPACE

  // Minimum number of coins paid out to the miner of a block (the coinbase
  // decreases with each block). Note that elsewhere in the API currency is
  // typically returned in hastings and as a bignum. This is not the case
  // here.
  "minimumcoinbase": 6000, // SPACE

  // Initial target.
  "roottarget": [0,0,0,0,32,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],

  // Initial depth.
  "rootdepth": [255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255,255],

  // Largest allowed ratio between the old difficulty and the new difficulty.
  "maxtargetadjustmentup": "5/2",

  // Smallest allowed ratio between the old difficulty and the new difficulty.
  "maxtargetadjustmentdown": "2/5",

  // Number of Hastings in one SPACE.
  "siacoinprecision": "1000000000000000000000000" // hastings per SPACE
}
```

#### /daemon/stop [GET]

cleanly shuts down the daemon. May take a few seconds.

###### Response
standard success or error response. See
[#standard-responses](#standard-responses).

#### /daemon/version [GET]

returns the version of the Hyperspace daemon currently running.

###### JSON Response
```javascript
{
  // Version number of the running Hyperspace Daemon. This number is visible to its
  // peers on the network.
  "version": "1.0.0"
}
```

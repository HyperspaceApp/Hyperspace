Explorer API
=============

This document contains detailed descriptions of the explorer API routes. For
an overview of the explorer API routes, see
[API.md#explorer](/doc/API.md#explorer).  For an overview of all API routes,
see [API.md](/doc/API.md)

There may be functional API calls which are not documented. These are not
guaranteed to be supported beyond the current release, and should not be used
in production.

Overview
--------

The explorer API allows retrieval of information about the status of the blockchain.
It is designed to allow developers build monitoring services. The explorer API
also supplies a websocket API to receive push notifications about new blocks and
transactions.

Index
-----

| Route                                                                       | HTTP verb |
| --------------------------------------------------------------------------- | --------- |
| [/explorer](#explorer-get)                                                  | GET       |
| [/explorer/pending](#explorerpending-get)                                   | GET       |
| [/explorer/blocks/:height](#explorerblocksheight-get)                       | GET       |
| [/explorer/hashes/:hash](#explorerhasheshash-get)                           | GET       |
| [/explorer/ws](#explorerws-get)                                             | GET       |
| [/explorer/tx/ws](#explorertxws-get)                                        | GET       |

#### /explorer [GET]

Returns information about the explorer set, such as the current height
and the miner payouts.

###### Response
The JSON formatted block or a standard error response.

```
{
  "blocks": [
    {
      "minerpayoutids": [
        "7cbeae04cf8c04f3b88caf944868b3683f31abf584ee67862a4644b33af20915",
        "4c025711c82ffe2d124e0000736157b7a1f0264f68f3fa7466fc39bc889e86f7"
      ],
      "transactions": [
        {
          "id": "b07eeb468d53352500ff22f28839921bb2fe16ad894f45317499f0b84a85d4d0",
          "height": 1915,
          "parent": "0000000000000004e7378cc352603272319a593cbbf0bd2b62d613b4ef49691c",
          "rawtransaction": {
            "siacoininputs": [
              {
                "parentid": "ba14d07bbad645b51799ddb49b209c9eeca0bef4aa5d63c4d79e2f841baedb5d",
                "unlockconditions": {
                  "timelock": 0,
                  "publickeys": [
                    {
                      "algorithm": "ed25519",
                      "key": "QsITebmVU+PeTo3ae+kbtxzevs8hPociivEkIFZ1Dvc="
                    }
                  ],
                  "signaturesrequired": 1
                }
              }
            ],
            "siacoinoutputs": [
              {
                "value": "2653571271520000000000000000",
                "unlockhash": "189ab45b49aea23a8295891762a7461b10f584c617c01da21175b96e9cdc0351f5a16b6284b3"
              },
              ...
            ],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [
              "63600000000000000000000"
            ],
            "arbitrarydata": [],
            "transactionsignatures": [
              {
                "parentid": "ba14d07bbad645b51799ddb49b209c9eeca0bef4aa5d63c4d79e2f841baedb5d",
                "publickeyindex": 0,
                "timelock": 0,
                "coveredfields": {
                  "wholetransaction": true,
                  "siacoininputs": [],
                  "siacoinoutputs": [],
                  "filecontracts": [],
                  "filecontractrevisions": [],
                  "storageproofs": [],
                  "minerfees": [],
                  "arbitrarydata": [],
                  "transactionsignatures": []
                },
                "signature": "TS3lEL5qcK+Krz/qfZKC+60TEkyuk4PTfCranjm+UbpygjL8pJdF6vjrue5QHm8KnbOXZE9GFumUZAjxvU3hAg=="
              }
            ]
          },
          "siacoininputoutputs": [
            {
              "value": "27373960874010000000000000000",
              "unlockhash": "645c9264602fa7e17f901513b1e212251d596c255fa909a7f1dfc9e32f987fb09358888c86fa"
            }
          ],
          "siacoinoutputids": [
            "f46560371f9412cadcde00ba1d857aea14d9a375f4483636d2b8d44039966f3f",
            "7df2b09d936d69149e53bd963242f61f87da8987287a622af88bde114e53d59a"
          ],
          "filecontractids": null,
          "filecontractvalidproofoutputids": null,
          "filecontractmissedproofoutputids": null,
          "filecontractrevisionvalidproofoutputids": null,
          "filecontractrevisionmissedproofoutputids": null,
          "storageproofoutputids": null,
          "storageproofoutputs": null
        },
        {
          "id": "3e255f0d9ed13d238540b1eefa0ca77fb820e47f53043c3606fba0c302cd94ca",
          "height": 1915,
          "parent": "0000000000000004e7378cc352603272319a593cbbf0bd2b62d613b4ef49691c",
          "rawtransaction": {
            "siacoininputs": [],
            "siacoinoutputs": [],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [],
            "arbitrarydata": [
              "Tm9uU2lhAAAAAAAAAAAAAExVWE9SAABTSVAxAABL+aGMAAAAAAAAAJAAHyAnPwQA"
            ],
            "transactionsignatures": []
          },
          "siacoininputoutputs": null,
          "siacoinoutputids": null,
          "filecontractids": null,
          "filecontractvalidproofoutputids": null,
          "filecontractmissedproofoutputids": null,
          "filecontractrevisionvalidproofoutputids": null,
          "filecontractrevisionmissedproofoutputids": null,
          "storageproofoutputids": null,
          "storageproofoutputs": null
        }
      ],
      "rawblock": {
        "parentid": "0000000000000008a44bc37f770822739b7c284d40037e30026b750a2c0d605b",
        "nonce": [
          28,
          ...
        ],
        "timestamp": 1533653027,
        "minerpayouts": [
          {
            "value": "53655903600000000000000000000",
            "unlockhash": "0ded9676c5c22351a1c6e99a19f570cd4f1e00b01406422311aa18b74cb7c9b4972f2148f9d6"
          },
          ...
        ],
        "transactions": [
          {
            "siacoininputs": [
              {
                "parentid": "ba14d07bbad645b51799ddb49b209c9eeca0bef4aa5d63c4d79e2f841baedb5d",
                "unlockconditions": {
                  "timelock": 0,
                  "publickeys": [
                    {
                      "algorithm": "ed25519",
                      "key": "QsITebmVU+PeTo3ae+kbtxzevs8hPociivEkIFZ1Dvc="
                    }
                  ],
                  "signaturesrequired": 1
                }
              }
            ],
            "siacoinoutputs": [
              {
                "value": "2653571271520000000000000000",
                "unlockhash": "189ab45b49aea23a8295891762a7461b10f584c617c01da21175b96e9cdc0351f5a16b6284b3"
              },
              ...
            ],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [
              "63600000000000000000000"
            ],
            "arbitrarydata": [],
            "transactionsignatures": [
              {
                "parentid": "ba14d07bbad645b51799ddb49b209c9eeca0bef4aa5d63c4d79e2f841baedb5d",
                "publickeyindex": 0,
                "timelock": 0,
                "coveredfields": {
                  "wholetransaction": true,
                  "siacoininputs": [],
                  "siacoinoutputs": [],
                  "filecontracts": [],
                  "filecontractrevisions": [],
                  "storageproofs": [],
                  "minerfees": [],
                  "arbitrarydata": [],
                  "transactionsignatures": []
                },
                "signature": "TS3lEL5qcK+Krz/qfZKC+60TEkyuk4PTfCranjm+UbpygjL8pJdF6vjrue5QHm8KnbOXZE9GFumUZAjxvU3hAg=="
              }
            ]
          },
          {
            "siacoininputs": [],
            "siacoinoutputs": [],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [],
            "arbitrarydata": [
              "Tm9uU2lhAAAAAAAAAAAAAExVWE9SAABTSVAxAABL+aGMAAAAAAAAAJAAHyAnPwQA"
            ],
            "transactionsignatures": []
          }
        ]
      },
      "blockid": "0000000000000004e7378cc352603272319a593cbbf0bd2b62d613b4ef49691c",
      "difficulty": "1937380375959380416",
      "estimatedhashrate": "3126930912066593",
      "height": 1915,
      "maturitytimestamp": 1533557406,
      "target": [
        0,
        ...
      ],
      "totalcoins": "7252630537600000000000000000000000",
      "minerpayoutcount": 3828,
      "transactioncount": 14278,
      "siacoininputcount": 28346,
      "siacoinoutputcount": 254393,
      "filecontractcount": 25,
      "filecontractrevisioncount": 0,
      "storageproofcount": 0,
      "minerfeecount": 11644,
      "arbitrarydatacount": 1964,
      "transactionsignaturecount": 28346,
      "activecontractcost": "830441227784191212136514289",
      "activecontractcount": 25,
      "activecontractsize": "0",
      "totalcontractcost": "830441227784191212136514289",
      "totalcontractsize": "0",
      "totalrevisionvolume": "0"
    }
  ]
}
```

#### /explorer/pending [GET]

Returns informations about blocks found that are still waiting for confirmation.

###### Response
The JSON formatted block or a standard error response.

```
{
  "blocks": [
    {
      "transactions": [
        {
          "id": "4b1d3880d4ae584d1071721eb1f8e1a828a64bb4298f9ed2e29189b7b87f4ade",
          "height": 1916,
          "parent": "0000000000000008a44bc37f770822739b7c284d40037e30026b750a2c0d605b",
          "rawtransaction": {
            "siacoininputs": [
              {
                "parentid": "e390699dad738bf56e02b140f557ae3820fe2b335b895fd844b136c74424b8d9",
                "unlockconditions": {
                  "timelock": 0,
                  "publickeys": [
                    {
                      "algorithm": "ed25519",
                      "key": "NRCGh0eq44KAP80pd++mzpqi0gY7nOCQVlxOHrUnR1I="
                    }
                  ],
                  "signaturesrequired": 1
                }
              }
            ],
            "siacoinoutputs": [
              {
                "value": "1580738106770000000000000000",
                "unlockhash": "31b5b4addba02ed0f863b2eef2acfe8885a6a6bf08b78732f17a78e8df830007fcf3c34e9a08"
              },
              ...
            ],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [
              "85200000000000000000000"
            ],
            "arbitrarydata": [],
            "transactionsignatures": [
              {
                "parentid": "e390699dad738bf56e02b140f557ae3820fe2b335b895fd844b136c74424b8d9",
                "publickeyindex": 0,
                "timelock": 0,
                "coveredfields": {
                  "wholetransaction": true,
                  "siacoininputs": [],
                  "siacoinoutputs": [],
                  "filecontracts": [],
                  "filecontractrevisions": [],
                  "storageproofs": [],
                  "minerfees": [],
                  "arbitrarydata": [],
                  "transactionsignatures": []
                },
                "signature": "o1E7h9/tusvR2sN+uPPO2yg+bgda//4Y7UYDnSab8cBjTFVyBB+RkAV8CXak/3mf6A+jYiP0W/bGxE83ZbMwAA=="
              }
            ]
          },
          "siacoininputoutputs": [
            {
              "value": "194457234994100000000000000000",
              "unlockhash": "2075c075c8d423a0e20fe429c9eb4f17f03b92c2013686ab3e3f66b4671afe6e2e0a7a93a885"
            }
          ],
          "siacoinoutputids": [
            "acd99ed171ea8ed5d7e0ea503480b28ab0e4658fcf7da45e3fbbfb9c923e3272",
            ...
          ],
          "filecontractids": null,
          "filecontractvalidproofoutputids": null,
          "filecontractmissedproofoutputids": null,
          "filecontractrevisionvalidproofoutputids": null,
          "filecontractrevisionmissedproofoutputids": null,
          "storageproofoutputids": null,
          "storageproofoutputs": null
        }
      ],
      "rawblock": {
        "parentid": "0000000000000000000000000000000000000000000000000000000000000000",
        "nonce": [
          0,
          ...
        ],
        "timestamp": 0,
        "minerpayouts": null,
        "transactions": null
      },
      "blockid": "0000000000000004e7378cc352603272319a593cbbf0bd2b62d613b4ef49691c",
      "difficulty": "1937380375959380416",
      "estimatedhashrate": "3126930912066593",
      "height": 1915,
      "maturitytimestamp": 1533557406,
      "target": [
        0,
        ...
      ],
      "totalcoins": "7252630537600000000000000000000000",
      "minerpayoutcount": 3828,
      "transactioncount": 14278,
      "siacoininputcount": 28346,
      "siacoinoutputcount": 254393,
      "filecontractcount": 25,
      "filecontractrevisioncount": 0,
      "storageproofcount": 0,
      "minerfeecount": 11644,
      "arbitrarydatacount": 1964,
      "transactionsignaturecount": 28346,
      "activecontractcost": "830441227784191212136514289",
      "activecontractcount": 25,
      "activecontractsize": "0",
      "totalcontractcost": "830441227784191212136514289",
      "totalcontractsize": "0",
      "totalrevisionvolume": "0"
    }
  ]
}
```

#### /explorer/blocks/:height [GET]

Returns information about a specific block for a given height.

###### Query String Parameters
The following parameter can be specified.
```
// BlockHeight of the requested block.
height

```

###### Response
The JSON formatted block or a standard error response.

```
{
  "blocks": [
    {
      "transactions": [
        {
          "id": "e709f670ae9b1fad2cfd1768fee58c1e61de8b7b8c7a800f8b9e41b41129fa0e",
          "height": 0,
          "parent": "0d564b04ac438e76171af37efeebf6d99e0ee47ba6058ad9ac4381e30cc3949b",
          "rawtransaction": {
            "siacoininputs": [],
            "siacoinoutputs": [
              {
                "value": "3537376303200000000000000000000000",
                "unlockhash": "96cf6e01c2a4cce1bb0f7892fcac5e0000c487bc8e5ac388de7008a0de5cf11600072cfa9a9a"
              },
              ...
            ],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [],
            "arbitrarydata": [],
            "transactionsignatures": []
          },
          "siacoininputoutputs": null,
          "siacoinoutputids": [
            "16360ca68a4762230f8a8b71a50b4ac8005b8aa50013ac8be6b9cd669b1a21b5",
            ...
          ],
          "filecontractids": null,
          "filecontractvalidproofoutputids": null,
          "filecontractmissedproofoutputids": null,
          "filecontractrevisionvalidproofoutputids": null,
          "filecontractrevisionmissedproofoutputids": null,
          "storageproofoutputids": null,
          "storageproofoutputs": null
        }
      ],
      "rawblock": {
        "parentid": "0000000000000000000000000000000000000000000000000000000000000000",
        "nonce": [
          0,
          ...
        ],
        "timestamp": 1532510521,
        "minerpayouts": [],
        "transactions": [
          {
            "siacoininputs": [],
            "siacoinoutputs": [
              {
                "value": "3537376303200000000000000000000000",
                "unlockhash": "96cf6e01c2a4cce1bb0f7892fcac5e0000c487bc8e5ac388de7008a0de5cf11600072cfa9a9a"
              },
              ...
            ],
            "filecontracts": [],
            "filecontractrevisions": [],
            "storageproofs": [],
            "minerfees": [],
            "arbitrarydata": [],
            "transactionsignatures": []
          }
        ]
      },
      "blockid": "0d564b04ac438e76171af37efeebf6d99e0ee47ba6058ad9ac4381e30cc3949b",
      "difficulty": "1073741823",
      "estimatedhashrate": "0",
      "height": 0,
      "maturitytimestamp": 0,
      "target": [
        0,
        ...
      ],
      "totalcoins": "0",
      "minerpayoutcount": 0,
      "transactioncount": 1,
      "siacoininputcount": 0,
      "siacoinoutputcount": 0,
      "filecontractcount": 0,
      "filecontractrevisioncount": 0,
      "storageproofcount": 0,
      "minerfeecount": 0,
      "arbitrarydatacount": 0,
      "transactionsignaturecount": 0,
      "activecontractcost": "0",
      "activecontractcount": 0,
      "activecontractsize": "0",
      "totalcontractcost": "0",
      "totalcontractsize": "0",
      "totalrevisionvolume": "0"
    }
  ]
}
```

#### /explorer/hashes/:hash [GET]

Get information about a specific hash such as that of a transaction or
address.

###### Query String Parameters
The following parameter can be specified.
```
// Hash of the requested item.
hash
```

###### Response
The JSON formatted block or a standard error response.

```
{
  "hashtype": "siacoinoutputid",
  "block": null,
  "blocks": null,
  "transaction": null,
  "transactions": [
    {
      "id": "b7715808bc42534ecaf255e8ec8a66932361c43e2e03e9e5e6b999e23b6c13a9",
      "height": 1,
      "parent": "0000000345f6147e6bba01da7094812e643e0b15eee39b6c903a0b2b6197a5fd",
      "rawtransaction": {
        "siacoininputs": [
          {
            "parentid": "16360ca68a4762230f8a8b71a50b4ac8005b8aa50013ac8be6b9cd669b1a21b5",
            "unlockconditions": {
              "timelock": 0,
              "publickeys": [
                {
                  "algorithm": "ed25519",
                  "key": "IO/yeQAQTV+ttdIFdPCUxl2yFo+T6aTfsryv7AahyHU="
                }
              ],
              "signaturesrequired": 1
            }
          }
        ],
        "siacoinoutputs": [
          {
            "value": "123065439200000000000000000000000",
            "unlockhash": "650ab5cb18d7470832d9bfa7a0ae2f812c4fb4b7db3671391966cb573d7c986c0df2b26bb132"
          },
          ...
        ],
        "filecontracts": [],
        "filecontractrevisions": [],
        "storageproofs": [],
        "minerfees": [],
        "arbitrarydata": [],
        "transactionsignatures": [
          {
            "parentid": "16360ca68a4762230f8a8b71a50b4ac8005b8aa50013ac8be6b9cd669b1a21b5",
            "publickeyindex": 0,
            "timelock": 0,
            "coveredfields": {
              "wholetransaction": true,
              "siacoininputs": [],
              "siacoinoutputs": [],
              "filecontracts": [],
              "filecontractrevisions": [],
              "storageproofs": [],
              "minerfees": [],
              "arbitrarydata": [],
              "transactionsignatures": []
            },
            "signature": "3B0splikuD3el9yrKmJEwDixLT4oCEEhhApmDth/MH4rbDmiPixShHN89oVKW+hGJVmy/3I5PhcLFEj0GZYkAg=="
          }
        ]
      },
      "siacoininputoutputs": [
        {
          "value": "3537376303200000000000000000000000",
          "unlockhash": "96cf6e01c2a4cce1bb0f7892fcac5e0000c487bc8e5ac388de7008a0de5cf11600072cfa9a9a"
        }
      ],
      "siacoinoutputids": [
        "4b822e4aba6adbbae4e30793139bf1f937bbc28d761458f93c6328f3eb7a3615",
        ...
      ],
      "filecontractids": null,
      "filecontractvalidproofoutputids": null,
      "filecontractmissedproofoutputids": null,
      "filecontractrevisionvalidproofoutputids": null,
      "filecontractrevisionmissedproofoutputids": null,
      "storageproofoutputids": null,
      "storageproofoutputs": null
    }
  ]
}
```

#### /explorer/ws [GET]

Subscribe to the blocks websocket. The websocket notifies subscribers
when a new block is found in the blockchain.

#### /explorer/tx/ws [GET]

Subscribe to the transactions websocket. The websocket notifies subscribers
about new transactions in the blockchain.

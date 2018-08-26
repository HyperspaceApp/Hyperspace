# stratum protocol


## miner subscribe
```
{
  "id": 0,
  "method": "mining.subscribe",
  "params": [
    "cgminer/4.10.0" // client version
  ]
}
```

## server respond subscribe
```
{
  "error": null,
  "id": 0,
  "method": "mining.subscribe",
  "params": null,
  "result": [
    [
      [
        "mining.set_difficulty",
        "88aea8264161e21b" // difficulty
      ],
      [
        "mining.notify",
        "4d65822107fcfe1e" // session id
      ]
    ],
    "1ffefc07", // extra nonce 1
    4 // extra nonce 2 size
  ]
}
```

## miner request authorize
```
{
  "id": 1,
  "method": "mining.authorize",
  "params": [
    "a2fd0d1916d23262dcc03529ea8f94b95ef097df25f1dffb25aac81e9eb3d31269321e01c16c.obelisk", // address.workername
    "x" // password
  ]
}
```

## server respond authorize success
```
{
  "error": null,
  "id": 1,
  "method": "mining.authorize",
  "params": null,
  "result": true // sucess
}
```

## server set difficulty
```
{
  "id": 0,
  "method": "mining.set_difficulty",
  "params": [
    700 // difficulty
  ]
}
```

## server send notify
```
{
  "id": 0,
  "method": "mining.notify",
  "params": [
    "4d65822107fcfe20", // jobid
    "0000000000000008c499e08b5253cf743ba60432928c4b2d7df943e0ee2e8135", // block parent id
    "000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000062000000000000004e6f6e53696100000000000000000000002020202020536f6674776172653a20736961642d6d696e696e67706f6f6c2d6d6f64756c652076302e30330a506f6f6c206e616d653a2022757365617374506f6f6c22202020202000", // marshaled coinB1Txn
    "0000000000000000", // coinb2
    [
      "fe9851f1f0b9922b80eae7a9fc45dc464556f319b757383b45b25fadd9698eb2"
    ], // MerkleBranches
    "", // version
    "19092e48", // nbits
    "ee51825b00000000", // ntime
    true // cleanjobs
  ]
}
```

## miner submit nonce
```
{
  "params": [
    "a2fd0d1916d23262dcc03529ea8f94b95ef097df25f1dffb25aac81e9eb3d31269321e01c16c.obelisk", // address.workername
    "4d65822107fcfe20", // jobid
    "0f000000", // extra nonce 2
    "ee51825b00000000", // ntime
    "a3d52ac000000000" // nonce
  ],
  "id": 2,
  "method": "mining.submit"
}
```

## server reject
```
{
  "id": 2,
  "result": false,
  "error": [
    "22",
    "Submitted nonce did not reach pool diff target"
  ],
  "method": "mining.submit",
  "params": null
}
```

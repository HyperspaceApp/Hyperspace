Gateway API
===========

There may be functional API calls which are not documented. These are not
guaranteed to be supported beyond the current release, and should not be used
in production.

Overview
--------

The gateway maintains a peer to peer connection to the network and provides a
method for calling RPCs on connected peers. The gateway's API endpoints expose
methods for viewing the connected peers, manually connecting to peers, and
manually disconnecting from peers. The gateway may connect or disconnect from
peers on its own.

Index
-----

| Route                                                                              | HTTP verb | Examples                                                |
| ---------------------------------------------------------------------------------- | --------- | ------------------------------------------------------- |
| [/gateway](#gateway-get-example)                                                   | GET       | [Gateway info](#gateway-get-info)                       |
| [/gateway](#gateway-post-example)                                                  | POST      | [Gateway info](#gateway-post-info)                      |
| [/gateway/connect/___:netaddress___](#gatewayconnectnetaddress-post-example)       | POST      | [Connecting to a peer](#connecting-to-a-peer)           |
| [/gateway/disconnect/___:netaddress___](#gatewaydisconnectnetaddress-post-example) | POST      | [Disconnecting from a peer](#disconnecting-from-a-peer) |

#### /gateway [GET] [(example)](#gateway-get-info)

returns information about the gateway, including the list of connected peers.

###### JSON Response
```javascript
{
    // netaddress is the network address of the gateway as seen by the rest of
    // the network. The address consists of the external IP address and the
    // port Hyperspace is listening on. It represents a `modules.NetAddress`.
    "netaddress": String,

    // peers is an array of peers the gateway is connected to. It represents
    // an array of `modules.Peer`s.
    "peers":      []{
        // netaddress is the address of the peer. It represents a
        // `modules.NetAddress`.
        "netaddress": String,

        // version is the version number of the peer.
        "version":    String,

        // inbound is true when the peer initiated the connection. This field
        // is exposed as outbound peers are generally trusted more than inbound
        // peers, as inbound peers are easily manipulated by an adversary.
        "inbound":    Boolean,

        // local is true if the peer's IP address belongs to a local address
        // range such as 192.168.x.x or 127.x.x.x
        "local":      Boolean
    }
}
```

#### /gateway [POST] [(example)](#gateway-get-info)

modify settings that control the gateway's behavior.

###### Query String Parameters
// Max download speed permitted, speed provide in bytes per second
maxdownloadspeed

// Max upload speed permitted, speed provide in bytes per second
maxuploadspeed

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /gateway/connect/{netaddress} [POST] [(example)](#connecting-to-a-peer)

connects the gateway to a peer. The peer is added to the node list if it is not
already present. The node list is the list of all nodes the gateway knows
about, but is not necessarily connected to.

###### Path Parameters
```
// netaddress is the address of the peer to connect to. It should be a
// reachable ip address and port number, of the form 'IP:port'. IPV6 addresses
// must be enclosed in square brackets.
//
// Example IPV4 address: 123.456.789.0:123
// Example IPV6 address: [123::456]:789
:netaddress
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

#### /gateway/disconnect/{netaddress} [POST] [(example)](#disconnecting-from-a-peer)

disconnects the gateway from a peer. The peer remains in the node list.
Disconnecting from a peer does not prevent the gateway from automatically
connecting to the peer in the future.

###### Path Parameters
```
// netaddress is the address of the peer to connect to. It should be a
// reachable ip address and port number, of the form 'IP:port'. IPV6 addresses
// must be enclosed in square brackets.
//
// Example IPV4 address: 123.456.789.0:123
// Example IPV6 address: [123::456]:789
:netaddress
```

###### Response
standard success or error response. See
[API.md#standard-responses](/doc/API.md#standard-responses).

Examples
--------

#### Gateway info

###### Request
```
/gateway
```

###### Expected Response Code
```
200 OK
```

###### Example JSON Response
```json
{
    "netaddress":"333.333.333.333:5581",
    "peers":[
        {
            "netaddress":"222.222.222.222:5581",
            "version":"1.0.0",
            "inbound":false
        },
        {
            "netaddress":"111.111.111.111:5581",
            "version":"0.6.0",
            "inbound":true
        }
    ]
}
```

#### Connecting to a peer

###### Request
```
/gateway/connect/123.456.789.0:123
```

###### Expected Response Code
```
204 No Content
```

#### Disconnecting from a peer

###### Request
```
/gateway/disconnect/123.456.789.0:123
```

###### Expected Response Code
```
204 No Content
```

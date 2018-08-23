package modules

import (
	"net"

	"github.com/HyperspaceApp/Hyperspace/build"
)

const (
	// GatewayDir is the name of the directory used to store the gateway's
	// persistent data.
	GatewayDir = "gateway"
	// ShareNodesCmd requests that a node share a list of the other network
	// nodes it knows
	ShareNodesCmd = "ShareNodes"
	// DiscoverIPCmd requests that a node tell us what our external IP is
	DiscoverIPCmd = "DiscoverIP"
	// SendBlocksCmd requests that a node send us a list of consensus blocks
	SendBlocksCmd = "SendBlocks"
	// SendBlockCmd requests that a node send us a specific consensus block
	SendBlockCmd = "SendBlk"
	// RelayHeaderCmd sends a block header to a peer with the expectation
	// that the peer will pass on the header to other nodes
	RelayHeaderCmd = "RelayHeader"
	// RelayTransactionSetCmd sends a transaction set to a peer.
	RelayTransactionSetCmd = "RelayTransactionSet"
)

var (
	// BootstrapPeers is a list of peers that can be used to find other peers -
	// when a client first connects to the network, the only options for
	// finding peers are either manual entry of peers or to use a hardcoded
	// bootstrap point. While the bootstrap point could be a central service,
	// it can also be a list of peers that are known to be stable. We have
	// chosen to hardcode known-stable peers.
	BootstrapPeers = build.Select(build.Var{
		Standard: []NetAddress{
			"45.33.42.181:5581",
			"139.162.118.62:5581",
			"172.104.155.40:5581",
			"172.104.188.185:5581",
			"47.100.43.104:5581",
			"47.75.72.189:5581",
			"47.90.212.141:5581",
			"69.195.141.178:5581",
			"45.76.30.176:5581",
			"204.48.22.106:5581",
			"178.128.65.120:5581",
			"52.39.226.32:5581",
			"68.175.125.235:5581",
		},
		Dev: []NetAddress{
			"45.33.42.181:5581",
			"139.162.118.62:5581",
			"172.104.155.40:5581",
			"172.104.188.185:5581",
			"47.100.43.104:5581",
			"47.75.72.189:5581",
			"47.90.212.141:5581",
		},
		Testing: []NetAddress(nil),
	}).([]NetAddress)
)

type (
	// Peer contains all the info necessary to Broadcast to a peer.
	Peer struct {
		Inbound    bool       `json:"inbound"`
		Local      bool       `json:"local"`
		NetAddress NetAddress `json:"netaddress"`
		Version    string     `json:"version"`
	}

	// A PeerConn is the connection type used when communicating with peers during
	// an RPC. It is identical to a net.Conn with the additional RPCAddr method.
	// This method acts as an identifier for peers and is the address that the
	// peer can be dialed on. It is also the address that should be used when
	// calling an RPC on the peer.
	PeerConn interface {
		net.Conn
		RPCAddr() NetAddress
	}

	// RPCFunc is the type signature of functions that handle RPCs. It is used for
	// both the caller and the callee. RPCFuncs may perform locking. RPCFuncs may
	// close the connection early, and it is recommended that they do so to avoid
	// keeping the connection open after all necessary I/O has been performed.
	RPCFunc func(PeerConn) error

	// A Gateway facilitates the interactions between the local node and remote
	// nodes (peers). It relays incoming blocks and transactions to local modules,
	// and broadcasts outgoing blocks and transactions to peers. In a broad sense,
	// it is responsible for ensuring that the local consensus set is consistent
	// with the "network" consensus set.
	Gateway interface {
		// Connect establishes a persistent connection to a peer.
		Connect(NetAddress) error

		// Disconnect terminates a connection to a peer.
		Disconnect(NetAddress) error

		// DiscoverAddress discovers and returns the current public IP address
		// of the gateway. Contrary to Address, DiscoverAddress is blocking and
		// might take multiple minutes to return. A channel to cancel the
		// discovery can be supplied optionally.
		DiscoverAddress(cancel <-chan struct{}) (NetAddress, error)

		// Address returns the Gateway's address.
		Address() NetAddress

		// Peers returns the addresses that the Gateway is currently connected to.
		Peers() []Peer

		// RegisterRPC registers a function to handle incoming connections that
		// supply the given RPC ID.
		RegisterRPC(string, RPCFunc)

		// UnregisterRPC unregisters an RPC and removes all references to the RPCFunc
		// supplied in the corresponding RegisterRPC call. References to RPCFuncs
		// registered with RegisterConnectCall are not removed and should be removed
		// with UnregisterConnectCall. If the RPC does not exist no action is taken.
		UnregisterRPC(string)

		// RegisterConnectCall registers an RPC name and function to be called
		// upon connecting to a peer.
		RegisterConnectCall(string, RPCFunc)

		// UnregisterConnectCall unregisters an RPC and removes all references to the
		// RPCFunc supplied in the corresponding RegisterConnectCall call. References
		// to RPCFuncs registered with RegisterRPC are not removed and should be
		// removed with UnregisterRPC. If the RPC does not exist no action is taken.
		UnregisterConnectCall(string)

		// RPC calls an RPC on the given address. RPC cannot be called on an
		// address that the Gateway is not connected to.
		RPC(NetAddress, string, RPCFunc) error

		// Broadcast transmits obj, prefaced by the RPC name, to all of the
		// given peers in parallel.
		Broadcast(name string, obj interface{}, peers []Peer)

		// Online returns true if the gateway is connected to remote hosts
		Online() bool

		// Close safely stops the Gateway's listener process.
		Close() error
	}
)

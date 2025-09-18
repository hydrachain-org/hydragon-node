package network

import (
	"net"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/secrets"
	"github.com/multiformats/go-multiaddr"
)

// Config details the params for the base networking server
type Config struct {
	// The base directory for the network
	DataDir string
	// The address of the libp2p server
	Addr *net.TCPAddr
	// The NAT address
	NatAddr net.IP
	// The DNS address
	DNS multiaddr.Multiaddr
	// The maximum number of peers
	MaxPeers int64
	// The maximum number of inbound peers
	MaxInboundPeers int64
	// The maximum number of outbound peers
	MaxOutboundPeers int64
	// The chain configuration
	Chain *chain.Chain
	// The bootnodes to connect to
	Bootnodes []string
	// The secrets manager
	SecretsManager secrets.SecretsManager
	// Whether to disable peer discovery
	NoDiscover bool
}

// GetBootnodes returns the list of bootnodes
func (c *Config) GetBootnodes() []string {
	return c.Bootnodes
}

// DefaultConfig returns the default network configuration
func DefaultConfig() *Config {
	return &Config{
		// The discovery service is turned on by default
		NoDiscover: false,
		// Addresses are bound to localhost by default
		Addr: &net.TCPAddr{
			IP:   net.ParseIP("127.0.0.1"),
			Port: DefaultLibp2pPort,
		},
		// The default ratio for outbound / max peer connections is 0.20
		MaxPeers: 40,
		// The default ratio for outbound / inbound connections is 0.25
		MaxInboundPeers:  32,
		MaxOutboundPeers: 8,
	}
}

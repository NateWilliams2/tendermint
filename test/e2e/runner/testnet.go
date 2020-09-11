package main

import (
	"errors"
	"fmt"
	"net"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

// Testnet represents a single testnet
type Testnet struct {
	Name  string
	IP    *net.IPNet
	Nodes []*Node
}

// Node represents a Tendermint node in a testnet
type Node struct {
	Name      string
	Key       crypto.PrivKey
	IP        net.IP
	LocalPort uint32
}

// NewTestnet creates a testnet from a manifest.
func NewTestnet(manifest Manifest) (*Testnet, error) {
	_, ipNet, err := net.ParseCIDR(manifest.IP)
	if err != nil {
		return nil, fmt.Errorf("invalid network IP %q: %w", manifest.IP, err)
	}
	testnet := &Testnet{
		Name:  manifest.Name,
		IP:    ipNet,
		Nodes: []*Node{},
	}

	for name, nodeManifest := range manifest.Nodes {
		node, err := NewNode(name, nodeManifest)
		if err != nil {
			return nil, err
		}
		testnet.Nodes = append(testnet.Nodes, node)
	}

	if err := testnet.Validate(); err != nil {
		return nil, err
	}
	return testnet, nil
}

// NewNode creates a new testnet node from a node manifest.
func NewNode(name string, nodeManifest ManifestNode) (*Node, error) {
	ip := net.ParseIP(nodeManifest.IP)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP %q for node %q", nodeManifest.IP, name)
	}
	return &Node{
		Name:      name,
		Key:       ed25519.GenPrivKey(),
		IP:        ip,
		LocalPort: nodeManifest.LocalPort,
	}, nil
}

// Validate validates a testnet.
func (t Testnet) Validate() error {
	if t.Name == "" {
		return errors.New("network has no name")
	}
	if t.IP == nil {
		return errors.New("network has no IP")
	}
	if len(t.Nodes) == 0 {
		return errors.New("network has no nodes")
	}
	for _, node := range t.Nodes {
		if err := node.Validate(t); err != nil {
			return fmt.Errorf("invalid node %q: %w", node.Name, err)
		}
	}

	return nil
}

// Validate validates a node.
func (n Node) Validate(testnet Testnet) error {
	if n.Name == "" {
		return errors.New("node has no name")
	}
	if n.IP == nil {
		return errors.New("node has no IP address")
	}
	if !testnet.IP.Contains(n.IP) {
		return fmt.Errorf("node IP %v is not in testnet network %v", n.IP, testnet.IP)
	}
	if n.LocalPort > 0 {
		if n.LocalPort <= 1024 {
			return fmt.Errorf("local port %v must be >1024", n.LocalPort)
		}
		for _, peer := range testnet.Nodes {
			if peer.Name != n.Name && peer.LocalPort == n.LocalPort {
				return fmt.Errorf("peer %q also has local port %v", peer.Name, n.LocalPort)
			}
		}
	}
	return nil
}

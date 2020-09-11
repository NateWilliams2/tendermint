// nolint: gosec
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

// Testnet represents a single testnet
type Testnet struct {
	Name  string
	IP    *net.IPNet
	Nodes []*Node
}

// Node represents a Tendermint node in a testnet
type Node struct {
	Name string
	Key  crypto.PrivKey
	IP   net.IP
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
		Name: name,
		Key:  ed25519.GenPrivKey(),
		IP:   ip,
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
	return nil
}

// Setup sets up the testnet files in the given directory.
func (t Testnet) Setup(dir string, binaryPath string) error {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	compose, err := t.MakeDockerCompose()
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "docker-compose.yml"), compose, 0644); err != nil {
		return err
	}

	genesis, err := t.MakeGenesis()
	if err != nil {
		return err
	}
	for _, node := range t.Nodes {
		nodeDir := filepath.Join(dir, node.Name)
		cfg, err := node.MakeConfig(t)
		if err != nil {
			return err
		}
		pv := privval.NewFilePV(node.Key,
			filepath.Join(nodeDir, "config", "priv_validator_key.json"),
			filepath.Join(nodeDir, "data", "priv_validator_state.json"),
		)

		if err := os.MkdirAll(nodeDir, 0755); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(nodeDir, "tendermint")); err != nil {
			return err
		}
		if err := os.Link(binaryPath, filepath.Join(nodeDir, "tendermint")); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(nodeDir, "config"), 0755); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(nodeDir, "data"), 0755); err != nil {
			return err
		}
		if err := genesis.SaveAs(filepath.Join(nodeDir, "config", "genesis.json")); err != nil {
			return err
		}
		config.WriteConfigFile(filepath.Join(nodeDir, "config", "config.toml"), cfg) // panics
		if err := node.MakeNodeKey().SaveAs(filepath.Join(nodeDir, "config", "node_key.json")); err != nil {
			return err
		}
		pv.Save() // panics
	}

	return nil
}

// MakeDockerCompose generates a Docker Compose config for the testnet.
func (t Testnet) MakeDockerCompose() ([]byte, error) {
	tmpl, err := template.New("docker-compose").Parse(`version: '3'

networks:
  {{ .Name }}:
    driver: bridge
    ipam:
      driver: default
      config:
      - subnet: {{ .IP }}

services:
{{- range .Nodes }}
  {{ .Name }}:
    container_name: {{ .Name }}
    image: tendermint/e2e-node
    ports:
    - 26656-26657
    volumes:
    - ./{{ .Name }}:/tendermint
    networks:
      {{ $.Name }}:
        ipv4_address: {{ .IP }}

{{end}}`)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, t)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MakeGenesis generates a genesis document.
func (t Testnet) MakeGenesis() (types.GenesisDoc, error) {
	genesis := types.GenesisDoc{
		GenesisTime:     time.Now(),
		ChainID:         t.Name,
		ConsensusParams: types.DefaultConsensusParams(),
	}
	for _, node := range t.Nodes {
		genesis.Validators = append(genesis.Validators, types.GenesisValidator{
			Name:    node.Name,
			Address: node.Key.PubKey().Address(),
			PubKey:  node.Key.PubKey(),
			Power:   100,
		})
	}
	err := genesis.ValidateAndComplete()
	return genesis, err
}

// MakeConfig generates a Tendermint config for a node.
func (n Node) MakeConfig(testnet Testnet) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = n.Name
	cfg.ProxyApp = "kvstore"

	for _, peer := range testnet.Nodes {
		if peer.Name == n.Name {
			continue
		}
		if cfg.P2P.PersistentPeers != "" {
			cfg.P2P.PersistentPeers += ","
		}
		cfg.P2P.PersistentPeers += fmt.Sprintf("%x@%v:%v", peer.Key.PubKey().Address().Bytes(), peer.IP, 26656)
	}
	return cfg, nil
}

// MakeNodeKey generates a node key.
func (n Node) MakeNodeKey() *p2p.NodeKey {
	return &p2p.NodeKey{PrivKey: n.Key}
}

// nolint: gosec
package main

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

// Setup sets up testnet configuration in a directory.
func Setup(testnet *Testnet, dir string, binaryPath string) error {
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	compose, err := MakeDockerCompose(testnet)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(dir, "docker-compose.yml"), compose, 0644); err != nil {
		return err
	}

	genesis, err := MakeGenesis(testnet)
	if err != nil {
		return err
	}
	for _, node := range testnet.Nodes {
		nodeDir := filepath.Join(dir, node.Name)
		cfg, err := MakeConfig(testnet, node)
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
		if err := CopyBinary(binaryPath, filepath.Join(nodeDir, "tendermint")); err != nil {
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
		if err := MakeNodeKey(node).SaveAs(filepath.Join(nodeDir, "config", "node_key.json")); err != nil {
			return err
		}
		pv.Save() // panics
	}

	return nil
}

// CopyBinary copies the Tendermint binary, making sure it is an ELF binary.
func CopyBinary(binary string, target string) error {
	in, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer in.Close()

	binaryELF, err := elf.NewFile(in)
	if err, ok := err.(*elf.FormatError); ok {
		return fmt.Errorf("binary %q is not an ELF binary: %w", binary, err)
	} else if err != nil {
		return err
	}
	defer binaryELF.Close()
	switch binaryELF.OSABI {
	case elf.ELFOSABI_NONE, elf.ELFOSABI_LINUX:
	default:
		return fmt.Errorf("binary %q must be Linux ABI-compatible, got %v", binary, binaryELF.OSABI)
	}
	if binaryELF.Class != elf.ELFCLASS64 {
		return fmt.Errorf("binary %q must be 64-bit, got %v", binary, binaryELF.Class)
	}

	_, err = in.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	out, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}

// MakeDockerCompose generates a Docker Compose config for a testnet.
func MakeDockerCompose(testnet *Testnet) ([]byte, error) {
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
    - 26656
    - {{ if .LocalPort }}{{ .LocalPort }}:{{ end }}26657
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
	err = tmpl.Execute(&buf, testnet)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MakeGenesis generates a genesis document.
func MakeGenesis(testnet *Testnet) (types.GenesisDoc, error) {
	genesis := types.GenesisDoc{
		GenesisTime:     time.Now(),
		ChainID:         testnet.Name,
		ConsensusParams: types.DefaultConsensusParams(),
	}
	for _, node := range testnet.Nodes {
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
func MakeConfig(testnet *Testnet, node *Node) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = node.Name
	cfg.ProxyApp = "kvstore"
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"

	for _, peer := range testnet.Nodes {
		if peer.Name == node.Name {
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
func MakeNodeKey(node *Node) *p2p.NodeKey {
	return &p2p.NodeKey{PrivKey: node.Key}
}

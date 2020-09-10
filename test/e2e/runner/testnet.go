package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"text/template"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
)

// Testnet represents a single testnet
type Testnet struct {
	Name    string
	Network *net.IPNet
	Nodes   []*Node
}

// Node represents a Tendermint node in a testnet
type Node struct {
	Name string
	Key  crypto.PrivKey
	IP   net.IP
}

// NewTestnet creates a testnet from a manifest.
func NewTestnet(manifest Manifest) (*Testnet, error) {
	ip, ipNet, err := net.ParseCIDR("10.200.0.0/24")
	if err != nil {
		return nil, err
	}
	ip = incrIP(ip) // This now points to the gateway address
	testnet := &Testnet{
		Name:    manifest.Name,
		Network: ipNet,
		Nodes:   []*Node{},
	}

	for name, manifestNode := range manifest.Nodes {
		node := &Node{
			Name: name,
			IP:   ip,
			Key:  ed25519.GenPrivKey(),
		}
		switch manifestNode.Topology {
		case "host", "":
			ip = incrIP(ip)
			node.IP = ip
		default:
			return nil, fmt.Errorf("unknown topology %q", manifestNode.Topology)
		}
		testnet.Nodes = append(testnet.Nodes, node)
	}

	return testnet, nil
}

// WriteConfig writes the testnet configuration files under the given directory.
func (t Testnet) WriteConfig(dir string) error {
	err := os.MkdirAll(dir, os.ModeDir)
	if err != nil {
		return err
	}
	compose, err := t.GenerateDockerCompose()
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(dir, "docker-compose.yml"), compose, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

// GenerateDockerCompose generates a Docker Compose config for the testnet.
func (t Testnet) GenerateDockerCompose() ([]byte, error) {
	tmpl, err := template.New("docker-compose").Parse(`version: 3

networks:
  {{ .Name }}:
    driver: bridge
    ipam:
      driver: default
      config:
      - subnet: {{ .Network }}

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

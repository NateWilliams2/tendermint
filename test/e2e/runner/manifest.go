package main

import (
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
)

// Manifest represents a testnet manifest, specified as TOML.
type Manifest struct {
	Name  string
	Nodes map[string]ManifestNode `toml:"node"`
}

// ManifestNode represents a testnet manifest node.
type ManifestNode struct {
	Topology  string
	Validator bool
}

// ParseManifest parses a testnet manifest from TOML.
func ParseManifest(r io.Reader) (Manifest, error) {
	manifest := Manifest{}
	_, err := toml.DecodeReader(r, &manifest)
	return manifest, err
}

// LoadManifest loads a testnet manifest from a file.
func LoadManifest(file string) (Manifest, error) {
	r, err := os.Open(file)
	if err != nil {
		return Manifest{}, fmt.Errorf("failed to open testnet manifest %q: %w", file, err)
	}
	return ParseManifest(r)
}

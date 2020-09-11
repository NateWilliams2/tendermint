package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/libs/log"
)

var logger = log.NewTMLogger(log.NewSyncWriter(os.Stdout))

func main() {
	NewCLI().Run()
}

// CLI is the Cobra-based command-line interface.
type CLI struct {
	root    *cobra.Command
	testnet *Testnet
	dir     string
	binary  string
}

// NewCLI sets up the CLI.
func NewCLI() *CLI {
	cli := &CLI{}
	cli.root = &cobra.Command{
		Use:           "runner",
		Short:         "End-to-end test runner",
		SilenceUsage:  true,
		SilenceErrors: true, // we'll output them ourselves in Run()
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			file, err := cmd.Flags().GetString("file")
			if err != nil {
				return err
			}
			dir, err := cmd.Flags().GetString("dir")
			if err != nil {
				return err
			}
			if dir == "" {
				dir = strings.TrimSuffix(file, filepath.Ext(file))
			}
			binary, err := cmd.Flags().GetString("binary")
			if err != nil {
				return err
			}

			manifest, err := LoadManifest(file)
			if err != nil {
				return err
			}
			testnet, err := NewTestnet(manifest)
			if err != nil {
				return err
			}

			cli.testnet = testnet
			cli.dir = dir
			cli.binary = binary
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cli.Setup(); err != nil {
				return err
			}
			if err := cli.Cleanup(); err != nil {
				return err
			}
			return nil
		},
	}

	cli.root.PersistentFlags().StringP("file", "f", "", "Testnet TOML manifest")
	_ = cli.root.MarkPersistentFlagRequired("file")
	cli.root.PersistentFlags().StringP("dir", "d", "",
		"Directory to use for testnet data (defaults to manifest dir)")
	cli.root.PersistentFlags().StringP("binary", "b", "../../build/tendermint",
		"Tendermint binary to copy into containers")

	cli.root.AddCommand(&cobra.Command{
		Use:   "setup",
		Short: "Generates the testnet directory and configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Setup()
		},
	})

	cli.root.AddCommand(&cobra.Command{
		Use:   "cleanup",
		Short: "Removes the testnet directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Cleanup()
		},
	})

	return cli
}

// Run runs the CLI.
func (cli *CLI) Run() {
	if err := cli.root.Execute(); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// Setup generates the testnet configuration.
func (cli *CLI) Setup() error {
	logger.Info(fmt.Sprintf("Generating testnet files in %q", cli.dir))
	err := Setup(cli.testnet, cli.dir, cli.binary)
	if err != nil {
		return err
	}
	return nil
}

// Cleanup removes the testnet directory.
func (cli *CLI) Cleanup() error {
	if cli.dir == "" {
		return errors.New("no directory set")
	}
	logger.Info(fmt.Sprintf("Removing testnet directory %q", cli.dir))
	err := os.RemoveAll(cli.dir)
	if err != nil {
		return err
	}
	return nil
}

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
			if err := cli.Cleanup(); err != nil {
				return err
			}
			if err := cli.Setup(); err != nil {
				return err
			}
			if err := cli.Start(); err != nil {
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
		Use:   "start",
		Short: "Starts the Docker testnet, waiting for nodes to become available",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Start()
		},
	})

	cli.root.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stops the Docker testnet",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Stop()
		},
	})

	cli.root.AddCommand(&cobra.Command{
		Use:   "logs",
		Short: "Shows the testnet logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.Logs()
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

// runDocker runs a Docker Compose command.
func (cli *CLI) runDocker(args ...string) error {
	args = append([]string{"-f", filepath.Join(cli.dir, "docker-compose.yml")}, args...)
	cmd := exec.Command("docker-compose", args...)
	out, err := cmd.CombinedOutput()
	switch err := err.(type) {
	case nil:
		return nil
	case *exec.ExitError:
		return fmt.Errorf("failed to run docker-compose %q:\n%v", args, string(out))
	default:
		return err
	}
}

// runDocker runs a Docker Compose command and displays its output.
func (cli *CLI) runDockerOutput(args ...string) error {
	args = append([]string{"-f", filepath.Join(cli.dir, "docker-compose.yml")}, args...)
	cmd := exec.Command("docker-compose", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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

// Start starts the testnet. It waits for all nodes to become available.
func (cli *CLI) Start() error {
	for _, node := range cli.testnet.Nodes {
		logger.Info(fmt.Sprintf("Starting node %v...", node.Name))
		if err := cli.runDocker("up", "-d", node.Name); err != nil {
			return err
		}
	}
	return nil
}

// Logs outputs testnet logs.
func (cli *CLI) Logs() error {
	return cli.runDockerOutput("logs", "--follow")
}

// Stop stops the testnet and removes the containers.
func (cli *CLI) Stop() error {
	logger.Info("Stopping testnet")
	return cli.runDocker("down")
}

// Cleanup removes the Docker Compose containers and testnet directory.
func (cli *CLI) Cleanup() error {
	if cli.dir == "" {
		return errors.New("no directory set")
	}
	_, err := os.Stat(cli.dir)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	logger.Info("Removing Docker containers")
	err = cli.runDocker("rm", "--stop", "--force")
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("Removing testnet directory %q", cli.dir))
	err = os.RemoveAll(cli.dir)
	if err != nil {
		return err
	}
	return nil
}

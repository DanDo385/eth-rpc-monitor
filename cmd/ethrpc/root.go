package main

import (
	"github.com/spf13/cobra"

	"github.com/dando385/eth-rpc-monitor/internal/config"
)

// loadedCfg is the parsed providers.yaml, populated by rootCmd's
// PersistentPreRunE and shared with every subcommand's RunE. This is the
// standard small-cobra-app pattern for sharing config loaded once.
var loadedCfg *config.Config

// rootCmd is the top-level command. It owns the persistent --config flag and
// loads .env + providers.yaml before any subcommand runs.
var rootCmd = &cobra.Command{
	Use:   "ethrpc",
	Short: "Ethereum JSON-RPC monitor — latency, tail stats, and cross-provider agreement",
	Long: "eth-rpc-monitor measures Ethereum JSON-RPC over raw HTTP: block inspection, " +
		"tail-latency health checks, cross-provider snapshots, and a live monitoring dashboard.\n\n" +
		"Configure providers in config/providers.yaml (see config/providers.yaml.example).",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Load .env so API keys are available for ${VAR} URL expansion.
		config.LoadEnv()

		// Load the YAML config (expands ${VAR} and applies per-provider
		// timeout defaults).
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		loadedCfg = cfg
		return nil
	},
	SilenceUsage: true, // RunE errors are user-facing; don't dump usage on them
}

// cfgPath holds the persistent --config value, bound before Execute runs.
var cfgPath string

// Execute runs the root command and returns its error (if any) to main.
func Execute() error {
	return rootCmd.Execute()
}

// init binds the persistent --config flag and registers all subcommands.
func init() {
	rootCmd.PersistentFlags().StringVarP(
		&cfgPath, "config", "c", "config/providers.yaml",
		"Config file path",
	)

	rootCmd.AddCommand(blockCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(monitorCmd)
}

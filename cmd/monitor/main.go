package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

func main() {
	var cfgPath string

	rootCmd := &cobra.Command{
		Use:          "monitor",
		Short:        "Monitor Ethereum RPC infrastructure",
		SilenceUsage: true,
	}
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "config/providers.yaml", "Path to provider config")

	rootCmd.AddCommand(
		snapshotCmd(),
		watchCmd(),
		blocksCmd(),
		txsCmd(),
		callCmd(),
		statusCmd(),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rootCmd.SetContext(ctx)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

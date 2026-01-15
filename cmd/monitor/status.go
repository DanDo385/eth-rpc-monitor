package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/provider"
)

func statusCmd() *cobra.Command {
	var (
		samples int
		format  string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Quick health check and provider ranking",
		Long: `Perform a quick health check on all configured providers and rank them.

This is useful for:
- Seeing which providers are healthy
- Understanding which provider to use as primary
- Diagnosing provider issues

Example:
  monitor status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				cfgPath, _ = cmd.Root().PersistentFlags().GetString("config")
			}
			return runStatus(samples, format, cfgPath)
		},
	}

	cmd.Flags().IntVar(&samples, "samples", 5, "Number of samples per provider")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	return cmd
}

func runStatus(samples int, format string, cfgPath string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ranked, err := provider.QuickHealthCheck(ctx, cfg, samples)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if format == "json" {
		output.DisableColors()
		return output.RenderStatusJSON(ranked)
	}

	output.RenderStatusTerminal(ranked)
	return nil
}

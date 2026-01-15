package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/output"
)

func blocksCmd() *cobra.Command {
	var (
		rawOutput    bool
		providerName string
		format       string
	)

	cmd := &cobra.Command{
		Use:   "blocks [latest|number]",
		Short: "Fetch and display block details",
		Long: `Fetch block data from an Ethereum RPC provider.

Examples:
  monitor blocks latest
  monitor blocks 19000000
  monitor blocks 0x121eac0
  monitor blocks latest --raw
  monitor blocks latest --provider alchemy`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				cfgPath, _ = cmd.Root().PersistentFlags().GetString("config")
			}
			return runBlocks(cmd.Context(), args[0], rawOutput, providerName, format, cfgPath)
		},
	}

	cmd.Flags().BoolVar(&rawOutput, "raw", false, "Show raw JSON-RPC response")
	cmd.Flags().StringVar(&providerName, "provider", "", "Use specific provider (default: first available)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	return cmd
}

func runBlocks(ctx context.Context, blockArg string, rawOutput bool, providerName string, format string, cfgPath string) error {
	// Load config
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Parse block argument
	blockNum, err := parseBlockArg(blockArg)
	if err != nil {
		return err
	}

	// Get provider client
	client, usedProvider, err := getProviderClient(cfg, providerName)
	if err != nil {
		return err
	}

	// Fetch block
	// Use a timeout for the request
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Defaults.Timeout)
	defer cancel()

	start := time.Now()
	block, rawResponse, result := client.GetBlockByNumberWithRaw(reqCtx, blockNum, false)
	latency := time.Since(start)

	if !result.Success {
		return fmt.Errorf("failed to fetch block: %v", result.Error)
	}

	if block == nil {
		return fmt.Errorf("block %s not found", blockArg)
	}

	// Render output
	bd := &output.BlockDisplay{
		Block:          block,
		Provider:       usedProvider,
		Latency:       latency,
		RawResponse:    rawResponse,
		IsAutoSelected: providerName == "",
	}

	if format == "json" {
		output.DisableColors()
		return output.RenderBlockJSON(bd, rawOutput)
	}

	output.RenderBlockTerminal(bd, rawOutput)
	return nil
}

// parseBlockArg converts user input to hex block number for RPC
func parseBlockArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)

	if arg == "latest" || arg == "pending" || arg == "earliest" {
		return arg, nil
	}

	// Already hex
	if strings.HasPrefix(arg, "0x") {
		// Validate it's valid hex
		_, err := strconv.ParseUint(arg[2:], 16, 64)
		if err != nil {
			return "", fmt.Errorf("invalid hex block number: %s", arg)
		}
		return arg, nil
	}

	// Decimal - convert to hex
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid block number: %s (use decimal, hex with 0x prefix, or 'latest')", arg)
	}
	return fmt.Sprintf("0x%x", num), nil
}

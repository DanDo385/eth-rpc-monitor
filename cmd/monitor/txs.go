package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/output"
)

func txsCmd() *cobra.Command {
	var (
		rawOutput    bool
		providerName string
		limit        int
		format       string
	)

	cmd := &cobra.Command{
		Use:   "txs [latest|number]",
		Short: "List transactions in a block",
		Long: `List all transactions within a specified Ethereum block.

Examples:
  monitor txs latest
  monitor txs 19000000
  monitor txs 19000000 --limit 10
  monitor txs 19000000 --raw`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				cfgPath, _ = cmd.Root().PersistentFlags().GetString("config")
			}
			return runTxs(cmd.Context(), args[0], rawOutput, providerName, limit, format, cfgPath)
		},
	}

	cmd.Flags().BoolVar(&rawOutput, "raw", false, "Show raw JSON transaction array")
	cmd.Flags().StringVar(&providerName, "provider", "", "Use specific provider")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum transactions to display (0 for all)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	return cmd
}

func runTxs(ctx context.Context, blockArg string, rawOutput bool, providerName string, limit int, format string, cfgPath string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	blockNum, err := parseBlockArg(blockArg)
	if err != nil {
		return err
	}

	client, usedProvider, err := getProviderClient(cfg, providerName)
	if err != nil {
		return err
	}

	// Use a longer timeout for full transaction fetch
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Defaults.Timeout*2)
	defer cancel()

	start := time.Now()
	blockWithTxs, rawResponse, result := client.GetBlockWithTransactions(reqCtx, blockNum)
	latency := time.Since(start)

	if !result.Success {
		return fmt.Errorf("failed to fetch block: %v", result.Error)
	}

	if blockWithTxs == nil {
		return fmt.Errorf("block %s not found", blockArg)
	}

	td := &output.TxDisplay{
		BlockNumber:    blockWithTxs.Number,
		Transactions:   blockWithTxs.Transactions,
		TotalCount:     len(blockWithTxs.Transactions),
		Limit:          limit,
		Provider:       usedProvider,
		Latency:        latency,
		RawResponse:    rawResponse,
		IsAutoSelected: providerName == "",
	}

	if format == "json" {
		output.DisableColors()
		return output.RenderTxsJSON(td, rawOutput)
	}

	output.RenderTxsTerminal(td, rawOutput)
	return nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func main() {
	var (
		cfgPath  string
		provider string
		jsonOut  bool
	)

	rootCmd := &cobra.Command{
		Use:   "monitor [block]",
		Short: "RPC endpoint monitor and block inspector",
		Long: `Monitor Ethereum RPC endpoint performance and inspect blocks.

Examples:
  monitor                         # Latest block from fastest provider
  monitor latest                  # Same as above
  monitor 19000000                # Block by decimal number
  monitor 0x121eac0               # Block by hex
  monitor latest --provider alchemy
  monitor latest --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			block := "latest"
			if len(args) > 0 {
				block = args[0]
			}
			return runInspect(cfgPath, block, provider, jsonOut)
		},
	}

	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "config/providers.yaml", "Config file path")
	rootCmd.Flags().StringVar(&provider, "provider", "", "Use specific provider")
	rootCmd.Flags().BoolVar(&jsonOut, "json", false, "Output raw JSON")

	rootCmd.AddCommand(healthCmd())
	rootCmd.AddCommand(compareCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runInspect(cfgPath, blockArg, providerName string, jsonOut bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	// Select provider
	var client *rpc.Client
	if providerName != "" {
		for _, p := range cfg.Providers {
			if p.Name == providerName {
				client = rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
				break
			}
		}
		if client == nil {
			return fmt.Errorf("provider '%s' not found in config", providerName)
		}
	} else {
		client, err = selectFastestProvider(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Auto-selected: %s\n\n", client.Name())
	}

	// Fetch block
	block, latency, err := client.GetBlock(ctx, normalizeBlockArg(blockArg))
	if err != nil {
		return fmt.Errorf("failed to fetch block: %w", err)
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(block)
	}

	// Terminal output
	printBlock(block, client.Name(), latency)
	return nil
}

func printBlock(block *rpc.Block, provider string, latency time.Duration) {
	p := block.Parsed()

	fmt.Printf("\nBlock #%s\n", rpc.FormatNumber(p.Number))
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("  Hash:         %s\n", p.Hash)
	fmt.Printf("  Parent:       %s...\n", p.ParentHash[:14])
	fmt.Printf("  Timestamp:    %s\n", rpc.FormatTimestamp(p.Timestamp))
	fmt.Printf("  Gas:          %s / %s (%.1f%%)\n",
		rpc.FormatNumber(p.GasUsed),
		rpc.FormatNumber(p.GasLimit),
		float64(p.GasUsed)/float64(p.GasLimit)*100)
	fmt.Printf("  Base Fee:     %s\n", rpc.FormatGwei(p.BaseFeePerGas))
	fmt.Printf("  Transactions: %d\n", p.TxCount)
	fmt.Println()
	fmt.Printf("  Provider:     %s (%dms)\n", provider, latency.Milliseconds())
	fmt.Println()
}

func normalizeBlockArg(arg string) string {
	arg = strings.TrimSpace(strings.ToLower(arg))
	if arg == "latest" || arg == "pending" || arg == "earliest" || arg == "" {
		return "latest"
	}
	if strings.HasPrefix(arg, "0x") {
		return arg
	}
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		return arg // Let RPC handle the error
	}
	return fmt.Sprintf("0x%x", num)
}

// selectFastestProvider races all providers and returns the fastest responding one
func selectFastestProvider(ctx context.Context, cfg *config.Config) (*rpc.Client, error) {
	type result struct {
		client  *rpc.Client
		latency time.Duration
	}

	resultCh := make(chan result, len(cfg.Providers))
	var wg sync.WaitGroup

	for _, p := range cfg.Providers {
		wg.Add(1)
		go func(p config.Provider) {
			defer wg.Done()
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			_, latency, err := client.BlockNumber(ctx)
			if err == nil {
				resultCh <- result{client: client, latency: latency}
			}
		}(p)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Return first successful result
	for r := range resultCh {
		return r.client, nil
	}

	return nil, fmt.Errorf("no providers responded successfully")
}

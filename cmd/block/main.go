// cmd/block/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/env"
	"github.com/dmagro/eth-rpc-monitor/internal/reports"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func main() {
	env.Load()

	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		provider = flag.String("provider", "", "Use specific provider")
		jsonOut  = flag.Bool("json", false, "Output raw JSON")
	)

	// Parse flags - standard flag package stops at first non-flag arg
	flag.CommandLine.Parse(os.Args[1:])

	// Get block argument and check for flags in remaining args
	block := "latest"
	args := flag.Args()

	// Handle flags that might come after positional args
	for i, arg := range args {
		if arg == "--json" || arg == "-json" {
			*jsonOut = true
			// Remove this arg from the list
			args = append(args[:i], args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--provider=") {
			*provider = strings.TrimPrefix(arg, "--provider=")
			args = append(args[:i], args[i+1:]...)
			break
		}
		if arg == "--provider" || arg == "-provider" {
			if i+1 < len(args) {
				*provider = args[i+1]
				args = append(args[:i], args[i+2:]...)
			}
			break
		}
	}

	if len(args) > 0 {
		block = args[0]
	}

	if err := runInspect(*cfgPath, block, *provider, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
		filepath, err := reports.WriteJSON(block, "block")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
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
	fmt.Printf("  Parent:       %s\n", p.ParentHash)
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
// that is on the latest block
func selectFastestProvider(ctx context.Context, cfg *config.Config) (*rpc.Client, error) {
	type result struct {
		client   *rpc.Client
		blockNum uint64
		latency  time.Duration
	}

	var mu sync.Mutex
	results := make([]result, 0, len(cfg.Providers))

	g, gctx := errgroup.WithContext(ctx)
	for _, p := range cfg.Providers {
		p := p // capture loop variable
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			blockNum, latency, err := client.BlockNumber(gctx)
			if err != nil {
				return nil // Ignore errors, just skip this provider
			}
			mu.Lock()
			results = append(results, result{
				client:   client,
				blockNum: blockNum,
				latency:  latency,
			})
			mu.Unlock()
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("error selecting provider: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no providers responded successfully")
	}

	// Find the latest block number
	var latestBlock uint64
	for _, r := range results {
		if r.blockNum > latestBlock {
			latestBlock = r.blockNum
		}
	}

	// Find the fastest provider that has the latest block
	var fastest *rpc.Client
	var fastestLatency time.Duration
	found := false
	for _, r := range results {
		if r.blockNum == latestBlock {
			if !found || r.latency < fastestLatency {
				fastest = r.client
				fastestLatency = r.latency
				found = true
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("no provider is on the latest block (%d)", latestBlock)
	}

	return fastest, nil
}

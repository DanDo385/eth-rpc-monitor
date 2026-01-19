// cmd/monitor/compare.go
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func compareCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "compare [block]",
		Short: "Fetch the same block from all providers and compare results",
		Long: `Detects stale data or chain forks by comparing block hashes across providers.

Examples:
  monitor compare
  monitor compare latest
  monitor compare 19000000`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			block := "latest"
			if len(args) > 0 {
				block = args[0]
			}
			cfgPath, _ := cmd.Flags().GetString("config")
			return runCompare(cfgPath, block)
		},
	}
}

type CompareResult struct {
	Provider string
	Hash     string
	Height   uint64
	Latency  time.Duration
	Error    error
}

func runCompare(cfgPath, blockArg string) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	blockNum := normalizeBlockArg(blockArg)
	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	results := make([]CompareResult, len(cfg.Providers))
	var wg sync.WaitGroup

	for i, p := range cfg.Providers {
		wg.Add(1)
		go func(idx int, p config.Provider) {
			defer wg.Done()
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			block, latency, err := client.GetBlock(ctx, blockNum)

			r := CompareResult{Provider: p.Name, Latency: latency, Error: err}
			if err == nil && block != nil {
				r.Hash = block.Hash
				r.Height, _ = rpc.ParseHexUint64(block.Number)
			}
			results[idx] = r
		}(i, p)
	}

	wg.Wait()

	// Print results
	fmt.Printf("%-14s %10s %12s   %s\n", "Provider", "Latency", "Block Height", "Block Hash")
	fmt.Println(strings.Repeat("─", 90))

	hashGroups := make(map[string][]CompareResult) // hash -> results
	heightGroups := make(map[uint64][]CompareResult) // height -> results

	for _, r := range results {
		if r.Error != nil {
			fmt.Printf("%-14s %10s %12s   ERROR: %v\n", r.Provider, "—", "—", r.Error)
		} else {
			fmt.Printf("%-14s %8dms %12d   %s\n", r.Provider, r.Latency.Milliseconds(), r.Height, r.Hash)
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r)
			heightGroups[r.Height] = append(heightGroups[r.Height], r)
		}
	}

	// Consensus checks
	fmt.Println()
	successCount := 0
	for _, r := range results {
		if r.Error == nil {
			successCount++
		}
	}

	if successCount == 0 {
		fmt.Println("✗ No providers responded successfully")
	} else {
		// Check for height mismatches
		if len(heightGroups) > 1 {
			fmt.Println("⚠ BLOCK HEIGHT MISMATCH DETECTED:")
			for height, results := range heightGroups {
				providers := make([]string, len(results))
				for i, r := range results {
					providers[i] = r.Provider
				}
				fmt.Printf("  Height %d  →  %v\n", height, providers)
			}
			fmt.Println("\nThis may indicate lagging providers or propagation delays.")
			fmt.Println()
		}

		// Check for hash mismatches (only if heights match)
		if len(hashGroups) == 1 {
			fmt.Println("✓ All providers agree on block hash")
		} else if len(hashGroups) > 1 {
			fmt.Println("⚠ BLOCK HASH MISMATCH DETECTED:")
			for hash, results := range hashGroups {
				providers := make([]string, len(results))
				for i, r := range results {
					providers[i] = r.Provider
				}
				fmt.Printf("  %s...  →  %v\n", hash[:18], providers)
			}
			fmt.Println("\nThis may indicate stale caches, chain reorganization, or incorrect data.")
		}
	}
	fmt.Println()

	return nil
}

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
	fmt.Printf("%-14s %10s   %s\n", "Provider", "Latency", "Block Hash")
	fmt.Println(strings.Repeat("─", 80))

	hashGroups := make(map[string][]string) // hash -> providers

	for _, r := range results {
		if r.Error != nil {
			fmt.Printf("%-14s %10s   ERROR: %v\n", r.Provider, "—", r.Error)
		} else {
			fmt.Printf("%-14s %8dms   %s\n", r.Provider, r.Latency.Milliseconds(), r.Hash)
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r.Provider)
		}
	}

	// Consensus check
	fmt.Println()
	if len(hashGroups) == 0 {
		fmt.Println("✗ No providers responded successfully")
	} else if len(hashGroups) == 1 {
		fmt.Println("✓ All providers agree on block hash")
	} else {
		fmt.Println("⚠ HASH MISMATCH DETECTED:")
		for hash, providers := range hashGroups {
			fmt.Printf("  %s...  →  %v\n", hash[:18], providers)
		}
		fmt.Println("\nThis may indicate stale cache or chain reorganization.")
	}
	fmt.Println()

	return nil
}

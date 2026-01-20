// Package main implements the "snapshot" command for comparing block data across providers.
// This command fetches the same block from all configured providers simultaneously and
// detects mismatches in block hashes and heights, which can indicate sync issues,
// stale caches, or chain reorganizations.
//
// Usage:
//
//	snapshot [block_number] [flags]
//	snapshot latest
//
// The command is useful for detecting provider inconsistencies and ensuring data integrity.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func runSnapshot(cfg *config.Config, blockArg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	blockNum := rpc.NormalizeBlockArg(blockArg)
	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	results := commands.ExecuteAll(ctx, cfg, nil, func(ctx context.Context, client *rpc.Client, p config.Provider) commands.CompareResult {
		_, _, _ = client.BlockNumber(ctx)

		block, latency, err := client.GetBlock(ctx, blockNum)

		r := commands.CompareResult{Provider: p.Name, Latency: latency, Error: err}
		if err == nil && block != nil {
			r.Hash = block.Hash
			r.Height, _ = rpc.ParseHexUint64(block.Number)
		}
		return r
	})

	hashGroups := make(map[string][]commands.CompareResult)
	heightGroups := make(map[uint64][]commands.CompareResult)
	for _, r := range results {
		if r.Error == nil {
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r)
			heightGroups[r.Height] = append(heightGroups[r.Height], r)
		}
	}

	successCount := 0
	for _, r := range results {
		if r.Error == nil {
			successCount++
		}
	}

	hasHeightMismatch := len(heightGroups) > 1
	hasHashMismatch := len(hashGroups) > 1

	formatter := commands.NewCompareFormatter(results, successCount, heightGroups, hashGroups, hasHeightMismatch, hasHashMismatch)
	if err := formatter.Format(os.Stdout); err != nil {
		return fmt.Errorf("failed to display results: %w", err)
	}

	return nil
}

func main() {
	// Load environment variables from .env file (if present)
	config.LoadEnv()

	// Define command-line flags
	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
	)

	flag.Parse()

	// Extract block argument (defaults to "latest")
	block := "latest"
	args := flag.Args()
	if len(args) > 0 {
		block = args[0]
	}

	// Execute comparison
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := runSnapshot(cfg, block); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

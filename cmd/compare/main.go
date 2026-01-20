// cmd/compare/main.go
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

type CompareResult struct {
	Provider string
	Hash     string
	Height   uint64
	Latency  time.Duration
	Error    error
}

// CompareReport is the JSON-serializable version of compare results
type CompareReport struct {
	BlockArg          string              `json:"block_arg"`
	Timestamp         time.Time           `json:"timestamp"`
	Results           []CompareResultJSON `json:"results"`
	HeightGroups      map[uint64][]string `json:"height_groups,omitempty"`
	HashGroups        map[string][]string `json:"hash_groups,omitempty"`
	SuccessCount      int                 `json:"success_count"`
	TotalCount        int                 `json:"total_count"`
	HasHeightMismatch bool                `json:"has_height_mismatch"`
	HasHashMismatch   bool                `json:"has_hash_mismatch"`
}

// CompareResultJSON is JSON-serializable version of CompareResult
type CompareResultJSON struct {
	Provider  string `json:"provider"`
	Hash      string `json:"hash,omitempty"`
	Height    uint64 `json:"height,omitempty"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

func main() {
	env.Load()

	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	block := "latest"
	args := flag.Args()
	if len(args) > 0 {
		block = args[0]
	}

	if err := runCompare(*cfgPath, block, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCompare(cfgPath, blockArg string, jsonOut bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	blockNum := normalizeBlockArg(blockArg)
	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	results := make([]CompareResult, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for i, p := range cfg.Providers {
		i, p := i, p // capture loop variables
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			block, latency, err := client.GetBlock(gctx, blockNum)

			r := CompareResult{Provider: p.Name, Latency: latency, Error: err}
			if err == nil && block != nil {
				r.Hash = block.Hash
				r.Height, _ = rpc.ParseHexUint64(block.Number)
			}

			mu.Lock()
			results[i] = r
			mu.Unlock()

			return nil // Don't propagate errors, we track them in the result
		})
	}

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		return fmt.Errorf("error fetching blocks: %w", err)
	}

	hashGroups := make(map[string][]CompareResult)   // hash -> results
	heightGroups := make(map[uint64][]CompareResult) // height -> results

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

	// Prepare JSON report if requested
	if jsonOut {
		report := CompareReport{
			BlockArg:          blockArg,
			Timestamp:         time.Now(),
			Results:           make([]CompareResultJSON, len(results)),
			HeightGroups:      make(map[uint64][]string),
			HashGroups:        make(map[string][]string),
			SuccessCount:      successCount,
			TotalCount:        len(results),
			HasHeightMismatch: hasHeightMismatch,
			HasHashMismatch:   hasHashMismatch,
		}

		for i, r := range results {
			report.Results[i] = CompareResultJSON{
				Provider:  r.Provider,
				Hash:      r.Hash,
				Height:    r.Height,
				LatencyMs: r.Latency.Milliseconds(),
			}
			if r.Error != nil {
				report.Results[i].Error = r.Error.Error()
			}
		}

		for height, results := range heightGroups {
			providers := make([]string, len(results))
			for i, r := range results {
				providers[i] = r.Provider
			}
			report.HeightGroups[height] = providers
		}

		for hash, results := range hashGroups {
			providers := make([]string, len(results))
			for i, r := range results {
				providers[i] = r.Provider
			}
			report.HashGroups[hash] = providers
		}

		filepath, err := reports.WriteJSON(report, "compare")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	// Print results
	fmt.Printf("%-14s %10s %12s   %s\n", "Provider", "Latency", "Block Height", "Block Hash")
	fmt.Println(strings.Repeat("─", 90))

	for _, r := range results {
		if r.Error != nil {
			fmt.Printf("%-14s %10s %12s   ERROR: %v\n", r.Provider, "—", "—", r.Error)
		} else {
			fmt.Printf("%-14s %8dms %12d   %s\n", r.Provider, r.Latency.Milliseconds(), r.Height, r.Hash)
		}
	}

	// Consensus checks
	fmt.Println()
	if successCount == 0 {
		fmt.Println("✗ No providers responded successfully")
	} else {
		// Check for height mismatches
		if hasHeightMismatch {
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
		} else if hasHashMismatch {
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

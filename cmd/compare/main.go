// Package main implements the "compare" command for comparing block data across providers.
// This command fetches the same block from all configured providers simultaneously and
// detects mismatches in block hashes and heights, which can indicate sync issues,
// stale caches, or chain reorganizations.
//
// Usage:
//
//	compare [block_number] [flags]
//	compare latest --json
//
// The command is useful for detecting provider inconsistencies and ensuring data integrity.
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

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
	"github.com/dando385/eth-rpc-monitor/internal/reports"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

type CompareResult struct {
	Provider string
	Hash     string
	Height   uint64
	Latency  time.Duration
	Error    error
}

// CompareReport is the JSON-serializable version of compare results for report generation.
// This structure includes grouping information to show which providers agree/disagree,
// making it easy to identify consensus issues in JSON output.
type CompareReport struct {
	BlockArg          string              `json:"block_arg"`               // Block identifier that was queried
	Timestamp         time.Time           `json:"timestamp"`               // When the comparison was performed
	Results           []CompareResultJSON `json:"results"`                 // Individual provider results
	HeightGroups      map[uint64][]string `json:"height_groups,omitempty"` // Group providers by block height
	HashGroups        map[string][]string `json:"hash_groups,omitempty"`   // Group providers by block hash
	SuccessCount      int                 `json:"success_count"`           // Number of successful responses
	TotalCount        int                 `json:"total_count"`             // Total number of providers
	HasHeightMismatch bool                `json:"has_height_mismatch"`     // True if providers report different heights
	HasHashMismatch   bool                `json:"has_hash_mismatch"`       // True if providers report different hashes
}

// CompareResultJSON is a JSON-serializable version of CompareResult.
// Errors are converted to strings, and time.Duration is converted to milliseconds.
type CompareResultJSON struct {
	Provider  string `json:"provider"`         // Provider name
	Hash      string `json:"hash,omitempty"`   // Block hash (omitted if error)
	Height    uint64 `json:"height,omitempty"` // Block height (omitted if error)
	LatencyMs int64  `json:"latency_ms"`       // Request latency in milliseconds
	Error     string `json:"error,omitempty"`  // Error message (omitted if successful)
}

// main is the entry point for the compare command.
// It parses command-line arguments, loads environment variables, and delegates to runCompare.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	// Extract block argument (defaults to "latest")
	block := "latest"
	args := flag.Args()
	if len(args) > 0 {
		block = args[0]
	}

	// Execute comparison
	if err := runCompare(*cfgPath, block, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runCompare is the core function that performs block comparison across all providers.
// It fetches the same block from all providers concurrently, groups results by hash/height,
// and detects mismatches. Results can be displayed as terminal output or JSON report.
//
// Parameters:
//   - cfgPath: Path to providers.yaml configuration file
//   - blockArg: Block identifier ("latest", hex number, or decimal number)
//   - jsonOut: If true, output JSON report instead of terminal display
//
// Returns:
//   - error: Configuration, network, or report generation error
func runCompare(cfgPath, blockArg string, jsonOut bool) error {
	// Load configuration
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Create context with timeout (2x default for concurrent requests)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	// Normalize block argument to RPC format (e.g., "24277510" -> "0x172721e")
	blockNum := normalizeBlockArg(blockArg)
	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	// Results array and mutex for thread-safe access
	results := make([]CompareResult, len(cfg.Providers))
	var mu sync.Mutex

	// Use errgroup for concurrent block fetching with context cancellation
	g, gctx := errgroup.WithContext(ctx)
	for i, p := range cfg.Providers {
		i, p := i, p // Capture loop variables for goroutine
		g.Go(func() error {
			// Create RPC client for this provider
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)

			// Warm-up request to establish connection (discard result)
			// This eliminates connection setup overhead (TCP handshake, TLS negotiation, DNS lookup)
			// from measurements, making latency metrics more representative of actual RPC performance
			_, _, _ = client.BlockNumber(gctx)

			// Fetch block from this provider
			block, latency, err := client.GetBlock(gctx, blockNum)

			// Build result structure
			r := CompareResult{Provider: p.Name, Latency: latency, Error: err}
			if err == nil && block != nil {
				// Extract hash and height from successful response
				r.Hash = block.Hash
				r.Height, _ = rpc.ParseHexUint64(block.Number)
			}

			// Thread-safely store result
			mu.Lock()
			results[i] = r
			mu.Unlock()

			return nil // Don't propagate errors, we track them in the result
		})
	}

	// Wait for all concurrent fetches to complete
	if err := g.Wait(); err != nil {
		return fmt.Errorf("error fetching blocks: %w", err)
	}

	// Group results by hash and height to detect mismatches
	// These maps help identify which providers agree/disagree on block data
	hashGroups := make(map[string][]CompareResult)   // Maps block hash -> list of providers with that hash
	heightGroups := make(map[uint64][]CompareResult) // Maps block height -> list of providers with that height

	// Build grouping maps from successful results only
	for _, r := range results {
		if r.Error == nil {
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r)
			heightGroups[r.Height] = append(heightGroups[r.Height], r)
		}
	}

	// Count successful responses
	successCount := 0
	for _, r := range results {
		if r.Error == nil {
			successCount++
		}
	}

	// Detect mismatches: multiple groups means providers disagree
	hasHeightMismatch := len(heightGroups) > 1 // Multiple heights = sync lag or propagation delay
	hasHashMismatch := len(hashGroups) > 1     // Multiple hashes = fork, stale cache, or incorrect data

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

// normalizeBlockArg converts various block identifier formats to RPC-compatible format.
// This is the same logic as in block/main.go - it handles decimal numbers, hex numbers,
// and special tags like "latest", "pending", "earliest".
//
// Parameters:
//   - arg: Block identifier in various formats
//
// Returns:
//   - string: RPC-compatible block identifier (hex string or special tag)
func normalizeBlockArg(arg string) string {
	// Normalize input: trim whitespace and convert to lowercase
	arg = strings.TrimSpace(strings.ToLower(arg))

	// Handle special block tags
	if arg == "latest" || arg == "pending" || arg == "earliest" || arg == "" {
		return "latest"
	}

	// If already hex-encoded, return as-is
	if strings.HasPrefix(arg, "0x") {
		return arg
	}

	// Try to parse as decimal number and convert to hex
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		// Not a valid decimal number - return as-is and let RPC handle the error
		return arg
	}

	// Convert decimal to hex with "0x" prefix
	return fmt.Sprintf("0x%x", num)
}

// Package main implements the "monitor" command for continuous real-time monitoring.
// This command periodically queries all providers for their current block height and
// displays a live-updating dashboard showing block heights, latencies, and lag.
// Useful for detecting sync issues, network problems, or provider outages in real-time.
//
// Usage:
//
//	monitor [flags]
//	monitor --interval 10s --json
//
// The command runs until interrupted (Ctrl+C) and can optionally save a JSON report on exit.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
	"github.com/dando385/eth-rpc-monitor/internal/reports"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// WatchResult holds the result of querying a single provider's block height.
// Used for each refresh cycle to track current state and detect lag.
type WatchResult struct {
	Provider    string        // Provider name (e.g., "alchemy", "infura")
	BlockHeight uint64        // Current block height (0 if error occurred)
	Latency     time.Duration // Request latency
	Error       error         // Error if block number fetch failed
}

// WatchReport is the JSON-serializable version of monitor results for report generation.
// Generated when the command exits (Ctrl+C) if --json flag is set.
type WatchReport struct {
	Timestamp    time.Time         `json:"timestamp"`     // When the report was generated
	Interval     string            `json:"interval"`      // Refresh interval used (e.g., "30s")
	Results      []WatchResultJSON `json:"results"`       // Final snapshot of all providers
	HighestBlock uint64            `json:"highest_block"` // Highest block height observed
}

// WatchResultJSON is a JSON-serializable version of WatchResult.
// Errors are converted to strings, time.Duration is converted to milliseconds.
type WatchResultJSON struct {
	Provider    string `json:"provider"`               // Provider name
	BlockHeight uint64 `json:"block_height,omitempty"` // Block height (omitted if error)
	LatencyMs   int64  `json:"latency_ms,omitempty"`   // Request latency in milliseconds (omitted if error)
	Error       string `json:"error,omitempty"`        // Error message (omitted if successful)
	Lag         int64  `json:"lag,omitempty"`          // Blocks behind highest (omitted if error or up-to-date)
}

// main is the entry point for the monitor command.
// It parses command-line arguments, loads environment variables, and delegates to runWatch.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		interval = flag.Duration("interval", 0, "Refresh interval (0 = use config default)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory on exit")
	)

	flag.Parse()

	// Execute continuous monitoring
	if err := runWatch(*cfgPath, *interval, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runWatch is the core function that performs continuous monitoring of all providers.
// It runs in a loop, periodically fetching block heights from all providers and
// displaying an updated dashboard. The loop continues until interrupted (Ctrl+C).
//
// Parameters:
//   - cfgPath: Path to providers.yaml configuration file
//   - intervalOverride: Refresh interval (0 = use config default)
//   - jsonOut: If true, write JSON report on exit
//
// Returns:
//   - error: Configuration or report generation error
func runWatch(cfgPath string, intervalOverride time.Duration, jsonOut bool) error {
	// Load configuration
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Determine refresh interval: use override if provided, otherwise use config default
	interval := cfg.Defaults.WatchInterval
	if intervalOverride > 0 {
		interval = intervalOverride
	}

	// Create cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown on Ctrl+C
	// This allows the program to clean up and optionally write a JSON report
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
		cancel() // Cancel context to trigger cleanup
	}()

	// Create ticker for periodic refresh
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Track if this is the first display (skip screen clear on first render)
	firstDisplay := true

	// displayResults is a closure that renders the current state of all providers.
	// It clears the screen (except on first display) and shows a formatted table.
	displayResults := func(results []WatchResult) {
		// Find highest block height to calculate lag
		highestBlock := findHighestBlock(results)

		// Clear screen and move cursor to top-left (ANSI escape codes)
		// Skip clearing on first display to avoid flicker
		if !firstDisplay {
			// ESC[2J clears entire screen, ESC[H moves cursor to home position
			fmt.Print("\033[2J\033[H")
		}
		firstDisplay = false

		// Display header
		fmt.Printf("Monitoring %d providers (interval: %s, Ctrl+C to exit)...\n\n", len(cfg.Providers), interval)
		fmt.Printf("%-14s %12s %10s %12s\n", "Provider", "Block Height", "Latency", "Lag")
		fmt.Println(strings.Repeat("─", 60))

		// Display each provider's status
		for _, r := range results {
			if r.Error != nil {
				// Error case: show error indicator
				fmt.Printf("%-14s %12s %10s %12s\n",
					r.Provider,
					"ERROR",
					"—",
					"—")
			} else {
				// Success case: show block height, latency, and lag
				lag := highestBlock - r.BlockHeight
				lagStr := "—"
				if lag > 0 {
					// Provider is behind the highest block
					lagStr = fmt.Sprintf("-%d", lag)
				}
				fmt.Printf("%-14s %12d %8dms %12s\n",
					r.Provider,
					r.BlockHeight,
					r.Latency.Milliseconds(),
					lagStr)
			}
		}
		fmt.Println()
	}

	// Initial fetch and display (before first tick)
	results := fetchAllProviders(ctx, cfg)
	displayResults(results)

	// Track last results for JSON report on exit
	var lastResults []WatchResult

	// Main monitoring loop
	for {
		select {
		case <-ctx.Done():
			// Context cancelled (Ctrl+C received): cleanup and exit
			// Clear screen and restore cursor
			fmt.Print("\033[2J\033[H")
			fmt.Println("Exiting...")

			// Write JSON report if requested and we have results
			if jsonOut && lastResults != nil {
				if err := writeWatchReport(lastResults, cfg, interval); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to write JSON report: %v\n", err)
				}
			}
			return nil

		case <-ticker.C:
			// Ticker fired: time for next refresh
			// Check if context is cancelled before fetching (avoid unnecessary work)
			if ctx.Err() != nil {
				continue
			}

			// Fetch current state from all providers
			results := fetchAllProviders(ctx, cfg)
			lastResults = results // Save for JSON report

			// Update display
			displayResults(results)
		}
	}
}

// fetchAllProviders concurrently queries all providers for their current block height.
// This function is called on each refresh cycle to get the latest state from all providers.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - cfg: Configuration containing provider list
//
// Returns:
//   - []WatchResult: Results for all providers (includes errors if requests failed)
func fetchAllProviders(ctx context.Context, cfg *config.Config) []WatchResult {
	// Results array and mutex for thread-safe access
	results := make([]WatchResult, len(cfg.Providers))
	var mu sync.Mutex

	// Use errgroup for concurrent queries with context cancellation
	g, gctx := errgroup.WithContext(ctx)
	for i, p := range cfg.Providers {
		i, p := i, p // Capture loop variables for goroutine
		g.Go(func() error {
			// Create client and query block number
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			height, latency, err := client.BlockNumber(gctx)

			// Build result structure
			result := WatchResult{
				Provider:    p.Name,
				BlockHeight: height,
				Latency:     latency,
				Error:       err,
			}

			// Thread-safely store result
			mu.Lock()
			results[i] = result
			mu.Unlock()

			return nil // Don't propagate errors, we track them in the result
		})
	}

	// Wait for all concurrent queries to complete
	// Errors are ignored as they're tracked in individual results
	_ = g.Wait()
	return results
}

// findHighestBlock finds the highest block height among successful results.
// This is used to calculate lag for each provider (how many blocks behind they are).
//
// Parameters:
//   - results: Array of watch results from all providers
//
// Returns:
//   - uint64: Highest block height observed (0 if all providers failed)
func findHighestBlock(results []WatchResult) uint64 {
	var highest uint64
	for _, r := range results {
		// Only consider successful results (errors have BlockHeight = 0)
		if r.Error == nil && r.BlockHeight > highest {
			highest = r.BlockHeight
		}
	}
	return highest
}

// writeWatchReport generates a JSON report from the final monitor results.
// This function is called when the monitor command exits (Ctrl+C) if --json flag is set.
//
// Parameters:
//   - results: Final snapshot of all provider states
//   - cfg: Configuration (used for provider count in report)
//   - interval: Refresh interval that was used
//
// Returns:
//   - error: File creation or JSON encoding error
func writeWatchReport(results []WatchResult, cfg *config.Config, interval time.Duration) error {
	// Find highest block to calculate lag for each provider
	highestBlock := findHighestBlock(results)

	// Build report structure
	report := WatchReport{
		Timestamp:    time.Now(),
		Interval:     interval.String(),
		Results:      make([]WatchResultJSON, len(results)),
		HighestBlock: highestBlock,
	}

	// Convert each result to JSON-serializable format
	for i, r := range results {
		resultJSON := WatchResultJSON{
			Provider: r.Provider,
		}

		if r.Error != nil {
			// Error case: include error message
			resultJSON.Error = r.Error.Error()
		} else {
			// Success case: include block height, latency, and lag
			resultJSON.BlockHeight = r.BlockHeight
			resultJSON.LatencyMs = r.Latency.Milliseconds()

			// Calculate lag (blocks behind highest)
			if highestBlock > r.BlockHeight {
				resultJSON.Lag = int64(highestBlock - r.BlockHeight)
			}
		}
		report.Results[i] = resultJSON
	}

	// Write JSON report to disk
	filepath, err := reports.WriteJSON(report, "monitor")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
	return nil
}

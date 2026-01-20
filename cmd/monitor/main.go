// cmd/monitor/main.go
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

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/env"
	"github.com/dmagro/eth-rpc-monitor/internal/reports"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

type WatchResult struct {
	Provider    string
	BlockHeight uint64
	Latency     time.Duration
	Error       error
}

// WatchReport is the JSON-serializable version of watch results
type WatchReport struct {
	Timestamp    time.Time         `json:"timestamp"`
	Interval     string            `json:"interval"`
	Results      []WatchResultJSON `json:"results"`
	HighestBlock uint64            `json:"highest_block"`
}

// WatchResultJSON is JSON-serializable version of WatchResult
type WatchResultJSON struct {
	Provider    string `json:"provider"`
	BlockHeight uint64 `json:"block_height,omitempty"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
	Error       string `json:"error,omitempty"`
	Lag         int64  `json:"lag,omitempty"`
}

func main() {
	env.Load()

	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		interval = flag.Duration("interval", 0, "Refresh interval (defaults to config)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory on exit")
	)

	flag.Parse()

	if err := runWatch(*cfgPath, *interval, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runWatch(cfgPath string, intervalOverride time.Duration, jsonOut bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Use config default unless explicitly overridden
	interval := cfg.Defaults.WatchInterval
	if intervalOverride > 0 {
		interval = intervalOverride
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
		cancel()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Track if this is the first display
	firstDisplay := true

	// Display function to avoid duplication
	displayResults := func(results []WatchResult) {
		highestBlock := findHighestBlock(results)

		// Clear screen and move cursor to top-left
		// Use both sequences for maximum compatibility
		if !firstDisplay {
			// Clear entire screen: ESC[2J, then move to home: ESC[H
			fmt.Print("\033[2J\033[H")
		}
		firstDisplay = false

		fmt.Printf("Monitoring %d providers (interval: %s, Ctrl+C to exit)...\n\n", len(cfg.Providers), interval)
		fmt.Printf("%-14s %12s %10s %12s\n", "Provider", "Block Height", "Latency", "Lag")
		fmt.Println(strings.Repeat("─", 60))

		for _, r := range results {
			if r.Error != nil {
				fmt.Printf("%-14s %12s %10s %12s\n",
					r.Provider,
					"ERROR",
					"—",
					"—")
			} else {
				lag := highestBlock - r.BlockHeight
				lagStr := "—"
				if lag > 0 {
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

	// Initial fetch and display
	results := fetchAllProviders(ctx, cfg)
	displayResults(results)

	var lastResults []WatchResult
	for {
		select {
		case <-ctx.Done():
			// Clear screen and restore cursor on exit
			fmt.Print("\033[2J\033[H")
			fmt.Println("Exiting...")
			if jsonOut && lastResults != nil {
				if err := writeWatchReport(lastResults, cfg, interval); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to write JSON report: %v\n", err)
				}
			}
			return nil
		case <-ticker.C:
			// Check if context is cancelled before fetching
			if ctx.Err() != nil {
				continue
			}
			results := fetchAllProviders(ctx, cfg)
			lastResults = results
			displayResults(results)
		}
	}
}

func fetchAllProviders(ctx context.Context, cfg *config.Config) []WatchResult {
	results := make([]WatchResult, len(cfg.Providers))
	var wg sync.WaitGroup

	for i, p := range cfg.Providers {
		wg.Add(1)
		go func(idx int, p config.Provider) {
			defer wg.Done()
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			height, latency, err := client.BlockNumber(ctx)

			results[idx] = WatchResult{
				Provider:    p.Name,
				BlockHeight: height,
				Latency:     latency,
				Error:       err,
			}
		}(i, p)
	}

	wg.Wait()
	return results
}

func findHighestBlock(results []WatchResult) uint64 {
	var highest uint64
	for _, r := range results {
		if r.Error == nil && r.BlockHeight > highest {
			highest = r.BlockHeight
		}
	}
	return highest
}

func writeWatchReport(results []WatchResult, cfg *config.Config, interval time.Duration) error {
	highestBlock := findHighestBlock(results)

	report := WatchReport{
		Timestamp:    time.Now(),
		Interval:     interval.String(),
		Results:      make([]WatchResultJSON, len(results)),
		HighestBlock: highestBlock,
	}

	for i, r := range results {
		resultJSON := WatchResultJSON{
			Provider: r.Provider,
		}
		if r.Error != nil {
			resultJSON.Error = r.Error.Error()
		} else {
			resultJSON.BlockHeight = r.BlockHeight
			resultJSON.LatencyMs = r.Latency.Milliseconds()
			if highestBlock > r.BlockHeight {
				resultJSON.Lag = int64(highestBlock - r.BlockHeight)
			}
		}
		report.Results[i] = resultJSON
	}

	filepath, err := reports.WriteJSON(report, "monitor")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
	return nil
}

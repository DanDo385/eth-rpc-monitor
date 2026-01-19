// cmd/monitor/watch.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func watchCmd() *cobra.Command {
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Continuously monitor all providers",
		Long: `Continuously monitor all providers, showing block height and latency.

Examples:
  monitor watch
  monitor watch --interval 10s`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			return runWatch(cfgPath, interval)
		},
	}

	cmd.Flags().DurationVar(&interval, "interval", 0, "Refresh interval (defaults to config)")
	return cmd
}

type WatchResult struct {
	Provider   string
	BlockHeight uint64
	Latency    time.Duration
	Error      error
}

func runWatch(cfgPath string, intervalOverride time.Duration) error {
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
		<-sigCh
		cancel()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Display function to avoid duplication
	displayResults := func(results []WatchResult) {
		highestBlock := findHighestBlock(results)
		fmt.Print("\033[2J\033[H") // ANSI clear screen and move cursor to top
		fmt.Printf("Watching %d providers (interval: %s, Ctrl+C to exit)...\n\n", len(cfg.Providers), interval)
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

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nExiting...")
			return nil
		case <-ticker.C:
			results := fetchAllProviders(ctx, cfg)
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

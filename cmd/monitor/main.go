// Package main implements the "monitor" command for continuous real-time monitoring.
// This command periodically queries all providers for their current block height and
// displays a live-updating dashboard showing block heights, latencies, and lag.
// Useful for detecting sync issues, network problems, or provider outages in real-time.
//
// Usage:
//
//	monitor [flags]
//	monitor --interval 10s
//
// The command runs until interrupted (Ctrl+C).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func fetchAllProviders(ctx context.Context, cfg *config.Config, pool *rpc.ClientPool) []commands.WatchResult {
	return commands.ExecuteAll(ctx, cfg, pool, func(ctx context.Context, client *rpc.Client, p config.Provider) commands.WatchResult {
		height, latency, err := client.BlockNumber(ctx)
		return commands.WatchResult{
			Provider:    p.Name,
			BlockHeight: height,
			Latency:     latency,
			Error:       err,
		}
	})
}

func runMonitor(cfg *config.Config, intervalOverride time.Duration) error {
	interval := cfg.Defaults.WatchInterval
	if intervalOverride > 0 {
		interval = intervalOverride
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
		cancel()
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pool := rpc.NewClientPool()

	firstDisplay := true
	displayResults := func(results []commands.WatchResult) {
		formatter := commands.NewMonitorFormatter(results, interval, len(cfg.Providers), firstDisplay)
		if err := formatter.Format(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error displaying results: %v\n", err)
		}
		firstDisplay = false
	}

	results := fetchAllProviders(ctx, cfg, pool)
	displayResults(results)

	for {
		select {
		case <-ctx.Done():
			fmt.Print("\033[2J\033[H")
			fmt.Println("Exiting...")
			return nil

		case <-ticker.C:
			if ctx.Err() != nil {
				continue
			}

			results := fetchAllProviders(ctx, cfg, pool)
			displayResults(results)
		}
	}
}

func main() {
	// Load environment variables from .env file (if present)
	config.LoadEnv()

	// Define command-line flags
	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		interval = flag.Duration("interval", 0, "Refresh interval (0 = use config default)")
	)

	flag.Parse()

	// Execute continuous monitoring
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := runMonitor(cfg, *interval); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func fetchAllProviders(ctx context.Context, cfg *config.Config) []format.WatchResult {
	results := make([]format.WatchResult, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)
			height, latency, err := client.BlockNumber(gctx)
			
			r := format.WatchResult{
				Provider:    p.Name,
				BlockHeight: height,
				Latency:     latency,
				Error:       err,
			}

			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}

	g.Wait()
	return results
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

	firstDisplay := true
	displayResults := func(results []format.WatchResult) {
		format.FormatMonitor(os.Stdout, results, interval, !firstDisplay)
		firstDisplay = false
	}

	results := fetchAllProviders(ctx, cfg)
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

			results := fetchAllProviders(ctx, cfg)
			displayResults(results)
		}
	}
}

func main() {
	config.LoadEnv()

	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		interval = flag.Duration("interval", 0, "Refresh interval (0 = use config default)")
	)

	flag.Parse()

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

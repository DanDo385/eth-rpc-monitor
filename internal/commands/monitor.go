package commands

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/display"
	"github.com/dando385/eth-rpc-monitor/internal/provider"
	"github.com/dando385/eth-rpc-monitor/internal/report"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func RunMonitor(cfg *config.Config, intervalOverride time.Duration, jsonOut bool) error {
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
	displayResults := func(results []display.WatchResult) {
		formatter := display.NewMonitorFormatter(results, interval, len(cfg.Providers), firstDisplay)
		if err := formatter.Format(os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error displaying results: %v\n", err)
		}
		firstDisplay = false
	}

	results := fetchAllProviders(ctx, cfg, pool)
	displayResults(results)

	var lastResults []display.WatchResult

	for {
		select {
		case <-ctx.Done():
			fmt.Print("\033[2J\033[H")
			fmt.Println("Exiting...")

			if jsonOut && lastResults != nil {
				if err := writeWatchReport(lastResults, cfg, interval); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to write JSON report: %v\n", err)
				}
			}
			return nil

		case <-ticker.C:
			if ctx.Err() != nil {
				continue
			}

			results := fetchAllProviders(ctx, cfg, pool)
			lastResults = results
			displayResults(results)
		}
	}
}

func fetchAllProviders(ctx context.Context, cfg *config.Config, pool *rpc.ClientPool) []display.WatchResult {
	return provider.ExecuteAll(ctx, cfg, pool, func(ctx context.Context, client *rpc.Client, p config.Provider) display.WatchResult {
		height, latency, err := client.BlockNumber(ctx)
		return display.WatchResult{
			Provider:    p.Name,
			BlockHeight: height,
			Latency:     latency,
			Error:       err,
		}
	})
}

func writeWatchReport(results []display.WatchResult, cfg *config.Config, interval time.Duration) error {
	highestBlock := display.FindHighestBlock(results)

	intervalStr := interval.String()
	highestBlockCopy := highestBlock
	reportData := report.Report{
		Timestamp:    time.Now(),
		Interval:     &intervalStr,
		Results:      make([]report.Entry, len(results)),
		HighestBlock: &highestBlockCopy,
	}

	for i, r := range results {
		entry := report.Entry{Provider: r.Provider}

		if r.Error != nil {
			errStr := r.Error.Error()
			entry.Error = &errStr
		} else {
			blockHeight := r.BlockHeight
			entry.BlockHeight = &blockHeight

			latency := report.MillisDuration(r.Latency)
			entry.LatencyMS = &latency

			if highestBlock > r.BlockHeight {
				lag := int64(highestBlock - r.BlockHeight)
				entry.Lag = &lag
			}
		}
		reportData.Results[i] = entry
	}

	filepath, err := report.WriteJSON(reportData, "monitor")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
	return nil
}

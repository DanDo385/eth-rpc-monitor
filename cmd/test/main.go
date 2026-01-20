// Package main implements the "test" command for provider health checks.
// This command tests all providers and compares tail latency performance.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func testProvider(client *rpc.Client, p config.Provider, samples int) commands.HealthResult {
	ctx := context.Background()

	var latencies []time.Duration
	var lastHeight uint64
	success := 0

	fmt.Fprintf(os.Stderr, "\n[%s] Testing with %d samples...\n", p.Name, samples)

	_, _, _ = client.BlockNumber(ctx)

	for i := 0; i < samples; i++ {
		height, latency, err := client.BlockNumber(ctx)
		if err == nil {
			success++
			latencies = append(latencies, latency)
			lastHeight = height
			fmt.Fprintf(os.Stderr, "  Sample %d/%d: %dms\n", i+1, samples, latency.Milliseconds())
		} else {
			fmt.Fprintf(os.Stderr, "  Sample %d/%d: ERROR - %v\n", i+1, samples, err)
		}

		if i < samples-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	tailLatency := commands.CalculateTailLatency(latencies)

	fmt.Fprintf(os.Stderr, "[%s] Calculated percentiles:\n", p.Name)
	fmt.Fprintf(os.Stderr, "  P50: %dms, P95: %dms, P99: %dms, Max: %dms\n",
		tailLatency.P50.Milliseconds(), tailLatency.P95.Milliseconds(), tailLatency.P99.Milliseconds(), tailLatency.Max.Milliseconds())

	return commands.HealthResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		P50Latency:  tailLatency.P50,
		P95Latency:  tailLatency.P95,
		P99Latency:  tailLatency.P99,
		MaxLatency:  tailLatency.Max,
		BlockHeight: lastHeight,
		Latencies:   latencies,
	}
}

func runTest(cfg *config.Config, samplesOverride int, jsonOut bool) error {
	samples := cfg.Defaults.HealthSamples
	if samplesOverride > 0 {
		samples = samplesOverride
	}

	fmt.Printf("\nTesting %d providers with %d samples each...\n\n", len(cfg.Providers), samples)

	ctx := context.Background()
	results := commands.ExecuteAll(ctx, cfg, nil, func(ctx context.Context, client *rpc.Client, p config.Provider) commands.HealthResult {
		return testProvider(client, p, samples)
	})

	if jsonOut {
		samplesCopy := samples
		reportData := commands.Report{
			Timestamp: time.Now(),
			Samples:   &samplesCopy,
			Results:   make([]commands.ReportEntry, len(results)),
		}

		for i, r := range results {
			latenciesMs := make([]int64, len(r.Latencies))
			for j, lat := range r.Latencies {
				latenciesMs[j] = lat.Milliseconds()
			}

			successCopy := r.Success
			totalCopy := r.Total
			blockHeightCopy := r.BlockHeight
			p50 := commands.MillisDuration(r.P50Latency)
			p95 := commands.MillisDuration(r.P95Latency)
			p99 := commands.MillisDuration(r.P99Latency)
			max := commands.MillisDuration(r.MaxLatency)

			reportData.Results[i] = commands.ReportEntry{
				Name:         r.Name,
				Type:         r.Type,
				Success:      &successCopy,
				Total:        &totalCopy,
				P50LatencyMS: &p50,
				P95LatencyMS: &p95,
				P99LatencyMS: &p99,
				MaxLatencyMS: &max,
				BlockHeight:  &blockHeightCopy,
				LatenciesMS:  &latenciesMs,
			}
		}

		filepath, err := commands.WriteJSON(reportData, "health")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	formatter := commands.NewHealthFormatter(results)
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
		samples = flag.Int("samples", 0, "Number of test samples per provider (0 = use config default)")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	// Execute health check
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := runTest(cfg, *samples, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// cmd/health/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
	"github.com/dando385/eth-rpc-monitor/internal/provider"
	"github.com/dando385/eth-rpc-monitor/internal/reports"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
	"github.com/dando385/eth-rpc-monitor/internal/stats"
)

type HealthResult struct {
	Name        string
	Type        string
	Success     int
	Total       int
	P50Latency  time.Duration
	P95Latency  time.Duration
	P99Latency  time.Duration
	MaxLatency  time.Duration
	BlockHeight uint64
	Latencies   []time.Duration // Raw latency samples for tracing
}

// HealthReport is the JSON-serializable version of health test results.
// Used when --json flag is set to generate timestamped reports in the reports directory.
type HealthReport struct {
	Timestamp time.Time          `json:"timestamp"` // When the health test was performed
	Samples   int                `json:"samples"`   // Number of samples per provider
	Results   []HealthResultJSON `json:"results"`   // Health results for each provider
}

// HealthResultJSON is a JSON-serializable version of HealthResult.
// All time.Duration values are converted to milliseconds (int64) for JSON compatibility.
type HealthResultJSON struct {
	Name         string  `json:"name"`           // Provider name
	Type         string  `json:"type"`           // Provider type
	Success      int     `json:"success"`        // Successful request count
	Total        int     `json:"total"`          // Total request count
	P50LatencyMs int64   `json:"p50_latency_ms"` // Median latency in milliseconds
	P95LatencyMs int64   `json:"p95_latency_ms"` // 95th percentile latency in milliseconds
	P99LatencyMs int64   `json:"p99_latency_ms"` // 99th percentile latency in milliseconds
	MaxLatencyMs int64   `json:"max_latency_ms"` // Maximum latency in milliseconds
	BlockHeight  uint64  `json:"block_height"`   // Final block height
	LatenciesMs  []int64 `json:"latencies_ms"`   // Raw latency samples in milliseconds
}

// main is the entry point for the health command.
// It parses command-line arguments, loads environment variables, and delegates to runHealth.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		samples = flag.Int("samples", 0, "Number of test samples per provider (0 = use config default)")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	// Execute health check
	if err := runHealth(*cfgPath, *samples, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runHealth is the core function that performs health checks on all providers.
// It runs concurrent tests against each provider, calculates latency percentiles,
// and either displays results in terminal or generates a JSON report.
//
// Parameters:
//   - cfgPath: Path to providers.yaml configuration file
//   - samplesOverride: Number of samples per provider (0 = use config default)
//   - jsonOut: If true, output JSON report instead of terminal display
//
// Returns:
//   - error: Configuration, network, or report generation error
func runHealth(cfgPath string, samplesOverride int, jsonOut bool) error {
	// Load configuration
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Determine number of samples: use override if provided, otherwise use config default
	samples := cfg.Defaults.HealthSamples
	if samplesOverride > 0 {
		samples = samplesOverride
	}

	fmt.Printf("\nTesting %d providers with %d samples each...\n\n", len(cfg.Providers), samples)

	exec := provider.ExecuteAll(context.Background(), cfg.Providers, func(_ context.Context, p config.Provider) (HealthResult, error) {
		return testProvider(p, cfg.Defaults.MaxRetries, samples), nil
	})

	results := make([]HealthResult, len(exec))
	for i, r := range exec {
		results[i] = r.Value
	}

	// Prepare JSON report if requested
	if jsonOut {
		report := HealthReport{
			Timestamp: time.Now(),
			Samples:   samples,
			Results:   make([]HealthResultJSON, len(results)),
		}

		for i, r := range results {
			latenciesMs := make([]int64, len(r.Latencies))
			for j, lat := range r.Latencies {
				latenciesMs[j] = lat.Milliseconds()
			}

			report.Results[i] = HealthResultJSON{
				Name:         r.Name,
				Type:         r.Type,
				Success:      r.Success,
				Total:        r.Total,
				P50LatencyMs: r.P50Latency.Milliseconds(),
				P95LatencyMs: r.P95Latency.Milliseconds(),
				P99LatencyMs: r.P99Latency.Milliseconds(),
				MaxLatencyMs: r.MaxLatency.Milliseconds(),
				BlockHeight:  r.BlockHeight,
				LatenciesMs:  latenciesMs,
			}
		}

		filepath, err := reports.WriteJSON(report, "health")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	// Print results table
	fmt.Printf("%-14s %-6s %8s %8s %8s %8s %8s %12s\n",
		"Provider", "Type", "Success", "P50", "P95", "P99", "Max", "Block")
	fmt.Println(strings.Repeat("─", 90))

	for _, r := range results {
		successPct := float64(r.Success) / float64(r.Total) * 100
		fmt.Printf("%-14s %-6s %7.0f%% %7dms %7dms %7dms %7dms %12d\n",
			r.Name,
			r.Type,
			successPct,
			r.P50Latency.Milliseconds(),
			r.P95Latency.Milliseconds(),
			r.P99Latency.Milliseconds(),
			r.MaxLatency.Milliseconds(),
			r.BlockHeight)
	}
	fmt.Println()

	// Check for block height mismatches (similar to compare command)
	heightGroups := make(map[uint64][]HealthResult)
	for _, r := range results {
		if r.Success > 0 { // Only include providers that had at least one successful sample
			heightGroups[r.BlockHeight] = append(heightGroups[r.BlockHeight], r)
		}
	}

	if len(heightGroups) > 1 {
		fmt.Println("⚠ BLOCK HEIGHT MISMATCH DETECTED:")
		for height, results := range heightGroups {
			providers := make([]string, len(results))
			for i, r := range results {
				providers[i] = r.Name
			}
			fmt.Printf("  Height %d  →  %v\n", height, providers)
		}
		fmt.Println("\nThis may indicate lagging providers or propagation delays.")
		fmt.Println()
	}

	return nil
}

// testProvider performs health testing on a single provider by making multiple
// BlockNumber requests and collecting latency samples. It calculates percentile
// statistics to assess provider performance characteristics.
//
// The function includes a warm-up request to eliminate connection setup overhead
// from measurements, ensuring latency metrics reflect actual RPC performance.
//
// Parameters:
//   - p: Provider configuration
//   - maxRetries: Maximum retry attempts for each request
//   - samples: Number of latency samples to collect
//
// Returns:
//   - HealthResult: Complete health test results including percentiles and success rate
func testProvider(p config.Provider, maxRetries, samples int) HealthResult {
	// Create RPC client for this provider
	client := rpc.NewClient(p.Name, p.URL, p.Timeout, maxRetries)
	ctx := context.Background()

	// Track latency samples and final state
	var latencies []time.Duration
	var lastHeight uint64
	success := 0

	// Log testing start (to stderr so it doesn't interfere with JSON output)
	fmt.Fprintf(os.Stderr, "\n[%s] Testing with %d samples...\n", p.Name, samples)

	// Warm-up request to establish connection (discard result)
	// This eliminates connection setup overhead (TCP handshake, TLS negotiation, DNS lookup)
	// from measurements, making latency metrics more representative of actual RPC performance
	_ = client.Warmup(ctx)

	// Collect latency samples
	for i := 0; i < samples; i++ {
		height, latency, err := client.BlockNumber(ctx)
		if err == nil {
			// Successful request: record latency and update height
			success++
			latencies = append(latencies, latency)
			lastHeight = height
			fmt.Fprintf(os.Stderr, "  Sample %d/%d: %dms\n", i+1, samples, latency.Milliseconds())
		} else {
			// Failed request: log error but continue testing
			fmt.Fprintf(os.Stderr, "  Sample %d/%d: ERROR - %v\n", i+1, samples, err)
		}

		// Small delay between samples to avoid hammering the endpoint
		// No delay after the last sample
		if i < samples-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Calculate percentile statistics from collected latencies
	tailLatency := stats.CalculateTailLatency(latencies)

	// Log calculated percentiles (to stderr for tracing)
	fmt.Fprintf(os.Stderr, "[%s] Calculated percentiles:\n", p.Name)
	fmt.Fprintf(os.Stderr, "  P50: %dms, P95: %dms, P99: %dms, Max: %dms\n",
		tailLatency.P50.Milliseconds(), tailLatency.P95.Milliseconds(), tailLatency.P99.Milliseconds(), tailLatency.Max.Milliseconds())

	return HealthResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		P50Latency:  tailLatency.P50, // Median latency
		P95Latency:  tailLatency.P95, // 95th percentile (captures outliers)
		P99Latency:  tailLatency.P99, // 99th percentile (worst-case scenarios)
		MaxLatency:  tailLatency.Max, // Absolute maximum
		BlockHeight: lastHeight,
		Latencies:   latencies, // Raw samples for detailed analysis
	}
}

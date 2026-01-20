// cmd/health/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/env"
	"github.com/dmagro/eth-rpc-monitor/internal/reports"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
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

	// Results array and mutex for thread-safe access
	results := make([]HealthResult, len(cfg.Providers))
	var mu sync.Mutex

	// Use errgroup for concurrent provider testing
	g, _ := errgroup.WithContext(context.Background())
	for i, p := range cfg.Providers {
		i, p := i, p // Capture loop variables for goroutine
		g.Go(func() error {
			// Test this provider with specified number of samples
			result := testProvider(p, cfg.Defaults.MaxRetries, samples)

			// Thread-safely store result
			mu.Lock()
			results[i] = result
			mu.Unlock()

			return nil // Don't propagate errors, we track them in the result
		})
	}

	// Wait for all concurrent tests to complete
	if err := g.Wait(); err != nil {
		return fmt.Errorf("error testing providers: %w", err)
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
	_, _, _ = client.BlockNumber(ctx)

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
	p50, p95, p99, max := calculateTailLatency(latencies)

	// Log calculated percentiles (to stderr for tracing)
	fmt.Fprintf(os.Stderr, "[%s] Calculated percentiles:\n", p.Name)
	fmt.Fprintf(os.Stderr, "  P50: %dms, P95: %dms, P99: %dms, Max: %dms\n",
		p50.Milliseconds(), p95.Milliseconds(), p99.Milliseconds(), max.Milliseconds())

	return HealthResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		P50Latency:  p50, // Median latency
		P95Latency:  p95, // 95th percentile (captures outliers)
		P99Latency:  p99, // 99th percentile (worst-case scenarios)
		MaxLatency:  max, // Absolute maximum
		BlockHeight: lastHeight,
		Latencies:   latencies, // Raw samples for detailed analysis
	}
}

// calculateTailLatency computes tail latency percentiles (P50, P95, P99, Max) from samples.
// Tail latency metrics are critical for understanding provider performance characteristics:
//   - P50 (median): Typical performance
//   - P95: Captures outliers (95% of requests faster than this)
//   - P99: Worst-case scenarios (99% of requests faster than this)
//   - Max: Absolute worst observed latency
//
// Parameters:
//   - latencies: Slice of latency measurements (may be empty)
//
// Returns:
//   - p50, p95, p99, max: Calculated percentiles (all zero if latencies is empty)
//
// Algorithm:
//  1. Sort latencies in ascending order
//  2. Use nearest-rank method to calculate percentiles
//  3. For small sample sizes, P95/P99 naturally equal Max (correct behavior)
func calculateTailLatency(latencies []time.Duration) (p50, p95, p99, max time.Duration) {
	// Handle empty samples (all requests failed)
	if len(latencies) == 0 {
		return 0, 0, 0, 0
	}

	// Sort latencies in ascending order for percentile calculation
	// Create a copy to avoid mutating the original slice
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Calculate percentiles from sorted samples using nearest-rank method
	n := len(sorted)
	p50 = percentile(sorted, n, 0.50) // Median
	p95 = percentile(sorted, n, 0.95) // 95th percentile
	p99 = percentile(sorted, n, 0.99) // 99th percentile
	max = sorted[n-1]                 // Maximum is the last element after sorting

	return p50, p95, p99, max
}

// percentile returns the value at the given percentile using the nearest-rank method.
// This method ensures that with small sample sizes, high percentiles (P95, P99) correctly
// equal the maximum value, which is the expected behavior.
//
// Parameters:
//   - sorted: Pre-sorted slice of latencies (ascending order)
//   - n: Length of sorted slice
//   - p: Percentile as decimal (e.g., 0.95 for 95th percentile)
//
// Returns:
//   - time.Duration: Value at the requested percentile
//
// Formula: index = ceil(n * p) - 1, clamped to valid range [0, n-1]
//
// Examples with 3 samples [a, b, c] (sorted):
//   - P50: ceil(3 * 0.50) - 1 = ceil(1.5) - 1 = 2 - 1 = 1 -> sorted[1] (middle value)
//   - P95: ceil(3 * 0.95) - 1 = ceil(2.85) - 1 = 3 - 1 = 2 -> sorted[2] (max value)
//   - P99: ceil(3 * 0.99) - 1 = ceil(2.97) - 1 = 3 - 1 = 2 -> sorted[2] (max value)
//
// This ensures P95/P99 = Max for small sample sizes, which is correct.
func percentile(sorted []time.Duration, n int, p float64) time.Duration {
	if n == 0 {
		return 0
	}

	// Nearest-rank method: ceil(n * p) - 1
	// This rounds up to ensure we capture the appropriate percentile
	index := int(math.Ceil(float64(n)*p)) - 1

	// Clamp index to valid range [0, n-1]
	if index >= n {
		index = n - 1
	}
	if index < 0 {
		index = 0
	}

	return sorted[index]
}

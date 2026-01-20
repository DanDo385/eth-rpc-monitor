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

// HealthReport is the JSON-serializable version of health results
type HealthReport struct {
	Timestamp time.Time          `json:"timestamp"`
	Samples   int                `json:"samples"`
	Results   []HealthResultJSON `json:"results"`
}

// HealthResultJSON is JSON-serializable version of HealthResult
type HealthResultJSON struct {
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	Success      int     `json:"success"`
	Total        int     `json:"total"`
	P50LatencyMs int64   `json:"p50_latency_ms"`
	P95LatencyMs int64   `json:"p95_latency_ms"`
	P99LatencyMs int64   `json:"p99_latency_ms"`
	MaxLatencyMs int64   `json:"max_latency_ms"`
	BlockHeight  uint64  `json:"block_height"`
	LatenciesMs  []int64 `json:"latencies_ms"` // Raw latency samples
}

func main() {
	env.Load()

	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		samples = flag.Int("samples", 0, "Number of test samples per provider (defaults to config)")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	if err := runHealth(*cfgPath, *samples, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runHealth(cfgPath string, samplesOverride int, jsonOut bool) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Use config default unless explicitly overridden
	samples := cfg.Defaults.HealthSamples
	if samplesOverride > 0 {
		samples = samplesOverride
	}

	fmt.Printf("\nTesting %d providers with %d samples each...\n\n", len(cfg.Providers), samples)

	results := make([]HealthResult, len(cfg.Providers))
	var wg sync.WaitGroup

	for i, p := range cfg.Providers {
		wg.Add(1)
		go func(idx int, p config.Provider) {
			defer wg.Done()
			results[idx] = testProvider(p, cfg.Defaults.MaxRetries, samples)
		}(i, p)
	}

	wg.Wait()

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

func testProvider(p config.Provider, maxRetries, samples int) HealthResult {
	client := rpc.NewClient(p.Name, p.URL, p.Timeout, maxRetries)
	ctx := context.Background()

	var latencies []time.Duration
	var lastHeight uint64
	success := 0

	fmt.Fprintf(os.Stderr, "\n[%s] Testing with %d samples...\n", p.Name, samples)

	// Warm-up request to establish connection (discard result)
	// This eliminates connection setup overhead (TCP handshake, TLS negotiation, DNS lookup)
	// from measurements, making latency metrics more representative of actual RPC performance
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
			time.Sleep(200 * time.Millisecond) // Don't hammer the endpoint
		}
	}

	p50, p95, p99, max := calculateTailLatency(latencies)

	fmt.Fprintf(os.Stderr, "[%s] Calculated percentiles:\n", p.Name)
	fmt.Fprintf(os.Stderr, "  P50: %dms, P95: %dms, P99: %dms, Max: %dms\n",
		p50.Milliseconds(), p95.Milliseconds(), p99.Milliseconds(), max.Milliseconds())

	return HealthResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		P50Latency:  p50,
		P95Latency:  p95,
		P99Latency:  p99,
		MaxLatency:  max,
		BlockHeight: lastHeight,
		Latencies:   latencies,
	}
}

// calculateTailLatency computes P50, P95, P99, and Max from sorted samples
// Empty samples return zero values
func calculateTailLatency(latencies []time.Duration) (p50, p95, p99, max time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0, 0
	}

	// Sort latencies for percentile calculation
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Calculate percentiles from sorted samples
	n := len(sorted)
	p50 = percentile(sorted, n, 0.50)
	p95 = percentile(sorted, n, 0.95)
	p99 = percentile(sorted, n, 0.99)
	max = sorted[n-1] // Maximum is the last element after sorting

	return p50, p95, p99, max
}

// percentile returns the value at the given percentile using the nearest-rank method
// For small sample sizes, high percentiles (P95, P99) will naturally equal Max
// Formula: index = ceil(n * p) - 1, clamped to valid range
func percentile(sorted []time.Duration, n int, p float64) time.Duration {
	if n == 0 {
		return 0
	}
	// Nearest-rank method: ceil(n * p) - 1
	// Examples with 3 samples:
	//   P50: ceil(3 * 0.50) - 1 = ceil(1.5) - 1 = 2 - 1 = 1 (middle)
	//   P95: ceil(3 * 0.95) - 1 = ceil(2.85) - 1 = 3 - 1 = 2 (max)
	//   P99: ceil(3 * 0.99) - 1 = ceil(2.97) - 1 = 3 - 1 = 2 (max)
	index := int(math.Ceil(float64(n)*p)) - 1
	if index >= n {
		index = n - 1
	}
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

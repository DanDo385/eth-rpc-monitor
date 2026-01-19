// cmd/monitor/health.go
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func healthCmd() *cobra.Command {
	var samples int

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check health of all configured RPC providers",
		Long: `Test each provider multiple times and report tail latency statistics.

Examples:
  monitor health
  monitor health --samples 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			return runHealth(cfgPath, samples)
		},
	}

	cmd.Flags().IntVar(&samples, "samples", 0, "Number of test samples per provider (defaults to config)")
	return cmd
}

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
}

func runHealth(cfgPath string, samplesOverride int) error {
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

	return nil
}

func testProvider(p config.Provider, maxRetries, samples int) HealthResult {
	client := rpc.NewClient(p.Name, p.URL, p.Timeout, maxRetries)
	ctx := context.Background()

	var latencies []time.Duration
	var lastHeight uint64
	success := 0

	for i := 0; i < samples; i++ {
		height, latency, err := client.BlockNumber(ctx)
		if err == nil {
			success++
			latencies = append(latencies, latency)
			lastHeight = height
		}
		if i < samples-1 {
			time.Sleep(200 * time.Millisecond) // Don't hammer the endpoint
		}
	}

	p50, p95, p99, max := calculateTailLatency(latencies)

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

// percentile returns the value at the given percentile in sorted slice
func percentile(sorted []time.Duration, n int, p float64) time.Duration {
	if n == 0 {
		return 0
	}
	index := int(float64(n-1) * p)
	if index >= n {
		index = n - 1
	}
	return sorted[index]
}

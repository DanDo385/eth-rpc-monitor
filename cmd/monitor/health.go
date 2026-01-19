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
		Long: `Test each provider multiple times and report latency statistics.

Examples:
  monitor health
  monitor health --samples 10`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			return runHealth(cfgPath, samples)
		},
	}

	cmd.Flags().IntVar(&samples, "samples", 5, "Number of test samples per provider")
	return cmd
}

type HealthResult struct {
	Name        string
	Type        string
	Success     int
	Total       int
	AvgLatency  time.Duration
	P95Latency  time.Duration
	BlockHeight uint64
}

func runHealth(cfgPath string, samples int) error {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
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
	fmt.Printf("%-14s %-6s %8s %10s %10s %12s\n",
		"Provider", "Type", "Success", "Avg", "P95", "Block")
	fmt.Println(strings.Repeat("─", 70))

	for _, r := range results {
		successPct := float64(r.Success) / float64(r.Total) * 100
		fmt.Printf("%-14s %-6s %7.0f%% %8dms %8dms %12d\n",
			r.Name,
			r.Type,
			successPct,
			r.AvgLatency.Milliseconds(),
			r.P95Latency.Milliseconds(),
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

	avg, p95 := calculateLatencyStats(latencies)

	return HealthResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		AvgLatency:  avg,
		P95Latency:  p95,
		BlockHeight: lastHeight,
	}
}

func calculateLatencyStats(latencies []time.Duration) (avg, p95 time.Duration) {
	if len(latencies) == 0 {
		return 0, 0
	}

	// Calculate average
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	avg = total / time.Duration(len(latencies))

	// Calculate P95
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	p95Index := int(float64(len(sorted)) * 0.95)
	if p95Index >= len(sorted) {
		p95Index = len(sorted) - 1
	}
	p95 = sorted[p95Index]

	return avg, p95
}

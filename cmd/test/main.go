package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func writeJSON(data interface{}, prefix string) (string, error) {
	os.MkdirAll("reports", 0755)
	filename := fmt.Sprintf("reports/%s-%s.json", prefix, time.Now().Format("20060102-150405"))
	file, _ := os.Create(filename)
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	enc.Encode(data)
	return filename, nil
}

type TestReport struct {
	Timestamp time.Time     `json:"timestamp"`
	Samples   int           `json:"samples"`
	Results   []TestReportEntry `json:"results"`
}

type TestReportEntry struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Success      int      `json:"success"`
	Total        int      `json:"total"`
	P50LatencyMS int64    `json:"p50_latency_ms"`
	P95LatencyMS int64    `json:"p95_latency_ms"`
	P99LatencyMS int64    `json:"p99_latency_ms"`
	MaxLatencyMS int64    `json:"max_latency_ms"`
	BlockHeight  uint64   `json:"block_height"`
	LatenciesMS  []int64  `json:"latencies_ms"`
}

func testProvider(client *rpc.Client, p config.Provider, samples int) format.TestResult {
	ctx := context.Background()

	var latencies []time.Duration
	var lastHeight uint64
	success := 0

	fmt.Fprintf(os.Stderr, "\n[%s] Testing with %d samples...\n", p.Name, samples)

	client.BlockNumber(ctx)

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

	tailLatency := format.CalculateTailLatency(latencies)

	fmt.Fprintf(os.Stderr, "[%s] Calculated percentiles:\n", p.Name)
	fmt.Fprintf(os.Stderr, "  P50: %dms, P95: %dms, P99: %dms, Max: %dms\n",
		tailLatency.P50.Milliseconds(), tailLatency.P95.Milliseconds(), tailLatency.P99.Milliseconds(), tailLatency.Max.Milliseconds())

	return format.TestResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		Latencies:   latencies,
		BlockHeight: lastHeight,
	}
}

func runTest(cfg *config.Config, samplesOverride int, jsonOut bool) error {
	samples := cfg.Defaults.HealthSamples
	if samplesOverride > 0 {
		samples = samplesOverride
	}

	fmt.Printf("\nTesting %d providers with %d samples each...\n\n", len(cfg.Providers), samples)

	results := make([]format.TestResult, len(cfg.Providers))
	var mu sync.Mutex

	g, _ := errgroup.WithContext(context.Background())

	for i, p := range cfg.Providers {
		i, p := i, p
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)
			result := testProvider(client, p, samples)
			
			mu.Lock()
			results[i] = result
			mu.Unlock()
			return nil
		})
	}

	g.Wait()

	g.Wait()

	if jsonOut {
		reportData := TestReport{
			Timestamp: time.Now(),
			Samples:   samples,
			Results:   make([]TestReportEntry, len(results)),
		}

		for i, r := range results {
			latenciesMs := make([]int64, len(r.Latencies))
			for j, lat := range r.Latencies {
				latenciesMs[j] = lat.Milliseconds()
			}

			tail := format.CalculateTailLatency(r.Latencies)
			reportData.Results[i] = TestReportEntry{
				Name:         r.Name,
				Type:         r.Type,
				Success:      r.Success,
				Total:        r.Total,
				P50LatencyMS: tail.P50.Milliseconds(),
				P95LatencyMS: tail.P95.Milliseconds(),
				P99LatencyMS: tail.P99.Milliseconds(),
				MaxLatencyMS: tail.Max.Milliseconds(),
				BlockHeight:  r.BlockHeight,
				LatenciesMS:  latenciesMs,
			}
		}

		filepath, err := writeJSON(reportData, "health")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	format.FormatTest(os.Stdout, results)
	return nil
}

func main() {
	config.LoadEnv()

	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		samples = flag.Int("samples", 0, "Number of test samples per provider (0 = use config default)")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

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

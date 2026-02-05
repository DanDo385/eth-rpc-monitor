// =============================================================================
// FILE: cmd/test/main.go
// ROLE: Health Check Command — Provider Reliability and Tail Latency Testing
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This is the entry point for the `test` command, the most data-intensive
// tool in the suite. While `block` fetches one data point and `snapshot`
// fetches one block from each provider, `test` runs N samples per provider
// to build a STATISTICAL picture of each provider's performance.
//
// Usage examples:
//   test                  ← 30 samples per provider (default from config)
//   test --samples 10     ← 10 samples per provider (quick check)
//   test --json           ← Export detailed report with raw latency data
//
// EXECUTION FLOW
// ==============
//
//   1. main()
//      │
//      ├─ config.LoadEnv()          ← Load .env file
//      ├─ flag.Parse()              ← Parse --config, --samples, --json flags
//      ├─ config.Load(*cfgPath)     ← Read providers.yaml
//      └─ runTest(cfg, ...)         ← Execute the health check
//           │
//           ├─ For each provider (concurrently via errgroup):
//           │   └─ testProvider(client, provider, samples)
//           │       │
//           │       ├─ Warm-up call (BlockNumber — connection priming)
//           │       └─ For i = 0..samples:
//           │           ├─ Call BlockNumber()
//           │           ├─ Record latency (if success)
//           │           ├─ Log to stderr (real-time tracing)
//           │           └─ Sleep 200ms between samples
//           │
//           └─ Output:
//               ├─ --json? → Build TestReport → writeJSON()
//               └─ Terminal? → format.FormatTest()
//
// CS CONCEPTS IN THIS FILE
// =========================
// 1. SAMPLING: Why N measurements are better than 1, and how to choose N
// 2. WARM-UP: Eliminating measurement bias from connection setup
// 3. INTER-SAMPLE DELAY: Why we sleep 200ms between samples
// 4. CONCURRENT TESTING: Running all providers in parallel
// 5. MUTEX PROTECTION: Thread-safe writes to shared result slice
//
// SAMPLING AND WARM-UP: WHY THEY MATTER
// =======================================
// A single latency measurement is noisy — it might catch a lucky fast path
// or an unlucky slow path. By taking N samples, we can compute percentiles
// that reveal the TRUE distribution:
//
//   Sample 1:  23ms  ← might just be lucky
//   Sample 2:  25ms
//   Sample 3:  142ms ← network hiccup
//   ...
//   Sample 30: 21ms
//
// With 30 samples, we can reliably estimate P50 (typical) and P95 (worst 5%).
// The warm-up call (first BlockNumber call whose result is discarded) ensures
// the TCP connection and TLS handshake are already done before we start
// measuring, so samples reflect steady-state performance, not one-time setup.
//
// The 200ms inter-sample delay prevents:
//   1. Rate limiting by the provider (which would artificially inflate latency)
//   2. Hammering the provider's infrastructure (which could affect other users)
//   3. Saturating our own network connection
// =============================================================================

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

// =============================================================================
// SECTION 1: JSON Report Utilities
// =============================================================================

// writeJSON writes any JSON-serializable data to a timestamped file.
// See cmd/block/main.go for detailed documentation — same function, same pattern.
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

// =============================================================================
// SECTION 2: JSON Report Types
// =============================================================================

// TestReport is the top-level JSON structure for health check reports.
//
// This struct wraps the test metadata (when and how many samples) with the
// per-provider results. It's serialized directly to JSON by writeJSON.
//
// time.Time is serialized as an ISO 8601 string by Go's JSON encoder:
//   "timestamp": "2024-01-15T14:32:18.123456789Z"
type TestReport struct {
	Timestamp time.Time         `json:"timestamp"` // When the test was run
	Samples   int               `json:"samples"`   // Number of samples per provider
	Results   []TestReportEntry `json:"results"`   // Per-provider results
}

// TestReportEntry holds one provider's test results for JSON export.
//
// This is separate from format.TestResult because:
//   1. JSON reports need latencies as int64 milliseconds (not time.Duration)
//   2. JSON reports include pre-computed percentiles (not raw latency slices)
//   3. The JSON schema is a stable external API; internal types can change freely
//
// FIELD: LatenciesMS []int64
// ==========================
// The raw latency samples, converted from time.Duration to int64 milliseconds.
// This array is included in JSON reports so external tools can perform their
// own statistical analysis (e.g., compute different percentiles, plot histograms,
// or detect patterns like latency spikes).
type TestReportEntry struct {
	Name         string  `json:"name"`           // Provider name
	Type         string  `json:"type"`           // Provider type (informational)
	Success      int     `json:"success"`        // Successful sample count
	Total        int     `json:"total"`          // Total sample count
	P50LatencyMS int64   `json:"p50_latency_ms"` // 50th percentile in ms
	P95LatencyMS int64   `json:"p95_latency_ms"` // 95th percentile in ms
	P99LatencyMS int64   `json:"p99_latency_ms"` // 99th percentile in ms
	MaxLatencyMS int64   `json:"max_latency_ms"` // Maximum observed latency in ms
	BlockHeight  uint64  `json:"block_height"`   // Last observed block height
	LatenciesMS  []int64 `json:"latencies_ms"`   // All raw latency samples in ms
}

// =============================================================================
// SECTION 3: Per-Provider Testing — The Sample Loop
// =============================================================================

// testProvider runs N sample RPC calls against a single provider and collects
// latency statistics.
//
// This function runs WITHIN a goroutine (one per provider). Multiple
// testProvider calls execute concurrently, each testing a different provider.
//
// PARAMETERS
// ==========
// - client *rpc.Client: POINTER to the RPC client for this provider.
//   The `*` means we receive the address — the Client was heap-allocated
//   by rpc.NewClient() in the calling goroutine. We call methods on this
//   client (BlockNumber), which are pointer-receiver methods. The pointer
//   is shared between our call and the HTTP connection pool inside the client.
//
// - p config.Provider: The provider configuration, passed BY VALUE (copy).
//   Provider is a small struct (~80 bytes), so copying is efficient.
//   We only read p.Name and p.Type, so a copy is fine.
//
// - samples int: Number of test samples to collect.
//
// RETURN VALUE: format.TestResult (by value)
// ===========================================
// Returns a TestResult struct BY VALUE. The struct contains a []time.Duration
// slice (Latencies), which internally holds a pointer to the heap-allocated
// latency array. Returning by value copies the slice header (24 bytes) but
// NOT the underlying latency data — both the caller and the returned struct
// share the same heap array. This is safe because testProvider is done with
// the data when it returns.
//
// MEASUREMENT METHODOLOGY
// ========================
//   1. WARM-UP: One discarded BlockNumber call to establish the TCP connection
//   2. SAMPLE LOOP: N measured BlockNumber calls with 200ms delays
//   3. TRACING: Each sample is logged to stderr for real-time visibility
//   4. PERCENTILE COMPUTATION: After all samples, compute P50/P95/P99/Max
//
// The stderr tracing is valuable during long test runs — you can see progress
// in real time, and the interleaved output from concurrent goroutines shows
// which providers are responding and which are timing out.
func testProvider(client *rpc.Client, p config.Provider, samples int) format.TestResult {
	// context.Background() creates an empty context with no timeout.
	// Individual RPC calls are bounded by the client's HTTP timeout
	// (configured per-provider in providers.yaml).
	ctx := context.Background()

	// var latencies []time.Duration — nil slice (no allocation yet).
	// append() will allocate the underlying array on first use.
	// We don't pre-allocate because we don't know how many will succeed.
	var latencies []time.Duration
	var lastHeight uint64
	success := 0

	// Log the start of testing to stderr.
	fmt.Fprintf(os.Stderr, "\n[%s] Testing with %d samples...\n", p.Name, samples)

	// WARM-UP CALL: Prime the HTTP connection.
	// client.BlockNumber(ctx) makes one RPC call whose result is discarded.
	// This ensures the TCP connection and TLS handshake are done before
	// we start measuring, isolating steady-state latency from setup overhead.
	client.BlockNumber(ctx)

	// SAMPLE LOOP: Collect N latency measurements.
	for i := 0; i < samples; i++ {
		height, latency, err := client.BlockNumber(ctx)
		if err == nil {
			success++
			// append() adds the latency to the slice, growing the underlying
			// array if needed. Go's append uses an amortized doubling strategy:
			// when the array is full, it allocates a new one at 2x capacity
			// and copies existing elements. This gives O(1) amortized appends.
			latencies = append(latencies, latency)
			lastHeight = height
			// Log each successful sample to stderr.
			// The format "  alchemy 1/30: 23ms" shows provider, progress, and latency.
			fmt.Fprintf(os.Stderr, "  %s %d/%d: %dms\n", p.Name, i+1, samples, latency.Milliseconds())
		} else {
			fmt.Fprintf(os.Stderr, "  %s %d/%d: ERROR - %v\n", p.Name, i+1, samples, err)
		}

		// INTER-SAMPLE DELAY: Sleep 200ms between samples.
		// Skip the delay after the last sample (no point waiting).
		// time.Sleep blocks the current goroutine (not the OS thread),
		// allowing other goroutines to run during the sleep.
		if i < samples-1 {
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Compute percentiles from the collected latencies.
	// CalculateTailLatency sorts a copy of the latencies and computes P50/P95/P99/Max.
	tailLatency := format.CalculateTailLatency(latencies)

	// Log the computed percentiles to stderr for tracing.
	fmt.Fprintf(os.Stderr, "[%s] Calculated percentiles:\n", p.Name)
	fmt.Fprintf(os.Stderr, "  P50: %dms, P95: %dms, P99: %dms, Max: %dms\n",
		tailLatency.P50.Milliseconds(), tailLatency.P95.Milliseconds(), tailLatency.P99.Milliseconds(), tailLatency.Max.Milliseconds())

	// Return the result by value. The Latencies slice header is copied,
	// but the underlying array remains on the heap and is shared.
	return format.TestResult{
		Name:        p.Name,
		Type:        p.Type,
		Success:     success,
		Total:       samples,
		Latencies:   latencies,
		BlockHeight: lastHeight,
	}
}

// =============================================================================
// SECTION 4: Test Orchestration — Running All Providers Concurrently
// =============================================================================

// runTest orchestrates the health check across all configured providers.
//
// CONCURRENCY ARCHITECTURE
// ========================
// This function spawns one goroutine per provider using errgroup. All providers
// are tested SIMULTANEOUSLY, which means:
//
//   - A 4-provider test with 30 samples takes ~6 seconds (30 * 200ms delay)
//     regardless of provider count (parallel, not serial)
//   - stderr output is interleaved (samples from different providers appear
//     in real time as they arrive)
//   - The sync.Mutex ensures thread-safe writes to the shared results slice
//
// Timeline with 4 providers:
//
//   Time 0s    1s    2s    3s    4s    5s    6s
//   alchemy:   [############################]  ← 30 samples
//   infura:    [############################]  ← 30 samples (parallel)
//   llamanodes:[############################]  ← 30 samples (parallel)
//   publicnode:[############################]  ← 30 samples (parallel)
//   g.Wait() ─────────────────────────────────▶ all done
//
// PARAMETER: cfg *config.Config
// =============================
// A POINTER to the Config struct. The `*` means we receive the address —
// cfg is a pointer that lets us access Providers and Defaults without copying
// the entire Config (which contains a slice of providers, each with strings
// for name and URL).
func runTest(cfg *config.Config, samplesOverride int, jsonOut bool) error {
	// Determine sample count: flag override > config default.
	samples := cfg.Defaults.HealthSamples
	if samplesOverride > 0 {
		samples = samplesOverride
	}

	fmt.Printf("\nTesting %d providers with %d samples each...\n\n", len(cfg.Providers), samples)

	// Pre-allocate one result slot per provider.
	// make([]format.TestResult, len(cfg.Providers)) creates a slice with
	// the exact size needed. Each slot is zero-initialized.
	results := make([]format.TestResult, len(cfg.Providers))
	var mu sync.Mutex

	// Create errgroup for structured concurrency.
	// The _ discards the derived context because testProvider creates its own
	// background context — test samples don't need to be cancelled as a group.
	g, _ := errgroup.WithContext(context.Background())

	for i, p := range cfg.Providers {
		i, p := i, p // Shadow loop variables for goroutine safety
		g.Go(func() error {
			// Each goroutine creates its own client for the provider.
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)
			result := testProvider(client, p, samples)

			// Write the result to the shared slice under mutex protection.
			//
			// mu.Lock() acquires exclusive access to the results slice.
			// No other goroutine can read or write results while we hold the lock.
			// mu.Unlock() releases the lock, allowing other goroutines to proceed.
			//
			// results[i] = result copies the TestResult struct (including
			// the Latencies slice header) into the pre-allocated slot.
			mu.Lock()
			results[i] = result
			mu.Unlock()
			return nil
		})
	}

	// Wait for ALL goroutines to complete.
	// g.Wait() blocks the calling goroutine until every g.Go() function returns.
	g.Wait()

	g.Wait()

	// --- Output ---
	if jsonOut {
		// Build the JSON report structure.
		// time.Now() captures the timestamp of when the test completed.
		reportData := TestReport{
			Timestamp: time.Now(),
			Samples:   samples,
			Results:   make([]TestReportEntry, len(results)),
		}

		for i, r := range results {
			// Convert time.Duration latencies to int64 milliseconds for JSON.
			// JSON doesn't have a duration type — milliseconds as integers are
			// the most universally parseable format.
			latenciesMs := make([]int64, len(r.Latencies))
			for j, lat := range r.Latencies {
				latenciesMs[j] = lat.Milliseconds()
			}

			// Re-compute percentiles for the JSON report.
			// We compute them again (rather than storing from testProvider)
			// to keep the data flow clear and avoid adding fields to TestResult.
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

	// Terminal display: render the formatted comparison table.
	format.FormatTest(os.Stdout, results)
	return nil
}

// =============================================================================
// SECTION 5: Entry Point
// =============================================================================
//
// main() follows the same pattern as cmd/block/main.go:
//   1. Load .env environment variables
//   2. Parse command-line flags (returns pointers)
//   3. Load YAML configuration (dereference flag pointer with *)
//   4. Delegate to runTest() (dereference flag pointers with *)
//
// See cmd/block/main.go SECTION 7 for detailed flag/pointer documentation.
// =============================================================================

func main() {
	config.LoadEnv()

	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		samples = flag.Int("samples", 0, "Number of test samples per provider (0 = use config default)")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	// *cfgPath dereferences the pointer returned by flag.String to get the
	// actual string value for the config file path.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// *samples and *jsonOut dereference the flag pointers to get the actual
	// int and bool values, respectively.
	if err := runTest(cfg, *samples, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

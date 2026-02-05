// =============================================================================
// FILE: internal/format/test.go
// ROLE: Statistical Analysis & Display — Tail Latency Metrics and Test Results
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file is the analytical heart of the `test` command. It defines the data
// structures for test results, implements percentile calculations (P50, P95,
// P99), and renders a comparison table across all providers.
//
// DATA FLOW
// =========
//
//   cmd/test/main.go
//       │
//       │  testProvider() runs N samples per provider
//       │  collects []time.Duration latencies
//       │
//       ▼
//   TestResult struct (this file)
//       │
//       │  CalculateTailLatency() computes percentiles
//       │
//       ▼
//   TailLatency struct (this file)
//       │
//       │  FormatTest() renders comparison table
//       │
//       ▼
//   Terminal output:
//   ┌────────────────────────────────────────────────────────────┐
//   │ Provider       Type   Success  P50   P95   P99   Max  Block│
//   │ ──────────────────────────────────────────────────────────  │
//   │ alchemy        public  100%   23ms  45ms  52ms  78ms  21M  │
//   │ infura         public  100%   19ms  38ms  47ms  65ms  21M  │
//   └────────────────────────────────────────────────────────────┘
//
// CS CONCEPTS: PERCENTILES AND TAIL LATENCY
// ==========================================
// "Average latency" is misleading for systems performance. Consider:
//
//   Provider A: 99 requests at 20ms, 1 request at 5000ms
//   Average: (99*20 + 5000) / 100 = 69.8ms  ← looks fine!
//
//   Provider B: all 100 requests at 65ms
//   Average: 65ms  ← looks similar to A!
//
// But Provider A is MUCH worse — 1% of its requests take 5 seconds!
// If you're making 1000 RPC calls per day, that's 10 calls experiencing
// 5-second delays. For trading, those 10 delayed calls could each cost
// hundreds of dollars.
//
// PERCENTILES solve this by answering: "What is the worst latency that
// X% of requests experience?"
//
//   P50 (median): 50% of requests are faster than this
//   P95: 95% of requests are faster (5% are slower)
//   P99: 99% of requests are faster (1% are slower — the "tail")
//   Max: The absolute worst case observed
//
// The GAP between P50 and P99 reveals consistency:
//   P50=20ms, P99=25ms  → Very consistent (tight distribution)
//   P50=20ms, P99=500ms → Highly variable (long tail of slow requests)
//
// For trading systems, P99 and Max matter most — they represent the
// worst-case scenarios that can cause missed opportunities.
//
// WHAT A READER SHOULD UNDERSTAND
// ================================
// 1. Why percentiles are superior to averages for latency analysis
// 2. The nearest-rank method for percentile calculation
// 3. How Go slices are sorted and indexed
// 4. The io.Writer pattern for testable output
// =============================================================================

package format

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// =============================================================================
// SECTION 1: Data Types for Test Results
// =============================================================================

// TestResult holds the outcome of testing a single provider.
//
// This struct is populated by cmd/test/main.go's testProvider() function,
// which runs N sample requests and records each latency.
//
// SLICE FIELD: Latencies []time.Duration
// ======================================
// The Latencies field is a SLICE, not a fixed array. It grows dynamically
// as successful samples are recorded. Failed samples are NOT added to this
// slice — they're counted in (Total - Success) but don't appear in latency
// statistics, because a failed request has no meaningful latency.
//
// In memory:
//
//   TestResult (on stack or heap)
//   ┌────────────────────────────────┐
//   │ Name: "alchemy"                │
//   │ Type: "public"                 │
//   │ Success: 28                    │
//   │ Total: 30                      │
//   │ Latencies: ────────────────────┼──▶ [23ms, 25ms, 21ms, ..., 45ms]
//   │   (slice header: ptr,len,cap)  │     (28 elements on the heap)
//   │ BlockHeight: 21234567          │
//   └────────────────────────────────┘
type TestResult struct {
	Name        string          // Provider name (e.g., "alchemy")
	Type        string          // Provider type (e.g., "public") — informational
	Success     int             // Count of successful RPC calls
	Total       int             // Total number of RPC calls attempted
	Latencies   []time.Duration // Latency of each SUCCESSFUL call
	BlockHeight uint64          // Last observed block height from this provider
}

// TailLatency holds the computed percentile values for a set of latency samples.
//
// These four values together paint a complete picture of a provider's
// performance distribution:
//
//   P50 (median): The "typical" request. Half are faster, half are slower.
//   P95:          The "mostly-worst" case. Only 5% of requests are slower.
//   P99:          The "almost-worst" case. Only 1% of requests are slower.
//   Max:          The absolute worst observed. The true tail.
//
// Example interpretation:
//   {P50: 23ms, P95: 45ms, P99: 52ms, Max: 78ms}
//   → "Typical request: 23ms. Worst 1%: ~52ms. Absolute worst: 78ms."
//   → Tight distribution, reliable provider.
//
//   {P50: 23ms, P95: 200ms, P99: 1500ms, Max: 5000ms}
//   → "Typical request: 23ms. But 1% take 1.5 seconds!"
//   → Dangerous for trading — the tail will bite you.
type TailLatency struct {
	P50, P95, P99, Max time.Duration
}

// =============================================================================
// SECTION 2: Percentile Calculation — The Nearest-Rank Method
// =============================================================================

// CalculateTailLatency computes P50, P95, P99, and Max from a slice of latencies.
//
// ALGORITHM: Nearest-Rank Method
// ==============================
// This is the simplest and most widely used percentile algorithm. It works by:
//   1. Sort all values in ascending order
//   2. For percentile P, find the index: index = len * P / 100
//   3. The value at that index IS the percentile
//
// Step-by-step example with 10 latency samples (already sorted):
//
//   Index:  [0]   [1]   [2]   [3]   [4]   [5]   [6]   [7]   [8]   [9]
//   Value:  15ms  18ms  20ms  22ms  23ms  25ms  28ms  35ms  42ms  78ms
//
//   P50: index = 10 * 50 / 100 = 5  → sorted[5] = 25ms
//   P95: index = 10 * 95 / 100 = 9  → sorted[9] = 78ms
//   P99: index = 10 * 99 / 100 = 9  → sorted[min(9, 9)] = 78ms
//   Max: sorted[9] = 78ms
//
// Note: With only 10 samples, P95 = P99 = Max. This is expected — you need
// at least 100 samples for P99 to differ from Max, and at least 20 samples
// for P95 to differ from Max. The default of 30 samples gives meaningful
// P50 and P95 values but P99 will often equal Max.
//
// WHY COPY THE SLICE BEFORE SORTING?
// ====================================
// We create a copy: sorted := make([]time.Duration, len(latencies))
// and then copy(sorted, latencies).
//
// If we sorted the original slice directly (sort.Slice(latencies, ...)),
// we would modify the caller's data — the original order would be lost.
// This matters because the caller might want to use the original order
// for other purposes (e.g., time-series analysis).
//
// The make() call allocates a new slice on the heap with the same length.
// copy() then copies the contents (the time.Duration values, not just the
// header). After this, `sorted` and `latencies` are fully independent —
// sorting one does not affect the other.
//
// In memory:
//
//   latencies (original)                sorted (our copy)
//   ┌─────────────────────┐            ┌─────────────────────┐
//   │ ptr ──▶ [23, 45, 21]│            │ ptr ──▶ [21, 23, 45]│ ← sorted
//   │ len: 3              │            │ len: 3              │
//   │ cap: 3              │            │ cap: 3              │
//   └─────────────────────┘            └─────────────────────┘
//   (different underlying arrays — independent)
//
// EDGE CASE: Empty latencies slice
// =================================
// If no samples succeeded, latencies is empty. We return a zero-valued
// TailLatency{} to avoid index-out-of-bounds panics. The caller sees
// P50=0, P95=0, P99=0, Max=0, which correctly indicates "no data."
func CalculateTailLatency(latencies []time.Duration) TailLatency {
	if len(latencies) == 0 {
		return TailLatency{}
	}

	// Create a copy to avoid mutating the caller's data.
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)

	// Sort ascending. sort.Slice takes a "less" function that returns true
	// when element i should come before element j.
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Calculate percentile indices using integer arithmetic.
	// Integer division truncates (floors), which is the nearest-rank method.
	n := len(sorted)
	return TailLatency{
		P50: sorted[n*50/100],
		P95: sorted[n*95/100],
		// P99 uses min() to prevent index overflow for small sample sizes.
		// Without min(), n*99/100 could equal n for certain sizes, causing
		// an out-of-bounds access. Example: n=1 → 1*99/100 = 0, which is
		// fine. But the min() is a safety guard for edge cases.
		P99: sorted[min(n*99/100, n-1)],
		Max: sorted[n-1],
	}
}

// =============================================================================
// SECTION 3: Test Results Table Rendering
// =============================================================================

// FormatTest renders a comparison table of all provider test results.
//
// The table includes:
//   - Provider name and type
//   - Success rate (color-coded: green/yellow/red)
//   - P50, P95, P99, Max latencies (each color-coded by speed)
//   - Block height (to detect sync lag)
//   - Height mismatch warnings at the bottom
//
// PARAMETER: results []TestResult
// ================================
// This is a SLICE of TestResult structs passed by value. However, "by value"
// for a slice means only the slice HEADER is copied (pointer, length, capacity
// — 24 bytes total). The underlying array of TestResult structs is NOT copied.
// Both the caller's slice and our `results` parameter point to the same data.
//
// This is safe because FormatTest only reads the data — it never modifies it.
//
// HEIGHT MISMATCH DETECTION
// ==========================
// After rendering the table, this function checks whether all providers
// reported the same block height. If they differ, it groups providers by
// height and shows a warning. This detects:
//   - Providers lagging behind (propagation delay)
//   - Stale caches returning old data
//   - Network partitions where different nodes see different chain tips
//
// The map[uint64][]string pattern (heightGroups) groups provider names by
// their reported block height. If the map has more than one key, there's
// a mismatch.
func FormatTest(w io.Writer, results []TestResult) {
	// Render the table header with bold labels.
	// The format specifiers (%-14s, %5s, etc.) control column widths:
	//   %-14s = left-aligned, 14 characters wide
	//   %5s   = right-aligned, 5 characters wide
	//   %7s   = right-aligned, 7 characters wide
	fmt.Fprintf(w, "%s %s %s %s  %s  %s  %s %s\n",
		Bold(fmt.Sprintf("%-14s", "Provider")),
		Bold(fmt.Sprintf("%-6s", "Type")),
		Bold(fmt.Sprintf("%5s", "Success")),
		Bold(fmt.Sprintf("%3s", "P50")),
		Bold(fmt.Sprintf("%3s", "P95")),
		Bold(fmt.Sprintf("%3s", "P99")),
		Bold(fmt.Sprintf("%3s", "Max")),
		Bold(fmt.Sprintf("%7s", "Block")))
	fmt.Fprintln(w, strings.Repeat("─", 90))

	// Render one row per provider.
	// The `for _, r := range results` iterates over the slice, copying each
	// TestResult into `r`. Since we only read `r`, the copy is fine.
	for _, r := range results {
		// Calculate percentiles for this provider's latencies.
		tail := CalculateTailLatency(r.Latencies)

		// Render the row with color-coded values.
		// padRight() handles ANSI-aware padding so columns align despite
		// invisible color codes in the strings.
		fmt.Fprintf(w, "%-14s %-6s %s %s  %s  %s  %s %7d\n",
			r.Name,
			Dim(r.Type),
			padRight(ColorSuccess(r.Success, r.Total), 5),
			padRight(ColorLatency(tail.P50.Milliseconds()), 3),
			padRight(ColorLatency(tail.P95.Milliseconds()), 3),
			padRight(ColorLatency(tail.P99.Milliseconds()), 3),
			padRight(ColorLatency(tail.Max.Milliseconds()), 3),
			r.BlockHeight)
	}
	fmt.Fprintln(w)

	// --- Height Mismatch Detection ---
	//
	// Build a map from block height → list of provider names at that height.
	// make(map[uint64][]string) creates an empty map where:
	//   - Keys are uint64 (block heights)
	//   - Values are []string (slices of provider names)
	//
	// Only providers with at least one success are included — a provider
	// that failed all requests has no reliable block height to report.
	heightGroups := make(map[uint64][]string)
	for _, r := range results {
		if r.Success > 0 {
			heightGroups[r.BlockHeight] = append(heightGroups[r.BlockHeight], r.Name)
		}
	}

	// If more than one distinct height exists, some providers are out of sync.
	if len(heightGroups) > 1 {
		fmt.Fprintln(w, Yellow("⚠"), Bold("BLOCK HEIGHT MISMATCH DETECTED:"))
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w)
	}
}

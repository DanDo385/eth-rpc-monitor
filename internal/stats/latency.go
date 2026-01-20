// internal/stats/latency.go
package stats

import (
	"math"
	"sort"
	"time"
)

// TailLatency holds p50, p95, p99, and max latency values.
type TailLatency struct {
	P50, P95, P99, Max time.Duration
}

// CalculateTailLatency computes tail latency percentiles (P50, P95, P99, Max) from samples.
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
//   - TailLatency: Struct containing calculated percentiles
//
// Algorithm:
//  1. Sort latencies in ascending order
//  2. Use nearest-rank method to calculate percentiles
//  3. For small sample sizes, P95/P99 naturally equal Max (correct behavior)
func CalculateTailLatency(latencies []time.Duration) TailLatency {
	// Handle empty samples (all requests failed)
	if len(latencies) == 0 {
		return TailLatency{}
	}

	// Sort latencies in ascending order for percentile calculation
	// Create a copy to avoid mutating the original slice
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Calculate percentiles from sorted samples using nearest-rank method
	return TailLatency{
		P50: Percentile(sorted, 0.50), // Median
		P95: Percentile(sorted, 0.95), // 95th percentile
		P99: Percentile(sorted, 0.99), // 99th percentile
		Max: sorted[len(sorted)-1],    // Maximum is the last element after sorting
	}
}

// Percentile returns the value at the given percentile using the nearest-rank method.
// This method ensures that with small sample sizes, high percentiles (P95, P99) correctly
// equal the maximum value, which is the expected behavior.
//
// Parameters:
//   - sorted: Pre-sorted slice of latencies (ascending order)
//   - p: Percentile as decimal (e.g., 0.95 for 95th percentile)
//
// Returns:
//   - time.Duration: Value at the requested percentile
//
// Formula: index = ceil(n * p) - 1, clamped to valid range [0, n-1]
func Percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
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

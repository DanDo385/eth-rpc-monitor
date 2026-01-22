package format

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type TestResult struct {
	Name        string
	Type        string
	Success     int
	Total       int
	Latencies   []time.Duration
	BlockHeight uint64
}

type TailLatency struct {
	P50, P95, P99, Max time.Duration
}

func CalculateTailLatency(latencies []time.Duration) TailLatency {
	if len(latencies) == 0 {
		return TailLatency{}
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	n := len(sorted)
	return TailLatency{
		P50: sorted[n*50/100],
		P95: sorted[n*95/100],
		P99: sorted[min(n*99/100, n-1)],
		Max: sorted[n-1],
	}
}

func FormatTest(w io.Writer, results []TestResult) {
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

	for _, r := range results {
		tail := CalculateTailLatency(r.Latencies)

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

	// Check for height mismatches
	heightGroups := make(map[uint64][]string)
	for _, r := range results {
		if r.Success > 0 {
			heightGroups[r.BlockHeight] = append(heightGroups[r.BlockHeight], r.Name)
		}
	}

	if len(heightGroups) > 1 {
		fmt.Fprintln(w, Yellow("⚠"), Bold("BLOCK HEIGHT MISMATCH DETECTED:"))
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w)
	}
}

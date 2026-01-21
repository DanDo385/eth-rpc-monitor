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
	fmt.Fprintf(w, "%-14s %-6s %8s %8s %8s %8s %8s %12s\n",
		"Provider", "Type", "Success", "P50", "P95", "P99", "Max", "Block")
	fmt.Fprintln(w, strings.Repeat("─", 90))

	for _, r := range results {
		tail := CalculateTailLatency(r.Latencies)
		successPct := float64(r.Success) / float64(r.Total) * 100

		fmt.Fprintf(w, "%-14s %-6s %7.0f%% %7dms %7dms %7dms %7dms %12d\n",
			r.Name,
			r.Type,
			successPct,
			tail.P50.Milliseconds(),
			tail.P95.Milliseconds(),
			tail.P99.Milliseconds(),
			tail.Max.Milliseconds(),
			r.BlockHeight)
	}
	fmt.Fprintln(w)
}

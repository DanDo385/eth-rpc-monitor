package display

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// HealthResult is the terminal-facing summary for a single provider.
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

// HealthFormatter formats the health command output table.
type HealthFormatter struct {
	Results []HealthResult
}

// Format writes the formatted health table (and mismatch warnings) to w.
func (f *HealthFormatter) Format(w io.Writer) error {
	fmt.Fprintf(w, "%-14s %-6s %8s %8s %8s %8s %8s %12s\n",
		"Provider", "Type", "Success", "P50", "P95", "P99", "Max", "Block")
	fmt.Fprintln(w, strings.Repeat("─", 90))

	for _, r := range f.Results {
		successPct := float64(r.Success) / float64(r.Total) * 100
		fmt.Fprintf(w, "%-14s %-6s %7.0f%% %7dms %7dms %7dms %7dms %12d\n",
			r.Name,
			r.Type,
			successPct,
			r.P50Latency.Milliseconds(),
			r.P95Latency.Milliseconds(),
			r.P99Latency.Milliseconds(),
			r.MaxLatency.Milliseconds(),
			r.BlockHeight)
	}
	fmt.Fprintln(w)

	// Height mismatch detection (only among providers with at least one success).
	heightGroups := make(map[uint64][]string)
	for _, r := range f.Results {
		if r.Success > 0 {
			heightGroups[r.BlockHeight] = append(heightGroups[r.BlockHeight], r.Name)
		}
	}

	if len(heightGroups) > 1 {
		fmt.Fprintln(w, "⚠ BLOCK HEIGHT MISMATCH DETECTED:")
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w, "\nThis may indicate lagging providers or propagation delays.")
		fmt.Fprintln(w)
	}

	return nil
}

package format

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type WatchResult struct {
	Provider    string
	BlockHeight uint64
	Latency     time.Duration
	Error       error
}

func FormatMonitor(w io.Writer, results []WatchResult, interval time.Duration, clearScreen bool) {
	if clearScreen {
		fmt.Fprint(w, "\033[2J\033[H")
	}

	// Find highest block
	var highest uint64
	for _, r := range results {
		if r.Error == nil && r.BlockHeight > highest {
			highest = r.BlockHeight
		}
	}

	fmt.Fprintf(w, "Monitoring %d providers (interval: %s, Ctrl+C to exit)...\n\n", len(results), interval)
	fmt.Fprintf(w, "%-14s %12s %10s %12s\n", "Provider", "Block Height", "Latency", "Lag")
	fmt.Fprintln(w, strings.Repeat("─", 60))

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(w, "%-14s %12s %10s %12s\n", r.Provider, "ERROR", "—", "—")
			continue
		}

		lag := highest - r.BlockHeight
		lagStr := "—"
		if lag > 0 {
			lagStr = fmt.Sprintf("-%d", lag)
		}
		fmt.Fprintf(w, "%-14s %12d %8dms %12s\n", r.Provider, r.BlockHeight, r.Latency.Milliseconds(), lagStr)
	}
	fmt.Fprintln(w)
}

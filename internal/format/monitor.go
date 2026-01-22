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

	fmt.Fprintf(w, "Monitoring %d providers %s\n\n",
		len(results),
		Dim(fmt.Sprintf("(interval: %s, Ctrl+C to exit)", interval)))

	fmt.Fprintf(w, "%s %s %s %s\n",
		Bold(fmt.Sprintf("%-14s", "Provider")),
		Bold(fmt.Sprintf("%12s", "Block Height")),
		Bold(fmt.Sprintf("%7s", "Latency")),
		Bold(fmt.Sprintf("%3s", "Lag")))
	fmt.Fprintln(w, strings.Repeat("─", 60))

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(w, "%-14s %12s %7s %3s\n",
				r.Provider,
				padRight(Red("ERROR"), 12),
				padRight(Dim("—"), 7),
				padRight(Dim("—"), 3))
			continue
		}

		lag := highest - r.BlockHeight
		fmt.Fprintf(w, "%-14s %12d %7s %3s\n",
			r.Provider,
			r.BlockHeight,
			padRight(ColorLatency(r.Latency.Milliseconds()), 7),
			padRight(ColorLag(lag), 3))
	}
	fmt.Fprintln(w)
}

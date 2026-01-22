package format

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type SnapshotResult struct {
	Provider string
	Hash     string
	Height   uint64
	Latency  time.Duration
	Error    error
}

func FormatSnapshot(w io.Writer, results []SnapshotResult) {
	fmt.Fprintf(w, "%s %s        %s   %s\n",
		Bold(fmt.Sprintf("%-14s", "Provider")),
		Bold(fmt.Sprintf("%7s", "Latency")),
		Bold(fmt.Sprintf("%12s", "Block Height")),
		Bold("Block Hash"))
	fmt.Fprintln(w, strings.Repeat("─", 90))

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(w, "%-14s %s        %s   %s %v\n",
				r.Provider,
				padRight(Dim("—"), 7),
				padRight(Dim("—"), 12),
				Red("ERROR:"),
				r.Error)
		} else {
			fmt.Fprintf(w, "%-14s %s        %12d   %s\n",
				r.Provider,
				padRight(ColorLatency(r.Latency.Milliseconds()), 7),
				r.Height,
				Dim(r.Hash))
		}
	}

	// Check for mismatches
	hashGroups := make(map[string][]string)
	heightGroups := make(map[uint64][]string)
	for _, r := range results {
		if r.Error == nil {
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r.Provider)
			heightGroups[r.Height] = append(heightGroups[r.Height], r.Provider)
		}
	}

	fmt.Fprintln(w)

	if len(heightGroups) > 1 {
		fmt.Fprintln(w, Yellow("⚠"), Bold("BLOCK HEIGHT MISMATCH DETECTED:"))
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w)
	}

	if len(hashGroups) > 1 {
		fmt.Fprintln(w, Yellow("⚠"), Bold("BLOCK HASH MISMATCH DETECTED:"))
		for hash, providers := range hashGroups {
			fmt.Fprintf(w, "  %s...  →  %v\n", hash[:18], providers)
		}
		fmt.Fprintln(w)
	} else if len(hashGroups) == 1 {
		fmt.Fprintln(w, Green("✓"), "All providers agree on block hash")
	}
}

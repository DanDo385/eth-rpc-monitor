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
	fmt.Fprintf(w, "%-14s %10s %12s   %s\n", "Provider", "Latency", "Block Height", "Block Hash")
	fmt.Fprintln(w, strings.Repeat("─", 90))

	for _, r := range results {
		if r.Error != nil {
			fmt.Fprintf(w, "%-14s %10s %12s   ERROR: %v\n", r.Provider, "—", "—", r.Error)
		} else {
			fmt.Fprintf(w, "%-14s %8dms %12d   %s\n", r.Provider, r.Latency.Milliseconds(), r.Height, r.Hash)
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
		fmt.Fprintln(w, "⚠ BLOCK HEIGHT MISMATCH DETECTED:")
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w)
	}

	if len(hashGroups) > 1 {
		fmt.Fprintln(w, "⚠ BLOCK HASH MISMATCH DETECTED:")
		for hash, providers := range hashGroups {
			fmt.Fprintf(w, "  %s...  →  %v\n", hash[:18], providers)
		}
		fmt.Fprintln(w)
	} else if len(hashGroups) == 1 {
		fmt.Fprintln(w, "✓ All providers agree on block hash")
	}
}

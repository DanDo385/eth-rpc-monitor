package display

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// CompareResult is the terminal-facing result for a single provider in compare.
type CompareResult struct {
	Provider string
	Hash     string
	Height   uint64
	Latency  time.Duration
	Err      error
}

// CompareFormatter formats compare output (table + mismatch warnings).
type CompareFormatter struct {
	BlockArg string
	Results  []CompareResult
}

// Format writes the formatted compare output to w.
func (f *CompareFormatter) Format(w io.Writer) error {
	fmt.Fprintf(w, "%-14s %10s %12s   %s\n", "Provider", "Latency", "Block Height", "Block Hash")
	fmt.Fprintln(w, strings.Repeat("─", 90))

	hashGroups := make(map[string][]string)
	heightGroups := make(map[uint64][]string)
	successCount := 0

	for _, r := range f.Results {
		if r.Err != nil {
			fmt.Fprintf(w, "%-14s %10s %12s   ERROR: %v\n", r.Provider, "—", "—", r.Err)
			continue
		}

		successCount++
		hashGroups[r.Hash] = append(hashGroups[r.Hash], r.Provider)
		heightGroups[r.Height] = append(heightGroups[r.Height], r.Provider)
		fmt.Fprintf(w, "%-14s %8dms %12d   %s\n", r.Provider, r.Latency.Milliseconds(), r.Height, r.Hash)
	}

	fmt.Fprintln(w)
	if successCount == 0 {
		fmt.Fprintln(w, "✗ No providers responded successfully")
		fmt.Fprintln(w)
		return nil
	}

	if len(heightGroups) > 1 {
		fmt.Fprintln(w, "⚠ BLOCK HEIGHT MISMATCH DETECTED:")
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w, "\nThis may indicate lagging providers or propagation delays.")
		fmt.Fprintln(w)
	}

	if len(hashGroups) == 1 {
		fmt.Fprintln(w, "✓ All providers agree on block hash")
		fmt.Fprintln(w)
		return nil
	}

	fmt.Fprintln(w, "⚠ BLOCK HASH MISMATCH DETECTED:")
	for hash, providers := range hashGroups {
		short := hash
		if len(short) > 18 {
			short = short[:18]
		}
		fmt.Fprintf(w, "  %s...  →  %v\n", short, providers)
	}
	fmt.Fprintln(w, "\nThis may indicate stale caches, chain reorganization, or incorrect data.")
	return nil
}

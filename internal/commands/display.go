package commands

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// CompareResult holds the result of fetching a block from a provider.
type CompareResult struct {
	Provider string
	Hash     string
	Height   uint64
	Latency  time.Duration
	Error    error
}

// WatchResult holds the result of querying a provider's block number.
type WatchResult struct {
	Provider    string
	BlockHeight uint64
	Latency     time.Duration
	Error       error
}

// HealthResult holds aggregated health statistics for a provider.
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
	Latencies   []time.Duration
}

// FindHighestBlock finds the highest block height among successful watch results.
func FindHighestBlock(results []WatchResult) uint64 {
	var highest uint64
	for _, r := range results {
		if r.Error == nil && r.BlockHeight > highest {
			highest = r.BlockHeight
		}
	}
	return highest
}

// BlockFormatter formats a single block view.
type BlockFormatter struct {
	block    *rpc.Block
	provider string
	latency  time.Duration
}

func NewBlockFormatter(block *rpc.Block, provider string, latency time.Duration) *BlockFormatter {
	return &BlockFormatter{block: block, provider: provider, latency: latency}
}

func (f *BlockFormatter) Format(w io.Writer) error {
	p, err := f.block.Parsed()
	if err != nil {
		return err
	}

	fmt.Fprintf(w, "\nBlock #%s\n", rpc.FormatNumber(p.Number))
	fmt.Fprintln(w, "═══════════════════════════════════════════════════")
	fmt.Fprintf(w, "  Hash:         %s\n", p.Hash)
	fmt.Fprintf(w, "  Parent:       %s\n", p.ParentHash)
	fmt.Fprintf(w, "  Timestamp:    %s\n", rpc.FormatTimestamp(p.Timestamp))
	fmt.Fprintf(w, "  Gas:          %s / %s (%.1f%%)\n",
		rpc.FormatNumber(p.GasUsed),
		rpc.FormatNumber(p.GasLimit),
		float64(p.GasUsed)/float64(p.GasLimit)*100)
	fmt.Fprintf(w, "  Base Fee:     %s\n", rpc.FormatGwei(p.BaseFeePerGas))
	fmt.Fprintf(w, "  Transactions: %d\n", p.TxCount)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Provider:     %s (%dms)\n", f.provider, f.latency.Milliseconds())
	fmt.Fprintln(w)
	return nil
}

// CompareFormatter formats the compare command output.
type CompareFormatter struct {
	results           []CompareResult
	successCount      int
	heightGroups      map[uint64][]CompareResult
	hashGroups        map[string][]CompareResult
	hasHeightMismatch bool
	hasHashMismatch   bool
}

func NewCompareFormatter(
	results []CompareResult,
	successCount int,
	heightGroups map[uint64][]CompareResult,
	hashGroups map[string][]CompareResult,
	hasHeightMismatch bool,
	hasHashMismatch bool,
) *CompareFormatter {
	return &CompareFormatter{
		results:           results,
		successCount:      successCount,
		heightGroups:      heightGroups,
		hashGroups:        hashGroups,
		hasHeightMismatch: hasHeightMismatch,
		hasHashMismatch:   hasHashMismatch,
	}
}

func (f *CompareFormatter) Format(w io.Writer) error {
	fmt.Fprintf(w, "%-14s %10s %12s   %s\n", "Provider", "Latency", "Block Height", "Block Hash")
	fmt.Fprintln(w, strings.Repeat("─", 90))

	for _, r := range f.results {
		if r.Error != nil {
			fmt.Fprintf(w, "%-14s %10s %12s   ERROR: %v\n", r.Provider, "—", "—", r.Error)
		} else {
			fmt.Fprintf(w, "%-14s %8dms %12d   %s\n", r.Provider, r.Latency.Milliseconds(), r.Height, r.Hash)
		}
	}

	fmt.Fprintln(w)
	if f.successCount == 0 {
		fmt.Fprintln(w, "✗ No providers responded successfully")
	} else {
		if f.hasHeightMismatch {
			fmt.Fprintln(w, "⚠ BLOCK HEIGHT MISMATCH DETECTED:")
			for height, results := range f.heightGroups {
				providers := make([]string, len(results))
				for i, r := range results {
					providers[i] = r.Provider
				}
				fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
			}
			fmt.Fprintln(w, "\nThis may indicate lagging providers or propagation delays.")
			fmt.Fprintln(w)
		}

		if len(f.hashGroups) == 1 {
			fmt.Fprintln(w, "✓ All providers agree on block hash")
		} else if f.hasHashMismatch {
			fmt.Fprintln(w, "⚠ BLOCK HASH MISMATCH DETECTED:")
			for hash, results := range f.hashGroups {
				providers := make([]string, len(results))
				for i, r := range results {
					providers[i] = r.Provider
				}
				preview := hash
				if len(preview) > 18 {
					preview = preview[:18]
				}
				fmt.Fprintf(w, "  %s...  →  %v\n", preview, providers)
			}
			fmt.Fprintln(w, "\nThis may indicate stale caches, chain reorganization, or incorrect data.")
		}
	}
	fmt.Fprintln(w)
	return nil
}

// HealthFormatter formats the health command output.
type HealthFormatter struct {
	results []HealthResult
}

func NewHealthFormatter(results []HealthResult) *HealthFormatter {
	return &HealthFormatter{results: results}
}

func (f *HealthFormatter) Format(w io.Writer) error {
	fmt.Fprintf(w, "%-14s %-6s %8s %8s %8s %8s %8s %12s\n",
		"Provider", "Type", "Success", "P50", "P95", "P99", "Max", "Block")
	fmt.Fprintln(w, strings.Repeat("─", 90))

	for _, r := range f.results {
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

	heightGroups := make(map[uint64][]HealthResult)
	for _, r := range f.results {
		if r.Success > 0 {
			heightGroups[r.BlockHeight] = append(heightGroups[r.BlockHeight], r)
		}
	}

	if len(heightGroups) > 1 {
		fmt.Fprintln(w, "⚠ BLOCK HEIGHT MISMATCH DETECTED:")
		for height, results := range heightGroups {
			providers := make([]string, len(results))
			for i, r := range results {
				providers[i] = r.Name
			}
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w, "\nThis may indicate lagging providers or propagation delays.")
		fmt.Fprintln(w)
	}

	return nil
}

// MonitorFormatter formats the monitor command output.
type MonitorFormatter struct {
	results       []WatchResult
	interval      time.Duration
	providerCount int
	firstDisplay  bool
}

func NewMonitorFormatter(results []WatchResult, interval time.Duration, providerCount int, firstDisplay bool) *MonitorFormatter {
	return &MonitorFormatter{
		results:       results,
		interval:      interval,
		providerCount: providerCount,
		firstDisplay:  firstDisplay,
	}
}

func (f *MonitorFormatter) Format(w io.Writer) error {
	highestBlock := FindHighestBlock(f.results)

	if !f.firstDisplay {
		fmt.Fprint(w, "\033[2J\033[H")
	}

	fmt.Fprintf(w, "Monitoring %d providers (interval: %s, Ctrl+C to exit)...\n\n", f.providerCount, f.interval)
	fmt.Fprintf(w, "%-14s %12s %10s %12s\n", "Provider", "Block Height", "Latency", "Lag")
	fmt.Fprintln(w, strings.Repeat("─", 60))

	for _, r := range f.results {
		if r.Error != nil {
			fmt.Fprintf(w, "%-14s %12s %10s %12s\n",
				r.Provider,
				"ERROR",
				"—",
				"—")
			continue
		}

		lag := highestBlock - r.BlockHeight
		lagStr := "—"
		if lag > 0 {
			lagStr = fmt.Sprintf("-%d", lag)
		}
		fmt.Fprintf(w, "%-14s %12d %8dms %12s\n",
			r.Provider,
			r.BlockHeight,
			r.Latency.Milliseconds(),
			lagStr)
	}
	fmt.Fprintln(w)
	return nil
}

// Helpers for deterministic output in tests (if needed).
// Currently unused, but kept minimal for coherence.
func sortedKeysUint64(m map[uint64][]CompareResult) []uint64 {
	keys := make([]uint64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

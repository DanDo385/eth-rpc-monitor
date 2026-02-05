// =============================================================================
// FILE: internal/format/snapshot.go
// ROLE: Consensus Analysis & Display — Fork Detection and Provider Agreement
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file renders the output of the `snapshot` command, which answers a
// critical question: "Do all of my RPC providers agree on the current state
// of the blockchain?"
//
// In a healthy network, every provider should return the same block hash for
// the same block number. When they disagree, something is wrong — and this
// file's job is to clearly display WHAT disagrees and WHO disagrees.
//
// DATA FLOW
// =========
//
//   cmd/snapshot/main.go
//       │
//       │  Fetches the SAME block from ALL providers concurrently
//       │  Records: hash, height, latency, or error for each
//       │
//       ▼
//   SnapshotResult struct (this file)
//       │
//       │  FormatSnapshot() renders comparison + detects mismatches
//       │
//       ▼
//   Terminal output:
//   ┌──────────────────────────────────────────────────────────────────┐
//   │ Provider       Latency   Block Height   Block Hash              │
//   │ ──────────────────────────────────────────────────────────────── │
//   │ alchemy          43ms       21234567   0xa1b2c3d4...            │
//   │ infura           39ms       21234567   0xa1b2c3d4...            │
//   │ llamanodes      167ms       21234566   0x9876fedc...            │
//   │                                                                  │
//   │ ⚠ BLOCK HEIGHT MISMATCH DETECTED:                               │
//   │   Height 21234567  →  [alchemy infura]                          │
//   │   Height 21234566  →  [llamanodes]                              │
//   │                                                                  │
//   │ ⚠ BLOCK HASH MISMATCH DETECTED:                                │
//   │   0xa1b2c3d4e5f678...  →  [alchemy infura]                     │
//   │   0x9876fedc01234...   →  [llamanodes]                         │
//   └──────────────────────────────────────────────────────────────────┘
//
// CS CONCEPTS: CONSENSUS AND FORK DETECTION
// ==========================================
// Blockchain networks achieve consensus — all nodes agree on the same chain
// of blocks. But in practice, disagreements happen:
//
// 1. PROPAGATION DELAY: When a new block is mined, it takes time to propagate
//    across the network. For a few seconds, some nodes know about block N+1
//    while others are still on block N. This shows as a HEIGHT mismatch and
//    is usually harmless.
//
// 2. CHAIN REORGANIZATION (REORG): Occasionally, two miners find valid blocks
//    at nearly the same time, creating a temporary fork. The network eventually
//    picks the "heavier" (more total difficulty) chain, and the shorter fork
//    is abandoned. During a reorg, different providers might return different
//    hashes for the same block number — a HASH mismatch.
//
// 3. STALE CACHES: Some RPC providers cache block data aggressively. If their
//    cache hasn't been updated, they might serve outdated hashes or heights.
//
// For trading applications, using stale data can be catastrophic — executing
// a trade based on a block that gets reorganized away means the trade never
// actually happened, but your internal state thinks it did.
//
// WHAT A READER SHOULD UNDERSTAND
// ================================
// 1. Why multiple providers might disagree on block data
// 2. The difference between height mismatches and hash mismatches
// 3. How maps are used for grouping/aggregation in Go
// 4. How the error interface works in Go struct fields
// =============================================================================

package format

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// =============================================================================
// SECTION 1: Snapshot Result Type
// =============================================================================

// SnapshotResult holds the outcome of fetching a specific block from one provider.
//
// ERROR FIELD: error (interface type)
// ====================================
// The Error field is typed as `error`, which is a Go INTERFACE, not a struct.
//
// In Go, an interface defines a set of methods that a type must implement.
// The `error` interface requires only one method:
//
//   type error interface {
//       Error() string
//   }
//
// Any type that has an Error() string method satisfies this interface.
// Common implementations include:
//   - *errors.errorString (from errors.New("..."))
//   - *fmt.wrapError (from fmt.Errorf("...: %w", err))
//   - *net.OpError (network errors)
//   - *url.Error (URL parsing errors)
//
// An interface value in memory has two components:
//
//   ┌──────────────────────┐
//   │ Error (interface)    │
//   │  type pointer:  ─────┼──▶ type descriptor (e.g., *net.OpError)
//   │  value pointer: ─────┼──▶ actual error data on heap
//   └──────────────────────┘
//
// When Error is nil, BOTH pointers are nil — this means "no error."
// We check this with `r.Error == nil` (or `r.Error != nil`).
//
// Note: `error` is lowercase, meaning it's a built-in predeclared identifier,
// not an exported type from a package. It's one of Go's few special names.
type SnapshotResult struct {
	Provider string        // Provider name (e.g., "alchemy")
	Hash     string        // Block hash returned by this provider (empty on error)
	Height   uint64        // Block height returned by this provider (0 on error)
	Latency  time.Duration // Time taken for the RPC call
	Error    error         // nil on success; non-nil describes the failure
}

// =============================================================================
// SECTION 2: Snapshot Table Rendering and Mismatch Detection
// =============================================================================

// FormatSnapshot renders a comparison table and detects block disagreements.
//
// This function performs TWO tasks:
//   1. DISPLAY: Render a table showing each provider's response
//   2. ANALYSIS: Detect and report height and hash mismatches
//
// The analysis uses Go maps as GROUP BY operations — similar to SQL's GROUP BY:
//
//   hashGroups:   "0xa1b2..." → ["alchemy", "infura"]
//                 "0x9876..." → ["llamanodes"]
//
//   heightGroups: 21234567    → ["alchemy", "infura"]
//                 21234566    → ["llamanodes"]
//
// If either map has more than one key, there's a mismatch.
//
// PARAMETER: results []SnapshotResult
// ====================================
// Passed by value, but recall that a slice value is just a header (pointer +
// length + capacity = 24 bytes). The underlying SnapshotResult array is on
// the heap and is NOT copied. This function only reads the data.
func FormatSnapshot(w io.Writer, results []SnapshotResult) {
	// Render the table header.
	fmt.Fprintf(w, "%s %s        %s   %s\n",
		Bold(fmt.Sprintf("%-14s", "Provider")),
		Bold(fmt.Sprintf("%7s", "Latency")),
		Bold(fmt.Sprintf("%12s", "Block Height")),
		Bold("Block Hash"))
	fmt.Fprintln(w, strings.Repeat("─", 90))

	// Render one row per provider, handling errors gracefully.
	for _, r := range results {
		if r.Error != nil {
			// Provider failed — show error instead of data.
			// Dim dashes replace the missing values to maintain column alignment.
			// The `%v` verb prints the error using its Error() method.
			fmt.Fprintf(w, "%-14s %s        %s   %s %v\n",
				r.Provider,
				padRight(Dim("—"), 7),
				padRight(Dim("—"), 12),
				Red("ERROR:"),
				r.Error)
		} else {
			// Provider succeeded — show block data.
			// The hash is dimmed because it's long and secondary to the
			// height information. Latency is color-coded by speed.
			fmt.Fprintf(w, "%-14s %s        %12d   %s\n",
				r.Provider,
				padRight(ColorLatency(r.Latency.Milliseconds()), 7),
				r.Height,
				Dim(r.Hash))
		}
	}

	// --- Mismatch Detection ---
	//
	// Build two maps that group providers by their reported data:
	//   hashGroups:   block hash → providers reporting that hash
	//   heightGroups: block height → providers reporting that height
	//
	// Only successful providers are included — failed providers have no
	// reliable data to compare.
	//
	// make(map[K]V) creates an empty map. Maps in Go are reference types —
	// the variable holds a pointer to the underlying hash table, so the
	// map is allocated on the heap.
	hashGroups := make(map[string][]string)
	heightGroups := make(map[uint64][]string)
	for _, r := range results {
		if r.Error == nil {
			// append() adds the provider name to the slice for this hash/height.
			// If the key doesn't exist yet, the map returns the zero value
			// for []string, which is nil. append(nil, "alchemy") creates a
			// new slice containing ["alchemy"].
			hashGroups[r.Hash] = append(hashGroups[r.Hash], r.Provider)
			heightGroups[r.Height] = append(heightGroups[r.Height], r.Provider)
		}
	}

	fmt.Fprintln(w)

	// Height mismatch: providers report different block numbers.
	// This usually means some providers are lagging behind the network tip.
	// Common cause: propagation delay, overloaded nodes, or rate limiting.
	if len(heightGroups) > 1 {
		fmt.Fprintln(w, Yellow("⚠"), Bold("BLOCK HEIGHT MISMATCH DETECTED:"))
		for height, providers := range heightGroups {
			fmt.Fprintf(w, "  Height %d  →  %v\n", height, providers)
		}
		fmt.Fprintln(w)
	}

	// Hash mismatch: providers report different block hashes for the same height.
	// This is more concerning than height mismatches — it means providers
	// disagree on the actual block CONTENT, which could indicate:
	//   - An active chain reorganization
	//   - A stale cache serving old data
	//   - In rare cases, a consensus failure
	//
	// hash[:18] truncates the hash to 18 characters for display. The full
	// hash is 66 characters (0x + 64 hex digits), which is too long for a
	// clean table. The truncated form is enough to visually distinguish
	// different hashes.
	if len(hashGroups) > 1 {
		fmt.Fprintln(w, Yellow("⚠"), Bold("BLOCK HASH MISMATCH DETECTED:"))
		for hash, providers := range hashGroups {
			fmt.Fprintf(w, "  %s...  →  %v\n", hash[:18], providers)
		}
		fmt.Fprintln(w)
	} else if len(hashGroups) == 1 {
		// All providers agree — show a reassuring green checkmark.
		fmt.Fprintln(w, Green("✓"), "All providers agree on block hash")
	}
}

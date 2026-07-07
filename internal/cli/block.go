// =============================================================================
// FILE: internal/cli/block.go
// ROLE: Block Inspector Workflow — Fetch and Display a Single Ethereum Block
// =============================================================================
//
// This is the workflow logic for the `block` subcommand, the simplest and most
// direct tool in the eth-rpc-monitor suite. It answers the question:
// "What does block N look like, and which provider can give it to me fastest?"
//
// Usage examples (after the cobra layer wires flags):
//   block                           ← Latest block from fastest provider
//   block 19000000                  ← Specific block by decimal number
//   block 0x121eac0                 ← Specific block by hex
//   block latest --provider alchemy ← Latest block from specific provider
//   block latest --json             ← Export block data as JSON report
//
// EXECUTION FLOW
// ==============
//
//   RunBlock(cfg, blockArg, provider, jsonOut)
//      │
//      ├─ Provider selection:
//      │   ├─ --provider flag? → Find by name, create client
//      │   └─ Auto-select?    → SelectFastestProvider()
//      │                           │
//      │                           ├─ Query ALL providers concurrently
//      │                           ├─ Find who has the latest block
//      │                           └─ Pick the fastest among those
//      │
//      ├─ Warm-up call (BlockNumber) ← Prime the HTTP connection
//      ├─ Fetch block (GetBlock)     ← The actual data fetch
//      │
//      └─ Output:
//          ├─ --json flag? → ConvertBlockToJSON() → reportjson.Write()
//          └─ Terminal?    → format.FormatBlock()
//
// CS CONCEPTS COVERED IN THIS FILE
// ==================================
// 1. CONCURRENCY: Parallel provider queries with errgroup and sync.Mutex
// 2. CONTEXT: Timeout propagation and cancellation via context.Context
// 3. POINTERS: Extensive use of * and & for struct allocation and parameter passing
// 4. CLOSURES: Loop variable shadowing for goroutine safety
// 5. ERROR WRAPPING: Using %w in fmt.Errorf for error chains
// =============================================================================

package cli

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/reportjson"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// =============================================================================
// SECTION 1: JSON Report Types
// =============================================================================

// BlockJSON is the JSON-serializable representation of a block for report output.
//
// This struct exists because the raw rpc.Block has hex strings and the parsed
// rpc.ParsedBlock has Go-native types — neither is ideal for JSON reports
// consumed by external tools. BlockJSON provides:
//   - Decimal numbers (not hex) for easier parsing by downstream tools
//   - ISO 8601 timestamps (not Unix seconds) for human readability
//   - Base fee in gwei (not wei) for practical interpretation
//
// POINTER FIELD: *float64 for BaseFeePerGas
// ==========================================
// BaseFeePerGas is *float64 (a POINTER to float64), not float64 (a value).
// This allows the field to be absent from JSON output for pre-EIP-1559 blocks.
//
// The `omitempty` tag with a pointer type means:
//   - nil pointer → field is OMITTED from JSON entirely
//   - non-nil pointer → field appears with the pointed-to value
//
// If we used plain float64, the field would appear as 0.0 in the JSON for
// pre-London blocks, which is misleading (0.0 gwei is a valid fee, but
// "no fee" is semantically different from "zero fee").
type BlockJSON struct {
	Number        uint64   `json:"number"`                  // Block height as decimal
	Hash          string   `json:"hash"`                    // Block hash
	ParentHash    string   `json:"parentHash"`              // Parent block hash
	Timestamp     string   `json:"timestamp"`               // ISO 8601 timestamp
	GasUsed       uint64   `json:"gasUsed"`                 // Gas used as decimal
	GasLimit      uint64   `json:"gasLimit"`                // Gas limit as decimal
	BaseFeePerGas *float64 `json:"baseFeePerGas,omitempty"` // Base fee in gwei; nil = omitted
	Transactions  []string `json:"transactions"`            // Transaction hashes
}

// =============================================================================
// SECTION 2: Block Data Conversion for JSON Export
// =============================================================================

// ConvertBlockToJSON transforms a raw RPC Block into a JSON-friendly format.
//
// This function bridges three representations:
//  1. rpc.Block (hex strings from the wire)
//  2. Native Go types (uint64, *big.Int)
//  3. BlockJSON (decimal numbers, ISO timestamps, gwei units)
//
// PARAMETER: block *rpc.Block
// ===========================
// The `*` means this receives a POINTER to an rpc.Block. The function reads
// fields from the block through the pointer (e.g., block.Number dereferences
// automatically) but does NOT modify the original block.
//
// POINTER CREATION: &gweiFloat
// ============================
//
//	gweiFloat, _ := gwei.Float64()  ← gweiFloat is a float64 VALUE (on stack)
//	baseFeePerGas = &gweiFloat      ← & takes its address, creating a *float64
//
// Go's escape analysis detects that gweiFloat's address is stored in a struct
// field that outlives this scope, so it allocates gweiFloat on the heap
// instead of the stack, ensuring the pointer remains valid after the
// function returns.
func ConvertBlockToJSON(block *rpc.Block) BlockJSON {
	// Parse hex fields to native types.
	number, _ := rpc.ParseHexUint64(block.Number)
	timestampUnix, _ := rpc.ParseHexUint64(block.Timestamp)
	gasUsed, _ := rpc.ParseHexUint64(block.GasUsed)
	gasLimit, _ := rpc.ParseHexUint64(block.GasLimit)

	// Convert Unix timestamp to ISO 8601 format (e.g., "2024-01-15T14:32:18Z").
	// time.RFC3339 is Go's constant for the ISO 8601 / RFC 3339 format.
	timestampStr := time.Unix(int64(timestampUnix), 0).UTC().Format(time.RFC3339)

	// Convert base fee from wei (big.Int) to gwei (float64).
	//
	// var baseFeePerGas *float64 initializes to nil (pointer zero value).
	// If the block has no base fee (pre-EIP-1559), it stays nil and is
	// omitted from JSON output by the `omitempty` tag.
	var baseFeePerGas *float64
	if block.BaseFeePerGas != "" {
		baseFee := rpc.ParseHexBigInt(block.BaseFeePerGas)
		if baseFee != nil {
			// Convert wei → gwei using arbitrary-precision arithmetic.
			// .Quo() performs: result = baseFee / 1e9
			gwei := new(big.Float).Quo(
				new(big.Float).SetInt(baseFee),
				big.NewFloat(1e9),
			)
			gweiFloat, _ := gwei.Float64()
			// &gweiFloat creates a pointer to the float64 value.
			baseFeePerGas = &gweiFloat
		}
	}

	return BlockJSON{
		Number:        number,
		Hash:          block.Hash,
		ParentHash:    block.ParentHash,
		Timestamp:     timestampStr,
		GasUsed:       gasUsed,
		GasLimit:      gasLimit,
		BaseFeePerGas: baseFeePerGas,
		Transactions:  block.Transactions,
	}
}

// =============================================================================
// SECTION 3: Provider Selection — Finding the Fastest Provider
// =============================================================================

// providerResult holds the outcome of a single provider's block number query
// during the selection process.
//
// This is a small, unexported struct (lowercase name) used only within this
// file. It carries just enough data for the selection algorithm: did the
// provider respond? What block is it on? How fast did it respond?
type providerResult struct {
	blockNum uint64        // Latest block number reported by this provider
	latency  time.Duration // Round-trip time for the query
	hasError bool          // True if the query failed
}

// SelectFastestProvider queries all providers concurrently and selects the
// fastest one that is on the latest block.
//
// ALGORITHM: Two-phase selection
// ==============================
//
//	Phase 1 — Concurrent Discovery:
//	  Query ALL providers simultaneously to get their block numbers and latencies.
//	  This uses Go's errgroup for structured concurrency.
//
//	Phase 2 — Selection:
//	  a) Find the highest block number across all successful responses.
//	  b) Among providers ON that highest block, pick the one with lowest latency.
//
// Why not just pick the fastest regardless of block height?
// Because a provider might be fast but STALE — returning old data.
// We want the fastest provider that also has the LATEST data.
//
// LOOP VARIABLE SHADOWING: i, p := i, p
// =======================================
// The line `i, p := i, p` inside the for loop is critical for correctness.
// Without it, all goroutines would share the SAME loop variables, which
// change with each iteration. By the time the goroutines execute, the loop
// would have finished, and all goroutines would see the LAST values of i and p.
//
// The shadowing creates NEW variables `i` and `p` that are LOCAL to each
// iteration, captured by the closure. (Go 1.22+ makes each iteration create
// new variables by default, but the explicit shadowing remains for clarity.)
func SelectFastestProvider(ctx context.Context, cfg *config.Config) (*rpc.Client, error) {
	// Pre-allocate result and client slices with one slot per provider.
	results := make([]providerResult, len(cfg.Providers))
	clients := make([]*rpc.Client, len(cfg.Providers))
	var mu sync.Mutex

	// Create an errgroup with a derived context.
	// If any goroutine returns a non-nil error, gctx is cancelled, which would
	// cause all other in-flight HTTP requests to abort. (In practice, our
	// goroutines always return nil — errors are captured in hasError instead.)
	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p // Shadow loop variables for goroutine safety
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)
			blockNum, latency, err := client.BlockNumber(gctx)

			r := providerResult{hasError: err != nil}
			if err == nil {
				r.blockNum = blockNum
				r.latency = latency
			}

			// Write results under mutex protection.
			mu.Lock()
			results[i] = r
			clients[i] = client
			mu.Unlock()
			return nil
		})
	}

	// Block until all goroutines complete.
	g.Wait()

	// --- Phase 2: Selection ---

	// Find the highest block number among successful responses.
	var latestBlock uint64
	successCount := 0
	for _, r := range results {
		if !r.hasError {
			successCount++
			if r.blockNum > latestBlock {
				latestBlock = r.blockNum
			}
		}
	}

	if successCount == 0 {
		return nil, fmt.Errorf("no providers responded successfully")
	}

	// Among providers on the latest block, find the one with lowest latency.
	var fastest *rpc.Client
	var fastestLatency time.Duration
	found := false
	for i, r := range results {
		if !r.hasError && r.blockNum == latestBlock {
			if !found || r.latency < fastestLatency {
				fastest = clients[i]
				fastestLatency = r.latency
				found = true
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("no provider is on the latest block (%d)", latestBlock)
	}

	return fastest, nil
}

// =============================================================================
// SECTION 4: Block Argument Normalization
// =============================================================================

// NormalizeBlockArg converts a user-provided block identifier into the format
// expected by the Ethereum JSON-RPC API.
//
// The Ethereum RPC accepts block identifiers in two forms:
//  1. Special tags: "latest", "pending", "earliest"
//  2. Hex-encoded numbers: "0x10d4f"
//
// But users naturally type decimal numbers ("19000000"), so we need to convert.
//
// Conversion logic:
//
//	""           → "latest"     (default)
//	"latest"     → "latest"     (pass-through)
//	"pending"    → "pending"    (pass-through)
//	"earliest"   → "earliest"   (pass-through)
//	"0x121eac0"  → "0x121eac0"  (already hex, pass-through)
//	"19000000"   → "0x121eac0"  (decimal → hex conversion)
//	"garbage"    → "garbage"    (invalid — let the RPC server return an error)
func NormalizeBlockArg(arg string) string {
	arg = strings.TrimSpace(strings.ToLower(arg))

	if arg == "" {
		return "latest"
	}
	if arg == "latest" || arg == "pending" || arg == "earliest" {
		return arg
	}

	// If already hex-encoded (starts with "0x"), pass through unchanged.
	if strings.HasPrefix(arg, "0x") {
		return arg
	}

	// Try to parse as decimal and convert to hex.
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		// Not a valid decimal number — return as-is and let RPC handle the error.
		// This is a "fail-open" design: we don't validate exhaustively.
		return arg
	}

	// Convert decimal to hex with "0x" prefix.
	return fmt.Sprintf("0x%x", num)
}

// =============================================================================
// SECTION 5: Main Logic — RunBlock
// =============================================================================

// RunBlock executes the block inspection workflow.
//
// This is the core orchestrator for the block command. It handles:
//  1. Provider selection (manual or automatic)
//  2. Connection warm-up
//  3. Block fetching
//  4. Output formatting (terminal or JSON)
//
// CONTEXT AND TIMEOUT
// ===================
// context.WithTimeout creates a new context that automatically cancels
// after cfg.Defaults.Timeout*2. The "* 2" gives headroom: the first half
// covers provider selection (if auto-selecting), and the second half covers
// the actual block fetch. The `defer cancel()` releases the context's
// resources when RunBlock returns.
//
// ERROR WRAPPING: %w
// ==================
// fmt.Errorf("failed to fetch block: %w", err) wraps the original error
// with additional context, preserving the original error so callers can use
// errors.Is() or errors.Unwrap() to inspect it.
func RunBlock(cfg *config.Config, blockArg, providerName string, jsonOut bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	// --- Provider Selection ---
	var client *rpc.Client
	var err error
	if providerName != "" {
		// Manual selection: find the provider by name in the config.
		for _, p := range cfg.Providers {
			if p.Name == providerName {
				client = rpc.NewClient(p.Name, p.URL, p.Timeout)
				break
			}
		}
		if client == nil {
			return fmt.Errorf("provider '%s' not found in config", providerName)
		}
	} else {
		// Automatic selection: query all providers, pick the fastest on latest block.
		client, err = SelectFastestProvider(ctx, cfg)
		if err != nil {
			return err
		}
		// Print selection to stderr (not stdout) so it doesn't contaminate
		// piped output. Unix convention: stderr for diagnostics, stdout for data.
		fmt.Fprintf(os.Stderr, "Auto-selected: %s\n\n", client.Name())
	}

	// --- Warm-up Call ---
	// client.BlockNumber(ctx) is called here but its result is discarded.
	// This "primes" the HTTP connection: DNS, TCP, TLS handshake, and adds
	// the connection to http.Transport's pool. The subsequent GetBlock reuses
	// this pooled connection, so its measured latency reflects only RPC
	// processing time, not one-time connection setup overhead.
	client.BlockNumber(ctx)

	// --- Fetch the Block ---
	block, latency, err := client.GetBlock(ctx, blockArg)
	if err != nil {
		return fmt.Errorf("failed to fetch block: %w", err)
	}

	// --- Output ---
	if jsonOut {
		// JSON export: convert to JSON-friendly format and write to file.
		blockJSON := ConvertBlockToJSON(block)
		filepath, err := reportjson.Write(blockJSON, "block")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	// Terminal display: render formatted, color-coded block information.
	format.FormatBlock(os.Stdout, block, client.Name(), latency)
	return nil
}

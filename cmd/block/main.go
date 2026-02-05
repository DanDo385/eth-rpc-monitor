// =============================================================================
// FILE: cmd/block/main.go
// ROLE: Block Inspector Command — Fetch and Display a Single Ethereum Block
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This is the entry point for the `block` command, the simplest and most
// direct tool in the eth-rpc-monitor suite. It answers the question:
// "What does block N look like, and which provider can give it to me fastest?"
//
// Usage examples:
//   block                           ← Latest block from fastest provider
//   block 19000000                  ← Specific block by decimal number
//   block 0x121eac0                 ← Specific block by hex
//   block latest --provider alchemy ← Latest block from specific provider
//   block latest --json             ← Export block data as JSON report
//
// EXECUTION FLOW
// ==============
//
//   1. main()
//      │
//      ├─ config.LoadEnv()          ← Load .env file (optional)
//      ├─ flag.Parse()              ← Parse command-line flags
//      ├─ config.Load(*cfgPath)     ← Read providers.yaml
//      └─ runBlock(cfg, ...)        ← Execute the block inspection
//           │
//           ├─ Provider selection:
//           │   ├─ --provider flag? → Find by name, create client
//           │   └─ Auto-select?    → selectFastestProvider()
//           │                           │
//           │                           ├─ Query ALL providers concurrently
//           │                           ├─ Find who has the latest block
//           │                           └─ Pick the fastest among those
//           │
//           ├─ Warm-up call (BlockNumber) ← Prime the HTTP connection
//           ├─ Fetch block (GetBlock)     ← The actual data fetch
//           │
//           └─ Output:
//               ├─ --json flag? → convertBlockToJSON() → writeJSON()
//               └─ Terminal?    → format.FormatBlock()
//
// ARCHITECTURE: THE CMD PATTERN
// ==============================
// Go's convention for multi-binary projects places each command in its own
// directory under cmd/. Each cmd/*/main.go has its own `package main` and
// `func main()`, producing a separate binary when compiled:
//
//   go build -o bin/block ./cmd/block     ← compiles THIS file
//   go build -o bin/test ./cmd/test       ← compiles cmd/test/main.go
//
// This pattern lets one repository produce multiple related tools that
// share the same internal packages (rpc, config, format) without linking
// unnecessary code into each binary.
//
// CS CONCEPTS COVERED IN THIS FILE
// ==================================
// 1. CONCURRENCY: Parallel provider queries with errgroup and sync.Mutex
// 2. CONTEXT: Timeout propagation and cancellation via context.Context
// 3. POINTERS: Extensive use of * and & for struct allocation and parameter passing
// 4. CLOSURES: Loop variable shadowing for goroutine safety
// 5. FLAG PARSING: Command-line argument handling with Go's flag package
// 6. ERROR WRAPPING: Using %w in fmt.Errorf for error chains
// =============================================================================

package main

import (
	"context"
	"encoding/json"
	"flag"
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
// In memory:
//
//   Post-London block:              Pre-London block:
//   ┌──────────────────┐            ┌──────────────────┐
//   │ BlockJSON        │            │ BlockJSON        │
//   │  BaseFeePerGas:──┼──▶ 25.43  │  BaseFeePerGas:──┼──▶ nil (omitted)
//   └──────────────────┘            └──────────────────┘
//
//   JSON output:                     JSON output:
//   { "baseFeePerGas": 25.43, ... }  { ... }  ← field absent entirely
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
// SECTION 2: JSON Report Writing
// =============================================================================

// writeJSON writes any JSON-serializable data to a timestamped file in reports/.
//
// PARAMETER: data interface{}
// ===========================
// The `interface{}` type (Go's "any" type) means this function accepts ANY
// value. The JSON encoder uses reflection to inspect the actual type at
// runtime and serialize its fields accordingly. This lets us reuse writeJSON
// for BlockJSON, TestReport, or any other struct.
//
// The prefix parameter creates filenames like:
//   "reports/block-20240115-143218.json"
//   "reports/health-20240115-143218.json"
//
// TIME FORMAT: "20060102-150405"
// ==============================
// Go's reference time (Mon Jan 2 15:04:05 MST 2006) encoded here produces:
//   Year: 2006 → actual year
//   Month: 01 → zero-padded month
//   Day: 02 → zero-padded day
//   Hour: 15 → 24-hour format
//   Minute: 04 → zero-padded minute
//   Second: 05 → zero-padded second
func writeJSON(data interface{}, prefix string) (string, error) {
	os.MkdirAll("reports", 0755)
	filename := fmt.Sprintf("reports/%s-%s.json", prefix, time.Now().Format("20060102-150405"))
	file, _ := os.Create(filename)
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	enc.Encode(data)
	return filename, nil
}

// =============================================================================
// SECTION 3: Block Data Conversion for JSON Export
// =============================================================================

// convertBlockToJSON transforms a raw RPC Block into a JSON-friendly format.
//
// This function bridges three representations:
//   1. rpc.Block (hex strings from the wire)
//   2. Native Go types (uint64, *big.Int)
//   3. BlockJSON (decimal numbers, ISO timestamps, gwei units)
//
// PARAMETER: block *rpc.Block
// ===========================
// The `*` means this receives a POINTER to an rpc.Block. The function reads
// fields from the block through the pointer (e.g., block.Number dereferences
// automatically) but does NOT modify the original block.
//
// POINTER CREATION: &gweiFloat
// ============================
// This function contains one of the most instructive pointer patterns in the
// codebase: creating a *float64 from a local variable.
//
//   gweiFloat, _ := gwei.Float64()  ← gweiFloat is a float64 VALUE (on stack)
//   baseFeePerGas = &gweiFloat      ← & takes its address, creating a *float64
//
// In memory:
//
//   BEFORE &gweiFloat:              AFTER &gweiFloat:
//   Stack                           Stack
//   ┌──────────────────┐            ┌──────────────────┐
//   │ gweiFloat: 25.43 │            │ gweiFloat: 25.43 │ ← still here
//   └──────────────────┘            └──────────────────┘
//                                        ▲
//   baseFeePerGas: nil              baseFeePerGas: ───┘ (points to gweiFloat)
//
// Go's escape analysis detects that gweiFloat's address is stored in a struct
// field that outlives this scope, so it allocates gweiFloat on the heap
// instead of the stack, ensuring the pointer remains valid after the
// function returns.
//
// COMMON MISCONCEPTION: "&gweiFloat creates a copy." It does NOT. The &
// operator takes the address of the EXISTING variable. No copy is made.
// However, because escape analysis moves it to the heap, the variable
// effectively "escapes" the stack frame.
func convertBlockToJSON(block *rpc.Block) BlockJSON {
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
			// new(big.Float) allocates on the heap and returns *big.Float.
			// .SetInt(baseFee) converts the big.Int to big.Float for division.
			// .Quo() performs: result = baseFee / 1e9
			gwei := new(big.Float).Quo(
				new(big.Float).SetInt(baseFee),
				big.NewFloat(1e9),
			)
			gweiFloat, _ := gwei.Float64()
			// &gweiFloat creates a pointer to the float64 value.
			// See the POINTER CREATION comment above for the full explanation.
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
// SECTION 4: Provider Selection — Finding the Fastest Provider
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

// selectFastestProvider queries all providers concurrently and selects the
// fastest one that is on the latest block.
//
// ALGORITHM: Two-phase selection
// ==============================
//   Phase 1 — Concurrent Discovery:
//     Query ALL providers simultaneously to get their block numbers and latencies.
//     This uses Go's errgroup for structured concurrency (see below).
//
//   Phase 2 — Selection:
//     a) Find the highest block number across all successful responses.
//     b) Among providers ON that highest block, pick the one with lowest latency.
//
// Why not just pick the fastest regardless of block height?
// Because a provider might be fast but STALE — returning old data.
// We want the fastest provider that also has the LATEST data.
//
// CONCURRENCY MODEL: errgroup + sync.Mutex
// =========================================
// This function demonstrates Go's structured concurrency pattern:
//
//   errgroup.WithContext(ctx) creates:
//     1. A Group `g` that manages goroutine lifecycle
//     2. A derived context `gctx` that is cancelled if any goroutine returns an error
//
//   g.Go(func() error { ... }) launches a goroutine managed by the group.
//   g.Wait() blocks until ALL goroutines complete.
//
//   The sync.Mutex `mu` protects the shared `results` and `clients` slices.
//   Even though each goroutine writes to a DIFFERENT index (results[i]),
//   the Go race detector considers any concurrent slice access a data race.
//   The mutex makes this safe and explicit.
//
// In memory during concurrent execution:
//
//   Main goroutine                 Worker goroutines (one per provider)
//   ┌───────────────┐
//   │ g.Wait()      │             ┌─────────────────────┐
//   │ (blocking)    │             │ goroutine 0          │
//   └───────────────┘             │  query alchemy       │
//                                 │  mu.Lock()           │
//                                 │  results[0] = ...    │
//                                 │  clients[0] = ...    │
//                                 │  mu.Unlock()         │
//                                 └─────────────────────┘
//                                 ┌─────────────────────┐
//                                 │ goroutine 1          │
//                                 │  query infura        │
//                                 │  mu.Lock()           │
//                                 │  results[1] = ...    │
//                                 │  clients[1] = ...    │
//                                 │  mu.Unlock()         │
//                                 └─────────────────────┘
//                                 ... (one per provider)
//
// RETURN TYPE: (*rpc.Client, error)
// ==================================
// Returns a POINTER to the selected Client. The Client was heap-allocated
// by rpc.NewClient() inside the goroutine and stored in the clients[] slice.
// After selection, we return a pointer to that same heap object.
//
// LOOP VARIABLE SHADOWING: i, p := i, p
// =======================================
// The line `i, p := i, p` inside the for loop is critical for correctness.
// Without it, all goroutines would share the SAME loop variables, which
// change with each iteration. By the time the goroutines execute, the loop
// would have finished, and all goroutines would see the LAST values of i and p.
//
// The shadowing creates NEW variables `i` and `p` that are LOCAL to each
// iteration, captured by the closure:
//
//   Iteration 0: captures i=0, p=alchemy   (own copy)
//   Iteration 1: captures i=1, p=infura    (own copy)
//   Iteration 2: captures i=2, p=llamanodes (own copy)
//
// This is one of Go's most well-known gotchas. Starting in Go 1.22, the
// loop variable semantics changed to make each iteration create new variables
// by default, but the explicit shadowing remains common for compatibility
// and clarity.
func selectFastestProvider(ctx context.Context, cfg *config.Config) (*rpc.Client, error) {
	// Pre-allocate result and client slices with one slot per provider.
	// make() creates slices of the exact size needed — no wasted memory.
	results := make([]providerResult, len(cfg.Providers))
	clients := make([]*rpc.Client, len(cfg.Providers))
	var mu sync.Mutex

	// Create an errgroup with a derived context.
	// If any goroutine returns a non-nil error, gctx is cancelled,
	// which would cause all other in-flight HTTP requests to abort.
	// (In practice, our goroutines always return nil — errors are captured
	// in the hasError field instead of propagated to errgroup.)
	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p // Shadow loop variables for goroutine safety (see comment above)
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
	//
	// var fastest *rpc.Client — initialized to nil (pointer zero value).
	// The `found` flag tracks whether we've seen any candidate yet.
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
// SECTION 5: Block Argument Normalization
// =============================================================================

// normalizeBlockArg converts a user-provided block identifier into the format
// expected by the Ethereum JSON-RPC API.
//
// The Ethereum RPC accepts block identifiers in two forms:
//   1. Special tags: "latest", "pending", "earliest"
//   2. Hex-encoded numbers: "0x10d4f"
//
// But users naturally type decimal numbers ("19000000"), so we need to convert.
//
// Conversion logic:
//   ""           → "latest"     (default)
//   "latest"     → "latest"     (pass-through)
//   "pending"    → "pending"    (pass-through, note: currently mapped to "latest")
//   "earliest"   → "earliest"   (pass-through, note: currently mapped to "latest")
//   "0x121eac0"  → "0x121eac0"  (already hex, pass-through)
//   "19000000"   → "0x121eac0"  (decimal → hex conversion)
//   "garbage"    → "garbage"    (invalid — let the RPC server return an error)
//
// Note: The current implementation maps "pending" and "earliest" to "latest"
// since they share the same conditional branch. The Ethereum RPC would handle
// them correctly if passed through individually.
func normalizeBlockArg(arg string) string {
	arg = strings.TrimSpace(strings.ToLower(arg))

	// Handle special block tags.
	if arg == "latest" || arg == "pending" || arg == "earliest" || arg == "" {
		return "latest"
	}

	// If already hex-encoded (starts with "0x"), pass through unchanged.
	if strings.HasPrefix(arg, "0x") {
		return arg
	}

	// Try to parse as decimal and convert to hex.
	// strconv.ParseUint(arg, 10, 64) interprets `arg` as base-10, fitting in 64 bits.
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		// Not a valid decimal number — return as-is and let RPC handle the error.
		// This is a "fail-open" design: we don't validate exhaustively, we let
		// the Ethereum node return a proper error message.
		return arg
	}

	// Convert decimal to hex with "0x" prefix.
	// fmt.Sprintf("0x%x", num) formats the number as lowercase hex.
	// Example: 19000000 → "0x121eac0"
	return fmt.Sprintf("0x%x", num)
}

// =============================================================================
// SECTION 6: Main Logic — The runBlock Function
// =============================================================================

// runBlock executes the block inspection workflow.
//
// This is the core orchestrator for the block command. It handles:
//   1. Provider selection (manual or automatic)
//   2. Connection warm-up
//   3. Block fetching
//   4. Output formatting (terminal or JSON)
//
// PARAMETER: cfg *config.Config
// =============================
// Receives a POINTER to the Config loaded from YAML. The `*` means we
// receive the address, not a copy. This is efficient (8 bytes for a pointer
// vs ~hundreds of bytes for the full Config with its Provider slice) and
// allows us to read the config without copying it.
//
// CONTEXT AND TIMEOUT
// ===================
// context.WithTimeout creates a new context that automatically cancels
// after the specified duration (cfg.Defaults.Timeout * 2). The "* 2" gives
// headroom: the first half covers provider selection (if auto-selecting),
// and the second half covers the actual block fetch.
//
// The `defer cancel()` ensures the context's resources are released when
// runBlock returns. Even if the timeout hasn't been reached, calling cancel()
// frees the internal timer that Go created for the deadline.
//
// ERROR WRAPPING: %w
// ==================
// fmt.Errorf("failed to fetch block: %w", err) wraps the original error
// with additional context. The %w verb (introduced in Go 1.13) creates an
// error chain that preserves the original error, so callers can use
// errors.Is() or errors.Unwrap() to inspect it. This is different from %v,
// which would convert the error to a string, losing the original.
func runBlock(cfg *config.Config, blockArg, providerName string, jsonOut bool) error {
	// Create a timeout context. All RPC calls within this function will
	// respect this deadline — if the timeout expires, in-flight HTTP requests
	// are cancelled automatically.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	// --- Provider Selection ---
	//
	// var client *rpc.Client — initialized to nil (pointer zero value).
	// We either create a client for the named provider or auto-select one.
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
		// If the loop finished without finding a match, client is still nil.
		if client == nil {
			return fmt.Errorf("provider '%s' not found in config", providerName)
		}
	} else {
		// Automatic selection: query all providers, pick the fastest on latest block.
		client, err = selectFastestProvider(ctx, cfg)
		if err != nil {
			return err
		}
		// Print selection to stderr (not stdout) so it doesn't contaminate
		// piped output. This follows Unix convention: stderr for diagnostics,
		// stdout for data.
		fmt.Fprintf(os.Stderr, "Auto-selected: %s\n\n", client.Name())
	}

	// --- Warm-up Call ---
	//
	// client.BlockNumber(ctx) is called here but its result is discarded.
	// This "primes" the HTTP connection:
	//   - DNS resolution happens
	//   - TCP connection is established
	//   - TLS handshake completes (for HTTPS)
	//   - Go's http.Transport adds the connection to its pool
	//
	// The subsequent GetBlock call reuses this pooled connection, so its
	// measured latency reflects only the RPC processing time, not the
	// one-time connection setup overhead.
	client.BlockNumber(ctx)

	// --- Fetch the Block ---
	//
	// client.GetBlock returns (*rpc.Block, time.Duration, error).
	// block is a *rpc.Block — a pointer to the deserialized block data.
	block, latency, err := client.GetBlock(ctx, blockArg)
	if err != nil {
		return fmt.Errorf("failed to fetch block: %w", err)
	}

	// --- Output ---
	if jsonOut {
		// JSON export: convert to JSON-friendly format and write to file.
		blockJSON := convertBlockToJSON(block)
		filepath, err := writeJSON(blockJSON, "block")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	// Terminal display: render formatted, color-coded block information.
	// block is passed as *rpc.Block — FormatBlock receives the pointer
	// and reads through it without copying the Block struct.
	format.FormatBlock(os.Stdout, block, client.Name(), latency)
	return nil
}

// =============================================================================
// SECTION 7: Entry Point — main()
// =============================================================================
//
// main() is the program's entry point. In Go, every executable must have
// exactly one `package main` with a `func main()`. This function:
//   1. Loads environment variables (for API key expansion)
//   2. Parses command-line flags
//   3. Normalizes the block argument
//   4. Loads configuration
//   5. Delegates to runBlock()
//
// FLAG PARSING AND POINTERS
// =========================
// Go's flag package returns POINTERS to the flag values:
//
//   cfgPath = flag.String("config", "config/providers.yaml", "...")
//
// flag.String returns *string (a pointer to string), NOT a string value.
// Before flag.Parse() is called, the pointer points to the default value.
// After flag.Parse(), it points to the user-provided value (if given) or
// the default.
//
// To get the actual string value, we DEREFERENCE with *:
//   *cfgPath  → "config/providers.yaml" (or whatever the user provided)
//
//   In memory:
//   ┌──────────────┐
//   │ cfgPath: ────┼──▶ "config/providers.yaml"
//   └──────────────┘     (this string may change after flag.Parse())
//
//   After: config.Load(*cfgPath)
//   The * dereferences the pointer, retrieving the string value.
//   This is passed BY VALUE to config.Load() — Load receives a copy of the
//   string (which in Go is just a pointer+length header, very cheap to copy).
//
// ERROR HANDLING PATTERN
// =====================
// The two-step pattern:
//   1. cfg, err := config.Load(...)
//   2. if err != nil { print error; os.Exit(1) }
//
// is the standard Go approach for handling errors in main(). Since main()
// doesn't return an error (its signature is `func main()`), we must handle
// errors explicitly by printing to stderr and exiting with a non-zero code.
//
// os.Exit(1) terminates the process immediately with exit code 1,
// signaling failure to the calling shell or script.
// =============================================================================

func main() {
	// Load .env file to set environment variables (for API key expansion).
	config.LoadEnv()

	// Define command-line flags. Each flag.Type() returns a POINTER.
	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		provider = flag.String("provider", "", "Use specific provider (empty = auto-select fastest)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	// Parse command-line arguments. This populates the values behind each
	// flag pointer. flag.Args() returns any non-flag arguments (positional args).
	flag.Parse()

	// The first positional argument (if present) is the block identifier.
	// normalizeBlockArg converts it to the format expected by the Ethereum RPC.
	block := "latest"
	args := flag.Args()
	if len(args) > 0 {
		block = normalizeBlockArg(args[0])
	}

	// Load provider configuration from YAML.
	// *cfgPath dereferences the pointer to get the actual string path.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Execute the block inspection.
	// *provider and *jsonOut dereference the flag pointers to get the actual values.
	if err := runBlock(cfg, block, *provider, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

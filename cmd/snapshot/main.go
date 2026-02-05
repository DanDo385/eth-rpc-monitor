// =============================================================================
// FILE: cmd/snapshot/main.go
// ROLE: Fork Detection Command — Comparing Block Data Across Providers
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This is the entry point for the `snapshot` command, which performs a point-
// in-time comparison of block data across all configured providers. Unlike
// the `test` command (which measures latency over many samples), `snapshot`
// focuses on a single question: "Do all providers agree on what the blockchain
// looks like right now?"
//
// Usage examples:
//   snapshot                ← Compare latest block across all providers
//   snapshot 19000000       ← Compare a specific historical block
//   snapshot latest         ← Explicit "latest" (same as no argument)
//
// EXECUTION FLOW
// ==============
//
//   1. main()
//      │
//      ├─ config.LoadEnv()              ← Load .env file
//      ├─ flag.Parse()                  ← Parse --config flag
//      ├─ config.Load(*cfgPath)         ← Read providers.yaml
//      ├─ context.WithTimeout()         ← Create deadline for all operations
//      │
//      └─ For each provider (concurrently via errgroup):
//          │
//          ├─ rpc.NewClient()           ← Create provider client
//          ├─ client.BlockNumber()      ← Warm-up (connection priming)
//          ├─ client.GetBlock()         ← Fetch the target block
//          │
//          ├─ Extract hash and height from the block
//          │
//          └─ mu.Lock(); results[i] = r; mu.Unlock()  ← Thread-safe write
//
//      g.Wait()  ← Wait for all providers to finish
//      format.FormatSnapshot()  ← Render comparison and detect mismatches
//
// ARCHITECTURAL SIMPLICITY
// ========================
// This is the simplest command in the suite — it's entirely contained in
// main() with no helper functions. This is intentional: the logic is linear
// enough that extracting functions would add indirection without improving
// clarity. The key operations are:
//   1. Configure and create context
//   2. Launch concurrent fetches
//   3. Wait for completion
//   4. Format and display
//
// Compare this with cmd/block/main.go, which extracts selectFastestProvider()
// and normalizeBlockArg() because those are reusable, testable units of
// logic. Here, everything is a one-shot pipeline.
//
// CS CONCEPTS IN THIS FILE
// =========================
// 1. CONCURRENT DATA COLLECTION with errgroup
// 2. CONTEXT WITH TIMEOUT for bounded execution
// 3. MUTEX-PROTECTED shared state
// 4. NULL/NIL CHECKING for pointer safety (block != nil)
// 5. VARIABLE SHADOWING for goroutine closure safety
// =============================================================================

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// =============================================================================
// main — Entry Point and Complete Execution Logic
// =============================================================================
//
// This function contains the entire snapshot workflow. Let's walk through it
// section by section.
//
// OVERALL STRATEGY
// ================
// Fetch the SAME block from EVERY provider, then compare the results.
// If all providers return the same hash for the same block number, they agree
// on the state of the chain. Any disagreement is a red flag.
//
// The flow:
//
//   All providers ──(concurrent)──▶ Same block request
//                                       │
//                                       ▼
//   ┌───────────────────────────────────────────────────────┐
//   │ Provider A: block #21M → hash 0xa1b2...   43ms       │
//   │ Provider B: block #21M → hash 0xa1b2...   39ms       │  ← Agreement
//   │ Provider C: block #21M → hash 0xa1b2...  167ms       │
//   │ Provider D: block #20M → hash 0x9876...  142ms       │  ← Mismatch!
//   └───────────────────────────────────────────────────────┘
//                                       │
//                                       ▼
//   FormatSnapshot() detects the mismatch and renders a warning
// =============================================================================

func main() {
	// --- Step 1: Environment and Configuration ---

	// Load .env file to make API keys available for URL expansion.
	config.LoadEnv()

	// Parse the --config flag. flag.String returns *string (a pointer).
	// After flag.Parse(), *cfgPath contains the flag value or default.
	cfgPath := flag.String("config", "config/providers.yaml", "Config file path")
	flag.Parse()

	// The first positional argument is the block identifier (default: "latest").
	// Unlike cmd/block, snapshot doesn't normalize the argument (no decimal→hex
	// conversion). If the user passes a decimal number, the Ethereum RPC will
	// likely return an error. This is acceptable for a focused tool — use the
	// `block` command for flexible block identification.
	blockArg := "latest"
	if args := flag.Args(); len(args) > 0 {
		blockArg = args[0]
	}

	// Load the provider configuration.
	// *cfgPath dereferences the pointer to get the actual string value.
	// config.Load returns *config.Config — see config.go for details.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// --- Step 2: Create Timeout Context ---
	//
	// context.WithTimeout(parent, duration) creates a derived context that
	// automatically cancels after `duration`. This bounds the total time
	// we'll wait for all providers to respond.
	//
	// cfg.Defaults.Timeout * 2 gives us twice the per-request timeout.
	// The extra headroom accounts for the warm-up call + the actual fetch.
	//
	// `defer cancel()` releases the context's resources when main() returns.
	// Even if the timeout hasn't been reached, calling cancel() frees the
	// internal timer, preventing a resource leak.
	//
	// ctx is passed to errgroup.WithContext, which creates a derived context
	// (gctx) that is ALSO cancelled if any goroutine returns a non-nil error.
	// This creates a cancellation hierarchy:
	//
	//   context.Background()
	//       └─ ctx (timeout after N seconds)
	//           └─ gctx (also cancelled if errgroup detects an error)
	//               └─ Each HTTP request uses gctx for cancellation
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	// --- Step 3: Concurrent Block Fetching ---
	//
	// Pre-allocate the results slice — one slot per provider.
	// Each goroutine writes to its own index (results[i]), protected by mu.
	results := make([]format.SnapshotResult, len(cfg.Providers))
	var mu sync.Mutex

	// Create errgroup with derived context.
	// g manages goroutine lifecycle; gctx carries cancellation signals.
	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		// LOOP VARIABLE SHADOWING: i, p := i, p
		// Creates new variables local to this iteration, captured by the closure.
		// Without this, all goroutines would see the LAST loop values.
		// See cmd/block/main.go selectFastestProvider for detailed explanation.
		i, p := i, p
		g.Go(func() error {
			// Create a client for this provider.
			// rpc.NewClient returns *rpc.Client (a pointer).
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)

			// WARM-UP CALL: Prime the HTTP connection.
			// The result is discarded — this is purely to establish the
			// TCP/TLS connection so the GetBlock call below measures only
			// RPC latency, not connection setup overhead.
			client.BlockNumber(gctx)

			// Fetch the target block from this provider.
			// client.GetBlock returns (*rpc.Block, time.Duration, error).
			//
			// block is a *rpc.Block — a POINTER to the block data.
			// This pointer could be nil if the block doesn't exist or
			// if the response was malformed, even when err is nil.
			block, latency, err := client.GetBlock(gctx, blockArg)

			// Build the result struct.
			// Start with the basic fields, then conditionally add block data.
			r := format.SnapshotResult{Provider: p.Name, Latency: latency, Error: err}

			// POINTER NIL CHECK: err == nil && block != nil
			// ==============================================
			// Two conditions are checked:
			//   1. err == nil    → The RPC call succeeded (no network or protocol error)
			//   2. block != nil  → The response contained actual block data
			//
			// Why check BOTH? Because GetBlock returns (*Block, error):
			//   - If err != nil, the call failed entirely (network error, timeout, etc.)
			//   - If err == nil but block == nil, the call succeeded but the block
			//     doesn't exist (e.g., requesting a future block number)
			//
			// block is a *rpc.Block (pointer). Checking != nil asks "does this
			// pointer point to anything?" If nil, dereferencing it (block.Hash)
			// would cause a nil pointer panic — a runtime crash.
			//
			// In memory:
			//
			//   block != nil:               block == nil:
			//   ┌──────────┐                ┌──────────┐
			//   │ block: ──┼──▶ Block data  │ block: ──┼──▶ (nothing)
			//   └──────────┘                └──────────┘
			//                                 ↑ Dereferencing here = CRASH
			if err == nil && block != nil {
				r.Hash = block.Hash
				// rpc.ParseHexUint64 converts the hex block number to uint64.
				// The _ discards the error (see types.go for rationale).
				r.Height, _ = rpc.ParseHexUint64(block.Number)
			}

			// Write the result to the shared slice under mutex protection.
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}

	// Wait for ALL goroutines to complete.
	// After g.Wait() returns, all results are populated and the mutex
	// is no longer needed — we have sole access to the results slice.
	g.Wait()

	// --- Step 4: Render Results ---
	//
	// FormatSnapshot displays the comparison table and detects mismatches.
	// It receives the results slice and writes to os.Stdout.
	// See internal/format/snapshot.go for the rendering and analysis logic.
	format.FormatSnapshot(os.Stdout, results)
}

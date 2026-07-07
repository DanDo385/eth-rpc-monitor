// =============================================================================
// FILE: internal/cli/snapshot.go
// ROLE: Fork Detection Workflow — Comparing Block Data Across Providers
// =============================================================================
//
// This is the workflow logic for the `snapshot` subcommand, which performs a
// point-in-time comparison of block data across all configured providers.
// Unlike `test` (which measures latency over many samples), `snapshot` focuses
// on a single question: "Do all providers agree on what the blockchain looks
// like right now?"
//
// Usage examples:
//   snapshot                ← Compare latest block across all providers
//   snapshot 19000000       ← Compare a specific historical block
//   snapshot latest         ← Explicit "latest" (same as no argument)
//
// EXECUTION FLOW
// ==============
//
//   RunSnapshot(cfg, blockArg)
//      │
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
// This is the simplest workflow in the suite — it's entirely contained in
// RunSnapshot with no helper functions. This is intentional: the logic is
// linear enough that extracting functions would add indirection without
// improving clarity.
//
// CS CONCEPTS IN THIS FILE
// =========================
// 1. CONCURRENT DATA COLLECTION with errgroup
// 2. CONTEXT WITH TIMEOUT for bounded execution
// 3. MUTEX-PROTECTED shared state
// 4. NULL/NIL CHECKING for pointer safety (block != nil)
// 5. VARIABLE SHADOWING for goroutine closure safety
// =============================================================================

package cli

import (
	"context"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// RunSnapshot fetches the same block from every configured provider
// concurrently and renders a comparison that flags any disagreement.
//
// OVERALL STRATEGY
// ================
// Fetch the SAME block from EVERY provider, then compare the results.
// If all providers return the same hash for the same block number, they agree
// on the state of the chain. Any disagreement is a red flag.
//
// BLOCK ARGUMENT
// ==============
// blockArg is NOT normalized here (no decimal→hex conversion). If the user
// passes a decimal number, the Ethereum RPC will likely return an error. This
// is acceptable for a focused tool — use the `block` subcommand for flexible
// block identification.
//
// CONTEXT
// =======
// context.WithTimeout(parent, cfg.Defaults.Timeout*2) bounds the total time
// we'll wait for all providers. The extra headroom accounts for the warm-up
// call + the actual fetch. ctx is passed to errgroup.WithContext, creating a
// derived context (gctx) that is ALSO cancelled if any goroutine returns an
// error — a cancellation hierarchy:
//
//	context.Background()
//	    └─ ctx (timeout after N seconds)
//	        └─ gctx (also cancelled if errgroup detects an error)
//	            └─ Each HTTP request uses gctx for cancellation
func RunSnapshot(cfg *config.Config, blockArg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	// Pre-allocate the results slice — one slot per provider.
	// Each goroutine writes to its own index (results[i]), protected by mu.
	results := make([]format.SnapshotResult, len(cfg.Providers))
	var mu sync.Mutex

	// Create errgroup with derived context.
	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		// LOOP VARIABLE SHADOWING: i, p := i, p
		// Creates new variables local to this iteration, captured by the closure.
		i, p := i, p
		g.Go(func() error {
			// Create a client for this provider.
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)

			// WARM-UP CALL: Prime the HTTP connection. The result is discarded
			// — purely to establish TCP/TLS so GetBlock measures only RPC
			// latency, not connection setup overhead.
			client.BlockNumber(gctx)

			// Fetch the target block from this provider.
			// block is a *rpc.Block — a POINTER to the block data. This pointer
			// could be nil if the block doesn't exist or the response was
			// malformed, even when err is nil.
			block, latency, err := client.GetBlock(gctx, blockArg)

			// Build the result struct.
			r := format.SnapshotResult{Provider: p.Name, Latency: latency, Error: err}

			// POINTER NIL CHECK: err == nil && block != nil
			// ==============================================
			// Two conditions:
			//   1. err == nil    → the RPC call succeeded
			//   2. block != nil  → the response contained actual block data
			//
			// Why check BOTH? Because GetBlock returns (*Block, error):
			//   - If err != nil, the call failed entirely.
			//   - If err == nil but block == nil, the call succeeded but the
			//     block doesn't exist (e.g., requesting a future block number).
			//
			// Dereferencing a nil block would cause a nil pointer panic.
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

	// Wait for ALL goroutines to complete. After g.Wait() returns, all results
	// are populated and the mutex is no longer needed.
	g.Wait()

	// FormatSnapshot displays the comparison table and detects mismatches.
	format.FormatSnapshot(os.Stdout, results)
	return nil
}

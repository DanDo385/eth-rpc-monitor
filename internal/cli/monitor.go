// =============================================================================
// FILE: internal/cli/monitor.go
// ROLE: Continuous Monitoring Workflow — Real-Time Provider Dashboard
// =============================================================================
//
// This is the workflow logic for the `monitor` subcommand, the only LONG-RUNNING
// tool in the suite. While the other workflows execute once and exit, monitor
// runs indefinitely — refreshing the dashboard every N seconds until the user
// presses Ctrl+C.
//
// Usage examples:
//   monitor                  ← Refresh every 30s (default from config)
//   monitor --interval 10s   ← Refresh every 10 seconds
//   monitor --interval 5s    ← Refresh every 5 seconds (aggressive)
//
// EXECUTION FLOW
// ==============
//
//   RunMonitor(cfg, interval)
//      │
//      ├─ Set up cancellable context
//      ├─ Set up signal handler (Ctrl+C → cancel)
//      ├─ Create ticker (fires every N seconds)
//      │
//      ├─ Initial fetch + display (immediate first render)
//      │
//      └─ Event loop (for { select { ... } }):
//          │
//          ├─ case <-ticker.C:    → Fetch + display (periodic refresh)
//          └─ case <-ctx.Done():  → Clean exit (user pressed Ctrl+C)
//
// CS CONCEPTS IN THIS FILE
// =========================
// 1. EVENT LOOPS: The select{} statement as a multiplexed event dispatcher
// 2. SIGNAL HANDLING: OS signals (SIGINT, SIGTERM) as cancellation triggers
// 3. CHANNELS: Go channels for inter-goroutine communication
// 4. CONTEXT CANCELLATION: Cooperative shutdown propagation
// 5. CLOSURES: The displayResults function capturing mutable state
// 6. TICKERS: Periodic event generation with time.Ticker
//
// SIGNAL HANDLING AND GRACEFUL SHUTDOWN
// ======================================
// When the user presses Ctrl+C, the OS sends SIGINT. signal.Notify() routes
// these signals to a channel, and a dedicated goroutine calls cancel() on the
// context, which:
//  1. Causes ctx.Done() to close (unblocking the select case)
//  2. Causes all in-flight HTTP requests to abort (via context propagation)
//  3. Triggers the event loop to exit cleanly
//
// This is COOPERATIVE cancellation — each component checks the context and
// stops voluntarily. No resources are leaked, no goroutines are orphaned.
// =============================================================================

package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// =============================================================================
// SECTION 1: Provider Polling — One Cycle of Data Collection
// =============================================================================

// FetchAllProviders queries every configured provider concurrently and returns
// their block heights and latencies.
//
// This function is called ONCE PER CYCLE in the monitoring loop. Each call
// represents one "frame" of the dashboard — a snapshot of all providers at
// approximately the same time. It is the testable extraction from RunMonitor
// (RunMonitor itself runs forever, so it is not unit-testable).
//
// PARAMETERS
// ==========
//   - ctx context.Context: The cancellable context from RunMonitor.
//     If the user presses Ctrl+C, this context is cancelled, which causes
//     all in-flight HTTP requests to abort immediately.
//   - cfg *config.Config: POINTER to the configuration. The `*` means we
//     receive the memory address; we read cfg.Providers without copying the
//     Config struct (which contains a slice of providers) on every call.
//
// CONCURRENCY MODEL
// =================
//   - errgroup.WithContext(ctx) creates a group of managed goroutines
//   - One goroutine per provider, each making an independent RPC call
//   - sync.Mutex protects writes to the shared results slice
//   - g.Wait() blocks until all goroutines complete
//
// Each goroutine creates its OWN rpc.Client. This is deliberate:
//   - No connection reuse across cycles (simpler, no stale state)
//   - Each measurement includes connection setup (realistic end-to-end latency)
//   - No shared mutable state between providers
func FetchAllProviders(ctx context.Context, cfg *config.Config) []format.WatchResult {
	results := make([]format.WatchResult, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p // Shadow loop variables for goroutine safety
		g.Go(func() error {
			// Create a fresh client for this provider.
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)

			// Query the provider's latest block number.
			// gctx carries cancellation — if the context is cancelled
			// (user pressed Ctrl+C), this HTTP request aborts immediately.
			height, latency, err := client.BlockNumber(gctx)

			// Build the result struct with all collected data.
			// If err is non-nil, height and latency are zero values, and
			// FormatMonitor will display "ERROR" instead of data.
			r := format.WatchResult{
				Provider:    p.Name,
				BlockHeight: height,
				Latency:     latency,
				Error:       err,
			}

			// Write to the shared results slice under mutex protection.
			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}

	g.Wait()
	return results
}

// =============================================================================
// SECTION 2: The Monitoring Loop — Event-Driven Dashboard Refresh
// =============================================================================

// RunMonitor starts the continuous monitoring loop and handles graceful shutdown.
//
// It combines several concurrent programming patterns:
//  1. CONTEXT CANCELLATION for cooperative shutdown
//  2. SIGNAL HANDLING for Ctrl+C detection
//  3. TICKER-BASED TIMING for periodic refresh
//  4. SELECT-BASED EVENT LOOP for multiplexed event handling
//  5. CLOSURE for stateful rendering (firstDisplay tracking)
//
// PARAMETER: cfg *config.Config
// =============================
// A POINTER to the Config. Passed through to FetchAllProviders on every cycle.
// The Config is never modified after loading — it's effectively immutable
// during the monitor's lifetime.
func RunMonitor(cfg *config.Config, intervalOverride time.Duration) error {
	// Determine the polling interval.
	// Flag override takes precedence over the config default.
	interval := cfg.Defaults.WatchInterval
	if intervalOverride > 0 {
		interval = intervalOverride
	}

	// --- Context Setup ---
	// context.WithCancel creates a context that can be manually cancelled.
	// Unlike WithTimeout (used in block and snapshot), this context has NO
	// automatic deadline — it runs until cancel() is explicitly called.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Signal Handling ---
	// make(chan os.Signal, 1) creates a BUFFERED channel with capacity 1.
	// The buffer ensures we don't miss the first Ctrl+C if the receiver hasn't
	// started yet (without a buffer, signal.Notify drops signals when full).
	//
	// signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM) routes SIGINT and
	// SIGTERM to this channel instead of using Go's default behavior
	// (immediate exit with stack trace), enabling graceful cleanup.
	//
	// The goroutine is a dedicated listener: it blocks on <-sigCh, and when a
	// signal arrives it calls cancel() to trigger shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
		cancel()
	}()

	// --- Ticker Setup ---
	// time.NewTicker(interval) creates a Ticker that sends the current time
	// on its channel every `interval`. defer ticker.Stop() ensures the ticker
	// is cleaned up when RunMonitor returns (without Stop(), the ticker
	// goroutine would continue running — a goroutine leak).
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// --- Display Logic ---
	// CLOSURE: displayResults captures `firstDisplay` by reference.
	//   State 1 (firstDisplay=true):  render WITHOUT clearing → transition to State 2
	//   State 2 (firstDisplay=false): render WITH clearing (stays in State 2)
	//
	// Why not use a method with state on a struct? Because this state is
	// trivial (one boolean) and scoped to RunMonitor only. A closure is the
	// simplest solution — no struct definition, no constructor.
	firstDisplay := true
	displayResults := func(results []format.WatchResult) {
		format.FormatMonitor(os.Stdout, results, interval, !firstDisplay)
		firstDisplay = false
	}

	// --- Initial Fetch and Display ---
	// Perform the first data fetch IMMEDIATELY (don't wait for the first tick)
	// to give the user instant feedback when they start the monitor.
	results := FetchAllProviders(ctx, cfg)
	displayResults(results)

	// --- Event Loop ---
	// `for { select { ... } }` is Go's event loop pattern. select blocks until
	// one of the cases fires:
	//   1. <-ctx.Done(): context cancelled (Ctrl+C or SIGTERM) → exit gracefully
	//   2. <-ticker.C:   ticker fired (N seconds passed) → fetch + display
	//
	// The ctx.Err() check in the ticker case is a guard: after cancel() is
	// called, both ctx.Done() and ticker.C might be ready simultaneously.
	// If select picks the ticker case first, ctx.Err() catches it and skips
	// the fetch (continue → back to select, which then picks ctx.Done()).
	for {
		select {
		case <-ctx.Done():
			// Graceful exit: clear screen and print farewell.
			// "\033[2J\033[H" clears the screen (see format/monitor.go).
			fmt.Print("\033[2J\033[H")
			fmt.Println("Exiting...")
			return nil

		case <-ticker.C:
			// Guard against processing ticks after cancellation.
			if ctx.Err() != nil {
				continue
			}

			// Fetch fresh data from all providers and update the display.
			// The `results` here is a NEW local variable (`:=`), shadowing the
			// outer `results`. Each cycle gets its own fresh slice.
			results := FetchAllProviders(ctx, cfg)
			displayResults(results)
		}
	}
}

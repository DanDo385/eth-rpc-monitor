// =============================================================================
// FILE: cmd/monitor/main.go
// ROLE: Continuous Monitoring Command — Real-Time Provider Dashboard
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This is the entry point for the `monitor` command, the only LONG-RUNNING
// tool in the suite. While the other commands (block, test, snapshot) execute
// once and exit, monitor runs indefinitely — refreshing the dashboard every
// N seconds until the user presses Ctrl+C.
//
// Usage examples:
//   monitor                  ← Refresh every 30s (default from config)
//   monitor --interval 10s   ← Refresh every 10 seconds
//   monitor --interval 5s    ← Refresh every 5 seconds (aggressive)
//
// EXECUTION FLOW
// ==============
//
//   1. main()
//      │
//      ├─ config.LoadEnv()          ← Load .env file
//      ├─ flag.Parse()              ← Parse --config, --interval flags
//      ├─ config.Load(*cfgPath)     ← Read providers.yaml
//      └─ runMonitor(cfg, interval) ← Start the monitoring loop
//           │
//           ├─ Set up cancellable context
//           ├─ Set up signal handler (Ctrl+C → cancel)
//           ├─ Create ticker (fires every N seconds)
//           │
//           ├─ Initial fetch + display (immediate first render)
//           │
//           └─ Event loop (for { select { ... } }):
//               │
//               ├─ case <-ticker.C:    → Fetch + display (periodic refresh)
//               └─ case <-ctx.Done():  → Clean exit (user pressed Ctrl+C)
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
// THE EVENT LOOP MODEL
// =====================
// Most monitoring tools follow the same fundamental pattern:
//
//   while true:
//       if user_quit: exit
//       if timer_fired: do_work(); display_results()
//
// In Go, this is expressed with `for { select { ... } }`:
//
//   for {
//       select {
//       case <-ctx.Done():      ← "user quit" event
//           return
//       case <-ticker.C:        ← "timer fired" event
//           results := fetch()
//           display(results)
//       }
//   }
//
// The `select` statement blocks until ONE of the cases is ready, then
// executes that case. It's like a multiplexed event dispatcher — whichever
// event fires first gets handled. This is far more efficient than polling
// (busy-waiting), because the goroutine is truly asleep until an event
// arrives.
//
// SIGNAL HANDLING AND GRACEFUL SHUTDOWN
// ======================================
// When the user presses Ctrl+C, the operating system sends SIGINT to the
// process. Go's signal.Notify() routes these signals to a channel, and a
// dedicated goroutine listens on that channel. When a signal arrives, the
// goroutine calls cancel() on the context, which:
//
//   1. Causes ctx.Done() to close (unblocking the select case)
//   2. Causes all in-flight HTTP requests to abort (via context propagation)
//   3. Triggers the event loop to exit cleanly
//
// This is COOPERATIVE cancellation — each component checks the context and
// stops voluntarily. No resources are leaked, no goroutines are orphaned.
//
// Signal flow:
//
//   User presses Ctrl+C
//       │
//       ▼
//   OS sends SIGINT to process
//       │
//       ▼
//   signal.Notify routes SIGINT to sigCh channel
//       │
//       ▼
//   Signal goroutine receives from sigCh
//       │
//       ▼
//   cancel() is called
//       │
//       ├──▶ ctx.Done() channel closes ──▶ Event loop exits
//       └──▶ gctx propagates cancellation ──▶ HTTP requests abort
// =============================================================================

package main

import (
	"context"
	"flag"
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

// fetchAllProviders queries every configured provider concurrently and returns
// their block heights and latencies.
//
// This function is called ONCE PER CYCLE in the monitoring loop. Each call
// represents one "frame" of the dashboard — a snapshot of all providers at
// approximately the same time.
//
// PARAMETERS
// ==========
// - ctx context.Context: The cancellable context from runMonitor.
//   If the user presses Ctrl+C, this context is cancelled, which causes
//   all in-flight HTTP requests to abort immediately.
//
// - cfg *config.Config: POINTER to the configuration.
//   The `*` means we receive the memory address. We read cfg.Providers
//   to know which providers to query. The pointer avoids copying the
//   Config struct (which contains a slice of providers) on every call.
//
// CONCURRENCY MODEL
// =================
// Same pattern as selectFastestProvider in cmd/block/main.go:
//   - errgroup.WithContext(ctx) creates a group of managed goroutines
//   - One goroutine per provider, each making an independent RPC call
//   - sync.Mutex protects writes to the shared results slice
//   - g.Wait() blocks until all goroutines complete
//
// Each goroutine creates its OWN rpc.Client. This is deliberate:
//   - No connection reuse across cycles (simpler, no stale state)
//   - Each measurement includes connection setup (realistic latency)
//   - No shared mutable state between providers
//
// LOOP VARIABLE SHADOWING: i, p := i, p
// =======================================
// The comment in the original code explains it well: shadowing prevents
// the "loop variable captured by func literal" bug. Each goroutine gets
// its own copy of i and p. See cmd/block/main.go for the detailed memory
// walkthrough.
func fetchAllProviders(ctx context.Context, cfg *config.Config) []format.WatchResult {
	results := make([]format.WatchResult, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p // Shadow loop variables for goroutine safety
		g.Go(func() error {
			// Create a fresh client for this provider.
			// rpc.NewClient returns *rpc.Client — a pointer to the heap-
			// allocated Client struct. See rpc/client.go for details.
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)

			// Query the provider's latest block number.
			// gctx carries cancellation — if the context is cancelled
			// (user pressed Ctrl+C), this HTTP request aborts immediately.
			height, latency, err := client.BlockNumber(gctx)

			// Build the result struct with all collected data.
			// If err is non-nil, height and latency are zero values,
			// and FormatMonitor will display "ERROR" instead of data.
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

// runMonitor starts the continuous monitoring loop and handles graceful shutdown.
//
// This is the most architecturally interesting function in the codebase.
// It combines several concurrent programming patterns:
//   1. CONTEXT CANCELLATION for cooperative shutdown
//   2. SIGNAL HANDLING for Ctrl+C detection
//   3. TICKER-BASED TIMING for periodic refresh
//   4. SELECT-BASED EVENT LOOP for multiplexed event handling
//   5. CLOSURE for stateful rendering (firstDisplay tracking)
//
// PARAMETER: cfg *config.Config
// =============================
// A POINTER to the Config. The `*` means cfg holds an address, not the
// entire Config struct. This pointer is passed through to fetchAllProviders
// on every cycle, which reads cfg.Providers to know which providers to query.
//
// The Config is never modified after loading — it's effectively immutable
// during the monitor's lifetime. Using a pointer here is primarily for
// consistency and efficiency.
func runMonitor(cfg *config.Config, intervalOverride time.Duration) error {
	// Determine the polling interval.
	// Flag override takes precedence over the config default.
	interval := cfg.Defaults.WatchInterval
	if intervalOverride > 0 {
		interval = intervalOverride
	}

	// --- Context Setup ---
	//
	// context.WithCancel creates a context that can be manually cancelled.
	// Unlike WithTimeout (used in block and snapshot), this context has
	// NO automatic deadline — it runs until cancel() is explicitly called.
	//
	// ctx is passed to fetchAllProviders, which passes it to each HTTP request.
	// When cancel() is called (on Ctrl+C), all in-flight requests abort.
	//
	// defer cancel() ensures resources are released when runMonitor returns,
	// even if an error occurs. This is a safety net — cancel is also called
	// by the signal handler goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Signal Handling ---
	//
	// make(chan os.Signal, 1) creates a BUFFERED channel with capacity 1.
	//
	// CHANNELS IN GO
	// ==============
	// A channel is a typed conduit for communication between goroutines.
	// Think of it as a thread-safe queue:
	//
	//   Sender ──▶ [ channel ] ──▶ Receiver
	//
	// The buffer size (1) means the channel can hold one signal before
	// blocking the sender. This is important because:
	//   - The OS delivers signals asynchronously
	//   - If the channel is full, signal.Notify drops the signal
	//   - A buffer of 1 ensures we don't miss the first Ctrl+C
	//
	// signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM) tells Go's
	// runtime: "Route SIGINT (Ctrl+C) and SIGTERM to this channel."
	// Without this, SIGINT would use Go's default behavior (immediate exit
	// with stack trace), which doesn't allow graceful cleanup.
	//
	// The goroutine `go func() { sig := <-sigCh; cancel() }()` is a
	// dedicated listener that:
	//   1. Blocks on <-sigCh (waits for a signal)
	//   2. When a signal arrives, calls cancel() to trigger shutdown
	//
	// `sig := <-sigCh` is a CHANNEL RECEIVE operation:
	//   - <-sigCh reads one value from the channel
	//   - It BLOCKS the goroutine until a value is available
	//   - sig receives the os.Signal value (e.g., os.Interrupt)
	//
	// In memory:
	//
	//   Main goroutine                Signal goroutine
	//   ┌───────────────┐             ┌─────────────────────┐
	//   │ Event loop    │             │ Blocked on <-sigCh  │
	//   │ (select{})    │             │                     │
	//   └───────────────┘             └─────────────────────┘
	//                                       │
	//   User presses Ctrl+C ────────────────▶ sig = os.Interrupt
	//                                       │
	//                                       ▼
	//                                 cancel() called
	//                                       │
	//                                       ▼
	//                                 ctx.Done() channel closes
	//                                       │
	//   ┌───────────────┐                   │
	//   │ select picks  │ ◀─────────────────┘
	//   │ ctx.Done()    │
	//   │ → exit loop   │
	//   └───────────────┘
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		fmt.Fprintf(os.Stderr, "\nReceived signal: %v\n", sig)
		cancel()
	}()

	// --- Ticker Setup ---
	//
	// time.NewTicker(interval) creates a Ticker that sends the current time
	// on its channel (ticker.C) every `interval` duration.
	//
	// Unlike time.After (one-shot), Ticker is REPEATING:
	//
	//   Time: 0s    30s    60s    90s    120s
	//         │      │      │      │      │
	//         │      ▼      ▼      ▼      ▼
	//         │   tick    tick    tick    tick
	//         │   (fires) (fires) (fires) (fires)
	//
	// ticker.C is a <-chan time.Time (receive-only channel of time.Time).
	// Reading from it (<-ticker.C in the select) blocks until the next tick.
	//
	// defer ticker.Stop() ensures the ticker is cleaned up when runMonitor
	// returns. Without Stop(), the ticker goroutine would continue running
	// (and the channel would continue filling) — a goroutine leak.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// --- Display Logic ---
	//
	// CLOSURE: displayResults captures `firstDisplay` by reference.
	//
	// A closure is a function that "closes over" (captures) variables from
	// its enclosing scope. Here, displayResults captures `firstDisplay`:
	//
	//   firstDisplay is declared as `true` (first render shouldn't clear screen)
	//   displayResults checks !firstDisplay to decide whether to clear
	//   After first call, displayResults sets firstDisplay = false
	//
	// This is equivalent to a small state machine:
	//   State 1 (firstDisplay=true):  render WITHOUT clearing → transition to State 2
	//   State 2 (firstDisplay=false): render WITH clearing (stays in State 2)
	//
	// Why not use a method with state on a struct? Because this state is
	// trivial (one boolean) and scoped to runMonitor only. A closure is
	// the simplest solution — no struct definition, no constructor, just a
	// function that remembers one variable.
	firstDisplay := true
	displayResults := func(results []format.WatchResult) {
		format.FormatMonitor(os.Stdout, results, interval, !firstDisplay)
		firstDisplay = false
	}

	// --- Initial Fetch and Display ---
	//
	// Perform the first data fetch IMMEDIATELY (don't wait for the first tick).
	// This gives the user instant feedback when they start the monitor.
	results := fetchAllProviders(ctx, cfg)
	displayResults(results)

	// --- Event Loop ---
	//
	// `for { select { ... } }` is Go's event loop pattern.
	//
	// The `for` loop runs forever (no condition). Inside, `select` blocks
	// until one of the two cases fires:
	//
	//   1. <-ctx.Done(): The context was cancelled (Ctrl+C or SIGTERM).
	//      Action: Clear the screen and exit gracefully.
	//
	//   2. <-ticker.C: The ticker fired (N seconds have passed).
	//      Action: Fetch fresh data and update the display.
	//
	// SELECT SEMANTICS
	// ================
	// - select blocks until at LEAST one case is ready
	// - If multiple cases are ready simultaneously, Go picks one at random
	// - There is no priority between cases — both are equally likely
	// - This means a tick might be processed even after cancellation,
	//   which is why we check ctx.Err() != nil inside the tick handler
	//
	// The ctx.Err() check in the ticker case is a guard:
	//   - After cancel() is called, both ctx.Done() and ticker.C might be
	//     ready simultaneously (the ticker doesn't stop until defer runs)
	//   - If select picks the ticker case first, ctx.Err() catches it and
	//     skips the fetch (continue goes back to select, which then picks
	//     ctx.Done())
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
			// The `results` here is a NEW local variable (`:=`), shadowing
			// the outer `results`. Each cycle gets its own fresh slice.
			results := fetchAllProviders(ctx, cfg)
			displayResults(results)
		}
	}
}

// =============================================================================
// SECTION 3: Entry Point
// =============================================================================
//
// main() follows the same pattern as the other commands:
//   1. Load .env environment variables
//   2. Parse command-line flags (returns pointers)
//   3. Load YAML configuration
//   4. Delegate to the command function (runMonitor)
//
// FLAG: flag.Duration
// ===================
// flag.Duration is like flag.String, but parses duration strings:
//   "10s"  → 10 * time.Second
//   "500ms" → 500 * time.Millisecond
//   "1m"   → 1 * time.Minute
//   "0"    → 0 (meaning "use config default")
//
// It returns *time.Duration (a pointer to time.Duration).
// *interval dereferences it to get the actual time.Duration value.
// =============================================================================

func main() {
	config.LoadEnv()

	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		interval = flag.Duration("interval", 0, "Refresh interval (0 = use config default)")
	)

	flag.Parse()

	// *cfgPath dereferences the *string pointer to get the config file path.
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// *interval dereferences the *time.Duration pointer to get the duration value.
	if err := runMonitor(cfg, *interval); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

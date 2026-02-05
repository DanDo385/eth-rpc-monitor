// =============================================================================
// FILE: internal/format/monitor.go
// ROLE: Live Dashboard Renderer — Continuous Monitoring Display
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file renders the real-time monitoring dashboard for the `monitor`
// command. Unlike the other format files (which render once and exit), this
// renderer is called repeatedly — every N seconds (configurable interval) —
// to update the terminal with fresh data.
//
// The key challenge here is SCREEN MANAGEMENT: each update needs to replace
// the previous display, not append below it. This is accomplished using ANSI
// escape codes to clear the screen and move the cursor.
//
// DATA FLOW
// =========
//
//   cmd/monitor/main.go (event loop)
//       │
//       │  Every N seconds: fetchAllProviders()
//       │  Returns []WatchResult
//       │
//       ▼
//   FormatMonitor() (THIS FILE)  ← called repeatedly
//       │
//       │  1. Clear screen (if not first render)
//       │  2. Find highest block across providers
//       │  3. Render table with lag calculations
//       │
//       ▼
//   Terminal:
//   ┌──────────────────────────────────────────────────────┐
//   │ Monitoring 4 providers (interval: 30s, Ctrl+C to exit)│
//   │                                                       │
//   │ Provider       Block Height   Latency   Lag           │
//   │ ─────────────────────────────────────────             │
//   │ alchemy           21234567      43ms    —             │
//   │ infura            21234567      39ms    —             │
//   │ llamanodes        21234566     167ms    -1            │
//   │ publicnode        21234567     142ms    —             │
//   └──────────────────────────────────────────────────────┘
//      ↑ This entire display is REPLACED each cycle
//
// CS CONCEPTS: TERMINAL CONTROL WITH ESCAPE CODES
// ================================================
// Modern terminals are stateful devices. They maintain:
//   - A cursor position (row, column)
//   - A screen buffer (grid of characters)
//   - Text attributes (color, bold, etc.)
//
// We control the terminal by writing special byte sequences:
//
//   \033[2J   → "Erase entire screen" (ED — Erase in Display)
//   \033[H    → "Move cursor to top-left corner" (CUP — Cursor Position)
//
// The \033 is the octal representation of the ESC character (0x1B).
// Together, "\033[2J\033[H" clears the screen and resets the cursor,
// creating the effect of a "refresh" — the new output replaces the old.
//
// This is the same mechanism used by programs like `top`, `htop`, and
// terminal-based dashboards. The alternative (printing new lines each cycle)
// would cause the output to scroll endlessly, making it unreadable.
//
// WHY NOT clearScreen ON FIRST RENDER?
// =====================================
// On the first render, we don't clear the screen because:
//   1. The user might have context from previous commands visible
//   2. Clearing before the first data is ready would show a blank screen
//   3. Only subsequent renders need to "replace" the previous display
//
// WHAT A READER SHOULD UNDERSTAND
// ================================
// 1. How terminal escape codes work for screen management
// 2. The concept of "lag" as relative block height difference
// 3. How this renderer is called in a loop vs. the one-shot renderers
// =============================================================================

package format

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// =============================================================================
// SECTION 1: Watch Result Type
// =============================================================================

// WatchResult holds the outcome of a single monitoring cycle for one provider.
//
// This struct is simpler than SnapshotResult because the monitor only tracks
// block HEIGHT (not hash) — it's designed for speed, not deep analysis.
// For fork detection, use the `snapshot` command instead.
//
// ERROR FIELD
// ===========
// Like SnapshotResult, the Error field uses the `error` interface.
// When Error is non-nil, BlockHeight and Latency are unreliable (zero values).
// FormatMonitor checks this field and renders "ERROR" in red instead of data.
type WatchResult struct {
	Provider    string        // Provider name
	BlockHeight uint64        // Latest block number from this provider
	Latency     time.Duration // Round-trip time for the eth_blockNumber call
	Error       error         // nil on success; non-nil on failure
}

// =============================================================================
// SECTION 2: Monitor Dashboard Rendering
// =============================================================================

// FormatMonitor renders a live monitoring dashboard to the terminal.
//
// This function is called repeatedly by the monitor's event loop. Each call
// renders a complete "frame" of the dashboard, optionally clearing the
// previous frame first.
//
// PARAMETERS
// ==========
// - w io.Writer: Output destination (typically os.Stdout for the terminal)
//
// - results []WatchResult: Current polling results from all providers.
//   This is a fresh slice created by fetchAllProviders() each cycle.
//
// - interval time.Duration: The polling interval, displayed for user context.
//
// - clearScreen bool: Whether to clear the terminal before rendering.
//   False on first call (don't erase the user's previous terminal content).
//   True on subsequent calls (replace the previous dashboard frame).
//
// LAG CALCULATION
// ===============
// "Lag" measures how many blocks behind the network leader each provider is:
//
//   lag = (highest block seen across all providers) - (this provider's block)
//
// This is a RELATIVE metric, not absolute. We don't know the "true" network
// tip — we only know what our configured providers report. The provider
// with the highest block becomes our reference point (lag = 0).
//
// Example:
//   alchemy:     block 21234567 → lag = 0 (highest)
//   infura:      block 21234567 → lag = 0 (tied)
//   llamanodes:  block 21234566 → lag = 1 (one block behind)
//   publicnode:  block 21234565 → lag = 2 (two blocks behind)
//
// Lag is inherently approximate because:
//   1. Each provider is queried at slightly different times
//   2. Blocks propagate across the network over ~1-2 seconds
//   3. Our "highest" might not be the true network tip
//
// Despite these limitations, persistent lag (>2 blocks over multiple cycles)
// reliably indicates a provider is struggling to stay synchronized.
func FormatMonitor(w io.Writer, results []WatchResult, interval time.Duration, clearScreen bool) {
	// Clear the screen if this isn't the first render.
	//
	// "\033[2J" — Erase entire screen:
	//   \033 = ESC character (0x1B)
	//   [2J  = "Erase in Display" command, mode 2 (entire screen)
	//
	// "\033[H" — Move cursor to position (1,1) (top-left):
	//   [H   = "Cursor Position" with no parameters (defaults to row 1, col 1)
	//
	// Together, these two codes create a "screen refresh" effect.
	// The old content is erased, and new content is drawn from the top.
	if clearScreen {
		fmt.Fprint(w, "\033[2J\033[H")
	}

	// --- Find the highest block across all providers ---
	//
	// This establishes the reference point for lag calculations.
	// We only consider providers that responded successfully (Error == nil).
	//
	// `var highest uint64` initializes to 0 (the zero value for uint64).
	// Since block numbers are always positive (block 0 is the genesis block,
	// and the current mainnet is at ~21 million), any successful provider
	// will update this value.
	var highest uint64
	for _, r := range results {
		if r.Error == nil && r.BlockHeight > highest {
			highest = r.BlockHeight
		}
	}

	// Render the dashboard header with interval info.
	// The Dim() wrapper makes the interval and exit instruction secondary
	// to the main "Monitoring N providers" message.
	fmt.Fprintf(w, "Monitoring %d providers %s\n\n",
		len(results),
		Dim(fmt.Sprintf("(interval: %s, Ctrl+C to exit)", interval)))

	// Render the column headers.
	fmt.Fprintf(w, "%s %s %s %s\n",
		Bold(fmt.Sprintf("%-14s", "Provider")),
		Bold(fmt.Sprintf("%12s", "Block Height")),
		Bold(fmt.Sprintf("%7s", "Latency")),
		Bold(fmt.Sprintf("%3s", "Lag")))
	fmt.Fprintln(w, strings.Repeat("─", 60))

	// Render one row per provider.
	for _, r := range results {
		if r.Error != nil {
			// Provider failed — show ERROR in red with dashes for missing data.
			// The `continue` keyword skips the rest of this iteration and
			// moves to the next provider. This avoids nested if/else blocks.
			fmt.Fprintf(w, "%-14s %12s %7s %3s\n",
				r.Provider,
				padRight(Red("ERROR"), 12),
				padRight(Dim("—"), 7),
				padRight(Dim("—"), 3))
			continue
		}

		// Calculate lag relative to the highest observed block.
		// Since `highest` is the maximum and `r.BlockHeight` is at most equal
		// to `highest`, this subtraction is safe (no underflow for uint64).
		lag := highest - r.BlockHeight
		fmt.Fprintf(w, "%-14s %12d %7s %3s\n",
			r.Provider,
			r.BlockHeight,
			padRight(ColorLatency(r.Latency.Milliseconds()), 7),
			padRight(ColorLag(lag), 3))
	}
	fmt.Fprintln(w)
}

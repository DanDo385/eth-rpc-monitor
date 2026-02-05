// =============================================================================
// FILE: internal/format/colors.go
// ROLE: Terminal Presentation Layer — Color Coding and ANSI String Handling
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file provides the visual language of the monitoring tool. When you see
// green latency numbers (fast), yellow numbers (moderate), or red numbers
// (slow) in the terminal, those colors are produced by functions in this file.
//
// Every other file in the format package imports and uses these color functions.
// They're the building blocks for all visual output.
//
// ARCHITECTURE POSITION
// =====================
//
//   internal/format/block.go    ──┐
//   internal/format/test.go     ──┤
//   internal/format/snapshot.go ──┤──▶ colors.go (THIS FILE)
//   internal/format/monitor.go  ──┘         │
//                                           ▼
//                                     Terminal (stdout)
//
// CS CONCEPTS: ANSI ESCAPE CODES
// ===============================
// Modern terminals support colored text via ANSI escape codes — special byte
// sequences that instruct the terminal to change text appearance. They were
// standardized in the 1970s for VT100 terminals and remain the universal
// mechanism for terminal styling.
//
// An ANSI escape code starts with ESC[ (bytes 0x1B 0x5B) followed by
// formatting parameters and a letter code:
//
//   \x1b[32m   → Set text color to green (code 32)
//   \x1b[0m    → Reset all formatting
//   \x1b[1m    → Bold text
//   \x1b[2m    → Dim/faint text
//
// So the string "\x1b[32m45ms\x1b[0m" renders as green "45ms" in the terminal,
// but is actually 16 bytes long (not 4). This discrepancy between byte length
// and visible length creates the padding problem solved by stripANSI and padRight.
//
// THE PADDING PROBLEM
// ===================
// When building aligned columns in terminal output:
//
//   Provider       Latency
//   ─────────────────────
//   alchemy        45ms      ← green (with ANSI codes: 16 bytes, visible: 4 chars)
//   infura         312ms     ← red   (with ANSI codes: 17 bytes, visible: 5 chars)
//
// If we pad based on byte length (len()), columns won't align because ANSI
// codes add invisible bytes. We need to strip the codes first, measure the
// visible length, then add the right amount of padding.
//
// WHAT A READER SHOULD UNDERSTAND AFTER THIS FILE
// ================================================
// 1. How terminal colors work (ANSI escape codes)
// 2. Why colored strings need special padding logic
// 3. The semantic meaning of each color in the monitoring context
// 4. How Go package-level variables and closures work
// =============================================================================

package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// =============================================================================
// SECTION 1: Color Function Definitions (Package-Level Variables)
// =============================================================================
//
// These package-level variables define our color palette. Each one is a
// FUNCTION (not a color value) that takes a string and returns a colored string.
//
// How color.New(color.FgGreen).SprintFunc() works:
//
//   1. color.New(color.FgGreen) creates a *color.Color with green foreground
//   2. .SprintFunc() returns a CLOSURE — a function that remembers the
//      color configuration and wraps any input string with ANSI codes
//   3. Calling Green("hello") returns "\x1b[32mhello\x1b[0m"
//
// CLOSURES
// ========
// A closure is a function that "closes over" (captures) variables from its
// enclosing scope. SprintFunc() captures the color configuration:
//
//   Green = func(args ...interface{}) string {
//       // Internally, this has access to the FgGreen configuration
//       // that was set up when color.New() was called
//       return "\x1b[32m" + fmt.Sprint(args...) + "\x1b[0m"
//   }
//
// The result type of SprintFunc() is func(a ...interface{}) string — a
// function that takes variadic arguments and returns a string. We store
// these functions in package-level variables so every file in the format
// package can use them: Green("text"), Red("error"), Bold("label"), etc.
//
// SEMANTIC COLOR MEANINGS IN THIS TOOL
// =====================================
//   Green  → Good / healthy / fast (latency < 100ms, 100% success)
//   Yellow → Warning / moderate (100-300ms latency, 80-99% success, 1 block lag)
//   Red    → Critical / slow / failing (> 300ms, < 80% success, > 1 block lag)
//   Bold   → Labels and emphasis
//   Dim    → Secondary information (types, absent values)
// =============================================================================

var (
	Green  = color.New(color.FgGreen).SprintFunc()  // Fast / healthy
	Red    = color.New(color.FgRed).SprintFunc()    // Slow / failing
	Yellow = color.New(color.FgYellow).SprintFunc() // Warning / moderate
	Bold   = color.New(color.Bold).SprintFunc()     // Labels and emphasis
	Dim    = color.New(color.Faint).SprintFunc()    // Secondary info
)

// =============================================================================
// SECTION 2: ANSI-Aware String Utilities
// =============================================================================

// ansiRegex matches ANSI escape code sequences in strings.
//
// The regex pattern: \x1b\[[0-9;]*[a-zA-Z]
//
// Breaking it down:
//   \x1b     → The ESC character (byte 0x1B / decimal 27)
//   \[       → A literal '[' character
//   [0-9;]*  → Zero or more digits and semicolons (the parameters)
//   [a-zA-Z] → A letter that terminates the sequence (e.g., 'm' for color)
//
// This matches sequences like:
//   \x1b[32m     → Green text (parameter: 32, terminator: m)
//   \x1b[1;31m   → Bold red text (parameters: 1;31, terminator: m)
//   \x1b[0m      → Reset all formatting
//
// regexp.MustCompile compiles the regex at program startup (package init time).
// The "Must" means it panics if the regex is invalid — appropriate here because
// this is a hardcoded, known-valid pattern, not user input.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes all ANSI escape codes from a string, leaving only the
// visible characters.
//
// Example:
//   stripANSI("\x1b[32m45ms\x1b[0m") → "45ms"
//   len("\x1b[32m45ms\x1b[0m") = 15     ← byte length (includes ANSI codes)
//   len(stripANSI(...))        = 4      ← visible character count
//
// This is used by padRight to calculate how many spaces to add for alignment.
func stripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// padRight pads a (possibly colored) string with spaces to reach the
// specified visible width.
//
// Why this exists: Go's fmt.Sprintf("%-10s", str) counts bytes, not visible
// characters. For colored strings with embedded ANSI codes, byte count >>
// visible character count, so Sprintf would add too few spaces.
//
// Example:
//   greenStr := Green("45ms")       // "\x1b[32m45ms\x1b[0m" (15 bytes, 4 visible)
//   padRight(greenStr, 10)          // "\x1b[32m45ms\x1b[0m      " (+ 6 spaces)
//   // Renders as: "45ms      " (10 visible characters, aligned)
//
// Algorithm:
//   1. Strip ANSI codes and measure the visible length
//   2. If visible length < desired width, append spaces
//   3. If visible length >= desired width, return unchanged (no truncation)
func padRight(str string, width int) string {
	visibleLen := len(stripANSI(str))
	if visibleLen < width {
		return str + strings.Repeat(" ", width-visibleLen)
	}
	return str
}

// =============================================================================
// SECTION 3: Semantic Color Functions — Domain-Specific Color Coding
// =============================================================================
//
// These functions apply color based on the MEANING of the value, not just
// its type. They encode the monitoring tool's "traffic light" semantics:
// green = good, yellow = caution, red = bad.
// =============================================================================

// ColorLatency applies traffic-light coloring to a latency value in milliseconds.
//
// Color thresholds:
//   < 100ms  → Green  (fast — suitable for production trading)
//   < 300ms  → Yellow (moderate — acceptable for most applications)
//   >= 300ms → Red    (slow — may indicate problems or poor-quality endpoint)
//
// These thresholds are chosen based on practical Ethereum RPC experience:
//   - Premium providers (Alchemy, Infura paid tiers): typically 20-80ms
//   - Free public endpoints: typically 100-500ms
//   - Self-hosted nodes: typically 1-10ms
//
// Returns a string with embedded ANSI color codes for terminal display.
func ColorLatency(ms int64) string {
	switch {
	case ms < 100:
		return Green(fmt.Sprintf("%dms", ms))
	case ms < 300:
		return Yellow(fmt.Sprintf("%dms", ms))
	default:
		return Red(fmt.Sprintf("%dms", ms))
	}
}

// ColorLag applies color coding to a block height lag value.
//
// "Lag" means how many blocks behind the network leader this provider is.
// It's calculated as: (highest block seen across all providers) - (this provider's block).
//
// Color mapping:
//   0 blocks behind → Dim dash ("—") — provider is at the tip, nothing to report
//   1 block behind  → Yellow "-1"    — minor lag, may be normal propagation delay
//   2+ blocks behind → Red "-N"     — significant lag, provider may be stale
//
// In Ethereum, new blocks are produced approximately every 12 seconds.
// Being 1 block behind (12s) is usually normal. Being 2+ blocks behind
// (24s+) suggests the provider is struggling to keep up.
func ColorLag(lag uint64) string {
	if lag == 0 {
		return Dim("—")
	}
	if lag <= 1 {
		return Yellow(fmt.Sprintf("-%d", lag))
	}
	return Red(fmt.Sprintf("-%d", lag))
}

// ColorSuccess applies color coding to a success rate.
//
// The success rate is calculated as (successful requests / total requests) * 100%.
//
// Color mapping:
//   100%    → Green  (perfect reliability)
//   80-99%  → Yellow (some failures, worth investigating)
//   < 80%   → Red    (significant failure rate, provider is unreliable)
//
// The 80% threshold is deliberately conservative. In production trading:
//   - 99.9% uptime is the minimum for paid providers
//   - 95% uptime means ~22 minutes of downtime per day
//   - Below 80% is essentially unusable for anything serious
//
// The format "%.0f%%" produces "100%", "97%", "83%", etc.
// The double %% is needed because % is a special character in format strings.
func ColorSuccess(success, total int) string {
	pct := float64(success) / float64(total) * 100
	str := fmt.Sprintf("%.0f%%", pct)
	switch {
	case pct >= 100:
		return Green(str)
	case pct >= 80:
		return Yellow(str)
	default:
		return Red(str)
	}
}

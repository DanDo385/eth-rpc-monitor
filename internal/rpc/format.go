// =============================================================================
// FILE: internal/rpc/format.go
// ROLE: Data Transformation Layer — Hex Parsing and Human-Readable Formatting
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file provides the utility functions that convert between Ethereum's wire
// representation (hex strings) and Go's native types, as well as functions that
// format those native types into human-readable strings for terminal display.
//
// Think of this file as the "translator" sitting between the raw data world
// (types.go) and the visual presentation world (internal/format/).
//
// Data flows through these functions in two stages:
//
//   Stage 1 — PARSING (hex → numeric):
//     Wire hex string  ──▶  ParseHexUint64()  ──▶  uint64
//     Wire hex string  ──▶  ParseHexBigInt()  ──▶  *big.Int
//
//   Stage 2 — FORMATTING (numeric → display string):
//     uint64           ──▶  FormatNumber()    ──▶  "21,234,567"
//     uint64           ──▶  FormatTimestamp() ──▶  "2024-01-15 14:32:18 UTC (12s ago)"
//     *big.Int         ──▶  FormatGwei()      ──▶  "25.43 gwei"
//
// WHICH FILES DEPEND ON THIS
// ==========================
//   - internal/rpc/types.go    → Parsed() calls ParseHexUint64, ParseHexBigInt
//   - internal/format/block.go → FormatBlock() calls FormatNumber, FormatTimestamp, FormatGwei
//   - cmd/block/main.go        → convertBlockToJSON() calls ParseHexUint64, ParseHexBigInt
//   - cmd/snapshot/main.go     → Calls ParseHexUint64 to convert block heights
//
// CS CONCEPTS: NUMBER BASES AND PRECISION
// ========================================
// Computers represent numbers in binary (base 2). Humans prefer decimal (base 10).
// Ethereum uses hexadecimal (base 16) as a compact representation of binary.
//
//   Decimal: 21,234,567   (base 10, what humans read)
//   Hex:     0x1444F07    (base 16, what Ethereum sends)
//   Binary:  1 0100 0100 0100 1111 0000 0111  (base 2, what the CPU works with)
//
// Why hex? Each hex digit maps to exactly 4 binary digits, making it a convenient
// shorthand for binary values. Two hex digits = 1 byte. This is why memory
// addresses, hashes, and EVM values are all expressed in hex.
//
// PRECISION: Some Ethereum values (like baseFeePerGas in wei) can be very large.
// A uint64 can hold up to 18,446,744,073,709,551,615 (about 18.4 * 10^18).
// Ethereum gas fees in wei can exceed this, so we use math/big.Int which
// provides arbitrary-precision integer arithmetic — it can hold numbers of
// any size, limited only by available memory.
// =============================================================================

package rpc

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// SECTION 1: Hex Parsing — Converting Wire Format to Native Types
// =============================================================================

// ParseHexUint64 converts a hex-encoded string (e.g., "0x1444F3B") to a uint64.
//
// Step-by-step:
//   1. strings.TrimPrefix removes the "0x" prefix, leaving "1444F3B"
//   2. strconv.ParseUint interprets "1444F3B" as base-16, fitting it into 64 bits
//   3. Returns the resulting uint64 value and any parsing error
//
// The third parameter to ParseUint (16) specifies base-16 (hexadecimal).
// The fourth parameter (64) specifies the bit size — the result must fit
// in 64 bits, or an overflow error is returned.
//
// Example:
//   ParseHexUint64("0x1444F3B") → (21,233,467, nil)
//   ParseHexUint64("0xZZZ")    → (0, error)   ← invalid hex
//   ParseHexUint64("")          → (0, error)   ← empty string
func ParseHexUint64(hex string) (uint64, error) {
	return strconv.ParseUint(strings.TrimPrefix(hex, "0x"), 16, 64)
}

// ParseHexBigInt converts a hex-encoded string to a *big.Int (arbitrary precision).
//
// POINTER RETURN TYPE: *big.Int
// =============================
// This function returns *big.Int (a POINTER to big.Int). Here's what happens
// in memory, step by step:
//
//   1. new(big.Int) allocates a big.Int on the HEAP and returns a pointer to it.
//
//      Stack                       Heap
//      ┌─────────────┐            ┌─────────────┐
//      │ val: ────────┼───────────▶│ big.Int     │
//      └─────────────┘            │ value: 0    │
//                                 └─────────────┘
//
//   2. val.SetString("59682F000", 16) parses the hex string and MUTATES the
//      big.Int in place — it doesn't create a new one.
//
//      Stack                       Heap
//      ┌─────────────┐            ┌────────────────┐
//      │ val: ────────┼───────────▶│ big.Int        │
//      └─────────────┘            │ value: 24000...│  ← mutated
//                                 └────────────────┘
//
//   3. return val — returns the POINTER (the address), not the big.Int itself.
//      The caller receives the same address, pointing to the same heap object.
//
// Why use new() instead of a stack-allocated big.Int?
//   - big.Int is designed to be used as a pointer type in Go's math/big package.
//   - All big.Int methods (SetString, Add, Quo, etc.) operate on pointer receivers.
//   - Returning a pointer lets the caller work with it directly without copying.
//
// IMPORTANT: new(big.Int) allocates zeroed memory. If SetString fails (returns
// false), val remains zero — not nil. The caller cannot distinguish "parsing
// failed" from "the value was actually zero" without checking SetString's
// return value. In this codebase, we accept this because the hex strings
// come from trusted Ethereum node responses.
func ParseHexBigInt(hex string) *big.Int {
	val := new(big.Int)
	val.SetString(strings.TrimPrefix(hex, "0x"), 16)
	return val
}

// =============================================================================
// SECTION 2: Display Formatting — Native Types to Human-Readable Strings
// =============================================================================

// FormatTimestamp converts a Unix timestamp (seconds since Jan 1, 1970) into
// a human-readable string with an "ago" suffix.
//
// Example output: "2024-01-15 14:32:18 UTC (12s ago)"
//
// Step-by-step:
//   1. time.Unix(int64(ts), 0) creates a time.Time from the Unix timestamp.
//      The int64() cast is needed because time.Unix takes int64, but our
//      timestamp is uint64. This is safe because Unix timestamps won't
//      exceed int64's max value until the year 292,277,026,596.
//   2. .UTC() converts to UTC timezone (Ethereum uses UTC for consistency).
//   3. time.Since(t) calculates the duration between then and now.
//   4. .Truncate(time.Second) rounds down to whole seconds (removes nanoseconds).
//   5. t.Format("2006-01-02 15:04:05 UTC") formats the time.
//
// GO'S TIME FORMAT REFERENCE DATE
// ================================
// Go uses a unique approach to time formatting: instead of format codes like
// "%Y-%m-%d", it uses a reference date: Mon Jan 2 15:04:05 MST 2006.
// The numbers in this date are chosen to be sequential: 01/02 03:04:05 06.
// When you write "2006-01-02 15:04:05", you're saying "show the year, month,
// day, hour, minute, second in this layout."
func FormatTimestamp(ts uint64) string {
	t := time.Unix(int64(ts), 0).UTC()
	ago := time.Since(t).Truncate(time.Second)
	return fmt.Sprintf("%s (%s ago)", t.Format("2006-01-02 15:04:05 UTC"), ago)
}

// FormatNumber adds thousand separators to a uint64 for readability.
//
// Example: FormatNumber(21234567) → "21,234,567"
//
// Algorithm walkthrough:
//   1. Convert the number to its decimal string representation: "21234567"
//   2. If the string is 3 characters or fewer, return it as-is (no commas needed).
//   3. Walk through the string character by character. Before each character
//      (except the first), check: "If I count from the END of the string back
//      to this position, is it a multiple of 3?" If yes, insert a comma.
//
// The key insight is the expression (len(s) - i) % 3 == 0:
//   - For "21234567" (len=8), at position i=2: (8-2)%3 = 6%3 = 0 → comma before '2'
//   - At position i=5: (8-5)%3 = 3%3 = 0 → comma before '5'
//   - Result: "21,234,567"
//
// This is a common formatting pattern. Some languages provide it built-in
// (e.g., Python's f"{n:,}"), but Go's standard library does not include
// locale-aware number formatting, so we implement it manually.
func FormatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// FormatGwei converts a base fee from wei (the smallest Ethereum unit) to
// gwei (gigawei, 10^9 wei) and formats it as a human-readable string.
//
// PARAMETER: *big.Int (pointer to big.Int)
// =========================================
// The parameter `wei` is a *big.Int — a POINTER to a big.Int value.
// This means:
//
//   - wei could be nil (for pre-EIP-1559 blocks with no base fee)
//   - If not nil, wei points to a big.Int on the heap
//   - We do NOT modify the original — we create new values for the division
//
// In memory when called with a valid fee:
//
//   Stack (FormatGwei)              Heap
//   ┌──────────────┐              ┌────────────────┐
//   │ wei: ────────┼──────────────▶│ big.Int        │
//   └──────────────┘              │ 24000000000    │  ← 24 Gwei in wei
//                                 └────────────────┘
//
//   After division (new allocations — original is NOT modified):
//
//   Stack                          Heap
//   ┌──────────────┐              ┌────────────────┐
//   │ gwei: ───────┼──────────────▶│ big.Float      │
//   └──────────────┘              │ value: 24.0    │  ← new allocation
//                                 └────────────────┘
//
// UNIT CONVERSION CHAIN
// =====================
// Ethereum gas economics use "wei" as the atomic unit (like cents to dollars):
//
//   1 ETH  = 1,000,000,000 Gwei  (10^9)
//   1 Gwei = 1,000,000,000 wei   (10^9)
//   1 ETH  = 10^18 wei
//
// Base fees are typically in the range of 10-100 Gwei, but they're stored
// as wei on-chain. So we divide by 10^9 to convert to the unit humans use.
//
// The new() calls below create new big.Float values on the heap:
//   - new(big.Float).SetInt(wei) → converts big.Int to big.Float (for division)
//   - big.NewFloat(1e9) → creates a big.Float with value 1,000,000,000
//   - .Quo() performs the division: wei / 1e9 = gwei
//
// The gwei.Float64() at the end converts the arbitrary-precision big.Float
// to a standard float64 for formatting. This loses precision for very large
// numbers, but base fees are small enough that float64 is adequate.
func FormatGwei(wei *big.Int) string {
	// If wei is nil, this block has no base fee (pre-EIP-1559).
	// Return an em-dash to indicate "not applicable."
	if wei == nil {
		return "—"
	}
	// Perform the division: wei / 10^9 = gwei.
	// Each new() allocates a fresh big.Float — we don't modify the input.
	gwei := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(1e9))
	f, _ := gwei.Float64()
	return fmt.Sprintf("%.2f gwei", f)
}

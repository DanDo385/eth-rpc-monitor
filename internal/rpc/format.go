// Package rpc (format.go) provides utility functions for parsing and formatting
// Ethereum-specific data types, including hex-to-decimal conversion and
// human-readable display formatting.
package rpc

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// ParseHexUint64 converts a hex-encoded string (with or without "0x" prefix) to uint64.
// This is used for parsing Ethereum values that fit in 64 bits, such as block numbers,
// timestamps, and gas values.
//
// Parameters:
//   - hex: Hex string (e.g., "0x172721e" or "172721e")
//
// Returns:
//   - uint64: Parsed decimal value
//   - error: Invalid hex string or value exceeds uint64 range
//
// Examples:
//   - "0x172721e" -> 24277534
//   - "0x0" -> 0
//   - "" -> 0 (empty string treated as zero)
func ParseHexUint64(hex string) (uint64, error) {
	// Remove "0x" prefix if present
	hex = strings.TrimPrefix(hex, "0x")

	// Empty string after prefix removal means zero
	if hex == "" {
		return 0, nil
	}

	// Use big.Int for parsing to handle validation
	val := new(big.Int)
	_, ok := val.SetString(hex, 16) // Parse as base-16 (hexadecimal)
	if !ok || !val.IsUint64() {
		return 0, fmt.Errorf("invalid hex: %s", hex)
	}
	return val.Uint64(), nil
}

// ParseHexBigInt converts a hex-encoded string to *big.Int for values that may
// exceed uint64 range (e.g., baseFeePerGas, large token amounts).
// This is necessary because Ethereum values can be arbitrarily large.
//
// Parameters:
//   - hex: Hex string (e.g., "0x14212d64" or "14212d64")
//
// Returns:
//   - *big.Int: Parsed value (can handle arbitrarily large numbers)
//   - error: Invalid hex string
//
// Examples:
//   - "0x14212d64" -> 337234788
//   - "0x0" -> 0
//   - "" -> 0 (empty string treated as zero)
func ParseHexBigInt(hex string) (*big.Int, error) {
	// Remove "0x" prefix if present
	hex = strings.TrimPrefix(hex, "0x")

	// Empty string after prefix removal means zero
	if hex == "" {
		return big.NewInt(0), nil
	}

	// Parse as base-16 (hexadecimal)
	val := new(big.Int)
	_, ok := val.SetString(hex, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex: %s", hex)
	}
	return val, nil
}

// FormatTimestamp converts a Unix timestamp to a human-readable string with relative time.
// The output includes both absolute UTC time and a relative "ago" suffix for quick reference.
//
// Parameters:
//   - ts: Unix timestamp (seconds since epoch)
//
// Returns:
//   - string: Formatted time string (e.g., "2026-01-20 17:02:23 UTC (14s ago)")
//
// The relative time uses appropriate units:
//   - < 1 minute: "Xs ago" (seconds)
//   - < 1 hour: "Xm ago" (minutes)
//   - < 24 hours: "Xh ago" (hours)
//   - >= 24 hours: "Xd ago" (days)
func FormatTimestamp(ts uint64) string {
	// Convert Unix timestamp to time.Time
	t := time.Unix(int64(ts), 0)

	// Calculate time elapsed since timestamp
	ago := time.Since(t)

	// Format relative time based on duration
	var agoStr string
	switch {
	case ago < time.Minute:
		agoStr = fmt.Sprintf("%ds ago", int(ago.Seconds()))
	case ago < time.Hour:
		agoStr = fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		agoStr = fmt.Sprintf("%dh ago", int(ago.Hours()))
	default:
		agoStr = fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	}

	// Combine absolute time (UTC) with relative time
	return fmt.Sprintf("%s (%s)", t.UTC().Format("2006-01-02 15:04:05 UTC"), agoStr)
}

// FormatNumber adds thousand separators (commas) to a number for readability.
// This makes large numbers like block numbers easier to read (e.g., "24,277,510").
//
// Parameters:
//   - n: Number to format
//
// Returns:
//   - string: Number with thousand separators
//
// Examples:
//   - 24277510 -> "24,277,510"
//   - 123 -> "123" (no separator needed)
//   - 1000 -> "1,000"
func FormatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)

	// Numbers with 3 or fewer digits don't need separators
	if len(s) <= 3 {
		return s
	}

	// Insert commas every 3 digits from right to left
	var result []byte
	for i, c := range s {
		// Insert comma before every group of 3 digits (except at the start)
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// FormatGwei converts wei (smallest Ethereum unit) to gwei (1 gwei = 10^9 wei) for display.
// Gas prices are typically displayed in gwei as they're more readable than wei.
//
// Parameters:
//   - wei: Value in wei (can be nil for blocks without base fee)
//
// Returns:
//   - string: Formatted value in gwei with 2 decimal places, or "—" if nil
//
// Examples:
//   - 337234788000000000 wei -> "337.23 gwei"
//   - nil -> "—"
//
// Note: 1 gwei = 1,000,000,000 wei (10^9)
func FormatGwei(wei *big.Int) string {
	if wei == nil {
		return "—" // Blocks before EIP-1559 don't have base fee
	}

	// Convert wei to gwei by dividing by 10^9
	// Use big.Float for precise division
	gwei := new(big.Float).Quo(
		new(big.Float).SetInt(wei), // Convert wei (big.Int) to big.Float
		big.NewFloat(1e9),          // Divide by 1 billion (10^9)
	)

	// Convert to float64 for formatting (precision sufficient for gwei display)
	f, _ := gwei.Float64()
	return fmt.Sprintf("%.2f gwei", f)
}

// NormalizeBlockArg converts block identifiers (decimal, hex, or tag) to RPC format.
// Returns "latest" for "latest", "pending", "earliest", or empty input.
//
// Parameters:
//   - arg: Block identifier as string (decimal number, hex with "0x" prefix, or special tag)
//
// Returns:
//   - string: Normalized block identifier in RPC format (hex for numbers, "latest" for tags)
//
// Examples:
//   - "latest" -> "latest"
//   - "12345" -> "0x3039" (decimal converted to hex)
//   - "0x172721e" -> "0x172721e" (already hex, returned as-is)
//   - "" -> "latest" (empty string defaults to latest)
//
// This function handles the conversion of user-friendly block identifiers
// to the format expected by Ethereum JSON-RPC methods.
func NormalizeBlockArg(arg string) string {
	// Normalize input: trim whitespace and convert to lowercase
	arg = strings.TrimSpace(strings.ToLower(arg))

	// Handle special block tags
	if arg == "latest" || arg == "pending" || arg == "earliest" || arg == "" {
		return "latest"
	}

	// If already hex-encoded, return as-is
	if strings.HasPrefix(arg, "0x") {
		return arg
	}

	// Try to parse as decimal number and convert to hex
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		// Not a valid decimal number - return as-is and let RPC handle the error
		return arg
	}

	// Convert decimal to hex with "0x" prefix
	return fmt.Sprintf("0x%x", num)
}

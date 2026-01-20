// internal/util/block.go
package util

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeBlockArg converts block identifiers (decimal, hex, or tag) to RPC format.
// Returns "latest" for empty/invalid input, hex string for numbers, passthrough for tags.
func NormalizeBlockArg(arg string) string {
	// Normalize input: trim whitespace and convert to lowercase
	arg = strings.TrimSpace(strings.ToLower(arg))

	// Handle special block tags
	if arg == "latest" || arg == "pending" || arg == "earliest" || arg == "" {
		if arg == "" {
			return "latest"
		}
		return arg
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

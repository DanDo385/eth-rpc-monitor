// Package env provides environment variable loading from .env files.
// This allows sensitive configuration (like API keys) to be stored in .env
// files that are gitignored, rather than hardcoded in YAML config files.
package env

import (
	"os"
	"strings"
)

// Load reads environment variables from a .env file in the current working directory
// and sets them using os.Setenv. This function is called at the start of each command
// to ensure environment variables are available before config loading.
//
// File format:
//   - Each line contains KEY=VALUE
//   - Empty lines are ignored
//   - Lines starting with # are treated as comments
//   - Values can be quoted with single or double quotes (quotes are stripped)
//
// Examples:
//   ALCHEMY_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
//   INFURA_URL=https://mainnet.infura.io/v3/YOUR_KEY
//   # This is a comment
//
// Behavior:
//   - If .env file doesn't exist, function silently returns (no error)
//   - This allows the tool to work without .env files (using system env vars)
//   - Variables set in .env override system environment variables
func Load() {
	// Attempt to read .env file from current directory
	data, err := os.ReadFile(".env")
	if err != nil {
		// .env file not found - this is OK, just skip loading
		// System environment variables can still be used
		return
	}
	
	// Process each line in the .env file
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Split on first "=" to handle values that might contain "="
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			
			// Remove surrounding quotes (single or double) if present
			value = strings.Trim(value, `"'`)
			
			// Set environment variable
			os.Setenv(key, value)
		}
	}
}

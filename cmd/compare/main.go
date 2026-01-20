// Package main implements the "compare" command for comparing block data across providers.
// This command fetches the same block from all configured providers simultaneously and
// detects mismatches in block hashes and heights, which can indicate sync issues,
// stale caches, or chain reorganizations.
//
// Usage:
//
//	compare [block_number] [flags]
//	compare latest --json
//
// The command is useful for detecting provider inconsistencies and ensuring data integrity.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
)

// main is the entry point for the compare command.
// It parses command-line arguments, loads environment variables, and delegates to runCompare.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	// Extract block argument (defaults to "latest")
	block := "latest"
	args := flag.Args()
	if len(args) > 0 {
		block = args[0]
	}

	// Execute comparison
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := commands.RunCompare(cfg, block, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

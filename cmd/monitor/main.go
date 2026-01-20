// Package main implements the "monitor" command for continuous real-time monitoring.
// This command periodically queries all providers for their current block height and
// displays a live-updating dashboard showing block heights, latencies, and lag.
// Useful for detecting sync issues, network problems, or provider outages in real-time.
//
// Usage:
//
//	monitor [flags]
//	monitor --interval 10s --json
//
// The command runs until interrupted (Ctrl+C) and can optionally save a JSON report on exit.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
)

// main is the entry point for the monitor command.
// It parses command-line arguments, loads environment variables, and delegates to runWatch.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		interval = flag.Duration("interval", 0, "Refresh interval (0 = use config default)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory on exit")
	)

	flag.Parse()

	// Execute continuous monitoring
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := commands.RunMonitor(cfg, *interval, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

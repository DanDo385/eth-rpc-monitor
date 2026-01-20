// cmd/health/main.go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
)

// main is the entry point for the health command.
// It parses command-line arguments, loads environment variables, and delegates to runHealth.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath = flag.String("config", "config/providers.yaml", "Config file path")
		samples = flag.Int("samples", 0, "Number of test samples per provider (0 = use config default)")
		jsonOut = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	// Execute health check
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := commands.RunHealth(cfg, *samples, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// Package main implements the "block" command for inspecting Ethereum blocks.
// This command fetches block data from RPC providers and displays it in a
// human-readable format or outputs JSON for programmatic use.
//
// Usage:
//
//	block [block_number] [flags]
//	block latest --provider alchemy --json
//
// The command automatically selects the fastest provider unless --provider is specified.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
)

func main() {
	env.Load()

	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		provider = flag.String("provider", "", "Use specific provider (empty = auto-select fastest)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.CommandLine.Parse(os.Args[1:])

	block := "latest"
	args := flag.Args()

	for i, arg := range args {
		if arg == "--json" || arg == "-json" {
			*jsonOut = true
			args = append(args[:i], args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "--provider=") {
			*provider = strings.TrimPrefix(arg, "--provider=")
			args = append(args[:i], args[i+1:]...)
			break
		}
		if arg == "--provider" || arg == "-provider" {
			if i+1 < len(args) {
				*provider = args[i+1]
				args = append(args[:i], args[i+2:]...)
			}
			break
		}
	}

	if len(args) > 0 {
		block = args[0]
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := commands.RunBlock(cfg, block, *provider, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

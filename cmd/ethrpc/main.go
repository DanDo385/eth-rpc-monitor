// Package main is the entry point for the eth-rpc-monitor CLI binary.
//
// All command workflows live in internal/cli; this binary is a thin cobra
// command tree that wires flags to the cli.Run* functions and loads config
// once via the root command's PersistentPreRunE.
package main

import "os"

func main() {
	if err := Execute(); err != nil {
		os.Exit(1)
	}
}

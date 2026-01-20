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
	"context"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/commands"
	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// BlockJSON is a JSON-serializable version of Block with human-readable decimal values.
// This structure is used when --json flag is set to provide cleaner, more parseable
// JSON output compared to the raw hex-encoded Block structure from the RPC API.
//
// Key differences from Block:
//   - All hex strings converted to native types (uint64, string, float64)
//   - Timestamp formatted as ISO 8601 string instead of Unix timestamp
//   - BaseFeePerGas converted from wei to gwei (divided by 10^9)
type BlockJSON struct {
	Number        uint64   `json:"number"`                  // Block number as decimal
	Hash          string   `json:"hash"`                    // Block hash (0x-prefixed hex)
	ParentHash    string   `json:"parentHash"`              // Parent block hash
	Timestamp     string   `json:"timestamp"`               // ISO 8601 format (e.g., "2026-01-20T17:02:23Z")
	GasUsed       uint64   `json:"gasUsed"`                 // Gas used as decimal
	GasLimit      uint64   `json:"gasLimit"`                // Gas limit as decimal
	BaseFeePerGas *float64 `json:"baseFeePerGas,omitempty"` // Base fee in gwei (nil if not present)
	Transactions  []string `json:"transactions"`            // Transaction hashes array
}

// convertBlockToJSON converts a Block (with hex-encoded values) to BlockJSON (with decimal values).
// This function handles all hex-to-decimal conversions and unit conversions (wei to gwei).
// It's used when generating JSON reports to provide more readable output.
func convertBlockToJSON(block *rpc.Block) BlockJSON {
	number, _ := rpc.ParseHexUint64(block.Number)
	timestampUnix, _ := rpc.ParseHexUint64(block.Timestamp)
	gasUsed, _ := rpc.ParseHexUint64(block.GasUsed)
	gasLimit, _ := rpc.ParseHexUint64(block.GasLimit)

	// Convert timestamp to ISO 8601 format
	timestampStr := time.Unix(int64(timestampUnix), 0).UTC().Format(time.RFC3339)

	var baseFeePerGas *float64
	if block.BaseFeePerGas != "" {
		baseFee, _ := rpc.ParseHexBigInt(block.BaseFeePerGas)
		if baseFee != nil {
			// Convert wei to gwei
			gwei := new(big.Float).Quo(
				new(big.Float).SetInt(baseFee),
				big.NewFloat(1e9),
			)
			gweiFloat, _ := gwei.Float64()
			baseFeePerGas = &gweiFloat
		}
	}

	return BlockJSON{
		Number:        number,
		Hash:          block.Hash,
		ParentHash:    block.ParentHash,
		Timestamp:     timestampStr,
		GasUsed:       gasUsed,
		GasLimit:      gasLimit,
		BaseFeePerGas: baseFeePerGas,
		Transactions:  block.Transactions,
	}
}

type providerResult struct {
	blockNum uint64
	latency  time.Duration
	hasError bool
}

func selectFastestProvider(ctx context.Context, cfg *config.Config) (*rpc.Client, error) {
	results, clients := commands.ExecuteAllWithClients(ctx, cfg, nil, func(ctx context.Context, client *rpc.Client, _ config.Provider) providerResult {
		blockNum, latency, err := client.BlockNumber(ctx)
		if err != nil {
			return providerResult{hasError: true}
		}
		return providerResult{blockNum: blockNum, latency: latency}
	})

	var latestBlock uint64
	successCount := 0
	for _, r := range results {
		if !r.hasError {
			successCount++
			if r.blockNum > latestBlock {
				latestBlock = r.blockNum
			}
		}
	}

	if successCount == 0 {
		return nil, fmt.Errorf("no providers responded successfully")
	}

	var fastest *rpc.Client
	var fastestLatency time.Duration
	found := false
	for i, r := range results {
		if !r.hasError && r.blockNum == latestBlock {
			if !found || r.latency < fastestLatency {
				fastest = clients[i]
				fastestLatency = r.latency
				found = true
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("no provider is on the latest block (%d)", latestBlock)
	}

	return fastest, nil
}

func runBlock(cfg *config.Config, blockArg, providerName string, jsonOut bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	var client *rpc.Client
	var err error
	if providerName != "" {
		for _, p := range cfg.Providers {
			if p.Name == providerName {
				client = rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
				break
			}
		}
		if client == nil {
			return fmt.Errorf("provider '%s' not found in config", providerName)
		}
	} else {
		client, err = selectFastestProvider(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Auto-selected: %s\n\n", client.Name())
	}

	_ = client.Warmup(ctx)

	block, latency, err := client.GetBlock(ctx, rpc.NormalizeBlockArg(blockArg))
	if err != nil {
		return fmt.Errorf("failed to fetch block: %w", err)
	}

	if jsonOut {
		blockJSON := convertBlockToJSON(block)
		filepath, err := commands.WriteJSON(blockJSON, "block")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	formatter := commands.NewBlockFormatter(block, client.Name(), latency)
	if err := formatter.Format(os.Stdout); err != nil {
		return fmt.Errorf("failed to display block: %w", err)
	}
	return nil
}

func main() {
	config.LoadEnv()

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

	if err := runBlock(cfg, block, *provider, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

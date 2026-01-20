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
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/env"
	"github.com/dando385/eth-rpc-monitor/internal/reports"
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
//
// Parameters:
//   - block: Raw Block structure from RPC API with hex-encoded fields
//
// Returns:
//   - BlockJSON: Converted structure with decimal values and formatted timestamp
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

// main is the entry point for the block command.
// It parses command-line arguments, handles flag parsing (including flags after positional args),
// loads environment variables, and delegates to runInspect for the actual work.
func main() {
	// Load environment variables from .env file (if present)
	env.Load()

	// Define command-line flags
	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		provider = flag.String("provider", "", "Use specific provider (empty = auto-select fastest)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	// Parse flags - standard flag package stops at first non-flag arg
	// This means flags must come before positional args, or we need to handle them manually
	flag.CommandLine.Parse(os.Args[1:])

	// Default block argument is "latest"
	block := "latest"
	args := flag.Args()

	// Handle flags that might come after positional args (e.g., "block latest --json")
	// The standard flag package doesn't handle this, so we manually parse these cases
	for i, arg := range args {
		// Handle --json or -json flag
		if arg == "--json" || arg == "-json" {
			*jsonOut = true
			// Remove this arg from the list to avoid treating it as block number
			args = append(args[:i], args[i+1:]...)
			break
		}
		// Handle --provider=value format
		if strings.HasPrefix(arg, "--provider=") {
			*provider = strings.TrimPrefix(arg, "--provider=")
			args = append(args[:i], args[i+1:]...)
			break
		}
		// Handle --provider value or -provider value format
		if arg == "--provider" || arg == "-provider" {
			if i+1 < len(args) {
				*provider = args[i+1]
				// Remove both the flag and its value
				args = append(args[:i], args[i+2:]...)
			}
			break
		}
	}

	// Extract block number/argument from remaining args
	if len(args) > 0 {
		block = args[0]
	}

	// Execute the block inspection
	if err := runInspect(*cfgPath, block, *provider, *jsonOut); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runInspect is the core function that performs block inspection.
// It loads configuration, selects a provider (manual or auto), fetches the block,
// and either outputs JSON or displays formatted terminal output.
//
// Parameters:
//   - cfgPath: Path to providers.yaml configuration file
//   - blockArg: Block identifier ("latest", "pending", hex number, or decimal number)
//   - providerName: Specific provider to use (empty string = auto-select fastest)
//   - jsonOut: If true, output JSON report instead of terminal display
//
// Returns:
//   - error: Configuration, provider selection, or RPC call error
func runInspect(cfgPath, blockArg, providerName string, jsonOut bool) error {
	// Load configuration from YAML file
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Create context with timeout (2x default timeout for provider selection + block fetch)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	// Select provider: either use specified provider or auto-select fastest
	var client *rpc.Client
	if providerName != "" {
		// User specified a provider - find it in config
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
		// Auto-select fastest provider by racing all providers
		client, err = selectFastestProvider(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Auto-selected: %s\n\n", client.Name())
	}

	// Fetch block data from selected provider
	// normalizeBlockArg converts various input formats to RPC-compatible format
	block, latency, err := client.GetBlock(ctx, normalizeBlockArg(blockArg))
	if err != nil {
		return fmt.Errorf("failed to fetch block: %w", err)
	}

	// Handle JSON output mode
	if jsonOut {
		// Convert to JSON-friendly format with decimal values
		blockJSON := convertBlockToJSON(block)
		filepath, err := reports.WriteJSON(blockJSON, "block")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	// Terminal output: display formatted block information
	printBlock(block, client.Name(), latency)
	return nil
}

func printBlock(block *rpc.Block, provider string, latency time.Duration) {
	p := block.Parsed()

	fmt.Printf("\nBlock #%s\n", rpc.FormatNumber(p.Number))
	fmt.Println("═══════════════════════════════════════════════════")
	fmt.Printf("  Hash:         %s\n", p.Hash)
	fmt.Printf("  Parent:       %s\n", p.ParentHash)
	fmt.Printf("  Timestamp:    %s\n", rpc.FormatTimestamp(p.Timestamp))
	fmt.Printf("  Gas:          %s / %s (%.1f%%)\n",
		rpc.FormatNumber(p.GasUsed),
		rpc.FormatNumber(p.GasLimit),
		float64(p.GasUsed)/float64(p.GasLimit)*100)
	fmt.Printf("  Base Fee:     %s\n", rpc.FormatGwei(p.BaseFeePerGas))
	fmt.Printf("  Transactions: %d\n", p.TxCount)
	fmt.Println()
	fmt.Printf("  Provider:     %s (%dms)\n", provider, latency.Milliseconds())
	fmt.Println()
}

// normalizeBlockArg converts various block identifier formats to RPC-compatible format.
// Ethereum RPC accepts block numbers as hex strings (e.g., "0x123") or special tags
// ("latest", "pending", "earliest"). This function handles common input variations.
//
// Parameters:
//   - arg: Block identifier in various formats:
//   - "latest", "pending", "earliest" -> returned as-is
//   - Decimal number (e.g., "24277510") -> converted to hex ("0x172721e")
//   - Hex number (e.g., "0x172721e") -> returned as-is
//   - Empty string -> defaults to "latest"
//
// Returns:
//   - string: RPC-compatible block identifier
//
// Examples:
//   - "latest" -> "latest"
//   - "24277510" -> "0x172721e"
//   - "0x172721e" -> "0x172721e"
//   - "" -> "latest"
func normalizeBlockArg(arg string) string {
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

// selectFastestProvider races all configured providers concurrently to find the fastest one
// that is on the latest block. This ensures we get both speed and correctness (latest data).
//
// Algorithm:
//  1. Concurrently query all providers for their current block number
//  2. Collect successful responses with latency measurements
//  3. Find the highest block number (most up-to-date)
//  4. Among providers on the latest block, select the fastest one
//
// This approach balances speed and data freshness - we don't want a fast provider
// that's lagging behind the chain.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - cfg: Configuration containing provider list
//
// Returns:
//   - *rpc.Client: Client for the fastest provider on latest block
//   - error: If no providers respond or none are on latest block
func selectFastestProvider(ctx context.Context, cfg *config.Config) (*rpc.Client, error) {
	// Internal structure to hold provider race results
	type result struct {
		client   *rpc.Client   // RPC client for this provider
		blockNum uint64        // Current block number reported by provider
		latency  time.Duration // Response latency
	}

	// Thread-safe collection of results
	var mu sync.Mutex
	results := make([]result, 0, len(cfg.Providers))

	// Use errgroup for concurrent provider queries with context cancellation
	g, gctx := errgroup.WithContext(ctx)
	for _, p := range cfg.Providers {
		p := p // Capture loop variable for goroutine
		g.Go(func() error {
			// Create client and query block number
			client := rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			blockNum, latency, err := client.BlockNumber(gctx)

			// Ignore errors - just skip providers that fail
			// We'll check if we have any successful providers later
			if err != nil {
				return nil
			}

			// Thread-safely append result
			mu.Lock()
			results = append(results, result{
				client:   client,
				blockNum: blockNum,
				latency:  latency,
			})
			mu.Unlock()
			return nil
		})
	}

	// Wait for all concurrent queries to complete
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("error selecting provider: %w", err)
	}

	// Ensure at least one provider responded
	if len(results) == 0 {
		return nil, fmt.Errorf("no providers responded successfully")
	}

	// Find the highest block number (most up-to-date chain state)
	var latestBlock uint64
	for _, r := range results {
		if r.blockNum > latestBlock {
			latestBlock = r.blockNum
		}
	}

	// Among providers on the latest block, find the fastest one
	// This ensures we get both correctness (latest data) and speed
	var fastest *rpc.Client
	var fastestLatency time.Duration
	found := false
	for _, r := range results {
		if r.blockNum == latestBlock {
			// This provider is on the latest block
			if !found || r.latency < fastestLatency {
				fastest = r.client
				fastestLatency = r.latency
				found = true
			}
		}
	}

	// Ensure we found at least one provider on the latest block
	if !found {
		return nil, fmt.Errorf("no provider is on the latest block (%d)", latestBlock)
	}

	return fastest, nil
}

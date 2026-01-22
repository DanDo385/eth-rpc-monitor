// cmd/block/main.go
package main

import (
	"context"
	"encoding/json"
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
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

type BlockJSON struct {
	Number        uint64   `json:"number"`
	Hash          string   `json:"hash"`
	ParentHash    string   `json:"parentHash"`
	Timestamp     string   `json:"timestamp"`
	GasUsed       uint64   `json:"gasUsed"`
	GasLimit      uint64   `json:"gasLimit"`
	BaseFeePerGas *float64 `json:"baseFeePerGas,omitempty"`
	Transactions  []string `json:"transactions"`
}

func writeJSON(data interface{}, prefix string) (string, error) {
	os.MkdirAll("reports", 0755)
	filename := fmt.Sprintf("reports/%s-%s.json", prefix, time.Now().Format("20060102-150405"))
	file, _ := os.Create(filename)
	defer file.Close()
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	enc.Encode(data)
	return filename, nil
}

func convertBlockToJSON(block *rpc.Block) BlockJSON {
	number, _ := rpc.ParseHexUint64(block.Number)
	timestampUnix, _ := rpc.ParseHexUint64(block.Timestamp)
	gasUsed, _ := rpc.ParseHexUint64(block.GasUsed)
	gasLimit, _ := rpc.ParseHexUint64(block.GasLimit)

	timestampStr := time.Unix(int64(timestampUnix), 0).UTC().Format(time.RFC3339)

	var baseFeePerGas *float64
	if block.BaseFeePerGas != "" {
		baseFee := rpc.ParseHexBigInt(block.BaseFeePerGas)
		if baseFee != nil {
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
	results := make([]providerResult, len(cfg.Providers))
	clients := make([]*rpc.Client, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)
			blockNum, latency, err := client.BlockNumber(gctx)

			r := providerResult{hasError: err != nil}
			if err == nil {
				r.blockNum = blockNum
				r.latency = latency
			}

			mu.Lock()
			results[i] = r
			clients[i] = client
			mu.Unlock()
			return nil
		})
	}

	g.Wait()

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

func normalizeBlockArg(arg string) string {
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

func runBlock(cfg *config.Config, blockArg, providerName string, jsonOut bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	var client *rpc.Client
	var err error
	if providerName != "" {
		for _, p := range cfg.Providers {
			if p.Name == providerName {
				client = rpc.NewClient(p.Name, p.URL, p.Timeout)
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

	client.BlockNumber(ctx)

	block, latency, err := client.GetBlock(ctx, blockArg)
	if err != nil {
		return fmt.Errorf("failed to fetch block: %w", err)
	}

	if jsonOut {
		blockJSON := convertBlockToJSON(block)
		filepath, err := writeJSON(blockJSON, "block")
		if err != nil {
			return fmt.Errorf("failed to write JSON report: %w", err)
		}
		fmt.Fprintf(os.Stderr, "JSON report written to: %s\n", filepath)
		return nil
	}

	format.FormatBlock(os.Stdout, block, client.Name(), latency)
	return nil
}

func main() {
	config.LoadEnv()

	var (
		cfgPath  = flag.String("config", "config/providers.yaml", "Config file path")
		provider = flag.String("provider", "", "Use specific provider (empty = auto-select fastest)")
		jsonOut  = flag.Bool("json", false, "Output JSON report to reports directory")
	)

	flag.Parse()

	block := "latest"
	args := flag.Args()
	if len(args) > 0 {
		block = normalizeBlockArg(args[0])
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

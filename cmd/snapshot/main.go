package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/format"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func main() {
	config.LoadEnv()

	cfgPath := flag.String("config", "config/providers.yaml", "Config file path")
	flag.Parse()

	blockArg := "latest"
	if args := flag.Args(); len(args) > 0 {
		blockArg = args[0]
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Defaults.Timeout*2)
	defer cancel()

	fmt.Printf("\nFetching block %s from %d providers...\n\n", blockArg, len(cfg.Providers))

	results := make([]format.SnapshotResult, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p
		g.Go(func() error {
			client := rpc.NewClient(p.Name, p.URL, p.Timeout)

			// Warmup call
			client.BlockNumber(gctx)

			block, latency, err := client.GetBlock(gctx, blockArg)

			r := format.SnapshotResult{Provider: p.Name, Latency: latency, Error: err}
			if err == nil && block != nil {
				r.Hash = block.Hash
				r.Height, _ = rpc.ParseHexUint64(block.Number)
			}

			mu.Lock()
			results[i] = r
			mu.Unlock()
			return nil
		})
	}

	g.Wait()

	format.FormatSnapshot(os.Stdout, results)
}

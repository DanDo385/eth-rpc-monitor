package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func snapshotCmd() *cobra.Command {
	var samples int
	var intervalMs int
	var format string

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Generate a point-in-time report",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Root().PersistentFlags().GetString("config")
			interval := time.Duration(intervalMs) * time.Millisecond
			return runSnapshot(cmd.Context(), cfgPath, samples, interval, format)
		},
	}

	cmd.Flags().IntVar(&samples, "samples", 30, "Number of samples per provider")
	cmd.Flags().IntVar(&intervalMs, "interval", 100, "Interval between samples in milliseconds")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	return cmd
}

func runSnapshot(ctx context.Context, cfgPath string, samples int, interval time.Duration, format string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	clients := buildClients(cfg)
	collector := metrics.NewCollector()
	latestBlocks := make(map[string]*rpc.Block)

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	for name, client := range clients {
		name, client := name, client
		g.Go(func() error {
			for i := 0; i < samples; i++ {
				block, result := client.GetLatestBlock(gctx)
				mu.Lock()
				collector.Add(result)
				if result.Success && block != nil {
					latestBlocks[name] = block
				}
				mu.Unlock()

				if i < samples-1 {
					select {
					case <-gctx.Done():
						return gctx.Err()
					case <-time.After(interval):
					}
				}
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	providerMetrics := collector.Calculate()
	for name, block := range latestBlocks {
		if m, ok := providerMetrics[name]; ok {
			m.LatestBlock = block.Number
			m.LatestBlockHash = block.Hash
		}
	}

	consistency := buildConsistency(ctx, clients, providerMetrics)

	report := &output.SnapshotReport{
		Timestamp:   time.Now(),
		SampleCount: samples,
		Providers:   providerMetrics,
		Consistency: consistency,
	}

	switch strings.ToLower(format) {
	case "json":
		output.DisableColors()
		return output.RenderSnapshotJSON(report)
	case "terminal", "":
		output.RenderSnapshotTerminal(report)
		return nil
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

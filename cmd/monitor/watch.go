package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

func watchCmd() *cobra.Command {
	var refresh time.Duration

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream live provider health updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Root().PersistentFlags().GetString("config")
			return runWatch(cmd.Context(), cfgPath, refresh)
		},
	}

	cmd.Flags().DurationVar(&refresh, "refresh", 5*time.Second, "Refresh interval")

	return cmd
}

func runWatch(ctx context.Context, cfgPath string, refresh time.Duration) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	clients := buildClients(cfg)
	state := output.NewWatchState(refresh, 10)
	for name := range clients {
		state.Providers[name] = &output.WatchProviderState{
			Name:   name,
			Status: metrics.StatusDown,
		}
	}

	var consistency *metrics.ConsistencyReport
	var hashCheckTick int

	update := func() error {
		hashCheckTick++
		state.LastUpdate = time.Now()
		heights, successfulProviders, err := updateWatchProviders(ctx, state, clients)
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}

		if len(heights) == 0 {
			consistency = nil
		} else {
			refHeight, _ := minHeight(heights)
			hashesAtRef := make(map[string]string)
			shouldCheckHashes := hashCheckTick%3 == 0 && successfulProviders >= 2
			if shouldCheckHashes {
				hashesAtRef = fetchHashesAtHeight(ctx, clients, refHeight)
			}
			checker := metrics.NewConsistencyChecker()
			consistency = checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
		}

		output.RenderWatch(state, consistency)
		return nil
	}

	if err := update(); err != nil {
		return err
	}

	ticker := time.NewTicker(refresh)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := update(); err != nil {
				return err
			}
		}
	}
}

func updateWatchProviders(
	ctx context.Context,
	state *output.WatchState,
	clients map[string]*rpc.Client,
) (map[string]uint64, int, error) {
	heights := make(map[string]uint64)
	var successfulProviders int

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)

	for name, client := range clients {
		name, client := name, client
		g.Go(func() error {
			block, result := client.GetLatestBlock(gctx)
			updatedAt := time.Now()

			mu.Lock()
			defer mu.Unlock()

			provider := state.Providers[name]
			if provider == nil {
				provider = &output.WatchProviderState{Name: name}
				state.Providers[name] = provider
			}

			previousStatus := provider.Status
			provider.LastSeen = updatedAt
			provider.Latency = result.Latency

			if result.Success && block != nil {
				provider.BlockHeight = block.Number
				provider.BlockHash = block.Hash
				provider.Status = statusFromLatency(result.Latency)
				provider.LastError = ""
				heights[name] = block.Number
				successfulProviders++
			} else {
				provider.Status = metrics.StatusDown
				if result.Error != nil {
					provider.LastError = result.Error.Error()
				} else {
					provider.LastError = "unknown error"
				}
			}

			if previousStatus != provider.Status {
				state.AddEvent(name, statusChangeMessage(provider.Status, provider.LastError), statusSeverity(provider.Status))
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return heights, successfulProviders, err
	}

	return heights, successfulProviders, nil
}

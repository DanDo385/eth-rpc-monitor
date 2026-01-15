package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

var (
	configPath string
	format     string

	snapshotSamples    int
	snapshotIntervalMs int

	watchRefresh   time.Duration
	watchMaxEvents int
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "monitor",
		Short: "Ethereum RPC Infrastructure Monitor",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if format == "json" {
				output.DisableColors()
			}
		},
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config/providers.yaml", "Path to provider config YAML")
	rootCmd.PersistentFlags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Generate a snapshot report across providers",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runSnapshot(ctx)
		},
	}
	snapshotCmd.Flags().IntVar(&snapshotSamples, "samples", 30, "Number of samples per provider")
	snapshotCmd.Flags().IntVar(&snapshotIntervalMs, "interval", 100, "Sampling interval in milliseconds")

	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch provider health in real time",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runWatch(ctx)
		},
	}
	watchCmd.Flags().DurationVar(&watchRefresh, "refresh", 5*time.Second, "Refresh interval (e.g. 5s)")
	watchCmd.Flags().IntVar(&watchMaxEvents, "max-events", 20, "Max recent events to display")

	rootCmd.AddCommand(snapshotCmd, watchCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runSnapshot(ctx context.Context) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	clients := buildClients(cfg)

	collector := metrics.NewCollector()
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for name, client := range clients {
		name, client := name, client
		g.Go(func() error {
			for i := 0; i < snapshotSamples; i++ {
				_, result := client.GetLatestBlock(gctx)
				mu.Lock()
				collector.Add(result)
				mu.Unlock()

				if i < snapshotSamples-1 {
					select {
					case <-gctx.Done():
						return nil
					case <-time.After(time.Duration(snapshotIntervalMs) * time.Millisecond):
					}
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	providerMetrics := collector.Calculate()

	// Phase 1: Extract heights from metrics
	heights := make(map[string]uint64)
	for name, m := range providerMetrics {
		if m.LatestBlock > 0 {
			heights[name] = m.LatestBlock
		}
	}

	consistency := buildConsistencyReport(ctx, clients, heights, true)

	report := &output.SnapshotReport{
		Timestamp:   time.Now(),
		SampleCount: snapshotSamples,
		Providers:   providerMetrics,
		Consistency: consistency,
	}

	switch format {
	case "json":
		return output.RenderSnapshotJSON(report)
	default:
		output.RenderSnapshotTerminal(report)
		return nil
	}
}

func runWatch(ctx context.Context) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	clients := buildClients(cfg)

	state := output.NewWatchState(watchRefresh, watchMaxEvents)
	for name := range clients {
		state.Providers[name] = &output.WatchProviderState{Name: name}
	}

	ticker := time.NewTicker(watchRefresh)
	defer ticker.Stop()

	var hashCheckTick int
	checker := metrics.NewConsistencyChecker()

	for {
		hashCheckTick++

		var mu sync.Mutex
		successfulProviders := 0
		heights := make(map[string]uint64)

		g, gctx := errgroup.WithContext(ctx)
		for name, client := range clients {
			name, client := name, client
			g.Go(func() error {
				block, result := client.GetLatestBlock(gctx)
				now := time.Now()

				p := &output.WatchProviderState{
					Name:     name,
					Latency:  result.Latency,
					LastSeen: now,
				}

				if result.Success && block != nil {
					p.BlockHeight = block.Number
					p.BlockHash = block.Hash
					p.Status = classifyWatchStatus(true, result.Latency, result.ErrorType)
				} else {
					p.Status = classifyWatchStatus(false, result.Latency, result.ErrorType)
					if result.Error != nil {
						p.LastError = result.Error.Error()
					}
				}

				mu.Lock()
				state.Providers[name] = p
				if result.Success && block != nil {
					successfulProviders++
					heights[name] = block.Number
				} else if p.LastError != "" {
					state.AddEvent(name, p.LastError, output.SeverityWarning)
				}
				mu.Unlock()

				return nil
			})
		}
		_ = g.Wait()

		state.LastUpdate = time.Now()

		shouldCheckHashes := hashCheckTick%3 == 0 && successfulProviders >= 2
		var consistency *metrics.ConsistencyReport
		if len(heights) > 0 {
			hashesAtRef := map[string]string{}
			var refHeight uint64 = math.MaxUint64
			for _, h := range heights {
				if h < refHeight {
					refHeight = h
				}
			}

			if shouldCheckHashes {
				hashesAtRef = fetchHashesAtHeight(ctx, clients, heights, refHeight)
			}

			consistency = checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
		}

		output.RenderWatch(state, consistency)

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func buildConsistencyReport(
	ctx context.Context,
	clients map[string]*rpc.Client,
	heights map[string]uint64,
	fetchHashes bool,
) *metrics.ConsistencyReport {
	report := &metrics.ConsistencyReport{
		Heights:    heights,
		Hashes:     make(map[string]string),
		Consistent: false,
		Issues:     []string{"No providers returned a block height"},
	}
	if len(heights) == 0 {
		return report
	}

	// Find reference height (minimum height all providers have)
	var refHeight uint64 = math.MaxUint64
	for _, h := range heights {
		if h < refHeight {
			refHeight = h
		}
	}

	var hashesAtRef map[string]string
	if fetchHashes {
		hashesAtRef = fetchHashesAtHeight(ctx, clients, heights, refHeight)
	} else {
		hashesAtRef = map[string]string{}
	}

	checker := metrics.NewConsistencyChecker()
	return checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
}

func fetchHashesAtHeight(
	ctx context.Context,
	clients map[string]*rpc.Client,
	heights map[string]uint64,
	refHeight uint64,
) map[string]string {
	hashesAtRef := make(map[string]string)
	var mu sync.Mutex
	refHeightHex := fmt.Sprintf("0x%x", refHeight)

	g, gctx := errgroup.WithContext(ctx)
	for name := range heights {
		name := name
		client := clients[name]
		g.Go(func() error {
			block, result := client.GetBlockByNumber(gctx, refHeightHex, false)
			if result.Success && block != nil {
				mu.Lock()
				hashesAtRef[name] = block.Hash
				mu.Unlock()
			}
			return nil // Don't fail group on individual provider failure
		})
	}
	_ = g.Wait() // Ignore error since we handle missing hashes gracefully

	return hashesAtRef
}

func classifyWatchStatus(success bool, latency time.Duration, errType rpc.ErrorType) metrics.ProviderStatus {
	if success {
		if latency > 500*time.Millisecond {
			return metrics.StatusSlow
		}
		return metrics.StatusUp
	}

	switch errType {
	case rpc.ErrorTypeRateLimit, rpc.ErrorTypeServerError, rpc.ErrorTypeRPCError:
		return metrics.StatusDegraded
	default:
		return metrics.StatusDown
	}
}

func loadConfig(path string) (*config.Config, error) {
	if _, err := os.Stat(path); err != nil {
		// Friendly fallback when repo ships only the template.
		if os.IsNotExist(err) && path == "config/providers.yaml" {
			if _, err2 := os.Stat("config/providers.yaml.example"); err2 == nil {
				path = "config/providers.yaml.example"
			}
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func buildClients(cfg *config.Config) map[string]*rpc.Client {
	clients := make(map[string]*rpc.Client, len(cfg.Providers))
	for _, p := range cfg.Providers {
		clients[p.Name] = rpc.NewClient(rpc.ClientConfig{
			Name:           p.Name,
			URL:            p.URL,
			Timeout:        p.Timeout,
			MaxRetries:     cfg.Defaults.MaxRetries,
			BackoffInitial: cfg.Defaults.BackoffInitial,
			BackoffMax:     cfg.Defaults.BackoffMax,
		})
	}
	return clients
}

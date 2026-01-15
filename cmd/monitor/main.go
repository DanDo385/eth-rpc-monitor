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
	configPath  string
	samples     int
	formatJSON  bool
	refreshRate time.Duration
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "monitor",
		Short: "Ethereum RPC Infrastructure Monitor",
		Long:  `A reliability monitor for Ethereum RPC endpoints that checks health, latency, and data consistency.`,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config/providers.yaml", "Path to providers config file")
	rootCmd.PersistentFlags().BoolVar(&formatJSON, "format", false, "Output in JSON format (use --format for JSON)")

	// Snapshot command
	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Take a point-in-time snapshot of RPC endpoint health",
		RunE:  runSnapshot,
	}
	snapshotCmd.Flags().IntVarP(&samples, "samples", "s", 10, "Number of samples to collect per provider")

	// Watch command
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Continuously monitor RPC endpoints",
		RunE:  runWatch,
	}
	watchCmd.Flags().DurationVarP(&refreshRate, "refresh", "r", 5*time.Second, "Refresh interval")

	rootCmd.AddCommand(snapshotCmd, watchCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Create RPC clients
	clients := make(map[string]*rpc.Client)
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

	if formatJSON {
		output.DisableColors()
	}

	// Collect samples using errgroup
	collector := metrics.NewCollector()
	var mu sync.Mutex

	for i := 0; i < samples; i++ {
		g, gctx := errgroup.WithContext(ctx)

		for name, client := range clients {
			name, client := name, client // capture loop variables
			g.Go(func() error {
				// Get block number
				blockNum, result := client.BlockNumber(gctx)
				mu.Lock()
				collector.Add(result)
				mu.Unlock()

				// If successful, get block details
				if result.Success && blockNum > 0 {
					block, blockResult := client.GetLatestBlock(gctx)
					if blockResult.Success && block != nil {
						mu.Lock()
						// Update the metrics with block info - we'll extract this later
						_ = block
						_ = name
						mu.Unlock()
					}
				}
				return nil // Don't fail group on individual provider failure
			})
		}

		if err := g.Wait(); err != nil {
			return err
		}

		// Small delay between sample rounds
		if i < samples-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	// Calculate metrics
	providerMetrics := collector.Calculate()

	// Phase 1: Extract heights from a final round of block number calls
	heights := make(map[string]uint64)
	{
		g, gctx := errgroup.WithContext(ctx)
		var heightMu sync.Mutex

		for name, client := range clients {
			name, client := name, client
			g.Go(func() error {
				blockNum, result := client.BlockNumber(gctx)
				if result.Success && blockNum > 0 {
					heightMu.Lock()
					heights[name] = blockNum
					heightMu.Unlock()
				}
				return nil
			})
		}
		_ = g.Wait()
	}

	// Also update provider metrics with the latest block info
	{
		g, gctx := errgroup.WithContext(ctx)
		var metricsMu sync.Mutex

		for name, client := range clients {
			name, client := name, client
			g.Go(func() error {
				block, result := client.GetLatestBlock(gctx)
				if result.Success && block != nil {
					metricsMu.Lock()
					if m, ok := providerMetrics[name]; ok {
						m.LatestBlock = block.Number
						m.LatestBlockHash = block.Hash
					}
					// Also update heights if not already set
					if _, ok := heights[name]; !ok {
						heights[name] = block.Number
					}
					metricsMu.Unlock()
				}
				return nil
			})
		}
		_ = g.Wait()
	}

	// Find reference height (minimum height all providers have)
	var refHeight uint64 = math.MaxUint64
	for _, h := range heights {
		if h < refHeight {
			refHeight = h
		}
	}

	// Phase 2: Fetch hash at reference height from each provider
	hashesAtRef := make(map[string]string)
	if refHeight > 0 && refHeight < math.MaxUint64 {
		g, gctx := errgroup.WithContext(ctx)
		var hashMu sync.Mutex
		refHeightHex := fmt.Sprintf("0x%x", refHeight)

		for name, client := range clients {
			name, client := name, client
			g.Go(func() error {
				block, result := client.GetBlockByNumber(gctx, refHeightHex, false)
				if result.Success && block != nil {
					hashMu.Lock()
					hashesAtRef[name] = block.Hash
					hashMu.Unlock()
				}
				return nil // Don't fail group on individual provider failure
			})
		}
		_ = g.Wait() // Ignore error since we handle missing hashes gracefully
	}

	// Phase 3: Run consistency check with same-height hashes
	checker := metrics.NewConsistencyChecker()
	var consistency *metrics.ConsistencyReport
	if len(hashesAtRef) > 0 {
		consistency = checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
	} else {
		// Fallback if we couldn't get hashes
		consistency = &metrics.ConsistencyReport{
			Heights:         heights,
			Hashes:          make(map[string]string),
			Consistent:      true,
			HeightConsensus: true,
			HashConsensus:   true,
		}
	}

	// Build report
	report := &output.SnapshotReport{
		Timestamp:   time.Now(),
		SampleCount: samples,
		Providers:   providerMetrics,
		Consistency: consistency,
	}

	// Output
	if formatJSON {
		return output.RenderSnapshotJSON(report)
	}
	output.RenderSnapshotTerminal(report)
	return nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Create RPC clients
	clients := make(map[string]*rpc.Client)
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

	// Initialize watch state
	state := output.NewWatchState(refreshRate, 10)
	ticker := time.NewTicker(refreshRate)
	defer ticker.Stop()

	var hashCheckTick int

	// Initial poll
	pollAndRender(ctx, clients, state, &hashCheckTick)

	// Watch loop
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			pollAndRender(ctx, clients, state, &hashCheckTick)
		}
	}
}

func pollAndRender(ctx context.Context, clients map[string]*rpc.Client, state *output.WatchState, hashCheckTick *int) {
	heights := make(map[string]uint64)
	var mu sync.Mutex
	successfulProviders := 0

	// Poll all providers concurrently using errgroup
	g, gctx := errgroup.WithContext(ctx)

	for name, client := range clients {
		name, client := name, client
		g.Go(func() error {
			block, result := client.GetLatestBlock(gctx)

			mu.Lock()
			defer mu.Unlock()

			// Get or create provider state
			ps, ok := state.Providers[name]
			if !ok {
				ps = &output.WatchProviderState{Name: name}
				state.Providers[name] = ps
			}

			if result.Success && block != nil {
				ps.Status = metrics.StatusUp
				ps.Latency = result.Latency
				ps.BlockHeight = block.Number
				ps.BlockHash = block.Hash
				ps.LastSeen = time.Now()
				ps.LastError = ""
				heights[name] = block.Number
				successfulProviders++

				// Check for slow response
				if result.Latency > 500*time.Millisecond {
					ps.Status = metrics.StatusSlow
					state.AddEvent(name, fmt.Sprintf("Slow response: %v", result.Latency), output.SeverityWarning)
				}
			} else {
				if result.ErrorType == rpc.ErrorTypeCircuitOpen {
					ps.Status = metrics.StatusDown
				} else {
					ps.Status = metrics.StatusDegraded
				}
				ps.LastError = result.Error.Error()
				state.AddEvent(name, fmt.Sprintf("Error: %v", result.Error), output.SeverityError)
			}

			return nil
		})
	}
	_ = g.Wait()

	// Perform hash check every 3rd tick if we have 2+ successful providers
	*hashCheckTick++
	shouldCheckHashes := *hashCheckTick%3 == 0 && successfulProviders >= 2

	var consistency *metrics.ConsistencyReport

	if shouldCheckHashes && len(heights) >= 2 {
		// Find reference height (minimum height all providers have)
		var refHeight uint64 = math.MaxUint64
		for _, h := range heights {
			if h < refHeight {
				refHeight = h
			}
		}

		// Fetch hash at reference height from each provider
		hashesAtRef := make(map[string]string)
		if refHeight > 0 && refHeight < math.MaxUint64 {
			g2, gctx2 := errgroup.WithContext(ctx)
			var hashMu sync.Mutex
			refHeightHex := fmt.Sprintf("0x%x", refHeight)

			for name, client := range clients {
				name, client := name, client
				g2.Go(func() error {
					block, result := client.GetBlockByNumber(gctx2, refHeightHex, false)
					if result.Success && block != nil {
						hashMu.Lock()
						hashesAtRef[name] = block.Hash
						hashMu.Unlock()
					}
					return nil
				})
			}
			_ = g2.Wait()
		}

		// Run consistency check
		checker := metrics.NewConsistencyChecker()
		consistency = checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
	} else if len(heights) >= 1 {
		// Basic height-only consistency check
		checker := metrics.NewConsistencyChecker()
		consistency = checker.CheckWithSameHeightHashes(heights, make(map[string]string), 0)
	}

	state.LastUpdate = time.Now()
	output.RenderWatch(state, consistency)
}

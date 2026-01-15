package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

var (
	cfgFile      string
	samples      int
	interval     time.Duration
	watchRefresh time.Duration
	format       string
)

var rootCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Ethereum RPC Infrastructure Monitor",
	Long: `A tool for monitoring Ethereum RPC endpoint reliability, latency, 
and consistency. Designed for institutional operations teams.`,
}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Generate a detailed diagnostic report",
	Run:   runSnapshot,
}

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Real-time monitoring dashboard",
	Run:   runWatch,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config/providers.yaml", "config file path")
	
	snapshotCmd.Flags().IntVar(&samples, "samples", 30, "number of samples per provider")
	snapshotCmd.Flags().DurationVar(&interval, "interval", 100*time.Millisecond, "interval between samples")
	snapshotCmd.Flags().StringVar(&format, "format", "terminal", "output format (terminal|json)")

	watchCmd.Flags().DurationVar(&watchRefresh, "refresh", 5*time.Second, "refresh interval")

	rootCmd.AddCommand(snapshotCmd)
	rootCmd.AddCommand(watchCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid config: %v", err)
	}
	return cfg
}

func createClients(cfg *config.Config) map[string]*rpc.Client {
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
	return clients
}

func runSnapshot(cmd *cobra.Command, args []string) {
	cfg := loadConfig()
	clients := createClients(cfg)
	collector := metrics.NewCollector()

	if format == "json" {
		output.DisableColors()
	}

	ctx := context.Background()

	// Capture latest block info during sampling
	latestBlocks := make(map[string]uint64)
	latestHashes := make(map[string]string)
	var mu sync.Mutex

	// Collect samples
	for i := 0; i < samples; i++ {
		g, gctx := errgroup.WithContext(ctx)
		
		for _, client := range clients {
			client := client // capture for closure
			g.Go(func() error {
				// We prioritize latest block check as it gives us latency and block height
				block, result := client.GetLatestBlock(gctx)
				
				if result.Success && block != nil {
					mu.Lock()
					if block.Number > latestBlocks[client.Name()] {
						latestBlocks[client.Name()] = block.Number
						latestHashes[client.Name()] = block.Hash
					}
					mu.Unlock()
				}
				
				collector.Add(result)
				return nil
			})
		}
		
		if err := g.Wait(); err != nil {
			log.Printf("Error during sampling: %v", err)
		}

		time.Sleep(interval)
	}

	providerMetrics := collector.Calculate()
	
	// Populate metrics with captured block info
	for name, m := range providerMetrics {
		if h, ok := latestBlocks[name]; ok {
			m.LatestBlock = h
			m.LatestBlockHash = latestHashes[name]
		}
	}
	
	// Phase 1: Extract heights from metrics
	heights := make(map[string]uint64)
	for name, m := range providerMetrics {
		if m.LatestBlock > 0 {
			heights[name] = m.LatestBlock
		}
	}

	// Find reference height (minimum height all providers have)
	var refHeight uint64 = math.MaxUint64
	var foundHeight bool
	for _, h := range heights {
		if h < refHeight {
			refHeight = h
			foundHeight = true
		}
	}
	
	var consistency *metrics.ConsistencyReport
	checker := metrics.NewConsistencyChecker()

	if foundHeight {
		// Phase 2: Fetch hash at reference height from each provider
		hashesAtRef := make(map[string]string)
		refHeightHex := fmt.Sprintf("0x%x", refHeight)

		g, gctx := errgroup.WithContext(ctx)
		for name, client := range clients {
			name, client := name, client // capture loop variables
			// Only fetch if provider reported a height (and thus is up)
			if _, ok := heights[name]; !ok {
				continue
			}

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

		// Phase 3: Run consistency check with same-height hashes
		consistency = checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
	} else {
		// Fallback if no heights collected (e.g. all down)
		consistency = &metrics.ConsistencyReport{
			Consistent: true, // Trivially true if no data
		}
	}

	report := &output.SnapshotReport{
		Timestamp:   time.Now(),
		SampleCount: samples,
		Providers:   providerMetrics,
		Consistency: consistency,
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Fatalf("Failed to encode report: %v", err)
		}
	} else {
		output.RenderSnapshotTerminal(report)
	}
}

func runWatch(cmd *cobra.Command, args []string) {
	cfg := loadConfig()
	clients := createClients(cfg)
	
	state := output.NewWatchState(watchRefresh, 20)
	checker := metrics.NewConsistencyChecker()
	
	// Initialize state
	for name := range clients {
		state.Providers[name] = &output.WatchProviderState{
			Name:   name,
			Status: metrics.StatusUp, // assume up initially
		}
	}

	output.ClearScreen()
	
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	ticker := time.NewTicker(watchRefresh)
	defer ticker.Stop()

	var hashCheckTick int

	// Initial tick
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var successfulProviders int
			heights := make(map[string]uint64)
			var mu sync.Mutex

			g, gctx := errgroup.WithContext(ctx)
			
			for name, client := range clients {
				name, client := name, client
				g.Go(func() error {
					start := time.Now()
					block, result := client.GetLatestBlock(gctx)
					latency := time.Since(start)

					mu.Lock()
					defer mu.Unlock()
					
					pState := state.Providers[name]
					pState.LastSeen = time.Now()
					
					if result.Success {
						pState.Latency = latency
						pState.BlockHeight = block.Number
						pState.BlockHash = block.Hash
						pState.LastError = ""
						
						// Simple status determination for watch mode
						if latency > 500*time.Millisecond {
							pState.Status = metrics.StatusSlow
						} else {
							pState.Status = metrics.StatusUp
						}
						
						heights[name] = block.Number
						successfulProviders++
					} else {
						pState.Status = metrics.StatusDown
						pState.LastError = result.Error.Error()
						state.AddEvent(name, fmt.Sprintf("Error: %s", result.Error), output.SeverityError)
					}
					
					return nil
				})
			}
			_ = g.Wait()

			// Consistency check
			var consistency *metrics.ConsistencyReport
			
			hashCheckTick++
			shouldCheckHashes := hashCheckTick%3 == 0 && successfulProviders >= 2

			if successfulProviders > 0 {
				if shouldCheckHashes {
					// Two-phase check
					// Find reference height
					var refHeight uint64 = math.MaxUint64
					for _, h := range heights {
						if h < refHeight {
							refHeight = h
						}
					}
					
					hashesAtRef := make(map[string]string)
					refHeightHex := fmt.Sprintf("0x%x", refHeight)

					g2, g2ctx := errgroup.WithContext(ctx)
					for name, client := range clients {
						name, client := name, client
						if _, ok := heights[name]; !ok {
							continue
						}
						
						g2.Go(func() error {
							block, result := client.GetBlockByNumber(g2ctx, refHeightHex, false)
							if result.Success && block != nil {
								mu.Lock()
								hashesAtRef[name] = block.Hash
								mu.Unlock()
							}
							return nil
						})
					}
					_ = g2.Wait()
					
					consistency = checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
				} else {
					// Construct partial report for height only
					// Use CheckWithSameHeightHashes with empty hashes to get height variance analysis
					var refHeight uint64 = math.MaxUint64
					for _, h := range heights {
						if h < refHeight {
							refHeight = h
						}
					}
					consistency = checker.CheckWithSameHeightHashes(heights, make(map[string]string), refHeight)
				}
			}

			output.RenderWatch(state, consistency)
		}
	}
}

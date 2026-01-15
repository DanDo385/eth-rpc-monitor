package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

func main() {
	var cfgPath string

	rootCmd := &cobra.Command{
		Use:          "monitor",
		Short:        "Monitor Ethereum RPC infrastructure",
		SilenceUsage: true,
	}
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "config/providers.yaml", "Path to provider config")

	var samples int
	var intervalMs int
	var format string
	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Generate a point-in-time report",
		RunE: func(cmd *cobra.Command, args []string) error {
			interval := time.Duration(intervalMs) * time.Millisecond
			return runSnapshot(cmd.Context(), cfgPath, samples, interval, format)
		},
	}
	snapshotCmd.Flags().IntVar(&samples, "samples", 30, "Number of samples per provider")
	snapshotCmd.Flags().IntVar(&intervalMs, "interval", 100, "Interval between samples in milliseconds")
	snapshotCmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	var refresh time.Duration
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream live provider health updates",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), cfgPath, refresh)
		},
	}
	watchCmd.Flags().DurationVar(&refresh, "refresh", 5*time.Second, "Refresh interval")

	rootCmd.AddCommand(snapshotCmd, watchCmd, blocksCmd(), txsCmd())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rootCmd.SetContext(ctx)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
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

func blocksCmd() *cobra.Command {
	var (
		rawOutput    bool
		providerName string
		format       string
	)

	cmd := &cobra.Command{
		Use:   "blocks [latest|number]",
		Short: "Fetch and display block details",
		Long: `Fetch block data from an Ethereum RPC provider.

Examples:
  monitor blocks latest
  monitor blocks 19000000
  monitor blocks 0x121eac0
  monitor blocks latest --raw
  monitor blocks latest --provider alchemy`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				// Fallback to searching persistent flags if not found directly
				cfgPath, _ = cmd.Root().PersistentFlags().GetString("config")
			}
			return runBlocks(cmd.Context(), args[0], rawOutput, providerName, format, cfgPath)
		},
	}

	cmd.Flags().BoolVar(&rawOutput, "raw", false, "Show raw JSON-RPC response")
	cmd.Flags().StringVar(&providerName, "provider", "", "Use specific provider (default: first available)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	return cmd
}

func runBlocks(ctx context.Context, blockArg string, rawOutput bool, providerName string, format string, cfgPath string) error {
	// Load config
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Parse block argument
	blockNum, err := parseBlockArg(blockArg)
	if err != nil {
		return err
	}

	// Get provider client
	client, usedProvider, err := getProviderClient(cfg, providerName)
	if err != nil {
		return err
	}

	// Fetch block
	// Use a timeout for the request
	reqCtx, cancel := context.WithTimeout(ctx, cfg.Defaults.Timeout)
	defer cancel()

	start := time.Now()
	block, rawResponse, result := client.GetBlockByNumberWithRaw(reqCtx, blockNum, false)
	latency := time.Since(start)

	if !result.Success {
		return fmt.Errorf("failed to fetch block: %v", result.Error)
	}

	if block == nil {
		return fmt.Errorf("block %s not found", blockArg)
	}

	// Render output
	bd := &output.BlockDisplay{
		Block:       block,
		Provider:    usedProvider,
		Latency:     latency,
		RawResponse: rawResponse,
	}

	if format == "json" {
		output.DisableColors()
		return output.RenderBlockJSON(bd, rawOutput)
	}

	output.RenderBlockTerminal(bd, rawOutput)
	return nil
}

// parseBlockArg converts user input to hex block number for RPC
func parseBlockArg(arg string) (string, error) {
	arg = strings.TrimSpace(arg)

	if arg == "latest" || arg == "pending" || arg == "earliest" {
		return arg, nil
	}

	// Already hex
	if strings.HasPrefix(arg, "0x") {
		// Validate it's valid hex
		_, err := strconv.ParseUint(arg[2:], 16, 64)
		if err != nil {
			return "", fmt.Errorf("invalid hex block number: %s", arg)
		}
		return arg, nil
	}

	// Decimal - convert to hex
	num, err := strconv.ParseUint(arg, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid block number: %s (use decimal, hex with 0x prefix, or 'latest')", arg)
	}
	return fmt.Sprintf("0x%x", num), nil
}

// getProviderClient returns a client for the specified provider or first available
func getProviderClient(cfg *config.Config, providerName string) (*rpc.Client, string, error) {
	for _, p := range cfg.Providers {
		if providerName == "" || p.Name == providerName {
			client := rpc.NewClient(rpc.ClientConfig{
				Name:           p.Name,
				URL:            p.URL,
				Timeout:        p.Timeout,
				MaxRetries:     cfg.Defaults.MaxRetries,
				BackoffInitial: cfg.Defaults.BackoffInitial,
				BackoffMax:     cfg.Defaults.BackoffMax,
			})
			return client, p.Name, nil
		}
	}

	if providerName != "" {
		return nil, "", fmt.Errorf("provider '%s' not found in config", providerName)
	}
	return nil, "", fmt.Errorf("no providers configured")
}

func loadConfig(path string) (*config.Config, error) {
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
	for _, provider := range cfg.Providers {
		clients[provider.Name] = rpc.NewClient(rpc.ClientConfig{
			Name:           provider.Name,
			URL:            provider.URL,
			Timeout:        provider.Timeout,
			MaxRetries:     cfg.Defaults.MaxRetries,
			BackoffInitial: cfg.Defaults.BackoffInitial,
			BackoffMax:     cfg.Defaults.BackoffMax,
		})
	}
	return clients
}

func buildConsistency(
	ctx context.Context,
	clients map[string]*rpc.Client,
	providerMetrics map[string]*metrics.ProviderMetrics,
) *metrics.ConsistencyReport {
	heights := make(map[string]uint64)
	for name, m := range providerMetrics {
		if m.LatestBlock > 0 {
			heights[name] = m.LatestBlock
		}
	}

	if len(heights) == 0 {
		return &metrics.ConsistencyReport{
			Heights:    heights,
			Hashes:     make(map[string]string),
			Consistent: false,
			Issues:     []string{"No successful block heights available for consistency check"},
		}
	}

	refHeight, _ := minHeight(heights)
	hashesAtRef := fetchHashesAtHeight(ctx, clients, refHeight)

	checker := metrics.NewConsistencyChecker()
	return checker.CheckWithSameHeightHashes(heights, hashesAtRef, refHeight)
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

func fetchHashesAtHeight(ctx context.Context, clients map[string]*rpc.Client, refHeight uint64) map[string]string {
	hashesAtRef := make(map[string]string)
	var mu sync.Mutex
	refHeightHex := fmt.Sprintf("0x%x", refHeight)

	g, gctx := errgroup.WithContext(ctx)
	for name, client := range clients {
		name, client := name, client
		g.Go(func() error {
			block, result := client.GetBlockByNumber(gctx, refHeightHex, false)
			if result.Success && block != nil {
				mu.Lock()
				hashesAtRef[name] = block.Hash
				mu.Unlock()
			}
			return nil
		})
	}
	_ = g.Wait()

	return hashesAtRef
}

func minHeight(heights map[string]uint64) (uint64, bool) {
	if len(heights) == 0 {
		return 0, false
	}
	minHeight := uint64(math.MaxUint64)
	for _, height := range heights {
		if height < minHeight {
			minHeight = height
		}
	}
	return minHeight, true
}

func statusFromLatency(latency time.Duration) metrics.ProviderStatus {
	if latency > 500*time.Millisecond {
		return metrics.StatusSlow
	}
	return metrics.StatusUp
}

func statusChangeMessage(status metrics.ProviderStatus, lastError string) string {
	switch status {
	case metrics.StatusUp:
		return "provider recovered"
	case metrics.StatusSlow:
		return "provider latency elevated"
	case metrics.StatusDown:
		if lastError != "" {
			return fmt.Sprintf("provider down: %s", lastError)
		}
		return "provider down"
	default:
		return "status updated"
	}
}

func statusSeverity(status metrics.ProviderStatus) output.EventSeverity {
	switch status {
	case metrics.StatusDown:
		return output.SeverityError
	case metrics.StatusSlow:
		return output.SeverityWarning
	default:
		return output.SeverityInfo
	}
}

func txsCmd() *cobra.Command {
	var (
		rawOutput    bool
		providerName string
		limit        int
		format       string
	)

	cmd := &cobra.Command{
		Use:   "txs [latest|number]",
		Short: "List transactions in a block",
		Long: `List all transactions within a specified Ethereum block.

Examples:
  monitor txs latest
  monitor txs 19000000
  monitor txs 19000000 --limit 10
  monitor txs 19000000 --raw
  monitor txs 19000000 --format json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			if cfgPath == "" {
				cfgPath, _ = cmd.Root().PersistentFlags().GetString("config")
			}
			return runTxs(cmd.Context(), args[0], rawOutput, providerName, limit, format, cfgPath)
		},
	}

	cmd.Flags().BoolVar(&rawOutput, "raw", false, "Show raw JSON transaction array")
	cmd.Flags().StringVar(&providerName, "provider", "", "Use specific provider")
	cmd.Flags().IntVar(&limit, "limit", 25, "Maximum transactions to display (0 for all)")
	cmd.Flags().StringVar(&format, "format", "terminal", "Output format: terminal|json")

	return cmd
}

func runTxs(ctx context.Context, blockArg string, rawOutput bool, providerName string, limit int, format string, cfgPath string) error {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	blockNum, err := parseBlockArg(blockArg)
	if err != nil {
		return err
	}

	client, usedProvider, err := getProviderClient(cfg, providerName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Defaults.Timeout*2) // Longer timeout for full txs
	defer cancel()

	start := time.Now()
	blockWithTxs, rawResponse, result := client.GetBlockWithTransactions(ctx, blockNum)
	latency := time.Since(start)

	if !result.Success {
		return fmt.Errorf("failed to fetch block: %v", result.Error)
	}

	if blockWithTxs == nil {
		return fmt.Errorf("block %s not found", blockArg)
	}

	td := &output.TxDisplay{
		BlockNumber:  blockWithTxs.Number,
		Transactions: blockWithTxs.Transactions,
		TotalCount:   len(blockWithTxs.Transactions),
		Limit:        limit,
		Provider:     usedProvider,
		Latency:      latency,
		RawResponse:  rawResponse,
	}

	if format == "json" {
		output.DisableColors()
		return output.RenderTxsJSON(td, rawOutput)
	}

	output.RenderTxsTerminal(td, rawOutput)
	return nil
}

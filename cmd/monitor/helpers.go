package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
	"github.com/dmagro/eth-rpc-monitor/internal/output"
	"github.com/dmagro/eth-rpc-monitor/internal/provider"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

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

// getProviderClient returns a client for the specified provider or auto-selects best
func getProviderClient(cfg *config.Config, providerName string) (*rpc.Client, string, error) {
	// If provider specified, use that one
	if providerName != "" {
		for _, p := range cfg.Providers {
			if p.Name == providerName {
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
		return nil, "", fmt.Errorf("provider '%s' not found in config", providerName)
	}

	// Auto-select: run quick health check
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ranked, err := provider.QuickHealthCheck(ctx, cfg, 3) // Fast check with 3 samples
	if err != nil {
		// Fallback to first provider
		if len(cfg.Providers) > 0 {
			p := cfg.Providers[0]
			client := rpc.NewClient(rpc.ClientConfig{
				Name:           p.Name,
				URL:            p.URL,
				Timeout:        p.Timeout,
				MaxRetries:     cfg.Defaults.MaxRetries,
				BackoffInitial: cfg.Defaults.BackoffInitial,
				BackoffMax:     cfg.Defaults.BackoffMax,
			})
			fmt.Fprintf(os.Stderr, "Warning: health check failed, using first provider: %s\n", p.Name)
			return client, p.Name, nil
		}
		return nil, "", fmt.Errorf("no providers available")
	}

	best, err := ranked.Best()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
	}

	// Find the provider config
	for _, p := range cfg.Providers {
		if p.Name == best.Name {
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

	return nil, "", fmt.Errorf("selected provider not found in config")
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

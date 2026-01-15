package provider

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/config"
	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
	"golang.org/x/sync/errgroup"
)

// ProviderHealth holds health check results for a provider
type ProviderHealth struct {
	Name          string
	Status        string // UP, SLOW, DEGRADED, DOWN
	SuccessRate   float64
	AvgLatency    time.Duration
	P95Latency    time.Duration
	BlockHeight   uint64
	BlockDelta    int
	Score         float64
	Excluded      bool
	ExcludeReason string
	Samples       int
}

// RankedProviders is a sorted list of providers by score
type RankedProviders []ProviderHealth

// QuickHealthCheck performs a fast health check on all providers
func QuickHealthCheck(ctx context.Context, cfg *config.Config, samples int) (RankedProviders, error) {
	if samples <= 0 {
		samples = 5
	}

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

	type sampleResult struct {
		provider string
		latency  time.Duration
		height   uint64
		success  bool
	}

	results := make([]sampleResult, 0, len(clients)*samples)
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for name, client := range clients {
		name, client := name, client
		g.Go(func() error {
			for i := 0; i < samples; i++ {
				select {
				case <-gctx.Done():
					return gctx.Err()
				default:
				}

				start := time.Now()
				height, result := client.BlockNumber(gctx)
				latency := time.Since(start)

				mu.Lock()
				results = append(results, sampleResult{
					provider: name,
					latency:  latency,
					height:   height,
					success:  result.Success,
				})
				mu.Unlock()

				if i < samples-1 {
					time.Sleep(50 * time.Millisecond)
				}
			}
			return nil
		})
	}

	_ = g.Wait()

	providerData := make(map[string]*struct {
		latencies []time.Duration
		heights   []uint64
		successes int
		total     int
	})

	for _, r := range results {
		if providerData[r.provider] == nil {
			providerData[r.provider] = &struct {
				latencies []time.Duration
				heights   []uint64
				successes int
				total     int
			}{}
		}
		pd := providerData[r.provider]
		pd.total++
		if r.success {
			pd.successes++
			pd.latencies = append(pd.latencies, r.latency)
			pd.heights = append(pd.heights, r.height)
		}
	}

	if len(providerData) == 0 {
		return nil, fmt.Errorf("no providers available")
	}

	var maxHeight uint64
	for _, pd := range providerData {
		for _, h := range pd.heights {
			if h > maxHeight {
				maxHeight = h
			}
		}
	}

	ranked := make(RankedProviders, 0, len(providerData))
	for name, pd := range providerData {
		health := ProviderHealth{
			Name:    name,
			Samples: pd.total,
		}

		if pd.total == 0 {
			health.Status = "DOWN"
			health.Excluded = true
			health.ExcludeReason = "no samples collected"
			ranked = append(ranked, health)
			continue
		}

		health.SuccessRate = float64(pd.successes) / float64(pd.total) * 100

		if len(pd.latencies) > 0 {
			health.AvgLatency = avgDuration(pd.latencies)
			health.P95Latency = percentileDuration(pd.latencies, 95)
		}

		if len(pd.heights) > 0 {
			health.BlockHeight = pd.heights[len(pd.heights)-1]
			health.BlockDelta = int(maxHeight - health.BlockHeight)
		}

		switch {
		case health.SuccessRate < 50:
			health.Status = "DOWN"
		case health.SuccessRate < 90:
			health.Status = "DEGRADED"
		case health.P95Latency > 500*time.Millisecond:
			health.Status = "SLOW"
		default:
			health.Status = "UP"
		}

		health.Score = calculateScore(health)

		if health.SuccessRate < 80 {
			health.Excluded = true
			health.ExcludeReason = fmt.Sprintf("success rate %.1f%% below threshold", health.SuccessRate)
		} else if health.BlockDelta > 5 {
			health.Excluded = true
			health.ExcludeReason = fmt.Sprintf("%d blocks behind", health.BlockDelta)
		}

		ranked = append(ranked, health)
	}

	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		return ranked[i].Name < ranked[j].Name
	})

	return ranked, nil
}

// Best returns the best non-excluded provider
func (rp RankedProviders) Best() (ProviderHealth, error) {
	for _, p := range rp {
		if !p.Excluded {
			return p, nil
		}
	}
	if len(rp) > 0 {
		return rp[0], fmt.Errorf("all providers degraded, using least-bad: %s", rp[0].Name)
	}
	return ProviderHealth{}, fmt.Errorf("no providers available")
}

func calculateScore(h ProviderHealth) float64 {
	successScore := h.SuccessRate / 100.0

	latencyMs := float64(h.P95Latency.Milliseconds())
	latencyScore := 1.0 - (latencyMs / 1000.0)
	if latencyScore < 0 {
		latencyScore = 0
	}

	freshnessScore := 1.0 - (float64(h.BlockDelta) / 10.0)
	if freshnessScore < 0 {
		freshnessScore = 0
	}

	return (successScore * 0.5) + (latencyScore * 0.3) + (freshnessScore * 0.2)
}

func avgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return total / time.Duration(len(durations))
}

func percentileDuration(durations []time.Duration, p int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	index := (p * len(sorted)) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

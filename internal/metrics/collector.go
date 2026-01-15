package metrics

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

// ProviderStatus represents the health state of a provider
type ProviderStatus string

const (
	StatusUp       ProviderStatus = "UP"
	StatusSlow     ProviderStatus = "SLOW"
	StatusDegraded ProviderStatus = "DEGRADED"
	StatusDown     ProviderStatus = "DOWN"
)

// ProviderMetrics holds calculated metrics for a single provider
type ProviderMetrics struct {
	Name        string
	Status      ProviderStatus
	LatencyAvg  time.Duration
	LatencyP50  time.Duration
	LatencyP95  time.Duration
	LatencyP99  time.Duration
	LatencyMax  time.Duration
	SuccessRate float64
	TotalCalls  int
	Failures    int

	// Error breakdown
	Timeouts     int
	RateLimits   int
	ServerErrors int
	ParseErrors  int
	OtherErrors  int

	// Block data (from most recent successful call)
	LatestBlock     uint64
	LatestBlockHash string

	// Raw samples for further analysis
	Samples []rpc.CallResult
}

// Collector aggregates call results and calculates metrics
type Collector struct {
	results map[string][]rpc.CallResult // provider name -> results
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		results: make(map[string][]rpc.CallResult),
	}
}

// Add records a call result for a provider
func (c *Collector) Add(result *rpc.CallResult) {
	c.results[result.Provider] = append(c.results[result.Provider], *result)
}

// Calculate computes metrics for all providers
func (c *Collector) Calculate() map[string]*ProviderMetrics {
	metrics := make(map[string]*ProviderMetrics)

	for provider, samples := range c.results {
		metrics[provider] = calculateProviderMetrics(provider, samples)
	}

	return metrics
}

// calculateProviderMetrics computes metrics for a single provider
func calculateProviderMetrics(name string, samples []rpc.CallResult) *ProviderMetrics {
	m := &ProviderMetrics{
		Name:    name,
		Samples: samples,
	}

	if len(samples) == 0 {
		m.Status = StatusDown
		return m
	}

	// Collect successful latencies
	var latencies []time.Duration
	var successCount int

	for _, s := range samples {
		m.TotalCalls++

		if s.Success {
			successCount++
			latencies = append(latencies, s.Latency)

			// Best-effort extract latest block number/hash from eth_getBlockByNumber responses.
			if s.Method == "eth_getBlockByNumber" && s.Response != nil {
				var blockData struct {
					Number string `json:"number"`
					Hash   string `json:"hash"`
				}
				if err := json.Unmarshal(s.Response.Result, &blockData); err == nil {
					if n, err := parseHexUint64(blockData.Number); err == nil {
						m.LatestBlock = n
						m.LatestBlockHash = blockData.Hash
					}
				}
			}
		} else {
			m.Failures++

			// Categorize error
			switch s.ErrorType {
			case rpc.ErrorTypeTimeout:
				m.Timeouts++
			case rpc.ErrorTypeRateLimit:
				m.RateLimits++
			case rpc.ErrorTypeServerError:
				m.ServerErrors++
			case rpc.ErrorTypeParseError:
				m.ParseErrors++
			default:
				m.OtherErrors++
			}
		}
	}

	// Calculate success rate
	m.SuccessRate = float64(successCount) / float64(m.TotalCalls) * 100

	// Calculate latency percentiles
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool {
			return latencies[i] < latencies[j]
		})

		m.LatencyAvg = avgDuration(latencies)
		m.LatencyP50 = percentile(latencies, 50)
		m.LatencyP95 = percentile(latencies, 95)
		m.LatencyP99 = percentile(latencies, 99)
		m.LatencyMax = latencies[len(latencies)-1]
	}

	// Determine status
	m.Status = determineStatus(m.SuccessRate, m.LatencyP95)

	return m
}

func parseHexUint64(hex string) (uint64, error) {
	hex = strings.TrimPrefix(hex, "0x")
	if hex == "" {
		return 0, nil
	}

	val := new(big.Int)
	_, ok := val.SetString(hex, 16)
	if !ok {
		return 0, fmt.Errorf("invalid hex string: %s", hex)
	}
	if !val.IsUint64() {
		return 0, fmt.Errorf("value overflows uint64: %s", hex)
	}
	return val.Uint64(), nil
}

// determineStatus categorizes provider health based on metrics
func determineStatus(successRate float64, p95Latency time.Duration) ProviderStatus {
	// Thresholds (these could be configurable)
	const (
		downThreshold     = 50.0 // <50% success = DOWN
		degradedThreshold = 90.0 // <90% success = DEGRADED
		slowLatency       = 500 * time.Millisecond
	)

	if successRate < downThreshold {
		return StatusDown
	}
	if successRate < degradedThreshold {
		return StatusDegraded
	}
	if p95Latency > slowLatency {
		return StatusSlow
	}
	return StatusUp
}

// percentile calculates the nth percentile of sorted durations
func percentile(sorted []time.Duration, n int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	index := (n * len(sorted)) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// avgDuration calculates the average of durations
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

package output

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
)

// JSONReport is the machine-readable output format
type JSONReport struct {
	Metadata    JSONMetadata              `json:"metadata"`
	Providers   []JSONProviderMetrics     `json:"providers"`
	Consistency JSONConsistencyReport     `json:"consistency"`
	Assessment  JSONAssessment            `json:"assessment"`
}

// JSONMetadata holds report metadata
type JSONMetadata struct {
	Timestamp   time.Time `json:"timestamp"`
	SampleCount int       `json:"sample_count"`
	Version     string    `json:"version"`
}

// JSONProviderMetrics holds provider metrics in JSON format
type JSONProviderMetrics struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	LatencyMs   JSONLatency `json:"latency_ms"`
	SuccessRate float64 `json:"success_rate"`
	TotalCalls  int     `json:"total_calls"`
	Errors      JSONErrors `json:"errors"`
	Block       JSONBlockInfo `json:"block"`
}

// JSONLatency holds latency percentiles
type JSONLatency struct {
	Avg float64 `json:"avg"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// JSONErrors holds error counts by type
type JSONErrors struct {
	Timeout     int `json:"timeout"`
	RateLimit   int `json:"rate_limit"`
	ServerError int `json:"server_error"`
	ParseError  int `json:"parse_error"`
	Other       int `json:"other"`
	Total       int `json:"total"`
}

// JSONBlockInfo holds block data for a provider
type JSONBlockInfo struct {
	Height uint64 `json:"height"`
	Hash   string `json:"hash"`
}

// JSONConsistencyReport holds consistency check results
type JSONConsistencyReport struct {
	HeightConsensus bool     `json:"height_consensus"`
	HashConsensus   bool     `json:"hash_consensus"`
	MaxHeight       uint64   `json:"max_height"`
	HeightVariance  int      `json:"height_variance_blocks"`
	Issues          []string `json:"issues,omitempty"`
}

// JSONAssessment holds the operational assessment
type JSONAssessment struct {
	Healthy     bool     `json:"healthy"`
	UpProviders []string `json:"up_providers"`
	DegradedProviders []string `json:"degraded_providers,omitempty"`
	DownProviders []string `json:"down_providers,omitempty"`
	Recommendation string `json:"recommendation,omitempty"`
}

// RenderSnapshotJSON outputs the report as JSON to stdout
func RenderSnapshotJSON(report *SnapshotReport) error {
	jsonReport := convertToJSON(report)

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	return encoder.Encode(jsonReport)
}

// convertToJSON transforms the internal report to JSON format
func convertToJSON(report *SnapshotReport) *JSONReport {
	jr := &JSONReport{
		Metadata: JSONMetadata{
			Timestamp:   report.Timestamp,
			SampleCount: report.SampleCount,
			Version:     "1.0.0",
		},
		Providers: make([]JSONProviderMetrics, 0, len(report.Providers)),
	}

	// Convert provider metrics
	var upProviders, degradedProviders, downProviders []string

	for name, m := range report.Providers {
		pm := JSONProviderMetrics{
			Name:        name,
			Status:      string(m.Status),
			SuccessRate: m.SuccessRate,
			TotalCalls:  m.TotalCalls,
			LatencyMs: JSONLatency{
				Avg: float64(m.LatencyAvg.Milliseconds()),
				P50: float64(m.LatencyP50.Milliseconds()),
				P95: float64(m.LatencyP95.Milliseconds()),
				P99: float64(m.LatencyP99.Milliseconds()),
				Max: float64(m.LatencyMax.Milliseconds()),
			},
			Errors: JSONErrors{
				Timeout:     m.Timeouts,
				RateLimit:   m.RateLimits,
				ServerError: m.ServerErrors,
				ParseError:  m.ParseErrors,
				Other:       m.OtherErrors,
				Total:       m.Failures,
			},
			Block: JSONBlockInfo{
				Height: m.LatestBlock,
				Hash:   m.LatestBlockHash,
			},
		}
		jr.Providers = append(jr.Providers, pm)

		// Categorize for assessment
		switch m.Status {
		case metrics.StatusUp:
			upProviders = append(upProviders, name)
		case metrics.StatusSlow, metrics.StatusDegraded:
			degradedProviders = append(degradedProviders, name)
		case metrics.StatusDown:
			downProviders = append(downProviders, name)
		}
	}

	// Convert consistency report
	jr.Consistency = JSONConsistencyReport{
		HeightConsensus: report.Consistency.HeightConsensus,
		HashConsensus:   report.Consistency.HashConsensus,
		MaxHeight:       report.Consistency.MaxHeight,
		HeightVariance:  report.Consistency.HeightVariance,
		Issues:          report.Consistency.Issues,
	}

	// Build assessment
	jr.Assessment = JSONAssessment{
		Healthy:           len(downProviders) == 0 && report.Consistency.Consistent,
		UpProviders:       upProviders,
		DegradedProviders: degradedProviders,
		DownProviders:     downProviders,
	}

	if len(upProviders) > 0 {
		jr.Assessment.Recommendation = fmt.Sprintf("Use providers in order: %v", upProviders)
	}

	return jr
}

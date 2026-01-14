package metrics

import (
	"fmt"
)

// ConsistencyReport holds the results of cross-provider consistency checks
type ConsistencyReport struct {
	// Block height analysis
	Heights         map[string]uint64 // provider -> block height
	MaxHeight       uint64
	HeightVariance  int    // max difference in blocks
	HeightConsensus bool   // all providers within acceptable range
	AuthoritativeProvider string // provider reporting highest block

	// Block hash analysis (at reference height)
	ReferenceHeight uint64
	Hashes          map[string]string // provider -> block hash
	HashConsensus   bool              // all providers agree on hash
	HashGroups      []HashGroup       // groups of providers with same hash

	// Overall assessment
	Consistent bool
	Issues     []string
}

// HashGroup represents providers that reported the same block hash
type HashGroup struct {
	Hash      string
	Providers []string
}

// ConsistencyChecker validates data consistency across providers
type ConsistencyChecker struct {
	acceptableHeightDrift int // max blocks behind before flagging
}

// NewConsistencyChecker creates a new consistency checker
func NewConsistencyChecker() *ConsistencyChecker {
	return &ConsistencyChecker{
		acceptableHeightDrift: 2, // 2 blocks (~24 seconds) is acceptable
	}
}

// BlockHeightData holds block information from a single provider
type BlockHeightData struct {
	Provider string
	Height   uint64
	Hash     string
	Success  bool
}

// Check performs consistency analysis across all provider data
func (c *ConsistencyChecker) Check(data []BlockHeightData) *ConsistencyReport {
	report := &ConsistencyReport{
		Heights:    make(map[string]uint64),
		Hashes:     make(map[string]string),
		Consistent: true,
	}

	// Collect heights and find max
	var maxHeight uint64
	var maxProvider string

	for _, d := range data {
		if !d.Success {
			continue
		}

		report.Heights[d.Provider] = d.Height
		report.Hashes[d.Provider] = d.Hash

		if d.Height > maxHeight {
			maxHeight = d.Height
			maxProvider = d.Provider
		}
	}

	report.MaxHeight = maxHeight
	report.AuthoritativeProvider = maxProvider

	// Check height consensus
	var minHeight uint64 = maxHeight
	for _, height := range report.Heights {
		if height < minHeight {
			minHeight = height
		}
	}

	report.HeightVariance = int(maxHeight - minHeight)
	report.HeightConsensus = report.HeightVariance <= c.acceptableHeightDrift

	if !report.HeightConsensus {
		report.Consistent = false
		report.Issues = append(report.Issues,
			fmt.Sprintf("Block height variance of %d blocks exceeds threshold", report.HeightVariance))
	}

	// Check hash consensus at the minimum common height
	// (We can only compare hashes at heights all providers have seen)
	report.ReferenceHeight = minHeight
	c.checkHashConsensus(report)

	return report
}

// checkHashConsensus analyzes hash agreement across providers
func (c *ConsistencyChecker) checkHashConsensus(report *ConsistencyReport) {
	// Group providers by hash
	hashToProviders := make(map[string][]string)

	for provider, hash := range report.Hashes {
		// Only include providers at or above reference height
		if report.Heights[provider] >= report.ReferenceHeight {
			hashToProviders[hash] = append(hashToProviders[hash], provider)
		}
	}

	// Build hash groups
	for hash, providers := range hashToProviders {
		report.HashGroups = append(report.HashGroups, HashGroup{
			Hash:      hash,
			Providers: providers,
		})
	}

	// Check consensus
	report.HashConsensus = len(report.HashGroups) <= 1

	if !report.HashConsensus {
		report.Consistent = false

		// Find the majority hash
		var majorityCount int
		for _, group := range report.HashGroups {
			if len(group.Providers) > majorityCount {
				majorityCount = len(group.Providers)
			}
		}

		// Flag minority providers
		for _, group := range report.HashGroups {
			if len(group.Providers) < majorityCount {
				report.Issues = append(report.Issues,
					fmt.Sprintf("Provider(s) %v report different block hash at height %d (possible reorg or stale data)",
						group.Providers, report.ReferenceHeight))
			}
		}
	}
}

// FormatHeightDrift returns a human-readable description of height drift
func FormatHeightDrift(drift int) string {
	if drift == 0 {
		return "all providers in sync"
	}

	// Assuming ~12 second block time
	seconds := drift * 12

	if seconds < 60 {
		return fmt.Sprintf("%d block(s) behind (~%ds)", drift, seconds)
	}

	minutes := seconds / 60
	return fmt.Sprintf("%d block(s) behind (~%dm)", drift, minutes)
}

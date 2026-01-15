package metrics

import (
	"fmt"
	"sort"
)

// ConsistencyReport holds the results of cross-provider consistency checks
type ConsistencyReport struct {
	// Block height analysis
	Heights               map[string]uint64 // provider -> block height
	MaxHeight             uint64
	HeightVariance        int    // max difference in blocks
	HeightConsensus       bool   // all providers within acceptable range
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

// HeightData holds just height information (Phase 1 of consistency check)
type HeightData struct {
	Provider string
	Height   uint64
	Success  bool
}

// HashData holds hash at a specific height (Phase 2 of consistency check)
type HashData struct {
	Provider string
	Height   uint64
	Hash     string
	Success  bool
}

// Check performs consistency analysis across all provider data
// DEPRECATED: Use CheckTwoPhase instead to avoid comparing hashes from different heights
func (c *ConsistencyChecker) Check(data []BlockHeightData) *ConsistencyReport {
	// For backward compatibility, extract heights and hashes separately
	heights := make([]HeightData, 0, len(data))
	hashes := make([]HashData, 0, len(data))

	for _, d := range data {
		if d.Success {
			heights = append(heights, HeightData{
				Provider: d.Provider,
				Height:   d.Height,
				Success:  true,
			})
		}
	}

	// Use two-phase check
	return c.CheckTwoPhase(heights, hashes)
}

// CheckTwoPhase performs consistency analysis using a two-phase approach:
// Phase 1: Collect heights from all providers, find reference height (min height)
// Phase 2: Compare hashes ONLY at the reference height
// This prevents false positives from comparing hashes of different blocks.
func (c *ConsistencyChecker) CheckTwoPhase(heights []HeightData, hashes []HashData) *ConsistencyReport {
	report := &ConsistencyReport{
		Heights:    make(map[string]uint64),
		Hashes:     make(map[string]string),
		Consistent: true,
	}

	// Phase 1: Collect heights and find max/min
	var maxHeight uint64
	var maxProvider string
	var minHeight uint64
	var hasValidHeight bool

	for _, d := range heights {
		if !d.Success {
			continue
		}

		report.Heights[d.Provider] = d.Height

		if d.Height > maxHeight {
			maxHeight = d.Height
			maxProvider = d.Provider
		}

		if !hasValidHeight || d.Height < minHeight {
			minHeight = d.Height
			hasValidHeight = true
		}
	}

	report.MaxHeight = maxHeight
	report.AuthoritativeProvider = maxProvider
	report.ReferenceHeight = minHeight

	// Check height consensus
	report.HeightVariance = int(maxHeight - minHeight)
	report.HeightConsensus = report.HeightVariance <= c.acceptableHeightDrift

	if !report.HeightConsensus {
		report.Consistent = false
		report.Issues = append(report.Issues,
			fmt.Sprintf("Block height variance of %d blocks exceeds threshold", report.HeightVariance))
	}

	// Phase 2: Check hash consensus ONLY at reference height
	// CRITICAL: Only compare hashes from the same block height
	for _, d := range hashes {
		if !d.Success {
			continue
		}

		// Only include hashes at exactly the reference height
		if d.Height == report.ReferenceHeight {
			report.Hashes[d.Provider] = d.Hash
		}
	}

	c.checkHashConsensus(report)

	return report
}

// CheckWithSameHeightHashes performs consistency check using hashes fetched at the same block height
func (c *ConsistencyChecker) CheckWithSameHeightHashes(
	heights map[string]uint64,
	hashesAtRef map[string]string,
	referenceHeight uint64,
) *ConsistencyReport {
	report := &ConsistencyReport{
		Heights:         heights,
		Hashes:          hashesAtRef,
		ReferenceHeight: referenceHeight,
		Consistent:      true,
	}

	// Find max height
	var maxHeight uint64
	var maxProvider string
	for provider, height := range heights {
		if height > maxHeight {
			maxHeight = height
			maxProvider = provider
		}
	}
	report.MaxHeight = maxHeight
	report.AuthoritativeProvider = maxProvider

	// Calculate height variance
	var minHeight uint64 = maxHeight
	for _, height := range heights {
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

	// Group providers by their hash at reference height
	hashToProviders := make(map[string][]string)
	for provider, hash := range hashesAtRef {
		if hash != "" { // Only include providers that successfully returned a hash
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

	// Sort hash groups by number of providers (descending) for consistent output
	sort.Slice(report.HashGroups, func(i, j int) bool {
		return len(report.HashGroups[i].Providers) > len(report.HashGroups[j].Providers)
	})

	// Check hash consensus
	report.HashConsensus = len(report.HashGroups) <= 1

	if !report.HashConsensus && len(report.HashGroups) > 1 {
		report.Consistent = false
		// Identify minority groups (not the largest)
		majorityCount := len(report.HashGroups[0].Providers)
		for _, group := range report.HashGroups[1:] {
			if len(group.Providers) < majorityCount {
				report.Issues = append(report.Issues,
					fmt.Sprintf("Provider(s) %v report different block hash at height %d (possible reorg or stale cache)",
						group.Providers, referenceHeight))
			}
		}
	}

	return report
}

// checkHashConsensus analyzes hash agreement across providers
// NOTE: This function assumes all hashes in report.Hashes are from the same height (report.ReferenceHeight)
func (c *ConsistencyChecker) checkHashConsensus(report *ConsistencyReport) {
	if len(report.Hashes) == 0 {
		// No hash data available
		report.HashConsensus = false
		return
	}

	// Group providers by hash
	hashToProviders := make(map[string][]string)

	for provider, hash := range report.Hashes {
		// All hashes should be at referenceHeight (enforced in CheckTwoPhase)
		hashToProviders[hash] = append(hashToProviders[hash], provider)
	}

	// Build hash groups
	for hash, providers := range hashToProviders {
		report.HashGroups = append(report.HashGroups, HashGroup{
			Hash:      hash,
			Providers: providers,
		})
	}

	// Check consensus - all providers should agree on the same hash
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

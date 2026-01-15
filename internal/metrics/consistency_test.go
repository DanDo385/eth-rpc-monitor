package metrics

import (
	"testing"
)

func TestCheckWithSameHeightHashes(t *testing.T) {
	checker := NewConsistencyChecker()

	tests := []struct {
		name          string
		heights       map[string]uint64
		hashes        map[string]string
		refHeight     uint64
		wantConsensus bool
		wantGroups    int
	}{
		{
			name:          "all_same_hash",
			heights:       map[string]uint64{"a": 100, "b": 100, "c": 100},
			hashes:        map[string]string{"a": "0xabc", "b": "0xabc", "c": "0xabc"},
			refHeight:     100,
			wantConsensus: true,
			wantGroups:    1,
		},
		{
			name:          "one_different_hash",
			heights:       map[string]uint64{"a": 100, "b": 100, "c": 100},
			hashes:        map[string]string{"a": "0xabc", "b": "0xabc", "c": "0xdef"},
			refHeight:     100,
			wantConsensus: false,
			wantGroups:    2,
		},
		{
			name:          "single_provider",
			heights:       map[string]uint64{"a": 100},
			hashes:        map[string]string{"a": "0xabc"},
			refHeight:     100,
			wantConsensus: true,
			wantGroups:    1,
		},
		{
			name:          "empty_hash_excluded",
			heights:       map[string]uint64{"a": 100, "b": 100},
			hashes:        map[string]string{"a": "0xabc", "b": ""},
			refHeight:     100,
			wantConsensus: true,
			wantGroups:    1,
		},
		{
			name:          "all_different_hashes",
			heights:       map[string]uint64{"a": 100, "b": 100, "c": 100},
			hashes:        map[string]string{"a": "0xabc", "b": "0xdef", "c": "0xghi"},
			refHeight:     100,
			wantConsensus: false,
			wantGroups:    3,
		},
		{
			name:          "no_providers",
			heights:       map[string]uint64{},
			hashes:        map[string]string{},
			refHeight:     0,
			wantConsensus: true,
			wantGroups:    0,
		},
		{
			name:          "height_variance_within_threshold",
			heights:       map[string]uint64{"a": 100, "b": 99, "c": 98},
			hashes:        map[string]string{"a": "0xabc", "b": "0xabc", "c": "0xabc"},
			refHeight:     98,
			wantConsensus: true,
			wantGroups:    1,
		},
		{
			name:          "height_variance_exceeds_threshold",
			heights:       map[string]uint64{"a": 100, "b": 95},
			hashes:        map[string]string{"a": "0xabc", "b": "0xabc"},
			refHeight:     95,
			wantConsensus: true, // Hash consensus is still true
			wantGroups:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := checker.CheckWithSameHeightHashes(tt.heights, tt.hashes, tt.refHeight)

			if report.HashConsensus != tt.wantConsensus {
				t.Errorf("HashConsensus = %v, want %v", report.HashConsensus, tt.wantConsensus)
			}
			if len(report.HashGroups) != tt.wantGroups {
				t.Errorf("HashGroups count = %d, want %d", len(report.HashGroups), tt.wantGroups)
			}
			if report.ReferenceHeight != tt.refHeight {
				t.Errorf("ReferenceHeight = %d, want %d", report.ReferenceHeight, tt.refHeight)
			}
		})
	}
}

func TestCheckWithSameHeightHashes_HeightVariance(t *testing.T) {
	checker := NewConsistencyChecker()

	// Test height variance detection
	heights := map[string]uint64{"a": 100, "b": 95}
	hashes := map[string]string{"a": "0xabc", "b": "0xabc"}
	refHeight := uint64(95)

	report := checker.CheckWithSameHeightHashes(heights, hashes, refHeight)

	if report.HeightConsensus {
		t.Error("HeightConsensus should be false when variance exceeds threshold")
	}
	if report.HeightVariance != 5 {
		t.Errorf("HeightVariance = %d, want 5", report.HeightVariance)
	}
	if report.MaxHeight != 100 {
		t.Errorf("MaxHeight = %d, want 100", report.MaxHeight)
	}
	if report.AuthoritativeProvider != "a" {
		t.Errorf("AuthoritativeProvider = %s, want 'a'", report.AuthoritativeProvider)
	}
}

func TestCheckWithSameHeightHashes_IssuesReported(t *testing.T) {
	checker := NewConsistencyChecker()

	// Test that issues are properly reported for minority hash
	heights := map[string]uint64{"a": 100, "b": 100, "c": 100}
	hashes := map[string]string{"a": "0xabc", "b": "0xabc", "c": "0xdef"}
	refHeight := uint64(100)

	report := checker.CheckWithSameHeightHashes(heights, hashes, refHeight)

	if report.Consistent {
		t.Error("Consistent should be false when there's a hash mismatch")
	}
	if len(report.Issues) == 0 {
		t.Error("Expected issues to be reported for hash mismatch")
	}

	// The majority group should be first (sorted by provider count)
	if len(report.HashGroups) < 2 {
		t.Fatal("Expected at least 2 hash groups")
	}
	if len(report.HashGroups[0].Providers) < len(report.HashGroups[1].Providers) {
		t.Error("Hash groups should be sorted by provider count (descending)")
	}
}

func TestFormatHeightDrift(t *testing.T) {
	tests := []struct {
		drift    int
		expected string
	}{
		{0, "all providers in sync"},
		{1, "1 block(s) behind (~12s)"},
		{2, "2 block(s) behind (~24s)"},
		{5, "5 block(s) behind (~1m)"},
		{10, "10 block(s) behind (~2m)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatHeightDrift(tt.drift)
			if result != tt.expected {
				t.Errorf("FormatHeightDrift(%d) = %s, want %s", tt.drift, result, tt.expected)
			}
		})
	}
}

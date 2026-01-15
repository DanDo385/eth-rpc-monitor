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
		})
	}
}

// Package report provides a unified JSON report model shared across commands
// and functionality for writing JSON report files.
//
// It standardizes common fields (timestamp, results, latency_ms, error) while allowing
// command-specific fields to be populated as needed, with unused fields omitted.
// Reports are saved to a "reports" directory with timestamped filenames
// to allow tracking results over time.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MillisDuration marshals a time.Duration as an integer millisecond count.
type MillisDuration time.Duration

func (d MillisDuration) MarshalJSON() ([]byte, error) {
	ms := time.Duration(d).Milliseconds()
	return json.Marshal(ms)
}

// Entry represents a single provider (or per-provider aggregate) row in a report.
// Fields are pointers so commands can precisely control omission vs. inclusion.
type Entry struct {
	Provider string `json:"provider,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`

	Hash        *string `json:"hash,omitempty"`
	Height      *uint64 `json:"height,omitempty"`
	BlockHeight *uint64 `json:"block_height,omitempty"`
	Lag         *int64  `json:"lag,omitempty"`

	LatencyMS *MillisDuration `json:"latency_ms,omitempty"`
	Error     *string         `json:"error,omitempty"`

	Success *int `json:"success,omitempty"`
	Total   *int `json:"total,omitempty"`

	P50LatencyMS *MillisDuration `json:"p50_latency_ms,omitempty"`
	P95LatencyMS *MillisDuration `json:"p95_latency_ms,omitempty"`
	P99LatencyMS *MillisDuration `json:"p99_latency_ms,omitempty"`
	MaxLatencyMS *MillisDuration `json:"max_latency_ms,omitempty"`

	LatenciesMS *[]int64 `json:"latencies_ms,omitempty"`
}

// Report is the unified JSON-serializable report structure.
// Commands should populate only the fields they currently output.
type Report struct {
	Timestamp time.Time `json:"timestamp"`
	Results   []Entry   `json:"results"`

	// compare
	BlockArg          *string             `json:"block_arg,omitempty"`
	HeightGroups      map[uint64][]string `json:"height_groups,omitempty"`
	HashGroups        map[string][]string `json:"hash_groups,omitempty"`
	SuccessCount      *int                `json:"success_count,omitempty"`
	TotalCount        *int                `json:"total_count,omitempty"`
	HasHeightMismatch *bool               `json:"has_height_mismatch,omitempty"`
	HasHashMismatch   *bool               `json:"has_hash_mismatch,omitempty"`

	// health
	Samples *int `json:"samples,omitempty"`

	// monitor
	Interval     *string `json:"interval,omitempty"`
	HighestBlock *uint64 `json:"highest_block,omitempty"`
}

// WriteJSON writes the given data structure to a JSON file in the reports directory.
// The file is created with a timestamped filename to prevent overwrites and enable
// historical tracking of command outputs.
//
// Parameters:
//   - data: Any Go value that can be JSON-encoded (structs, maps, slices, etc.)
//   - prefix: Filename prefix (e.g., "block", "health", "compare", "monitor")
//
// Returns:
//   - string: Full file path of the created JSON file
//   - error: File creation, directory creation, or JSON encoding error
//
// Filename format:
//
//	{prefix}-{timestamp}.json
//	Example: "block-20260120-124236.json"
//
// Timestamp format:
//
//	YYYYMMDD-HHMMSS (e.g., "20260120-124236" for Jan 20, 2026 at 12:42:36)
//
// Directory:
//
//	Files are written to "reports/" directory (created if it doesn't exist)
//	Permissions: 0755 (rwxr-xr-x)
func WriteJSON(data interface{}, prefix string) (string, error) {
	// Create reports directory if it doesn't exist
	// MkdirAll creates all necessary parent directories and doesn't error if dir exists
	reportsDir := "reports"
	if err := os.MkdirAll(reportsDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create reports directory: %w", err)
	}

	// Generate filename with timestamp in format: YYYYMMDD-HHMMSS
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.json", prefix, timestamp)
	filepath := filepath.Join(reportsDir, filename)

	// Create new file (will overwrite if exists, but timestamp prevents collisions)
	file, err := os.Create(filepath)
	if err != nil {
		return "", fmt.Errorf("failed to create report file: %w", err)
	}
	defer file.Close() // Ensure file is closed even if encoding fails

	// Create JSON encoder with indentation for readability
	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ") // 2-space indentation

	// Encode data to JSON and write to file
	if err := enc.Encode(data); err != nil {
		return "", fmt.Errorf("failed to encode JSON: %w", err)
	}

	return filepath, nil
}

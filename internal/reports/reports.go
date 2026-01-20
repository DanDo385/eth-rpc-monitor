// Package reports provides helpers for writing timestamped JSON report files.
//
// Commands use this package when the --json flag is set. Reports are written to the
// "reports/" directory in the current working directory.
package reports

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteJSON pretty-prints data as JSON into a timestamped file in the reports directory.
//
// Parameters:
//   - data: Any JSON-marshalable value to write.
//   - prefix: Filename prefix (e.g., "health", "compare", "block").
//
// Returns:
//   - string: The path to the written file.
//   - error: Any error creating the directory, marshaling JSON, or writing the file.
//
// Filenames follow: {prefix}-{YYYYMMDD-HHMMSS}.json
func WriteJSON(data any, prefix string) (string, error) {
	if prefix == "" {
		prefix = "report"
	}

	if err := os.MkdirAll("reports", 0o755); err != nil {
		return "", fmt.Errorf("create reports directory: %w", err)
	}

	ts := time.Now().UTC().Format("20060102-150405")
	path := filepath.Join("reports", fmt.Sprintf("%s-%s.json", prefix, ts))

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal JSON: %w", err)
	}

	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}

	return path, nil
}

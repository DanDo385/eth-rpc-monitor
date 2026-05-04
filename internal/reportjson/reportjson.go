// Package reportjson writes timestamped JSON artifacts under reports/.
package reportjson

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Write serializes data as pretty-printed JSON to reports/<prefix>-YYYYMMDD-HHMMSS.json.
// It returns the absolute path of the written file, or an error from mkdir, create, or encode.
func Write(data interface{}, prefix string) (string, error) {
	if err := os.MkdirAll("reports", 0755); err != nil {
		return "", fmt.Errorf("create reports directory: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", prefix, time.Now().Format("20060102-150405"))
	path := filepath.Join("reports", name)
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create report file: %w", err)
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return "", fmt.Errorf("encode json report: %w", err)
	}
	return path, nil
}

package format

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatTest_success(t *testing.T) {
	var buf bytes.Buffer
	results := []TestResult{
		{
			Name:        "a",
			Type:        "public",
			Success:     1,
			Total:       1,
			Latencies:   []time.Duration{10 * time.Millisecond},
			BlockHeight: 100,
		},
	}
	FormatTest(&buf, results)
	out := buf.String()
	if !strings.Contains(out, "a") || !strings.Contains(out, "100") {
		t.Fatalf("output: %s", out)
	}
}

func TestFormatTest_mismatch(t *testing.T) {
	var buf bytes.Buffer
	results := []TestResult{
		{Name: "a", Success: 1, Total: 1, Latencies: []time.Duration{10 * time.Millisecond}, BlockHeight: 100},
		{Name: "b", Success: 1, Total: 1, Latencies: []time.Duration{20 * time.Millisecond}, BlockHeight: 101},
	}
	FormatTest(&buf, results)
	out := buf.String()
	if !strings.Contains(out, "BLOCK HEIGHT MISMATCH") {
		t.Fatalf("output: %s", out)
	}
}

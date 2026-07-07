package format

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFormatMonitor_success(t *testing.T) {
	var buf bytes.Buffer
	results := []WatchResult{
		{Provider: "a", BlockHeight: 100, Latency: 10 * time.Millisecond, Error: nil},
		{Provider: "b", BlockHeight: 100, Latency: 20 * time.Millisecond, Error: nil},
	}
	FormatMonitor(&buf, results, 5*time.Second, false)
	out := buf.String()
	if !strings.Contains(out, "100") || !strings.Contains(out, "10ms") {
		t.Fatalf("output: %s", out)
	}
}

func TestFormatMonitor_lag(t *testing.T) {
	var buf bytes.Buffer
	results := []WatchResult{
		{Provider: "fast", BlockHeight: 100, Latency: 10 * time.Millisecond, Error: nil},
		{Provider: "slow", BlockHeight: 99, Latency: 20 * time.Millisecond, Error: nil},
	}
	FormatMonitor(&buf, results, 5*time.Second, false)
	out := buf.String()
	if !strings.Contains(out, "-1") { // lag 1 should be visible
		t.Fatalf("output: %s", out)
	}
}

func TestFormatMonitor_error(t *testing.T) {
	var buf bytes.Buffer
	results := []WatchResult{
		{Provider: "down", Error: errors.New("timeout")},
	}
	FormatMonitor(&buf, results, 5*time.Second, false)
	out := buf.String()
	if !strings.Contains(out, "ERROR") {
		t.Fatalf("output: %s", out)
	}
}

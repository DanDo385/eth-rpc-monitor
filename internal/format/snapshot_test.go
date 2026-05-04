package format

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFormatSnapshot_shortHashesNoPanic(t *testing.T) {
	var buf bytes.Buffer
	results := []SnapshotResult{
		{Provider: "a", Hash: "0xabc", Height: 1, Latency: time.Millisecond, Error: nil},
		{Provider: "b", Hash: "0xdef", Height: 1, Latency: time.Millisecond, Error: nil},
	}
	FormatSnapshot(&buf, results)
	out := buf.String()
	if !containsAll(out, []string{"BLOCK HASH MISMATCH", "0xabc", "0xdef"}) {
		t.Fatalf("output: %s", out)
	}
}

func TestFormatSnapshot_emptyHashNoPanic(t *testing.T) {
	var buf bytes.Buffer
	results := []SnapshotResult{
		{Provider: "a", Hash: "", Height: 1, Latency: time.Millisecond, Error: nil},
		{Provider: "b", Hash: "x", Height: 1, Latency: time.Millisecond, Error: nil},
	}
	FormatSnapshot(&buf, results)
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestFormatSnapshot_errorRow(t *testing.T) {
	var buf bytes.Buffer
	FormatSnapshot(&buf, []SnapshotResult{
		{Provider: "down", Error: errors.New("timeout")},
	})
	out := buf.String()
	if !containsAll(out, []string{"down", "timeout"}) {
		t.Fatalf("output: %s", out)
	}
}

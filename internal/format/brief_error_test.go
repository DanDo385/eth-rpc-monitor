package format

import (
	"errors"
	"strings"
	"testing"
)

func TestBriefError_plainShort(t *testing.T) {
	got := BriefError(errors.New("timeout"))
	if got != "timeout" {
		t.Fatalf("got %q", got)
	}
}

func TestBriefError_html503LongBody(t *testing.T) {
	long := `<html>
<head><title>503 Service Temporarily Unavailable</title></head>
<body><script>alert(1)</script></body></html>`
	got := BriefError(errors.New(long))
	if strings.Contains(got, "<script") {
		t.Fatalf("expected no script tag: %q", got)
	}
	if !strings.Contains(got, "503 Service Temporarily Unavailable") {
		t.Fatalf("expected title: %q", got)
	}
}

func TestBriefError_plainLongTruncated(t *testing.T) {
	msg := strings.Repeat("x", 500)
	got := BriefError(errors.New(msg))
	if len([]rune(got)) > 250 {
		t.Fatalf("too long: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis: %q", got)
	}
}

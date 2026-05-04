package format

import (
	"testing"
	"time"
)

func TestCalculateTailLatency_empty(t *testing.T) {
	tail := CalculateTailLatency(nil)
	if tail.P50 != 0 || tail.P95 != 0 || tail.P99 != 0 || tail.Max != 0 {
		t.Fatalf("expected zero tail, got %+v", tail)
	}
}

func TestCalculateTailLatency_single(t *testing.T) {
	d := 7 * time.Millisecond
	tail := CalculateTailLatency([]time.Duration{d})
	if tail.P50 != d || tail.P95 != d || tail.P99 != d || tail.Max != d {
		t.Fatalf("unexpected %+v", tail)
	}
}

func TestCalculateTailLatency_unsorted(t *testing.T) {
	tail := CalculateTailLatency([]time.Duration{
		30 * time.Millisecond,
		10 * time.Millisecond,
		20 * time.Millisecond,
	})
	if tail.P50 != 20*time.Millisecond || tail.Max != 30*time.Millisecond {
		t.Fatalf("unexpected %+v", tail)
	}
}

func TestCalculateTailLatency_nearestRank_n10(t *testing.T) {
	// Sorted ascending: 10,20,...,100 ms
	lat := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		lat[i] = time.Duration((i+1)*10) * time.Millisecond
	}
	tail := CalculateTailLatency(lat)
	// P50 index ceil(5)-1=4 -> 50ms
	if tail.P50 != 50*time.Millisecond {
		t.Fatalf("P50 want 50ms got %v", tail.P50)
	}
	// P95 index 9 -> 100ms
	if tail.P95 != 100*time.Millisecond || tail.P99 != 100*time.Millisecond || tail.Max != 100*time.Millisecond {
		t.Fatalf("P95/P99/Max want 100ms got P95=%v P99=%v Max=%v", tail.P95, tail.P99, tail.Max)
	}
}

func TestPercentileIndex(t *testing.T) {
	tests := []struct {
		n, want50, want95, want99 int
	}{
		{n: 1, want50: 0, want95: 0, want99: 0},
		{n: 10, want50: 4, want95: 9, want99: 9},
		{n: 100, want50: 49, want95: 94, want99: 98},
	}
	for _, tc := range tests {
		if got := percentileIndex(tc.n, 0.50); got != tc.want50 {
			t.Fatalf("n=%d P50 index got %d want %d", tc.n, got, tc.want50)
		}
		if got := percentileIndex(tc.n, 0.95); got != tc.want95 {
			t.Fatalf("n=%d P95 index got %d want %d", tc.n, got, tc.want95)
		}
		if got := percentileIndex(tc.n, 0.99); got != tc.want99 {
			t.Fatalf("n=%d P99 index got %d want %d", tc.n, got, tc.want99)
		}
	}
	if percentileIndex(0, 0.5) != 0 {
		t.Fatal("n=0")
	}
}

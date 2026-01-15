package output

import (
	"fmt"
	"time"
)

func renderAutoSelectedNote(provider string, successRate float64, p95Latency time.Duration) {
	fmt.Printf("  [Auto-selected: %s — %.0f%% success, %dms p95]\n\n",
		provider, successRate, p95Latency.Milliseconds())
}

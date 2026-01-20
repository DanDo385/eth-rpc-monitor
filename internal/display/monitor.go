package display

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// MonitorResult is the per-provider snapshot displayed by the monitor command.
type MonitorResult struct {
	Name        string
	BlockNumber uint64
	Latency     time.Duration
	Err         error
}

// MonitorFormatter formats monitor output for terminal display.
type MonitorFormatter struct {
	Results      []MonitorResult
	HighestBlock uint64
	Interval     time.Duration
	Timestamp    time.Time
}

// Format writes the monitor dashboard to w.
func (f *MonitorFormatter) Format(w io.Writer) error {
	fmt.Fprintf(w, "Monitoring %d providers (interval: %s, Ctrl+C to exit)...\n\n", len(f.Results), f.Interval)
	fmt.Fprintf(w, "%-14s %12s %10s %12s\n", "Provider", "Block Height", "Latency", "Lag")
	fmt.Fprintln(w, strings.Repeat("─", 60))

	for _, r := range f.Results {
		if r.Err != nil {
			fmt.Fprintf(w, "%-14s %12s %10s %12s\n", r.Name, "ERROR", "—", "—")
			continue
		}

		lag := uint64(0)
		if f.HighestBlock > r.BlockNumber {
			lag = f.HighestBlock - r.BlockNumber
		}

		lagStr := "—"
		if lag > 0 {
			lagStr = fmt.Sprintf("-%d", lag)
		}

		fmt.Fprintf(w, "%-14s %12d %8dms %12s\n", r.Name, r.BlockNumber, r.Latency.Milliseconds(), lagStr)
	}
	fmt.Fprintln(w)
	return nil
}

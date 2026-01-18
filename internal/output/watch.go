package output

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
)

// WatchState holds the current state for watch mode display
type WatchState struct {
	Providers  map[string]*WatchProviderState
	Events     []WatchEvent
	MaxEvents  int
	LastUpdate time.Time
	Refresh    time.Duration
}

// WatchProviderState holds current state for a single provider
type WatchProviderState struct {
	Name        string
	Status      metrics.ProviderStatus
	Latency     time.Duration
	BlockHeight uint64
	BlockHash   string
	LastError   string
	LastSeen    time.Time
}

// WatchEvent represents a notable event in watch mode
type WatchEvent struct {
	Timestamp time.Time
	Provider  string
	Message   string
	Severity  EventSeverity
}

// EventSeverity indicates the importance of an event
type EventSeverity int

const (
	SeverityInfo EventSeverity = iota
	SeverityWarning
	SeverityError
)

// NewWatchState creates a new watch state tracker
func NewWatchState(refresh time.Duration, maxEvents int) *WatchState {
	return &WatchState{
		Providers: make(map[string]*WatchProviderState),
		MaxEvents: maxEvents,
		Refresh:   refresh,
	}
}

// AddEvent adds an event to the watch state
func (w *WatchState) AddEvent(provider, message string, severity EventSeverity) {
	event := WatchEvent{
		Timestamp: time.Now(),
		Provider:  provider,
		Message:   message,
		Severity:  severity,
	}

	// Prepend (newest first)
	w.Events = append([]WatchEvent{event}, w.Events...)

	// Trim to max
	if len(w.Events) > w.MaxEvents {
		w.Events = w.Events[:w.MaxEvents]
	}
}

// RenderWatch outputs the watch mode display
func RenderWatch(state *WatchState, consistency *metrics.ConsistencyReport) {
	ClearScreen()

	now := time.Now().Format("15:04:05")

	// Header
	fmt.Printf("%s Ethereum RPC Monitor ─────────────────── %s (refresh: %s) %s\n",
		cyan("╭─"), now, state.Refresh, cyan("─╮"))

	// Provider status lines (sorted by name for consistent display)
	providerNames := make([]string, 0, len(state.Providers))
	for name := range state.Providers {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)

	for _, name := range providerNames {
		p := state.Providers[name]
		statusIcon := formatWatchStatus(p.Status)
		latencyStr := formatDuration(p.Latency)
		blockStr := fmt.Sprintf("#%d", p.BlockHeight)

		fmt.Printf("  %-12s %s  %8s  %s\n",
			p.Name,
			statusIcon,
			latencyStr,
			blockStr)
	}

	fmt.Println()

	// Consistency status
	if consistency != nil {
		if consistency.HeightConsensus && consistency.HashConsensus {
			fmt.Printf("  Block Sync: %s\n",
				green("✓ All providers in sync"))
		} else if !consistency.HashConsensus && len(consistency.HashGroups) > 1 {
			fmt.Printf("  Block Sync: %s at #%d\n",
				yellow("⚠ Hash mismatch"),
				consistency.ReferenceHeight)

			for i, group := range consistency.HashGroups {
				truncHash := truncateHash(group.Hash)
				providers := strings.Join(group.Providers, ", ")
				suffix := ""
				if i > 0 {
					suffix = " ← minority"
				}
				fmt.Printf("    %s: %s%s\n", truncHash, providers, suffix)
			}
		} else {
			var issues string
			if !consistency.HeightConsensus {
				issues = fmt.Sprintf("⚠ %d block(s) variance", consistency.HeightVariance)
			}
			if !consistency.HashConsensus {
				if issues != "" {
					issues += ", "
				}
				issues += "⚠ hash mismatch"
			}
			fmt.Printf("  Block Sync: %s\n",
				yellow(issues))
		}
	}

	fmt.Println()

	// Recent events
	fmt.Printf("  %s\n", bold("Recent Events:"))

	if len(state.Events) == 0 {
		fmt.Printf("    %s\n", "(no events)")
	} else {
		for i, event := range state.Events {
			if i >= 5 {
				break
			}
			timeStr := event.Timestamp.Format("15:04:05")
			line := fmt.Sprintf("%s  %-12s  %s", timeStr, event.Provider, event.Message)
			if len(line) > 60 {
				line = line[:57] + "..."
			}

			var coloredLine string
			switch event.Severity {
			case SeverityError:
				coloredLine = red(line)
			case SeverityWarning:
				coloredLine = yellow(line)
			default:
				coloredLine = line
			}

			fmt.Printf("    %s\n", coloredLine)
		}
	}

	fmt.Println()
	fmt.Println(cyan("╰───────────────────────────────────────────────────────────────────╯"))
	fmt.Println()
	fmt.Println("Press Ctrl+C to exit")
}

func formatWatchStatus(status metrics.ProviderStatus) string {
	switch status {
	case metrics.StatusUp:
		return green("✓ UP  ")
	case metrics.StatusSlow:
		return yellow("⚠ SLOW")
	case metrics.StatusDegraded:
		return yellow("⚠ DEG ")
	case metrics.StatusDown:
		return red("✗ DOWN")
	default:
		return "?     "
	}
}

func padToWidth(remaining int, suffix string) string {
	if remaining <= 0 {
		return suffix
	}
	padding := ""
	for i := 0; i < remaining; i++ {
		padding += " "
	}
	return padding + suffix
}

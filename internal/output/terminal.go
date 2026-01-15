package output

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/rodaine/table"

	"github.com/dmagro/eth-rpc-monitor/internal/metrics"
)

// Colors for status indicators
var (
	green  = color.New(color.FgGreen).SprintFunc()
	yellow = color.New(color.FgYellow).SprintFunc()
	red    = color.New(color.FgRed).SprintFunc()
	cyan   = color.New(color.FgCyan).SprintFunc()
	bold   = color.New(color.Bold).SprintFunc()
)

// SnapshotReport holds all data needed for the snapshot output
type SnapshotReport struct {
	Timestamp   time.Time
	SampleCount int
	Providers   map[string]*metrics.ProviderMetrics
	Consistency *metrics.ConsistencyReport
}

// RenderSnapshotTerminal outputs the full snapshot report to the terminal
func RenderSnapshotTerminal(report *SnapshotReport) {
	renderHeader(report.Timestamp, report.SampleCount)
	renderProviderPerformance(report.Providers)
	renderErrorBreakdown(report.Providers)
	renderConsistencyCheck(report.Consistency)
	renderAssessment(report)
}

func renderHeader(timestamp time.Time, samples int) {
	fmt.Println()
	fmt.Println(cyan("╭─────────────────────────────────────────────────────────────────╮"))
	fmt.Println(cyan("│") + bold("           Ethereum RPC Infrastructure Report                   ") + cyan("│"))
	fmt.Printf("%s                    %-37s%s\n", cyan("│"), timestamp.Format("2006-01-02 15:04:05 MST"), cyan("│"))
	fmt.Printf("%s                      Sample Size: %-25d%s\n", cyan("│"), samples, cyan("│"))
	fmt.Println(cyan("╰─────────────────────────────────────────────────────────────────╯"))
	fmt.Println()
}

func renderProviderPerformance(providers map[string]*metrics.ProviderMetrics) {
	fmt.Println(bold("Provider Performance"))

	headerFmt := color.New(color.FgCyan, color.Underline).SprintfFunc()
	tbl := table.New("Provider", "Status", "p50", "p95", "p99", "Max", "Success")
	tbl.WithHeaderFormatter(headerFmt)

	for _, m := range providers {
		status := formatStatus(m.Status)
		tbl.AddRow(
			m.Name,
			status,
			formatDuration(m.LatencyP50),
			formatDuration(m.LatencyP95),
			formatDuration(m.LatencyP99),
			formatDuration(m.LatencyMax),
			formatSuccessRate(m.SuccessRate),
		)
	}

	tbl.Print()
	fmt.Println()
}

func renderErrorBreakdown(providers map[string]*metrics.ProviderMetrics) {
	// Check if there are any errors to show
	hasErrors := false
	for _, m := range providers {
		if m.Failures > 0 {
			hasErrors = true
			break
		}
	}

	if !hasErrors {
		fmt.Println(green("No errors recorded during sampling period."))
		fmt.Println()
		return
	}

	fmt.Println(bold("Error Breakdown"))

	headerFmt := color.New(color.FgCyan, color.Underline).SprintfFunc()
	tbl := table.New("Provider", "Timeout", "429", "5xx", "Parse", "Other")
	tbl.WithHeaderFormatter(headerFmt)

	for _, m := range providers {
		tbl.AddRow(
			m.Name,
			formatErrorCount(m.Timeouts),
			formatErrorCount(m.RateLimits),
			formatErrorCount(m.ServerErrors),
			formatErrorCount(m.ParseErrors),
			formatErrorCount(m.OtherErrors),
		)
	}

	tbl.Print()
	fmt.Println()
}

func renderConsistencyCheck(c *metrics.ConsistencyReport) {
	fmt.Println(bold("Block Height Consistency"))

	fmt.Printf("  Network Height: %s (via %s)\n",
		cyan(fmt.Sprintf("%d", c.MaxHeight)),
		c.AuthoritativeProvider)
	fmt.Printf("  Reference Height: %s\n", cyan(fmt.Sprintf("%d", c.ReferenceHeight)))
	fmt.Println()

	headerFmt := color.New(color.FgCyan, color.Underline).SprintfFunc()
	tbl := table.New("Provider", "Block", "Delta", "Assessment")
	tbl.WithHeaderFormatter(headerFmt)

	for provider, height := range c.Heights {
		delta := int64(c.MaxHeight) - int64(height)
		assessment := assessHeightDrift(int(delta))

		deltaStr := fmt.Sprintf("%d", delta)
		if delta > 0 {
			deltaStr = fmt.Sprintf("-%d", delta)
		}

		tbl.AddRow(provider, height, deltaStr, assessment)
	}

	tbl.Print()
	fmt.Println()

	// Hash consistency
	if len(c.HashGroups) > 0 {
		fmt.Println(bold("Block Hash Consistency"))
		if c.HashConsensus {
			fmt.Printf("  %s All providers report consistent block hash\n", green("✓"))
		} else {
			fmt.Printf("  %s HASH MISMATCH DETECTED\n", red("✗"))
			for _, group := range c.HashGroups {
				fmt.Printf("    %s: %s\n",
					strings.Join(group.Providers, ", "),
					truncateHash(group.Hash))
			}
		}
		fmt.Println()
	}
}

func renderAssessment(report *SnapshotReport) {
	fmt.Println(bold("Operational Assessment"))
	fmt.Println("  ┌─────────────────────────────────────────────────────────────────┐")

	// Provider recommendations
	var upProviders, degradedProviders, downProviders []string

	for name, m := range report.Providers {
		switch m.Status {
		case metrics.StatusUp:
			upProviders = append(upProviders, name)
		case metrics.StatusSlow:
			degradedProviders = append(degradedProviders, name)
		case metrics.StatusDegraded:
			degradedProviders = append(degradedProviders, name)
		case metrics.StatusDown:
			downProviders = append(downProviders, name)
		}
	}

	if len(downProviders) > 0 {
		fmt.Printf("  │ %s %-60s│\n", red("✗"),
			fmt.Sprintf("%s unsuitable for production use", strings.Join(downProviders, ", ")))
	}

	if len(degradedProviders) > 0 {
		fmt.Printf("  │ %s %-60s│\n", yellow("⚠"),
			fmt.Sprintf("%s showing degraded performance", strings.Join(degradedProviders, ", ")))
	}

	if len(upProviders) > 0 {
		fmt.Printf("  │ %s %-60s│\n", green("✓"),
			fmt.Sprintf("%s performing within expected parameters", strings.Join(upProviders, ", ")))
	}

	// Consistency issues
	if !report.Consistency.Consistent {
		for _, issue := range report.Consistency.Issues {
			wrapped := wrapText(issue, 58)
			for _, line := range wrapped {
				fmt.Printf("  │ %s %-60s│\n", yellow("⚠"), line)
			}
		}
	}

	fmt.Println("  │                                                                 │")

	// Recommendation
	if len(upProviders) > 0 {
		rec := fmt.Sprintf("Recommended priority: %s", strings.Join(upProviders, " → "))
		fmt.Printf("  │ %-63s│\n", rec)
	}

	fmt.Println("  └─────────────────────────────────────────────────────────────────┘")
	fmt.Println()
}

// Helper formatting functions

func formatStatus(status metrics.ProviderStatus) string {
	switch status {
	case metrics.StatusUp:
		return green("✓ UP")
	case metrics.StatusSlow:
		return yellow("⚠ SLOW")
	case metrics.StatusDegraded:
		return yellow("⚠ DEG")
	case metrics.StatusDown:
		return red("✗ DOWN")
	default:
		return "?"
	}
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatSuccessRate(rate float64) string {
	str := fmt.Sprintf("%.1f%%", rate)
	if rate >= 99.0 {
		return green(str)
	}
	if rate >= 90.0 {
		return yellow(str)
	}
	return red(str)
}

func formatErrorCount(count int) string {
	if count == 0 {
		return green("0")
	}
	return red(fmt.Sprintf("%d", count))
}

func assessHeightDrift(delta int) string {
	if delta == 0 {
		return green("✓ In sync")
	}
	if delta <= 2 {
		return yellow(fmt.Sprintf("⚠ %d block(s) behind (~%ds)", delta, delta*12))
	}
	return red(fmt.Sprintf("✗ Stale (%d blocks / ~%ds behind)", delta, delta*12))
}

func truncateHash(hash string) string {
	if len(hash) <= 14 {
		return hash
	}
	return hash[:6] + "..." + hash[len(hash)-4:]
}

func wrapText(text string, width int) []string {
	if len(text) <= width {
		return []string{text}
	}

	var lines []string
	words := strings.Fields(text)
	var current string

	for _, word := range words {
		if len(current)+len(word)+1 > width {
			lines = append(lines, current)
			current = word
		} else {
			if current != "" {
				current += " "
			}
			current += word
		}
	}

	if current != "" {
		lines = append(lines, current)
	}

	return lines
}

// ClearScreen clears the terminal (for watch mode)
func ClearScreen() {
	fmt.Print("\033[2J\033[H")
}

// MoveCursor moves the cursor to the specified position
func MoveCursor(row, col int) {
	fmt.Printf("\033[%d;%dH", row, col)
}

// RenderWatchHeader outputs the watch mode header
func RenderWatchHeader(refresh time.Duration) {
	now := time.Now().Format("15:04:05")
	fmt.Printf("%s Ethereum RPC Monitor %s (refresh: %s) %s\n",
		cyan("╭─"),
		cyan("─────────────────"),
		refresh,
		cyan(fmt.Sprintf("─ %s ─╮", now)))
}

// DisableColors turns off color output (for non-TTY or JSON mode)
func DisableColors() {
	color.NoColor = true
}

// IsTerminal returns true if stdout is a terminal
func IsTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

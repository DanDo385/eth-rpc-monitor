package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/dmagro/eth-rpc-monitor/internal/provider"
)

// RenderStatusTerminal outputs provider status to terminal
func RenderStatusTerminal(ranked provider.RankedProviders) {
	bold := color.New(color.Bold).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Println()
	fmt.Printf("%s\n", bold("Provider Status (Quick Health Check)"))
	fmt.Println("═══════════════════════════════════════════════════════════════════════")
	fmt.Printf("  %-12s  %-8s  %-10s  %-12s  %-8s  %s\n",
		"Provider", "Status", "Success", "p95 Latency", "Delta", "Score")
	fmt.Println("───────────────────────────────────────────────────────────────────────")

	for _, p := range ranked {
		statusStr := formatProviderStatus(p.Status)
		successStr := fmt.Sprintf("%.1f%%", p.SuccessRate)
		latencyStr := fmt.Sprintf("%dms", p.P95Latency.Milliseconds())
		deltaStr := fmt.Sprintf("%d", p.BlockDelta)
		scoreStr := fmt.Sprintf("%.2f", p.Score)

		if p.Excluded {
			successStr = red(successStr)
			scoreStr = red(scoreStr)
		} else if p.SuccessRate >= 99 {
			successStr = green(successStr)
		}

		fmt.Printf("  %-12s  %-8s  %-10s  %-12s  %-8s  %s\n",
			p.Name, statusStr, successStr, latencyStr, deltaStr, scoreStr)
	}

	fmt.Println("═══════════════════════════════════════════════════════════════════════")

	// Recommendation
	best, err := ranked.Best()
	if err != nil {
		fmt.Printf("\n  %s %s\n", yellow("⚠"), err.Error())
	} else {
		fmt.Printf("\n  %s %s: %s\n", green("✓"), bold("Recommended"), best.Name)
		fmt.Printf("    Reason: %.1f%% success, %dms p95, %d blocks behind\n",
			best.SuccessRate, best.P95Latency.Milliseconds(), best.BlockDelta)
	}

	// Show excluded providers
	var excluded []string
	for _, p := range ranked {
		if p.Excluded {
			excluded = append(excluded, fmt.Sprintf("%s (%s)", p.Name, p.ExcludeReason))
		}
	}
	if len(excluded) > 0 {
		fmt.Printf("\n  %s Excluded: %s\n", yellow("⚠"), strings.Join(excluded, ", "))
	}

	fmt.Println()
}

func formatProviderStatus(status string) string {
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	switch status {
	case "UP":
		return green("✓ UP")
	case "SLOW":
		return yellow("⚠ SLOW")
	case "DEGRADED":
		return yellow("⚠ DEG")
	case "DOWN":
		return red("✗ DOWN")
	default:
		return status
	}
}

// RenderStatusJSON outputs provider status as JSON
func RenderStatusJSON(ranked provider.RankedProviders) error {
	best, bestErr := ranked.Best()
	
	providers := make([]map[string]interface{}, 0, len(ranked))
	for _, p := range ranked {
		providers = append(providers, map[string]interface{}{
			"name":         p.Name,
			"status":       p.Status,
			"successRate":  p.SuccessRate,
			"avgLatencyMs": p.AvgLatency.Milliseconds(),
			"p95LatencyMs": p.P95Latency.Milliseconds(),
			"blockHeight":  p.BlockHeight,
			"blockDelta":   p.BlockDelta,
			"score":        p.Score,
			"excluded":     p.Excluded,
			"excludeReason": p.ExcludeReason,
		})
	}

	output := map[string]interface{}{
		"providers": providers,
	}

	if bestErr == nil {
		output["recommended"] = map[string]interface{}{
			"name":   best.Name,
			"reason": fmt.Sprintf("%.1f%% success, %dms p95, %d blocks behind",
				best.SuccessRate, best.P95Latency.Milliseconds(), best.BlockDelta),
		}
	} else {
		output["warning"] = bestErr.Error()
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

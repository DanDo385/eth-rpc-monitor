package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/fatih/color"

	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
)

var (
	blockCyan = color.New(color.FgCyan).SprintFunc()
	blockBold = color.New(color.Bold).SprintFunc()
)

// BlockDisplay holds block data for rendering.
type BlockDisplay struct {
	Block       *rpc.Block
	Provider    string
	Latency     time.Duration
	RawResponse json.RawMessage
}

// RenderBlockTerminal outputs block details to terminal.
func RenderBlockTerminal(bd *BlockDisplay, showRaw bool) {
	if showRaw && bd.RawResponse != nil {
		var prettyJSON bytes.Buffer
		if err := json.Indent(&prettyJSON, bd.RawResponse, "", "  "); err == nil {
			fmt.Println(prettyJSON.String())
		} else {
			fmt.Println(string(bd.RawResponse))
		}
		return
	}

	block := bd.Block

	fmt.Println()
	fmt.Printf("%s\n", blockBold(fmt.Sprintf("Block #%d", block.Number)))
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  %s           %s\n", blockCyan("Hash:"), block.Hash)
	fmt.Printf("  %s         %s\n", blockCyan("Parent:"), block.ParentHash)
	fmt.Printf("  %s      %s\n", blockCyan("Timestamp:"), formatBlockTime(block.Timestamp))
	fmt.Println()
	fmt.Printf("  %s       %s / %s (%s)\n",
		blockCyan("Gas Used:"),
		formatWithCommas(block.GasUsed),
		formatWithCommas(block.GasLimit),
		formatGasPercent(block.GasUsed, block.GasLimit))
	fmt.Printf("  %s       %s\n", blockCyan("Base Fee:"), formatBaseFee(block.BaseFeePerGas))
	fmt.Printf("  %s   %d\n", blockCyan("Transactions:"), block.TxCount)
	fmt.Println()
	fmt.Printf("  %s    %s (%dms)\n", blockCyan("Fetched via:"), bd.Provider, bd.Latency.Milliseconds())
	fmt.Println()
}

// RenderBlockJSON outputs block as structured JSON.
func RenderBlockJSON(bd *BlockDisplay, includeRaw bool) error {
	output := map[string]interface{}{
		"block": map[string]interface{}{
			"number":        bd.Block.Number,
			"hash":          bd.Block.Hash,
			"parentHash":    bd.Block.ParentHash,
			"timestamp":     bd.Block.Timestamp,
			"timestampISO":  time.Unix(int64(bd.Block.Timestamp), 0).UTC().Format(time.RFC3339),
			"gasUsed":       bd.Block.GasUsed,
			"gasLimit":      bd.Block.GasLimit,
			"baseFeePerGas": formatBaseFeeRaw(bd.Block.BaseFeePerGas),
			"txCount":       bd.Block.TxCount,
		},
		"meta": map[string]interface{}{
			"provider":  bd.Provider,
			"latencyMs": bd.Latency.Milliseconds(),
		},
	}

	if includeRaw && bd.RawResponse != nil {
		output["rawResponse"] = json.RawMessage(bd.RawResponse)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func formatBlockTime(ts uint64) string {
	t := time.Unix(int64(ts), 0)
	ago := time.Since(t)

	var agoStr string
	switch {
	case ago < time.Minute:
		agoStr = fmt.Sprintf("%d seconds ago", int(ago.Seconds()))
	case ago < time.Hour:
		agoStr = fmt.Sprintf("%d minutes ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		agoStr = fmt.Sprintf("%d hours ago", int(ago.Hours()))
	default:
		agoStr = fmt.Sprintf("%d days ago", int(ago.Hours()/24))
	}

	return fmt.Sprintf("%s (%s)", t.Format("2006-01-02 15:04:05 UTC"), agoStr)
}

func formatWithCommas(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func formatGasPercent(used, limit uint64) string {
	if limit == 0 {
		return "—"
	}
	pct := float64(used) / float64(limit) * 100
	return fmt.Sprintf("%.1f%%", pct)
}

func formatBaseFee(fee *big.Int) string {
	if fee == nil {
		return "— (pre-EIP-1559)"
	}
	// Convert wei to gwei (divide by 10^9).
	gwei := new(big.Float).SetInt(fee)
	divisor := new(big.Float).SetInt64(1e9)
	gwei.Quo(gwei, divisor)
	f, _ := gwei.Float64()
	return fmt.Sprintf("%.2f gwei", f)
}

func formatBaseFeeRaw(fee *big.Int) string {
	if fee == nil {
		return ""
	}
	return fee.String()
}

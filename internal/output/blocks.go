package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
	"github.com/fatih/color"
)

var (
	blockCyan = color.New(color.FgCyan).SprintFunc()
	blockBold = color.New(color.Bold).SprintFunc()
)

// BlockDisplay holds block data for rendering
type BlockDisplay struct {
	Block       *rpc.Block
	Provider    string
	Latency     time.Duration
	RawResponse json.RawMessage
}

// RenderBlockTerminal outputs block details to terminal
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

// RenderBlockJSON outputs block as structured JSON
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
	// Convert wei to gwei (divide by 10^9)
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

// TxDisplay holds transaction list for rendering
type TxDisplay struct {
	BlockNumber  uint64
	Transactions []rpc.Transaction
	TotalCount   int
	Limit        int
	Provider     string
	Latency      time.Duration
	RawResponse  json.RawMessage
}

// RenderTxsTerminal outputs transaction list to terminal
func RenderTxsTerminal(td *TxDisplay, showRaw bool) {
	if showRaw && td.RawResponse != nil {
		// Extract just the transactions array from raw response
		var block map[string]json.RawMessage
		if err := json.Unmarshal(td.RawResponse, &block); err == nil {
			if txs, ok := block["transactions"]; ok {
				var prettyJSON bytes.Buffer
				json.Indent(&prettyJSON, txs, "", "  ")
				fmt.Println(prettyJSON.String())
				return
			}
		}
		// Fallback to full raw
		var prettyJSON bytes.Buffer
		json.Indent(&prettyJSON, td.RawResponse, "", "  ")
		fmt.Println(prettyJSON.String())
		return
	}

	shown := len(td.Transactions)
	if td.Limit > 0 && shown > td.Limit {
		shown = td.Limit
	}

	fmt.Println()
	fmt.Printf("%s\n", blockBold(fmt.Sprintf("Transactions in Block #%d", td.BlockNumber)))
	fmt.Printf("Showing %d of %d transactions\n", shown, td.TotalCount)
	fmt.Println("════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("  %-5s  %-15s  %-15s  %-15s  %s\n",
		"#", "Hash", "From", "To", "Value (ETH)")
	fmt.Println("────────────────────────────────────────────────────────────────────────────")

	displayTxs := td.Transactions
	if td.Limit > 0 && len(displayTxs) > td.Limit {
		displayTxs = displayTxs[:td.Limit]
	}

	for i, tx := range displayTxs {
		toDisplay := truncateAddress(tx.To)
		if tx.To == "" {
			toDisplay = color.New(color.FgYellow).Sprint("[Create]")
		}

		fmt.Printf("  %-5d  %s  %s  %s  %s\n",
			i,
			truncateHash(tx.Hash),
			truncateAddress(tx.From),
			toDisplay,
			formatEthValue(tx.Value))
	}

	fmt.Println("════════════════════════════════════════════════════════════════════════════")

	if td.Limit > 0 && td.TotalCount > td.Limit {
		fmt.Printf("  Use --limit %d to see more transactions\n", td.TotalCount)
	}

	fmt.Printf("  Fetched via: %s (%dms)\n", td.Provider, td.Latency.Milliseconds())
	fmt.Println()
}

// RenderTxsJSON outputs transactions as JSON
func RenderTxsJSON(td *TxDisplay, includeRaw bool) error {
	displayTxs := td.Transactions
	if td.Limit > 0 && len(displayTxs) > td.Limit {
		displayTxs = displayTxs[:td.Limit]
	}

	txList := make([]map[string]interface{}, 0, len(displayTxs))
	for _, tx := range displayTxs {
		txMap := map[string]interface{}{
			"hash":     tx.Hash,
			"from":     tx.From,
			"to":       tx.To,
			"value":    tx.Value.String(),
			"valueEth": formatEthValueRaw(tx.Value),
			"gas":      tx.Gas,
			"nonce":    tx.Nonce,
			"txIndex":  tx.TxIndex,
		}
		txList = append(txList, txMap)
	}

	output := map[string]interface{}{
		"blockNumber": td.BlockNumber,
		"totalCount":  td.TotalCount,
		"shownCount":  len(displayTxs),
		"transactions": txList,
		"meta": map[string]interface{}{
			"provider":  td.Provider,
			"latencyMs": td.Latency.Milliseconds(),
		},
	}

	if includeRaw && td.RawResponse != nil {
		output["rawResponse"] = json.RawMessage(td.RawResponse)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// Helper functions

func truncateAddress(addr string) string {
	if addr == "" {
		return ""
	}
	if len(addr) <= 15 {
		return addr
    }
	return addr[:6] + "..." + addr[len(addr)-4:]
}

func formatEthValue(wei *big.Int) string {
	if wei == nil || wei.Sign() == 0 {
		return "0.000000"
	}
	ethFloat := new(big.Float).SetInt(wei)
	divisor := new(big.Float).SetFloat64(1e18)
	ethFloat.Quo(ethFloat, divisor)
	f, _ := ethFloat.Float64()
	return fmt.Sprintf("%.6f", f)
}

func formatEthValueRaw(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	ethFloat := new(big.Float).SetInt(wei)
	divisor := new(big.Float).SetFloat64(1e18)
	ethFloat.Quo(ethFloat, divisor)
	return ethFloat.Text('f', 18)
}

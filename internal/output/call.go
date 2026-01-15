package output

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	"github.com/dmagro/eth-rpc-monitor/internal/rpc"
	"github.com/fatih/color"
)

// CallDisplay holds eth_call result for rendering
type CallDisplay struct {
	Contract     string
	ContractName string
	Method       string
	Address      string
	RawResult    string
	Calldata     string
	ParsedValue  *big.Int
	Decimals     int
	Symbol       string
	Provider     string
	Latency      time.Duration
	IsAutoSelected bool
}

// RenderCallTerminal outputs call result to terminal
func RenderCallTerminal(cd *CallDisplay, showRaw bool) {
	fmt.Println()
	if cd.IsAutoSelected {
		fmt.Printf("  %s\n", color.New(color.FgCyan, color.Italic).Sprintf("[Auto-selected best provider: %s]", cd.Provider))
		fmt.Println()
	}

	fmt.Printf("%s\n", blockBold(fmt.Sprintf("%s Balance Query", cd.Symbol)))
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("  %s      %s (%s)\n", blockCyan("Contract:"), truncateAddress(cd.Contract), cd.ContractName)
	fmt.Printf("  %s       %s\n", blockCyan("Address:"), cd.Address)
	fmt.Println()

	if showRaw {
		fmt.Printf("  %s      %s\n", blockCyan("Calldata:"), cd.Calldata)
		fmt.Printf("  %s    %s\n", blockCyan("Raw Result:"), cd.RawResult)
		fmt.Println()
	}

	formatted := rpc.FormatTokenAmount(cd.ParsedValue, cd.Decimals, cd.Symbol)
	fmt.Printf("  %s       %s\n", blockCyan("Balance:"), color.New(color.FgGreen, color.Bold).Sprint(formatted))
	
	if cd.ParsedValue != nil {
		fmt.Printf("  %s   %s (raw)\n", blockCyan("Raw Amount:"), cd.ParsedValue.String())
	}
	
	fmt.Println()
	fmt.Printf("  %s    %s (%dms)\n", blockCyan("Fetched via:"), cd.Provider, cd.Latency.Milliseconds())
	fmt.Println()
}

// RenderCallJSON outputs call result as JSON
func RenderCallJSON(cd *CallDisplay, includeRaw bool) error {
	output := map[string]interface{}{
		"contract": map[string]interface{}{
			"address": cd.Contract,
			"name":    cd.ContractName,
		},
		"query": map[string]interface{}{
			"method":  cd.Method,
			"address": cd.Address,
		},
		"result": map[string]interface{}{
			"rawValue":       cd.ParsedValue.String(),
			"formattedValue": rpc.FormatTokenAmount(cd.ParsedValue, cd.Decimals, cd.Symbol),
			"decimals":       cd.Decimals,
			"symbol":         cd.Symbol,
		},
		"meta": map[string]interface{}{
			"provider":  cd.Provider,
			"latencyMs": cd.Latency.Milliseconds(),
		},
	}

	if includeRaw {
		output["raw"] = map[string]interface{}{
			"calldata": cd.Calldata,
			"result":   cd.RawResult,
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

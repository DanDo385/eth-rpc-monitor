package format

import (
	"fmt"
	"io"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func FormatBlock(w io.Writer, block *rpc.Block, provider string, latency time.Duration) {
	p := block.Parsed()

	fmt.Fprintf(w, "\nBlock #%s\n", rpc.FormatNumber(p.Number))
	fmt.Fprintln(w, "══════════════════════════════════════════════════")
	fmt.Fprintf(w, "  Hash:         %s\n", p.Hash)
	fmt.Fprintf(w, "  Parent:       %s\n", p.ParentHash)
	fmt.Fprintf(w, "  Timestamp:    %s\n", rpc.FormatTimestamp(p.Timestamp))
	fmt.Fprintf(w, "  Gas:          %s / %s (%.1f%%)\n",
		rpc.FormatNumber(p.GasUsed),
		rpc.FormatNumber(p.GasLimit),
		float64(p.GasUsed)/float64(p.GasLimit)*100)
	fmt.Fprintf(w, "  Base Fee:     %s\n", rpc.FormatGwei(p.BaseFeePerGas))
	fmt.Fprintf(w, "  Transactions: %d\n", p.TxCount)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Provider:     %s (%dms)\n", provider, latency.Milliseconds())
	fmt.Fprintln(w)
}

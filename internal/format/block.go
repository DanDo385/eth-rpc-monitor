package format

import (
	"fmt"
	"io"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

func FormatBlock(w io.Writer, block *rpc.Block, provider string, latency time.Duration) {
	p := block.Parsed()

	fmt.Fprintf(w, "\n%s #%s\n", Bold("Block"), Bold(rpc.FormatNumber(p.Number)))
	fmt.Fprintln(w, "══════════════════════════════════════════════════")
	fmt.Fprintf(w, "  %s     %s\n", Bold("Hash:"), p.Hash)
	fmt.Fprintf(w, "  %s   %s\n", Bold("Parent:"), p.ParentHash)
	fmt.Fprintf(w, "  %s %s\n", Bold("Timestamp:"), rpc.FormatTimestamp(p.Timestamp))
	fmt.Fprintf(w, "  %s      %s / %s %s\n",
		Bold("Gas:"),
		rpc.FormatNumber(p.GasUsed),
		rpc.FormatNumber(p.GasLimit),
		Dim(fmt.Sprintf("(%.1f%%)", float64(p.GasUsed)/float64(p.GasLimit)*100)))
	fmt.Fprintf(w, "  %s %s\n", Bold("Base Fee:"), rpc.FormatGwei(p.BaseFeePerGas))
	fmt.Fprintf(w, "  %s %d\n", Bold("Transactions:"), p.TxCount)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s %s %s\n", Bold("Provider:"), provider, Dim(fmt.Sprintf("(%dms)", latency.Milliseconds())))
	fmt.Fprintln(w)
}

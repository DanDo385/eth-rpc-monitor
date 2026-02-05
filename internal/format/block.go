// =============================================================================
// FILE: internal/format/block.go
// ROLE: Block Display Renderer — Human-Readable Single-Block Output
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file contains exactly one function: FormatBlock. It takes raw block data
// from the RPC layer and renders it as a formatted, color-coded display in the
// terminal. It's used exclusively by the `block` command (cmd/block/main.go).
//
// DATA FLOW
// =========
//
//   RPC Node ──▶ client.GetBlock() ──▶ *rpc.Block (hex strings)
//                                          │
//                                          ▼
//                                     block.Parsed() ──▶ rpc.ParsedBlock (typed values)
//                                          │
//                                          ▼
//                                     FormatBlock() (THIS FILE)
//                                          │
//                                          ▼
//                                     Terminal output:
//                                     ┌──────────────────────────────────┐
//                                     │ Block #21,234,567               │
//                                     │ ════════════════════════════════ │
//                                     │   Hash:     0xa1b2c3d4...       │
//                                     │   Parent:   0x9876fedc...       │
//                                     │   Timestamp: 2024-01-15...      │
//                                     │   Gas:      29,847,293 / 30M    │
//                                     │   Base Fee: 25.43 gwei          │
//                                     │   Transactions: 342             │
//                                     │   Provider: alchemy (45ms)      │
//                                     └──────────────────────────────────┘
//
// DESIGN: io.Writer PATTERN
// ==========================
// FormatBlock writes to an io.Writer (an interface), not directly to os.Stdout.
// This is a fundamental Go design pattern:
//
//   - os.Stdout implements io.Writer    → for normal terminal output
//   - bytes.Buffer implements io.Writer → for capturing output in tests
//   - os.File implements io.Writer      → for writing to log files
//
// By accepting io.Writer, this function works with any output destination
// without knowing or caring which one it is. This is the "dependency
// inversion principle" in practice — depend on abstractions (io.Writer),
// not concretions (os.Stdout).
//
// WHAT A READER SHOULD UNDERSTAND
// ================================
// 1. How raw block data flows through Parsed() into formatted output
// 2. The io.Writer pattern and why it enables testability
// 3. How pointer parameters avoid copying large structs
// =============================================================================

package format

import (
	"fmt"
	"io"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// FormatBlock renders a single Ethereum block as formatted, color-coded text.
//
// PARAMETERS
// ==========
// - w io.Writer: The output destination (typically os.Stdout)
//
// - block *rpc.Block: POINTER to the raw block data from the RPC response.
//
//   WHY A POINTER (*rpc.Block)?
//   The `*` in *rpc.Block means "pointer to rpc.Block." This function
//   receives the MEMORY ADDRESS of the Block struct, not a copy of it.
//
//   In memory:
//   ┌───────────────────┐     ┌──────────────────────────┐
//   │ FormatBlock params │     │ Block (on heap)          │
//   │  block: ──────────┼────▶│  Number: "0x1444F3B"     │
//   └───────────────────┘     │  Hash: "0xa1b2c3d4..."   │
//                             │  Transactions: [300 hashes]
//                             └──────────────────────────┘
//
//   If we passed Block by value (without *), Go would COPY the entire struct
//   including the Transactions slice header. The pointer avoids this copy.
//   Since FormatBlock only reads the block (doesn't modify it), the choice
//   of pointer vs value doesn't affect correctness — but pointer is more
//   efficient and conventional for struct parameters.
//
// - provider string: The name of the provider that served this block.
//   Strings in Go are immutable and cheap to copy (they're internally a
//   pointer + length, 16 bytes total), so passing by value is fine.
//
// - latency time.Duration: How long the RPC call took. time.Duration is an
//   int64 under the hood (8 bytes), so passing by value is efficient.
//
// OUTPUT FORMAT
// =============
// The output is designed for terminal consumption with:
//   - Bold labels for readability
//   - Double-line separator (═) for visual hierarchy
//   - Comma-separated numbers (21,234,567) via rpc.FormatNumber
//   - Relative timestamps ("12s ago") via rpc.FormatTimestamp
//   - Gas percentage to show block utilization
//   - Dimmed secondary info (latency, gas percentage)
func FormatBlock(w io.Writer, block *rpc.Block, provider string, latency time.Duration) {
	// Call Parsed() to convert hex strings to native Go types.
	// block.Parsed() is called on the *Block pointer. Go automatically
	// dereferences the pointer to call the method — block.Parsed() is
	// syntactic sugar for (*block).Parsed().
	//
	// The returned ParsedBlock `p` is a VALUE (not a pointer), stored on
	// the stack. It contains typed fields ready for display formatting.
	p := block.Parsed()

	// Render the header: "Block #21,234,567"
	// Bold() wraps text with ANSI bold escape codes.
	// rpc.FormatNumber() adds thousand separators.
	fmt.Fprintf(w, "\n%s #%s\n", Bold("Block"), Bold(rpc.FormatNumber(p.Number)))
	fmt.Fprintln(w, "══════════════════════════════════════════════════")

	// Render block identity: hash and parent hash.
	// These are the 32-byte (64 hex character) identifiers that uniquely
	// identify each block and link it to its parent, forming the blockchain.
	fmt.Fprintf(w, "  %s     %s\n", Bold("Hash:"), p.Hash)
	fmt.Fprintf(w, "  %s   %s\n", Bold("Parent:"), p.ParentHash)

	// Render timestamp with human-readable "ago" suffix.
	// p.Timestamp is a uint64 Unix timestamp that FormatTimestamp converts
	// to "2024-01-15 14:32:18 UTC (12s ago)".
	fmt.Fprintf(w, "  %s %s\n", Bold("Timestamp:"), rpc.FormatTimestamp(p.Timestamp))

	// Render gas usage: gasUsed / gasLimit (percentage).
	// Gas is Ethereum's unit of computational effort. Each block has a limit
	// (currently ~30M gas), and the percentage shows how "full" the block is.
	// A consistently full block (>95%) indicates high network demand.
	// The Dim() wrapper makes the percentage less visually prominent.
	fmt.Fprintf(w, "  %s      %s / %s %s\n",
		Bold("Gas:"),
		rpc.FormatNumber(p.GasUsed),
		rpc.FormatNumber(p.GasLimit),
		Dim(fmt.Sprintf("(%.1f%%)", float64(p.GasUsed)/float64(p.GasLimit)*100)))

	// Render base fee (EIP-1559 gas pricing).
	// p.BaseFeePerGas is *big.Int — FormatGwei handles nil (pre-London blocks)
	// and converts wei to gwei for readability.
	fmt.Fprintf(w, "  %s %s\n", Bold("Base Fee:"), rpc.FormatGwei(p.BaseFeePerGas))

	// Render transaction count.
	// p.TxCount is derived from len(block.Transactions) in the Parsed() method.
	fmt.Fprintf(w, "  %s %d\n", Bold("Transactions:"), p.TxCount)
	fmt.Fprintln(w)

	// Render the provider attribution with latency.
	// The Dim() wrapper on the latency makes it secondary to the provider name.
	// latency.Milliseconds() converts time.Duration to int64 milliseconds.
	fmt.Fprintf(w, "  %s %s %s\n", Bold("Provider:"), provider, Dim(fmt.Sprintf("(%dms)", latency.Milliseconds())))
	fmt.Fprintln(w)
}

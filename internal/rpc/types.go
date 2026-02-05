// =============================================================================
// FILE: internal/rpc/types.go
// ROLE: Data Model Foundation — The "vocabulary" of the entire system
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file defines every data structure that flows through the eth-rpc-monitor
// system. Think of it as the shared language that all other packages speak.
// When the RPC client sends a request, it serializes a Request struct. When it
// receives bytes back over the wire, it deserializes them into a Response and
// then into a Block. When the formatting layer needs human-readable numbers,
// it calls Parsed() to convert the raw hex strings into native Go types.
//
// DEPENDENCY GRAPH
// ================
//
//   cmd/block ──┐
//   cmd/test ───┤
//   cmd/snapshot┤──▶ internal/rpc (this package) ──▶ net/http (stdlib)
//   cmd/monitor─┘         │
//                         ▼
//                  internal/format (consumes Block / ParsedBlock)
//
// Every command in cmd/ imports this package. The format package also imports
// it to access ParsedBlock fields and helper functions. This file is therefore
// the single most depended-upon file in the project.
//
// CS CONCEPTS: VALUES vs. REPRESENTATIONS
// ========================================
// Ethereum's JSON-RPC protocol returns ALL numeric values as hex-encoded
// strings (e.g., "0x1a2b3c"). This is a design choice inherited from
// JavaScript's inability to handle 64-bit integers natively. On the Go side,
// we need actual numeric types (uint64, *big.Int) for arithmetic, comparisons,
// and formatted display.
//
// This creates a two-layer type system:
//   Layer 1 (Block):       Raw wire format — hex strings exactly as the RPC returns.
//   Layer 2 (ParsedBlock): Typed values — uint64 for block numbers, *big.Int for fees.
//
// The Parsed() method bridges these two layers, converting from representation
// to value. This separation keeps deserialization simple (just map JSON fields
// to strings) while giving downstream code proper typed data to work with.
//
// WHAT A READER SHOULD UNDERSTAND AFTER THIS FILE
// ================================================
// 1. The JSON-RPC 2.0 request/response envelope (Request, Response, RPCError)
// 2. How Ethereum block data arrives as hex strings (Block)
// 3. How those hex strings become usable Go types (ParsedBlock)
// 4. The role of pointer types (*big.Int, *RPCError) — why some fields are
//    pointers and others are values
// 5. How Go struct tags (`json:"..."`) control serialization/deserialization
// =============================================================================

package rpc

import (
	"encoding/json"
	"math/big"
)

// =============================================================================
// SECTION 1: JSON-RPC 2.0 Protocol Envelope
// =============================================================================
//
// JSON-RPC is a lightweight remote procedure call protocol. The idea is simple:
// the client sends a JSON object describing what method to call and with what
// parameters, and the server sends back a JSON object with the result (or an
// error). Version "2.0" is the version used by Ethereum nodes.
//
// Every JSON-RPC exchange follows this pattern:
//
//   Client ──[ Request JSON ]──▶ Server
//   Client ◀──[ Response JSON ]── Server
//
// The Request and Response structs below are the Go representations of these
// JSON objects. Go's encoding/json package uses struct tags (the `json:"..."`
// annotations) to map between Go field names and JSON key names.
// =============================================================================

// Request represents a JSON-RPC 2.0 request sent to an Ethereum node.
//
// A concrete example — asking "what is the latest block number?":
//
//	{
//	    "jsonrpc": "2.0",
//	    "method":  "eth_blockNumber",
//	    "params":  [],
//	    "id":      1
//	}
//
// The Params field uses []interface{} (a slice of empty interfaces) because
// different RPC methods take different parameter types. For eth_blockNumber,
// params is empty ([]). For eth_getBlockByNumber, params might be
// ["0x10d4f", false] — a hex string and a boolean. The empty interface
// (interface{}) is Go's way of saying "any type" — it can hold a string,
// a number, a bool, or any other value. This flexibility is necessary because
// the JSON-RPC spec does not constrain parameter types.
//
// The ID field is used to match requests with responses in asynchronous
// protocols. Since we use synchronous HTTP (one request, one response per
// connection), we hardcode this to 1 everywhere.
type Request struct {
	JSONRPC string        `json:"jsonrpc"` // Always "2.0" — protocol version
	Method  string        `json:"method"`  // RPC method name, e.g., "eth_blockNumber"
	Params  []interface{} `json:"params"`  // Method arguments — varies per method
	ID      int           `json:"id"`      // Request identifier (always 1 in this codebase)
}

// Response represents a JSON-RPC 2.0 response from an Ethereum node.
//
// A concrete example — the server's reply to eth_blockNumber:
//
//	{
//	    "jsonrpc": "2.0",
//	    "id":      1,
//	    "result":  "0x14a0b3f"
//	}
//
// DEEP DIVE: json.RawMessage for the Result field
// ================================================
// The Result field is declared as json.RawMessage, NOT as a string or an
// interface{}. Why? Because the shape of "result" depends on which method
// was called:
//
//   - eth_blockNumber returns a single hex string:  "0x14a0b3f"
//   - eth_getBlockByNumber returns a full JSON object: { "number": "0x...", ... }
//
// json.RawMessage tells Go's JSON decoder: "Don't try to interpret this field
// yet — just store the raw bytes exactly as they arrived." This lets us defer
// the actual parsing to the caller, who knows what type to expect.
//
// Under the hood, json.RawMessage is simply a []byte (a byte slice). No
// allocation of parsed objects happens until someone explicitly calls
// json.Unmarshal on it.
//
// POINTER SEMANTICS: *RPCError
// ============================
// The Error field is declared as *RPCError (a POINTER to RPCError), not as
// RPCError (a value). This is critical:
//
//   - When there is NO error, the JSON field "error" is absent from the
//     response. Go's JSON decoder sets *RPCError to nil (the zero value for
//     a pointer).
//   - When there IS an error, the JSON decoder allocates an RPCError on the
//     heap and points this field to it.
//
// This lets us distinguish "no error" (nil pointer) from "empty error"
// (a zero-valued RPCError struct). If we used a plain RPCError value,
// we couldn't tell whether the server omitted the field or sent an error
// with code=0 and message="".
//
// In memory:
//
//   Successful response:          Error response:
//   ┌──────────────┐              ┌──────────────┐
//   │ Response     │              │ Response     │
//   │  Error: nil ─┼─▶ (nothing) │  Error: ─────┼──▶ ┌───────────┐
//   └──────────────┘              └──────────────┘    │ RPCError  │
//                                                     │ Code: -32600│
//                                                     │ Msg: "..." │
//                                                     └───────────┘
//
// The `omitempty` tag in the JSON annotation means: when serializing TO JSON,
// omit this field if the pointer is nil. When deserializing FROM JSON, if the
// key is missing, leave the pointer as nil.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`         // Always "2.0"
	ID      int             `json:"id"`              // Matches the request ID
	Result  json.RawMessage `json:"result"`          // Raw JSON — parsed later by caller
	Error   *RPCError       `json:"error,omitempty"` // nil when no error; pointer to RPCError on failure
}

// RPCError represents an error returned by the Ethereum JSON-RPC server.
//
// Standard error codes from the JSON-RPC 2.0 specification:
//   -32700  Parse error       — Invalid JSON
//   -32600  Invalid request   — JSON is valid but not a proper request
//   -32601  Method not found  — Method does not exist
//   -32602  Invalid params    — Invalid method parameters
//   -32603  Internal error    — Server-side error
//
// Ethereum nodes may also return custom error codes (e.g., -32000 for
// execution reverted). The Message field contains a human-readable
// description of what went wrong.
type RPCError struct {
	Code    int    `json:"code"`    // Numeric error code (negative = standard, positive = custom)
	Message string `json:"message"` // Human-readable error description
}

// =============================================================================
// SECTION 2: Ethereum Block Data — Raw Wire Format
// =============================================================================
//
// When we call eth_getBlockByNumber, the Ethereum node returns a JSON object
// with dozens of fields. The Block struct captures the fields we care about
// for monitoring: block identity (number, hash, parent), timing (timestamp),
// gas economics (gasUsed, gasLimit, baseFeePerGas), and transaction count.
//
// CRITICAL: Every numeric field arrives as a hex string.
//
// This is NOT a Go design choice — it's dictated by the Ethereum JSON-RPC
// specification. The Ethereum protocol chose hex encoding because:
//   1. JavaScript (the original web3.js language) cannot safely represent
//      integers larger than 2^53 using native Number types.
//   2. Some values (like baseFeePerGas) can exceed 2^64, requiring arbitrary
//      precision (which big.Int provides in Go).
//   3. Hex is the native representation in the EVM (Ethereum Virtual Machine).
//
// Example of raw block data from the wire:
//
//	{
//	    "number":        "0x1444F3B",        ← hex for 21,233,467
//	    "hash":          "0xa1b2c3d4...",
//	    "parentHash":    "0x9876fedc...",
//	    "timestamp":     "0x67830F1F",       ← hex for Unix timestamp
//	    "gasUsed":       "0xE4E1C0",         ← hex for 14,999,040
//	    "gasLimit":      "0x1C9C380",        ← hex for 30,000,000
//	    "baseFeePerGas": "0x59682F000",      ← hex for wei (needs big.Int)
//	    "transactions":  ["0xabc...", "0xdef...", ...]
//	}
// =============================================================================

// Block holds the raw JSON-RPC response data for an Ethereum block.
//
// All numeric fields are strings because they arrive as hex from the wire.
// To get usable numeric values, call the Parsed() method (see below).
//
// The Transactions field is a slice of transaction hash strings. We pass
// `false` as the second parameter to eth_getBlockByNumber, which tells the
// node to return only transaction hashes (not full transaction objects).
// This saves bandwidth and parsing time — for monitoring, we only need the
// count, not the details.
//
// The `omitempty` tag on BaseFeePerGas handles pre-EIP-1559 blocks (before
// the London hard fork in August 2021), which do not have a base fee field.
// For those blocks, the JSON key is simply absent, and this field remains
// an empty string.
type Block struct {
	Number        string   `json:"number"`                  // Block height as hex (e.g., "0x1444F3B")
	Hash          string   `json:"hash"`                    // 32-byte block hash as hex
	ParentHash    string   `json:"parentHash"`              // Hash of the parent block
	Timestamp     string   `json:"timestamp"`               // Unix timestamp as hex
	GasUsed       string   `json:"gasUsed"`                 // Gas consumed by all txns, as hex
	GasLimit      string   `json:"gasLimit"`                // Maximum gas allowed in this block, as hex
	BaseFeePerGas string   `json:"baseFeePerGas,omitempty"` // EIP-1559 base fee in wei, as hex (absent pre-London)
	Transactions  []string `json:"transactions"`            // Transaction hashes (not full tx objects)
}

// =============================================================================
// SECTION 3: Parsed Block Data — Typed Values for Application Logic
// =============================================================================
//
// ParsedBlock is the "processed" form of Block. Where Block stores hex strings
// (the wire format), ParsedBlock stores native Go types (the application format).
//
// This separation follows a common systems pattern:
//   Wire format → Parse → Application types → Business logic
//
// By keeping the parsing in one place (the Parsed() method), we ensure that:
//   1. Hex parsing code isn't scattered across the codebase
//   2. Every consumer gets properly typed data
//   3. Parsing errors are handled once, not everywhere
//
// POINTER TYPE: *big.Int for BaseFeePerGas
// =========================================
// BaseFeePerGas is declared as *big.Int (a POINTER to big.Int), not big.Int.
// There are two reasons:
//
// 1. OPTIONALITY: Pre-EIP-1559 blocks have no base fee. A nil pointer
//    cleanly represents "this field does not exist," while a zero-valued
//    big.Int would ambiguously mean "the fee is zero."
//
// 2. CONVENTION: Go's math/big package is designed around pointer receivers.
//    big.Int methods like SetString() and Quo() modify the receiver in place
//    and return a pointer. Using *big.Int aligns with how the standard
//    library expects you to use these values.
//
// In memory, when BaseFeePerGas is present:
//
//   Stack (ParsedBlock)            Heap
//   ┌───────────────────┐         ┌─────────────┐
//   │ Number: 21233467  │         │ big.Int      │
//   │ Hash: "0xa1b2..." │         │ value: 24000 │
//   │ BaseFeePerGas: ───┼────────▶│ 000000 (wei) │
//   │ TxCount: 342      │         └─────────────┘
//   └───────────────────┘
//
// When BaseFeePerGas is absent (pre-London blocks):
//
//   Stack (ParsedBlock)
//   ┌───────────────────┐
//   │ Number: 1000000   │
//   │ Hash: "0x9876..." │
//   │ BaseFeePerGas: nil┤ ──▶ (nothing — no heap allocation)
//   │ TxCount: 150      │
//   └───────────────────┘
// =============================================================================

// ParsedBlock holds block data as native Go types, ready for display and logic.
type ParsedBlock struct {
	Number        uint64   // Block height as a 64-bit unsigned integer
	Hash          string   // Block hash (already a string, no conversion needed)
	ParentHash    string   // Parent block hash
	Timestamp     uint64   // Unix timestamp as a 64-bit unsigned integer
	GasUsed       uint64   // Gas consumed by transactions in this block
	GasLimit      uint64   // Maximum gas allowed in this block
	BaseFeePerGas *big.Int // EIP-1559 base fee in wei; nil for pre-London blocks
	TxCount       int      // Number of transactions (derived from len(Transactions))
}

// =============================================================================
// SECTION 4: The Parsed() Method — Bridging Wire Format to Application Types
// =============================================================================
//
// Parsed() converts a Block (hex strings from the wire) into a ParsedBlock
// (native Go types). This is the single point where all hex-to-numeric
// conversion happens for block data.
//
// POINTER RECEIVER: (b *Block)
// ============================
// The method is declared on *Block (a pointer to Block), not Block (a value).
//
//   func (b *Block) Parsed() ParsedBlock
//         ^^^^^^^^
//         This is a POINTER receiver.
//
// What does this mean in memory?
//
// When you call: parsedBlock := myBlock.Parsed()
//
//   1. Go does NOT copy the entire Block struct onto the stack.
//   2. Instead, it passes a pointer (the memory address) of the existing Block.
//   3. Inside the method, `b` is a pointer — reading b.Number actually means
//      "go to the memory address stored in b, then read the Number field."
//
//   Before the call:
//   ┌────────────────────────────────────────┐
//   │ myBlock (Block)                        │
//   │  Number: "0x1444F3B"                   │
//   │  Hash:   "0xa1b2c3d4..."               │
//   │  ...                                   │
//   └────────────────────────────────────────┘
//        ▲
//        │  b points here (no copy made)
//        │
//   ┌────┼──────────────────┐
//   │ b  │ (pointer)        │  ← the receiver inside Parsed()
//   └───────────────────────┘
//
// Why use a pointer receiver here?
//   - Block contains a []string slice (Transactions), which internally holds
//     a pointer to an underlying array, a length, and a capacity.
//   - Copying the Block struct would copy the slice header but NOT the
//     underlying transaction data — this is safe but wasteful.
//   - A pointer receiver avoids even the small overhead of copying the struct.
//   - Convention: In Go, if ANY method on a type uses a pointer receiver,
//     ALL methods should, for consistency.
//
// ERROR HANDLING STRATEGY
// =======================
// Notice that errors from ParseHexUint64() are silently discarded:
//
//   num, _ := ParseHexUint64(b.Number)
//         ^
//         The underscore _ means "I'm ignoring this error."
//
// This is a deliberate design choice for a monitoring tool:
//   - If the Ethereum node returned malformed hex, the RPC layer has already
//     validated the response (we got valid JSON with valid structure).
//   - A parse failure would mean the hex string was empty or malformed, which
//     would default the uint64 to 0 — a visually obvious anomaly in output.
//   - Adding error propagation here would complicate every caller without
//     providing actionable recovery (what would we do differently?).
//
// In a trading system that executes based on these values, you would absolutely
// want to check these errors. In a monitoring/display tool, silent zero-defaults
// are an acceptable trade-off for simplicity.
// =============================================================================

// Parsed converts the raw hex-encoded Block into a ParsedBlock with native Go types.
//
// Each hex string is parsed into its corresponding numeric type:
//   - Number, Timestamp, GasUsed, GasLimit → uint64 via ParseHexUint64
//   - BaseFeePerGas → *big.Int via ParseHexBigInt (only if present)
//   - TxCount is derived from the length of the Transactions slice
//
// Hash and ParentHash are already strings and pass through unchanged.
func (b *Block) Parsed() ParsedBlock {
	// Parse each hex field into its native type.
	// The _ discards the error — see the ERROR HANDLING STRATEGY comment above.
	num, _ := ParseHexUint64(b.Number)
	ts, _ := ParseHexUint64(b.Timestamp)
	gasUsed, _ := ParseHexUint64(b.GasUsed)
	gasLimit, _ := ParseHexUint64(b.GasLimit)

	// BaseFeePerGas requires special handling because:
	// 1. It may be absent (empty string) for pre-EIP-1559 blocks
	// 2. It can exceed uint64 range, requiring *big.Int
	//
	// We initialize baseFee as nil (the zero value for a pointer type).
	// If the field is present, ParseHexBigInt allocates a new big.Int on the
	// heap and returns a pointer to it.
	//
	// In memory terms:
	//   b.BaseFeePerGas == ""  → baseFee stays nil (no allocation)
	//   b.BaseFeePerGas != ""  → baseFee = &big.Int{...} (heap allocation)
	var baseFee *big.Int
	if b.BaseFeePerGas != "" {
		baseFee = ParseHexBigInt(b.BaseFeePerGas)
	}

	// Return a ParsedBlock VALUE (not a pointer).
	// This struct is small enough (~80 bytes) that returning by value is
	// efficient. The caller gets their own copy on the stack, and the
	// BaseFeePerGas pointer inside it shares the same heap-allocated big.Int.
	//
	// Memory after return:
	//
	//   Caller's stack              Heap
	//   ┌────────────────┐         ┌──────────┐
	//   │ ParsedBlock    │         │ big.Int  │
	//   │  Number: 21M   │         │ (24 Gwei)│
	//   │  BaseFee: ─────┼────────▶│          │
	//   │  TxCount: 342  │         └──────────┘
	//   └────────────────┘
	return ParsedBlock{
		Number:        num,
		Hash:          b.Hash,
		ParentHash:    b.ParentHash,
		Timestamp:     ts,
		GasUsed:       gasUsed,
		GasLimit:      gasLimit,
		BaseFeePerGas: baseFee,
		TxCount:       len(b.Transactions),
	}
}

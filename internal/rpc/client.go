// =============================================================================
// FILE: internal/rpc/client.go
// ROLE: Network Layer — The HTTP JSON-RPC Client with Latency Measurement
// =============================================================================
//
// SYSTEM CONTEXT
// ==============
// This file is the system's only point of contact with the outside world.
// Every byte of data that enters the eth-rpc-monitor system — block numbers,
// block hashes, gas values, timestamps — comes through this file's Client.
//
// The Client wraps Go's standard net/http.Client to send JSON-RPC requests
// to Ethereum nodes and measure how long each round-trip takes. That latency
// measurement is the primary output of this monitoring tool.
//
// ARCHITECTURE POSITION
// =====================
//
//   ┌──────────────────────────────────────────┐
//   │  cmd/* (block, test, snapshot, monitor)  │
//   │  Creates Client instances, calls methods  │
//   └─────────────┬────────────────────────────┘
//                 │
//                 ▼
//   ┌──────────────────────────────────────────┐
//   │  internal/rpc/client.go (THIS FILE)      │
//   │  Serializes requests, measures latency,   │
//   │  deserializes responses                   │
//   └─────────────┬────────────────────────────┘
//                 │  HTTP POST (JSON body)
//                 ▼
//   ┌──────────────────────────────────────────┐
//   │  Remote Ethereum RPC Node                │
//   │  (Alchemy, Infura, local Geth, etc.)     │
//   └──────────────────────────────────────────┘
//
// DESIGN DECISIONS
// ================
// 1. NO RETRY LOGIC: This is a monitoring tool, not a production RPC client.
//    If a request fails, we want to know it failed — retries would hide
//    reliability problems, which is exactly what we're trying to detect.
//
// 2. NO CONNECTION POOLING: We create a new Client per provider per operation.
//    For a monitoring tool making a few requests per cycle, connection pooling
//    adds complexity without meaningful benefit.
//
// 3. LATENCY INCLUDES EVERYTHING: The measured latency spans from sending
//    the HTTP request to fully reading the response body. This is the
//    "real-world" latency — exactly what a trading bot would experience.
//
// 4. STANDARD LIBRARY ONLY: No external RPC libraries. The entire HTTP and
//    JSON-RPC layer is built on Go's net/http and encoding/json. This keeps
//    the dependency footprint minimal and the code auditable.
//
// CS CONCEPTS: HTTP AS A TRANSPORT, JSON-RPC AS A PROTOCOL
// =========================================================
// HTTP and JSON-RPC operate at different layers:
//
//   - HTTP is the TRANSPORT: it handles sending bytes to a server and getting
//     bytes back. It deals with TCP connections, TLS encryption, headers, etc.
//   - JSON-RPC is the APPLICATION PROTOCOL: it defines the structure of the
//     bytes being sent (what a "request" looks like, what a "response" looks
//     like, how errors are reported).
//
// Think of HTTP as the postal service (it delivers envelopes) and JSON-RPC
// as the letter format inside the envelope (how the message is structured).
//
// Every Ethereum RPC call follows this flow:
//   1. Build a JSON-RPC Request struct
//   2. Serialize it to JSON bytes (json.Marshal)
//   3. Send those bytes as an HTTP POST body
//   4. Receive the HTTP response body
//   5. Deserialize it from JSON bytes into a Response struct (json.Decode)
//   6. Extract the result and check for errors
// =============================================================================

package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// =============================================================================
// SECTION 1: Client Type — The RPC Connection Handle
// =============================================================================

// Client is an HTTP-based JSON-RPC client for a single Ethereum provider.
//
// Each Client instance represents a connection to ONE provider (e.g., Alchemy,
// Infura). The system creates multiple Client instances — one per provider —
// and queries them concurrently.
//
// STRUCT FIELDS AND VISIBILITY
// ============================
// All fields are lowercase (unexported), meaning they can only be accessed
// from within the rpc package. External packages must use the exported
// methods (Name, Call, BlockNumber, GetBlock) to interact with the Client.
// This is Go's form of encapsulation — similar to "private" in other languages.
//
// POINTER FIELD: *http.Client
// ===========================
// The httpClient field is *http.Client (a POINTER to http.Client).
//
// In memory:
//
//	Stack / Heap (our Client)           Heap (stdlib Client)
//	┌────────────────────────┐         ┌──────────────────┐
//	│ Client                 │         │ http.Client      │
//	│  name: "alchemy"       │         │  Timeout: 10s    │
//	│  url:  "https://..."   │         │  Transport: nil  │
//	│  httpClient: ──────────┼────────▶│  (uses default)  │
//	└────────────────────────┘         └──────────────────┘
//
// Why a pointer? Go's http.Client is designed to be used as a shared,
// long-lived object. It manages an internal connection pool (Transport)
// that reuses TCP connections across requests. Copying an http.Client
// by value could lead to multiple copies sharing the same Transport state,
// causing subtle concurrency bugs. The pointer ensures all references
// point to the same underlying client.
type Client struct {
	name       string       // Human-readable provider name (e.g., "alchemy", "infura")
	url        string       // Full RPC endpoint URL (e.g., "https://eth-mainnet.g.alchemy.com/v2/...")
	httpClient *http.Client // Go's HTTP client with configured timeout
}

// =============================================================================
// SECTION 2: Constructor — Creating a New Client
// =============================================================================

// NewClient creates a new RPC client for the given provider.
//
// RETURN TYPE: *Client (pointer to Client)
// ========================================
// This function returns *Client, not Client. Let's trace exactly what
// happens in memory:
//
//	return &Client{
//	    name:       name,
//	    url:        url,
//	    httpClient: &http.Client{Timeout: timeout},
//	}
//
// Step 1: &http.Client{Timeout: timeout}
//
//   - The `&` (address-of operator) does two things:
//     a) Allocates an http.Client struct on the heap
//     b) Returns the memory address (pointer) to that struct
//
//     BEFORE &:  A temporary http.Client value exists (conceptually)
//     AFTER &:   That value lives on the heap, and we have its address
//
//     Heap:
//     ┌──────────────────┐
//     │ http.Client      │ ← allocated by &http.Client{...}
//     │  Timeout: 10s    │
//     └──────────────────┘
//
// Step 2: &Client{name: ..., url: ..., httpClient: ...}
//
//   - Another `&` address-of operator, this time on our Client struct
//
//   - Allocates the Client on the heap and returns its address
//
//     Heap:
//     ┌────────────────────────┐    ┌──────────────────┐
//     │ Client                 │    │ http.Client      │
//     │  name: "alchemy"       │    │  Timeout: 10s    │
//     │  url:  "https://..."   │    └──────────────────┘
//     │  httpClient: ──────────┼────▶       ▲
//     └────────────────────────┘            │
//     └── same address stored in httpClient
//
// Why return a pointer?
//  1. Client has methods with pointer receivers (Call, BlockNumber, GetBlock).
//     In Go, a pointer receiver means the method can be called on *Client
//     but not on Client (a value). Returning a pointer lets callers call
//     methods immediately: rpc.NewClient(...).BlockNumber(ctx)
//  2. Consistency: the httpClient field is already a pointer, so the outer
//     struct should be too, to avoid accidental copies.
//  3. Convention: constructors in Go typically return pointers when the type
//     has pointer-receiver methods or contains reference types.
func NewClient(name, url string, timeout time.Duration) *Client {
	return &Client{
		name:       name,
		url:        url,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// Name returns the human-readable provider name for this client.
//
// POINTER RECEIVER: (c *Client)
// =============================
// Even though this method only reads data (doesn't mutate), it uses a
// pointer receiver for consistency — all Client methods use *Client.
// In Go, mixing value and pointer receivers on the same type is discouraged.
//
// When called:
//
//	c.Name()  →  "alchemy"
//
// Under the hood, `c` is a pointer (an address). Go automatically dereferences
// it to access the `name` field. So c.name is syntactic sugar for (*c).name.
func (c *Client) Name() string { return c.name }

// =============================================================================
// SECTION 3: Call — The Core RPC Method
// =============================================================================

// Call sends a JSON-RPC 2.0 request to the Ethereum node and measures latency.
//
// This is the foundational method — every other method (BlockNumber, GetBlock)
// is built on top of Call. It handles:
//  1. Building the JSON-RPC request envelope
//  2. Serializing it to JSON
//  3. Sending the HTTP POST request
//  4. Measuring round-trip latency
//  5. Deserializing the response
//  6. Checking for RPC-level errors
//
// PARAMETERS
// ==========
//
//   - ctx context.Context: Carries cancellation signals and deadlines. If the
//     context is cancelled (e.g., user presses Ctrl+C), the HTTP request is
//     aborted immediately. This is how Go handles cooperative cancellation —
//     every long-running operation checks the context.
//
//   - method string: The Ethereum RPC method name (e.g., "eth_blockNumber",
//     "eth_getBlockByNumber"). These are defined by the Ethereum JSON-RPC spec.
//
//   - params ...interface{}: Variadic parameters passed to the RPC method.
//     The `...` means "zero or more arguments of type interface{}." This lets
//     callers write:
//     c.Call(ctx, "eth_blockNumber")           → params = nil
//     c.Call(ctx, "eth_getBlockByNumber", "0x10d4f", false) → params = ["0x10d4f", false]
//
// RETURN VALUES: (*Response, time.Duration, error)
// =================================================
// Three return values — a Go convention for operations that can fail:
//
//	*Response     — Pointer to the deserialized response (nil on error)
//	time.Duration — Measured latency (0 on error)
//	error         — nil on success, non-nil describing what went wrong
//
// The *Response is a POINTER because:
//   - On error, we return nil (no response to return)
//   - On success, we return the address of the Response we deserialized
//   - The caller can check for nil before accessing fields
//
// LATENCY MEASUREMENT
// ===================
// The latency is measured from BEFORE the HTTP request to AFTER the response
// is fully read and parsed. This captures the full round-trip:
//
//	start := time.Now()     ← clock starts
//	[HTTP send]
//	[network travel]
//	[server processing]
//	[network travel back]
//	[HTTP receive]
//	[JSON decode]
//	latency := time.Since(start)  ← clock stops
//
// This is the latency that matters for real applications — it includes
// network overhead, TLS handshake (on first request), and server processing.
func (c *Client) Call(ctx context.Context, method string, params ...interface{}) (*Response, time.Duration, error) {
	// Ensure params is never nil in the JSON output.
	// JSON serializes nil slices as `null`, but the JSON-RPC spec requires
	// an array: `"params": []`. This substitution ensures spec compliance.
	if params == nil {
		params = []interface{}{}
	}

	// Build and serialize the JSON-RPC request.
	// json.Marshal converts the Request struct into a []byte of JSON.
	// The _ discards the error because marshaling a known-good struct
	// with simple types (string, int, []interface{}) cannot fail in practice.
	body, _ := json.Marshal(Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})

	// START the latency timer.
	// time.Now() captures the current monotonic clock reading.
	// "Monotonic" means it always moves forward — it's not affected by
	// system clock adjustments (like NTP corrections or daylight saving).
	start := time.Now()

	// Create an HTTP request with the context attached.
	// http.NewRequestWithContext ties the request to our context, so if
	// the context is cancelled (e.g., Ctrl+C), the HTTP request is aborted.
	//
	// bytes.NewReader(body) wraps the JSON bytes in an io.Reader interface.
	// This doesn't copy the data — it creates a thin wrapper that reads
	// from the existing byte slice.
	//
	// The _ discards the error because NewRequestWithContext only fails if
	// the method is invalid or the URL can't be parsed — both are programmer
	// errors, not runtime errors, and our inputs are validated at config time.
	req, _ := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Send the HTTP request and wait for the response.
	// c.httpClient.Do(req) performs the entire HTTP transaction:
	//   - DNS lookup (if needed)
	//   - TCP connection (or reuse from pool)
	//   - TLS handshake (for HTTPS endpoints)
	//   - Send request headers and body
	//   - Read response headers
	// The response body is NOT fully read yet — it streams lazily.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network errors: DNS failure, connection refused, timeout, TLS error,
		// context cancellation. Return immediately with zero latency.
		return nil, 0, err
	}
	// defer resp.Body.Close() ensures the response body is closed when this
	// function returns. This is CRITICAL — unclosed response bodies leak TCP
	// connections. The `defer` keyword schedules the Close() call to run when
	// the enclosing function exits, regardless of which return path is taken.
	defer resp.Body.Close()

	// Deserialize the JSON response body into our Response struct.
	//
	// IMPORTANT: &rpcResp — the `&` (address-of operator)
	// ====================================================
	// json.NewDecoder(...).Decode(&rpcResp) passes the ADDRESS of rpcResp
	// to the decoder. This is necessary because:
	//
	//   - Decode needs to WRITE INTO rpcResp (fill in its fields)
	//   - In Go, function arguments are passed by VALUE (copied)
	//   - If we passed rpcResp (without &), Decode would fill in a COPY,
	//     and our original rpcResp would remain empty
	//   - By passing &rpcResp, we give Decode the memory address where
	//     rpcResp lives, so it can write directly into our variable
	//
	// In memory:
	//
	//   WRONG (without &):               CORRECT (with &):
	//   ┌───────────┐                   ┌───────────┐
	//   │ rpcResp   │ ← stays empty     │ rpcResp   │ ← gets filled in
	//   └───────────┘                   └───────────┘
	//                                        ▲
	//   ┌───────────┐                        │
	//   │ copy      │ ← Decode fills this   │ Decode writes directly
	//   └───────────┘   (then discards it)   │ to this address via &
	//
	// This is one of the most fundamental patterns in Go: passing a pointer
	// to a function that needs to modify the caller's data.
	var rpcResp Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)

	// Check for RPC-level errors.
	// Even if the HTTP request succeeded (200 OK), the Ethereum node might
	// return an error at the JSON-RPC level. For example, requesting a
	// block number that doesn't exist, or calling an unsupported method.
	//
	// rpcResp.Error is a *RPCError (pointer). If the response JSON contained
	// no "error" field, this is nil. If it contained an error object, this
	// points to the parsed RPCError struct.
	//
	// The != nil check is a pointer nil check — it asks "does this pointer
	// point to anything?" If yes, there was an error.
	if rpcResp.Error != nil {
		return nil, 0, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	// STOP the latency timer and return.
	// time.Since(start) = time.Now() - start, giving us the total duration.
	//
	// &rpcResp — again, the `&` address-of operator. We return a POINTER to
	// our local rpcResp variable. In Go, this is safe — the compiler detects
	// that rpcResp's address is being returned (it "escapes" the function)
	// and automatically allocates it on the heap instead of the stack.
	// This is called "escape analysis" — the Go compiler decides where to
	// allocate variables based on how they're used, not where they're declared.
	//
	// In C, returning the address of a local variable would be a bug (dangling
	// pointer). In Go, the compiler handles this transparently.
	return &rpcResp, time.Since(start), nil
}

// =============================================================================
// SECTION 4: Convenience Methods — Typed Wrappers Around Call
// =============================================================================
//
// These methods wrap Call() for specific Ethereum RPC methods, adding type
// safety and parsing. Instead of returning raw *Response, they return the
// specific Go types the caller needs (uint64 for block numbers, *Block for
// block data).
// =============================================================================

// BlockNumber calls eth_blockNumber and returns the latest block height as uint64.
//
// This is the simplest and fastest RPC call — it returns a single hex string
// representing the current block number. Every monitoring command uses this
// to check how "up to date" a provider is.
//
// POINTER EXPLANATION: &hexStr
// ============================
// json.Unmarshal(resp.Result, &hexStr) passes the ADDRESS of hexStr to
// the JSON unmarshaler. Same pattern as Decode(&rpcResp) above:
//
//   - resp.Result is a json.RawMessage ([]byte) containing `"0x14a0b3f"`
//   - &hexStr gives Unmarshal a place to write the parsed string
//   - After the call, hexStr contains "0x14a0b3f" (without JSON quotes)
//
// If we wrote json.Unmarshal(resp.Result, hexStr) without &, Go would pass
// a COPY of the empty string, Unmarshal would try to fill in the copy,
// and our hexStr would remain empty.
func (c *Client) BlockNumber(ctx context.Context) (uint64, time.Duration, error) {
	resp, latency, err := c.Call(ctx, "eth_blockNumber")
	if err != nil {
		return 0, latency, err
	}

	var hexStr string
	json.Unmarshal(resp.Result, &hexStr)

	num, _ := ParseHexUint64(hexStr)
	return num, latency, nil
}

// GetBlock calls eth_getBlockByNumber and returns the full block data.
//
// Parameters:
//   - blockNum: Block identifier as a hex string ("0x10d4f") or tag ("latest")
//   - The second RPC parameter (false) means "return transaction hashes only,
//     not full transaction objects." This reduces response size significantly —
//     a block with 300 transactions would be ~10KB with hashes vs ~300KB with
//     full objects.
//
// RETURN TYPE: *Block (pointer to Block)
// =======================================
// We return *Block, not Block, for two reasons:
//
//  1. On error, we return nil — this is only possible with a pointer type.
//     A value type (Block) would require returning a zero-valued Block{},
//     and the caller couldn't distinguish "error" from "empty block."
//
//  2. The Block struct contains a []string slice (Transactions). While
//     returning by value would copy the slice header (cheap), returning
//     a pointer is more idiomatic for structs that are typically passed
//     around and not mutated.
//
// POINTER IN RETURN: &block
// =========================
// return &block — the `&` takes the address of our local `block` variable.
// Go's escape analysis detects this and allocates `block` on the heap so
// it survives after this function returns.
//
//	Inside GetBlock:                After return (caller has the pointer):
//	┌──────────┐                   ┌──────────┐
//	│ block    │ (on heap          │ block    │ (still on heap)
//	│ .Number  │  due to escape    │ .Number  │
//	│ .Hash    │  analysis)        │ .Hash    │
//	└──────────┘                   └──────────┘
//	     ▲                              ▲
//	     │                              │
//	&block (returned)              caller's pointer
func (c *Client) GetBlock(ctx context.Context, blockNum string) (*Block, time.Duration, error) {
	resp, latency, err := c.Call(ctx, "eth_getBlockByNumber", blockNum, false)
	if err != nil {
		return nil, latency, err
	}

	// Deserialize the raw JSON result into a Block struct.
	// &block passes the address so Unmarshal can write into our variable.
	var block Block
	json.Unmarshal(resp.Result, &block)
	return &block, latency, nil
}

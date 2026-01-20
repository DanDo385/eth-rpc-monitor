// Package rpc provides HTTP client functionality for Ethereum JSON-RPC endpoints.
// It handles request/response serialization, retry logic with exponential backoff,
// and latency measurement for performance monitoring.
package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client represents an HTTP client for a specific Ethereum RPC endpoint.
// Each provider (Alchemy, Infura, etc.) gets its own Client instance with
// configured timeout and retry behavior.
type Client struct {
	name       string       // Provider name (e.g., "alchemy", "infura")
	url        string       // RPC endpoint URL
	httpClient *http.Client // HTTP client with configured timeout
	maxRetries int          // Maximum number of retry attempts (0 = no retries)
}

// NewClient creates a new RPC client for the given provider.
// The client is configured with a timeout and retry policy.
// Parameters:
//   - name: Provider identifier (for logging/display)
//   - url: Full URL of the RPC endpoint
//   - timeout: HTTP request timeout duration
//   - maxRetries: Maximum retry attempts (0 = fail immediately, 3 = retry up to 3 times)
func NewClient(name, url string, timeout time.Duration, maxRetries int) *Client {
	return &Client{
		name:       name,
		url:        url,
		maxRetries: maxRetries,
		httpClient: &http.Client{Timeout: timeout}, // HTTP client with per-request timeout
	}
}

// Name returns the provider name associated with this client.
// Used for display purposes in command output.
func (c *Client) Name() string { return c.name }

// Call executes a JSON-RPC method call with retry logic and latency measurement.
// It implements exponential backoff retry strategy to handle transient network errors.
// Returns the response, measured latency, and any error encountered.
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - method: JSON-RPC method name (e.g., "eth_blockNumber", "eth_getBlockByNumber")
//   - params: Variable arguments for method parameters
//
// Returns:
//   - *Response: Parsed JSON-RPC response (nil on error)
//   - time.Duration: Measured latency from first attempt
//   - error: Error if all retry attempts failed
//
// Retry behavior:
//   - If maxRetries is 0, fails immediately on first error
//   - Otherwise retries up to maxRetries times with exponential backoff
//   - Backoff delays: 100ms, 200ms, 400ms, 800ms, etc.
func (c *Client) Call(ctx context.Context, method string, params ...interface{}) (*Response, time.Duration, error) {
	// Normalize nil params to empty slice for JSON marshaling
	if params == nil {
		params = []interface{}{}
	}

	// Construct JSON-RPC request
	req := Request{
		JSONRPC: "2.0", // JSON-RPC 2.0 specification version
		Method:  method,
		Params:  params,
		ID:      1, // Simple request ID (not used for matching in this implementation)
	}

	// Marshal request to JSON
	body, err := json.Marshal(req)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	start := time.Now() // Start timing from first attempt

	// Retry loop: attempt up to (maxRetries + 1) times
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		resp, err := c.doRequest(ctx, body)

		if err == nil {
			// Success: return response and total latency
			latency := time.Since(start)
			return resp, latency, nil
		}

		lastErr = err

		// Exponential backoff before retry: 100ms, 200ms, 400ms, 800ms...
		// Formula: 2^attempt * 100ms
		if attempt < c.maxRetries {
			backoff := time.Duration(1<<attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				// Context cancelled, abort retries
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
				// Backoff delay completed, continue to next attempt
			}
		}
	}

	// All retry attempts exhausted
	return nil, 0, fmt.Errorf("failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

// doRequest performs a single HTTP POST request to the RPC endpoint.
// This is the low-level HTTP implementation used by Call() for each retry attempt.
// It handles HTTP request creation, response reading, and JSON-RPC error checking.
//
// Parameters:
//   - ctx: Context for request cancellation
//   - body: Pre-marshaled JSON request body
//
// Returns:
//   - *Response: Parsed JSON-RPC response
//   - error: HTTP, network, or JSON-RPC error
func (c *Client) doRequest(ctx context.Context, body []byte) (*Response, error) {
	// Create HTTP POST request with context for cancellation
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Set Content-Type header required for JSON-RPC
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute HTTP request (uses client's configured timeout)
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err // Network error or timeout
	}
	defer httpResp.Body.Close() // Ensure response body is closed

	// Check HTTP status code (JSON-RPC should return 200 OK)
	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", httpResp.StatusCode)
	}

	// Read entire response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	// Unmarshal JSON-RPC response
	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}

	// Check for JSON-RPC level errors (method not found, invalid params, etc.)
	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

// BlockNumber fetches the current block height (latest block number) from the chain.
// This is a lightweight call used for health checks and provider selection.
//
// Returns:
//   - uint64: Current block number
//   - time.Duration: Request latency
//   - error: Network or parsing error
func (c *Client) BlockNumber(ctx context.Context) (uint64, time.Duration, error) {
	resp, latency, err := c.Call(ctx, "eth_blockNumber")
	if err != nil {
		return 0, latency, err
	}

	// Unmarshal hex-encoded block number string
	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, latency, err
	}

	// Parse hex string to uint64
	num, err := ParseHexUint64(hexStr)
	return num, latency, err
}

// GetBlock fetches a full block by block number or tag.
// The blockNum parameter can be:
//   - "latest": Most recent block
//   - "pending": Pending block (may not exist)
//   - "earliest": Genesis block
//   - Hex number: "0x123" or "0x172721e"
//
// The second parameter (false) indicates we want transaction hashes only,
// not full transaction objects, which reduces response size.
//
// Returns:
//   - *Block: Parsed block data
//   - time.Duration: Request latency
//   - error: Network or parsing error
func (c *Client) GetBlock(ctx context.Context, blockNum string) (*Block, time.Duration, error) {
	// Call eth_getBlockByNumber with block number and fullTransactions=false
	resp, latency, err := c.Call(ctx, "eth_getBlockByNumber", blockNum, false)
	if err != nil {
		return nil, latency, err
	}

	// Unmarshal block data
	var block Block
	if err := json.Unmarshal(resp.Result, &block); err != nil {
		return nil, latency, err
	}

	return &block, latency, nil
}

// Warmup performs a single BlockNumber request to establish connection.
func (c *Client) Warmup(ctx context.Context) error {
	_, _, err := c.BlockNumber(ctx)
	return err
}

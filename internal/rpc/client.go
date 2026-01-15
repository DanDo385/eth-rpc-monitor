package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// Client handles JSON-RPC communication with retry and circuit breaker logic
type Client struct {
	name           string
	url            string
	httpClient     *http.Client
	timeout        time.Duration
	maxRetries     int
	backoffInitial time.Duration
	backoffMax     time.Duration

	// Circuit breaker state
	mu               sync.RWMutex
	consecutiveFails int
	circuitOpen      bool
	circuitOpenUntil time.Time
	circuitThreshold int           // failures before opening
	circuitCooldown  time.Duration // how long to wait before retry
}

// ClientConfig holds configuration for creating a new RPC client
type ClientConfig struct {
	Name           string
	URL            string
	Timeout        time.Duration
	MaxRetries     int
	BackoffInitial time.Duration
	BackoffMax     time.Duration
}

// NewClient creates a new RPC client with the given configuration
func NewClient(cfg ClientConfig) *Client {
	return &Client{
		name:             cfg.Name,
		url:              cfg.URL,
		timeout:          cfg.Timeout,
		maxRetries:       cfg.MaxRetries,
		backoffInitial:   cfg.BackoffInitial,
		backoffMax:       cfg.BackoffMax,
		circuitThreshold: 3,
		circuitCooldown:  30 * time.Second,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Name returns the provider name for this client
func (c *Client) Name() string {
	return c.name
}

// Request represents a JSON-RPC request
type Request struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

// Response represents a JSON-RPC response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// CallResult contains the result of an RPC call along with metadata
type CallResult struct {
	Provider  string
	Method    string
	Success   bool
	Latency   time.Duration
	Error     error
	ErrorType ErrorType
	Response  *Response
	Timestamp time.Time
	Retries   int
}

// ErrorType categorizes the type of error encountered
type ErrorType string

const (
	ErrorTypeNone        ErrorType = ""
	ErrorTypeTimeout     ErrorType = "timeout"
	ErrorTypeRateLimit   ErrorType = "rate_limit"
	ErrorTypeServerError ErrorType = "server_error"
	ErrorTypeParseError  ErrorType = "parse_error"
	ErrorTypeRPCError    ErrorType = "rpc_error"
	ErrorTypeCircuitOpen ErrorType = "circuit_open"
	ErrorTypeUnknown     ErrorType = "unknown"
)

// Call executes a JSON-RPC method with retry and circuit breaker logic
func (c *Client) Call(ctx context.Context, method string, params ...interface{}) *CallResult {
	result := &CallResult{
		Provider:  c.name,
		Method:    method,
		Timestamp: time.Now(),
	}

	// Check circuit breaker
	if c.isCircuitOpen() {
		result.Success = false
		result.Error = fmt.Errorf("circuit breaker open for %s", c.name)
		result.ErrorType = ErrorTypeCircuitOpen
		return result
	}

	var lastErr error
	var lastErrType ErrorType

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff with jitter
			backoff := c.calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				result.Error = ctx.Err()
				result.ErrorType = ErrorTypeTimeout
				return result
			case <-time.After(backoff):
			}
		}

		start := time.Now()
		resp, err, errType := c.doCall(ctx, method, params)
		result.Latency = time.Since(start)
		result.Retries = attempt

		if err == nil {
			result.Success = true
			result.Response = resp
			c.recordSuccess()
			return result
		}

		lastErr = err
		lastErrType = errType

		// Don't retry on certain error types
		if errType == ErrorTypeRPCError || errType == ErrorTypeParseError {
			break
		}
	}

	// All retries exhausted
	result.Success = false
	result.Error = lastErr
	result.ErrorType = lastErrType
	c.recordFailure()

	return result
}

// doCall performs a single RPC call without retry logic
func (c *Client) doCall(ctx context.Context, method string, params []interface{}) (*Response, error, ErrorType) {
	if params == nil {
		params = []interface{}{}
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err), ErrorTypeParseError
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err), ErrorTypeUnknown
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Check if it's a timeout
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("request timeout"), ErrorTypeTimeout
		}
		return nil, fmt.Errorf("request failed: %w", err), ErrorTypeTimeout
	}
	defer httpResp.Body.Close()

	// Check HTTP status code
	if httpResp.StatusCode == 429 {
		return nil, fmt.Errorf("rate limited (429)"), ErrorTypeRateLimit
	}
	if httpResp.StatusCode >= 500 {
		return nil, fmt.Errorf("server error (%d)", httpResp.StatusCode), ErrorTypeServerError
	}

	// Read and parse response
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err), ErrorTypeParseError
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err), ErrorTypeParseError
	}

	// Check for RPC-level error
	if resp.Error != nil {
		return &resp, resp.Error, ErrorTypeRPCError
	}

	return &resp, nil, ErrorTypeNone
}

// calculateBackoff returns the backoff duration with jitter for the given attempt
func (c *Client) calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: initial * 2^attempt
	backoff := c.backoffInitial * time.Duration(1<<uint(attempt))
	if backoff > c.backoffMax {
		backoff = c.backoffMax
	}

	// Add jitter: random value between 0 and backoff/2
	jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
	return backoff + jitter
}

// Circuit breaker methods

func (c *Client) isCircuitOpen() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.circuitOpen {
		return false
	}

	// Check if cooldown has passed
	if time.Now().After(c.circuitOpenUntil) {
		// Allow a probe request (half-open state)
		return false
	}

	return true
}

func (c *Client) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.consecutiveFails = 0
	c.circuitOpen = false
}

func (c *Client) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.consecutiveFails++
	if c.consecutiveFails >= c.circuitThreshold {
		c.circuitOpen = true
		c.circuitOpenUntil = time.Now().Add(c.circuitCooldown)
	}
}

// IsCircuitOpen returns whether the circuit breaker is currently open (for external inspection)
func (c *Client) IsCircuitOpen() bool {
	return c.isCircuitOpen()
}

// ConsecutiveFailures returns the current consecutive failure count (for external inspection)
func (c *Client) ConsecutiveFailures() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.consecutiveFails
}

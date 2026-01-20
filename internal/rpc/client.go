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

type Client struct {
	name       string
	url        string
	httpClient *http.Client 
	maxRetries int
}

func NewClient(name, url string, timeout time.Duration, maxRetries int) *Client {
	return &Client{
		name:       name,
		url:        url,
		maxRetries: maxRetries,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Name() string { return c.name }

// Call executes JSON-RPC with simple exponential backoff retry
func (c *Client) Call(ctx context.Context, method string, params ...interface{}) (*Response, time.Duration, error) {
	if params == nil {
		params = []interface{}{}
	}

	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	body, _ := json.Marshal(req)

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		start := time.Now()
		resp, err := c.doRequest(ctx, body)
		latency := time.Since(start)

		if err == nil {
			return resp, latency, nil
		}

		lastErr = err

		// Exponential backoff: 100ms, 200ms, 400ms...
		if attempt < c.maxRetries {
			backoff := time.Duration(1<<attempt) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return nil, 0, fmt.Errorf("failed after %d attempts: %w", c.maxRetries+1, lastErr)
}

func (c *Client) doRequest(ctx context.Context, body []byte) (*Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", httpResp.StatusCode)
	}

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	return &resp, nil
}

// BlockNumber fetches current block height
func (c *Client) BlockNumber(ctx context.Context) (uint64, time.Duration, error) {
	resp, latency, err := c.Call(ctx, "eth_blockNumber")
	if err != nil {
		return 0, latency, err
	}

	var hexStr string
	if err := json.Unmarshal(resp.Result, &hexStr); err != nil {
		return 0, latency, err
	}

	num, err := ParseHexUint64(hexStr)
	return num, latency, err
}

// GetBlock fetches block by number (hex string like "0x123" or "latest")
func (c *Client) GetBlock(ctx context.Context, blockNum string) (*Block, time.Duration, error) {
	resp, latency, err := c.Call(ctx, "eth_getBlockByNumber", blockNum, false)
	if err != nil {
		return nil, latency, err
	}

	var block Block
	if err := json.Unmarshal(resp.Result, &block); err != nil {
		return nil, latency, err
	}

	return &block, latency, nil
}

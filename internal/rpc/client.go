package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	name       string
	url        string
	httpClient *http.Client
}

func NewClient(name, url string, timeout time.Duration) *Client {
	return &Client{
		name:       name,
		url:        url,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (c *Client) Name() string { return c.name }

func (c *Client) Call(ctx context.Context, method string, params ...interface{}) (*Response, time.Duration, error) {
	if params == nil {
		params = []interface{}{}
	}

	body, _ := json.Marshal(Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	})

	start := time.Now()

	req, _ := http.NewRequestWithContext(ctx, "POST", c.url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	var rpcResp Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)

	if rpcResp.Error != nil {
		return nil, 0, fmt.Errorf("RPC error: %s", rpcResp.Error.Message)
	}

	return &rpcResp, time.Since(start), nil
}

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

func (c *Client) GetBlock(ctx context.Context, blockNum string) (*Block, time.Duration, error) {
	resp, latency, err := c.Call(ctx, "eth_getBlockByNumber", blockNum, false)
	if err != nil {
		return nil, latency, err
	}

	var block Block
	json.Unmarshal(resp.Result, &block)
	return &block, latency, nil
}

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// BlockNumber calls eth_blockNumber and returns the current block height
func (c *Client) BlockNumber(ctx context.Context) (uint64, *CallResult) {
	result := c.Call(ctx, "eth_blockNumber")
	if !result.Success {
		return 0, result
	}

	// Parse the hex string result
	var hexStr string
	if err := json.Unmarshal(result.Response.Result, &hexStr); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to parse block number: %w", err)
		result.ErrorType = ErrorTypeParseError
		return 0, result
	}

	blockNum, err := parseHexUint64(hexStr)
	if err != nil {
		result.Success = false
		result.Error = err
		result.ErrorType = ErrorTypeParseError
		return 0, result
	}

	return blockNum, result
}

// Block represents a simplified Ethereum block
type Block struct {
	Number        uint64
	Hash          string
	ParentHash    string
	Timestamp     uint64
	GasUsed       uint64
	GasLimit      uint64
	BaseFeePerGas *big.Int // nil for pre-EIP-1559 blocks
	TxCount       int
}

// GetBlockByNumber calls eth_getBlockByNumber and returns block data
// If fullTx is false, only transaction hashes are returned (lighter call)
func (c *Client) GetBlockByNumber(ctx context.Context, blockNum string, fullTx bool) (*Block, *CallResult) {
	result := c.Call(ctx, "eth_getBlockByNumber", blockNum, fullTx)
	if !result.Success {
		return nil, result
	}

	// Parse the block response
	var blockData struct {
		Number        string        `json:"number"`
		Hash          string        `json:"hash"`
		ParentHash    string        `json:"parentHash"`
		Timestamp     string        `json:"timestamp"`
		GasUsed       string        `json:"gasUsed"`
		GasLimit      string        `json:"gasLimit"`
		BaseFeePerGas string        `json:"baseFeePerGas,omitempty"`
		Transactions  []interface{} `json:"transactions"`
	}

	if err := json.Unmarshal(result.Response.Result, &blockData); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to parse block: %w", err)
		result.ErrorType = ErrorTypeParseError
		return nil, result
	}

	blockNum64, _ := parseHexUint64(blockData.Number)
	timestamp, _ := parseHexUint64(blockData.Timestamp)
	gasUsed, _ := parseHexUint64(blockData.GasUsed)
	gasLimit, _ := parseHexUint64(blockData.GasLimit)

	var baseFee *big.Int
	if blockData.BaseFeePerGas != "" {
		baseFee, _ = parseHexBigInt(blockData.BaseFeePerGas)
	}

	block := &Block{
		Number:        blockNum64,
		Hash:          blockData.Hash,
		ParentHash:    blockData.ParentHash,
		Timestamp:     timestamp,
		GasUsed:       gasUsed,
		GasLimit:      gasLimit,
		BaseFeePerGas: baseFee,
		TxCount:       len(blockData.Transactions),
	}

	return block, result
}

// GetBlockByNumberWithRaw returns block data and the raw JSON "result" payload.
func (c *Client) GetBlockByNumberWithRaw(ctx context.Context, blockNum string, fullTx bool) (*Block, json.RawMessage, *CallResult) {
	result := c.Call(ctx, "eth_getBlockByNumber", blockNum, fullTx)
	if !result.Success {
		return nil, nil, result
	}

	// Store raw "result" before parsing.
	rawResponse := result.Response.Result

	// Parse the block response.
	var blockData struct {
		Number        string        `json:"number"`
		Hash          string        `json:"hash"`
		ParentHash    string        `json:"parentHash"`
		Timestamp     string        `json:"timestamp"`
		GasUsed       string        `json:"gasUsed"`
		GasLimit      string        `json:"gasLimit"`
		BaseFeePerGas string        `json:"baseFeePerGas,omitempty"`
		Transactions  []interface{} `json:"transactions"`
	}

	if err := json.Unmarshal(result.Response.Result, &blockData); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("failed to parse block: %w", err)
		result.ErrorType = ErrorTypeParseError
		return nil, nil, result
	}

	blockNum64, _ := parseHexUint64(blockData.Number)
	timestamp, _ := parseHexUint64(blockData.Timestamp)
	gasUsed, _ := parseHexUint64(blockData.GasUsed)
	gasLimit, _ := parseHexUint64(blockData.GasLimit)

	var baseFee *big.Int
	if blockData.BaseFeePerGas != "" {
		baseFee, _ = parseHexBigInt(blockData.BaseFeePerGas)
	}

	block := &Block{
		Number:        blockNum64,
		Hash:          blockData.Hash,
		ParentHash:    blockData.ParentHash,
		Timestamp:     timestamp,
		GasUsed:       gasUsed,
		GasLimit:      gasLimit,
		BaseFeePerGas: baseFee,
		TxCount:       len(blockData.Transactions),
	}

	return block, rawResponse, result
}

// GetLatestBlock is a convenience method to get the latest block
func (c *Client) GetLatestBlock(ctx context.Context) (*Block, *CallResult) {
	return c.GetBlockByNumber(ctx, "latest", false)
}

// parseHexUint64 converts a hex string (with or without 0x prefix) to uint64
func parseHexUint64(hex string) (uint64, error) {
	hex = strings.TrimPrefix(hex, "0x")
	if hex == "" {
		return 0, nil
	}

	val := new(big.Int)
	_, ok := val.SetString(hex, 16)
	if !ok {
		return 0, fmt.Errorf("invalid hex string: %s", hex)
	}

	if !val.IsUint64() {
		return 0, fmt.Errorf("value overflows uint64: %s", hex)
	}

	return val.Uint64(), nil
}

func parseHexBigInt(hex string) (*big.Int, error) {
	hex = strings.TrimPrefix(hex, "0x")
	if hex == "" {
		return big.NewInt(0), nil
	}
	val := new(big.Int)
	_, ok := val.SetString(hex, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex string: %s", hex)
	}
	return val, nil
}

// Uint64ToHex converts a uint64 to a hex string with 0x prefix for RPC calls
func Uint64ToHex(n uint64) string {
	return fmt.Sprintf("0x%x", n)
}

// Package rpc provides Ethereum JSON-RPC client functionality and data structures.
// It handles communication with Ethereum RPC endpoints, parsing hex-encoded values,
// and formatting blockchain data for display.
package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"
)

// Request represents a JSON-RPC 2.0 request structure.
// All Ethereum RPC calls follow this format with method name and parameters.
type Request struct {
	JSONRPC string        `json:"jsonrpc"` // JSON-RPC version, always "2.0"
	Method  string        `json:"method"`  // RPC method name (e.g., "eth_blockNumber")
	Params  []interface{} `json:"params"`  // Method parameters (can be empty)
	ID      int           `json:"id"`      // Request ID for matching responses
}

// Response represents a JSON-RPC 2.0 response structure.
// The Result field is kept as RawMessage to allow flexible unmarshaling
// based on the method called.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`         // JSON-RPC version, always "2.0"
	ID      int             `json:"id"`              // Request ID matching the request
	Result  json.RawMessage `json:"result"`          // Response data (method-specific)
	Error   *RPCError       `json:"error,omitempty"` // Error object if request failed
}

// RPCError represents an error returned by the JSON-RPC endpoint.
// Errors can occur due to invalid parameters, method not found, or server issues.
type RPCError struct {
	Code    int    `json:"code"`    // Error code (standard JSON-RPC error codes)
	Message string `json:"message"` // Human-readable error message
}

// Block represents an Ethereum block as returned by the JSON-RPC API.
// All numeric fields are hex-encoded strings (e.g., "0x123") as per Ethereum JSON-RPC spec.
// This structure matches the raw API response format.
type Block struct {
	Number        string   `json:"number"`                  // Block number in hex (e.g., "0x172721e")
	Hash          string   `json:"hash"`                    // Block hash (0x-prefixed hex string)
	ParentHash    string   `json:"parentHash"`              // Parent block hash
	Timestamp     string   `json:"timestamp"`               // Unix timestamp in hex
	GasUsed       string   `json:"gasUsed"`                 // Gas used in hex
	GasLimit      string   `json:"gasLimit"`                // Gas limit in hex
	BaseFeePerGas string   `json:"baseFeePerGas,omitempty"` // Base fee per gas in hex (EIP-1559, optional)
	Transactions  []string `json:"transactions"`            // Array of transaction hashes (full tx objects not included)
}

// ParsedBlock holds human-readable, parsed values from a Block.
// All hex strings are converted to native Go types for easier manipulation and display.
type ParsedBlock struct {
	Number        uint64   // Block number as uint64
	Hash          string   // Block hash (unchanged, already readable)
	ParentHash    string   // Parent block hash (unchanged)
	Timestamp     uint64   // Unix timestamp as uint64
	GasUsed       uint64   // Gas used as uint64
	GasLimit      uint64   // Gas limit as uint64
	BaseFeePerGas *big.Int // Base fee per gas as big.Int (can be very large)
	TxCount       int      // Number of transactions in the block
}

// Parsed converts a Block with hex-encoded strings to a ParsedBlock with native types.
// This method handles all hex-to-decimal conversions and returns errors on parse failures.
func (b *Block) Parsed() (ParsedBlock, error) {
	// Parse hex-encoded numeric fields to uint64
	num, err := ParseHexUint64(b.Number)
	if err != nil {
		return ParsedBlock{}, fmt.Errorf("parse block number: %w", err)
	}
	ts, err := ParseHexUint64(b.Timestamp)
	if err != nil {
		return ParsedBlock{}, fmt.Errorf("parse timestamp: %w", err)
	}
	gasUsed, err := ParseHexUint64(b.GasUsed)
	if err != nil {
		return ParsedBlock{}, fmt.Errorf("parse gas used: %w", err)
	}
	gasLimit, err := ParseHexUint64(b.GasLimit)
	if err != nil {
		return ParsedBlock{}, fmt.Errorf("parse gas limit: %w", err)
	}

	// BaseFeePerGas is optional (only present in post-EIP-1559 blocks)
	// Use big.Int to handle potentially very large values
	var baseFee *big.Int
	if b.BaseFeePerGas != "" {
		baseFee, err = ParseHexBigInt(b.BaseFeePerGas)
		if err != nil {
			return ParsedBlock{}, fmt.Errorf("parse base fee per gas: %w", err)
		}
	}

	return ParsedBlock{
		Number:        num,
		Hash:          b.Hash,              // Hash strings remain unchanged
		ParentHash:    b.ParentHash,        // Parent hash remains unchanged
		Timestamp:     ts,                  // Converted to Unix timestamp
		GasUsed:       gasUsed,             // Converted to uint64
		GasLimit:      gasLimit,            // Converted to uint64
		BaseFeePerGas: baseFee,             // Converted to big.Int (nil if not present)
		TxCount:       len(b.Transactions), // Count transactions
	}, nil
}

package rpc

import (
	"encoding/json"
	"math/big"
)

type Request struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
	ID      int           `json:"id"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Block is the core data structure - keep it simple
type Block struct {
	Number        string   `json:"number"`
	Hash          string   `json:"hash"`
	ParentHash    string   `json:"parentHash"`
	Timestamp     string   `json:"timestamp"`
	GasUsed       string   `json:"gasUsed"`
	GasLimit      string   `json:"gasLimit"`
	BaseFeePerGas string   `json:"baseFeePerGas,omitempty"`
	Transactions  []string `json:"transactions"`
}

// ParsedBlock holds human-readable values
type ParsedBlock struct {
	Number        uint64
	Hash          string
	ParentHash    string
	Timestamp     uint64
	GasUsed       uint64
	GasLimit      uint64
	BaseFeePerGas *big.Int
	TxCount       int
}

func (b *Block) Parsed() ParsedBlock {
	num, _ := ParseHexUint64(b.Number)
	ts, _ := ParseHexUint64(b.Timestamp)
	gasUsed, _ := ParseHexUint64(b.GasUsed)
	gasLimit, _ := ParseHexUint64(b.GasLimit)

	var baseFee *big.Int
	if b.BaseFeePerGas != "" {
		baseFee, _ = ParseHexBigInt(b.BaseFeePerGas)
	}

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

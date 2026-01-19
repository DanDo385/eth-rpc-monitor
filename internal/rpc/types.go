package rpc

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"
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

// ParseHexUint64 converts hex string to uint64
func ParseHexUint64(hex string) (uint64, error) {
	hex = strings.TrimPrefix(hex, "0x")
	if hex == "" {
		return 0, nil
	}
	val := new(big.Int)
	_, ok := val.SetString(hex, 16)
	if !ok || !val.IsUint64() {
		return 0, fmt.Errorf("invalid hex: %s", hex)
	}
	return val.Uint64(), nil
}

// ParseHexBigInt converts hex string to big.Int
func ParseHexBigInt(hex string) (*big.Int, error) {
	hex = strings.TrimPrefix(hex, "0x")
	if hex == "" {
		return big.NewInt(0), nil
	}
	val := new(big.Int)
	_, ok := val.SetString(hex, 16)
	if !ok {
		return nil, fmt.Errorf("invalid hex: %s", hex)
	}
	return val, nil
}

// FormatTimestamp returns human-readable time with "ago" suffix
func FormatTimestamp(ts uint64) string {
	t := time.Unix(int64(ts), 0)
	ago := time.Since(t)

	var agoStr string
	switch {
	case ago < time.Minute:
		agoStr = fmt.Sprintf("%ds ago", int(ago.Seconds()))
	case ago < time.Hour:
		agoStr = fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		agoStr = fmt.Sprintf("%dh ago", int(ago.Hours()))
	default:
		agoStr = fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	}

	return fmt.Sprintf("%s (%s)", t.UTC().Format("2006-01-02 15:04:05 UTC"), agoStr)
}

// FormatNumber adds thousand separators
func FormatNumber(n uint64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// FormatGwei converts wei to gwei for display
func FormatGwei(wei *big.Int) string {
	if wei == nil {
		return "—"
	}
	gwei := new(big.Float).Quo(
		new(big.Float).SetInt(wei),
		big.NewFloat(1e9),
	)
	f, _ := gwei.Float64()
	return fmt.Sprintf("%.2f gwei", f)
}

package rpc

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/sha3"
)

// Token constants for mainnet
const (
	USDCAddress  = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	USDTAddress  = "0xdAC17F958D2ee523a2206206994597C13D831ec7"
	USDCDecimals = 6
	USDTDecimals = 6
)

// FunctionSelector computes the 4-byte function selector from a signature
// e.g., "balanceOf(address)" -> 0x70a08231
func FunctionSelector(signature string) []byte {
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write([]byte(signature))
	return hasher.Sum(nil)[:4]
}

// EncodeAddress pads an Ethereum address to 32 bytes (left-padded with zeros)
func EncodeAddress(addr string) ([]byte, error) {
	addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
	if len(addr) != 40 {
		return nil, fmt.Errorf("invalid address length: expected 40 hex chars, got %d", len(addr))
	}

	addrBytes, err := hex.DecodeString(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address hex: %w", err)
	}

	// Left-pad to 32 bytes (address is 20 bytes, goes in last 20 bytes)
	padded := make([]byte, 32)
	copy(padded[12:], addrBytes)
	return padded, nil
}

// EncodeBalanceOfCalldata creates the calldata for balanceOf(address)
func EncodeBalanceOfCalldata(address string) (string, error) {
	selector := FunctionSelector("balanceOf(address)")
	
	addrEncoded, err := EncodeAddress(address)
	if err != nil {
		return "", fmt.Errorf("failed to encode address: %w", err)
	}

	calldata := append(selector, addrEncoded...)
	return "0x" + hex.EncodeToString(calldata), nil
}

// DecodeUint256 parses a hex string result into a big.Int
func DecodeUint256(hexResult string) (*big.Int, error) {
	hexResult = strings.TrimPrefix(hexResult, "0x")
	if hexResult == "" {
		return big.NewInt(0), nil
	}

	// Remove leading zeros for parsing but handle all-zero case
	hexResult = strings.TrimLeft(hexResult, "0")
	if hexResult == "" {
		return big.NewInt(0), nil
	}

	result := new(big.Int)
	_, ok := result.SetString(hexResult, 16)
	if !ok {
		return nil, fmt.Errorf("failed to parse hex result: %s", hexResult)
	}
	return result, nil
}

// FormatTokenAmount formats a raw token amount with decimals
func FormatTokenAmount(raw *big.Int, decimals int, symbol string) string {
	if raw == nil || raw.Sign() == 0 {
		return fmt.Sprintf("0.%s %s", strings.Repeat("0", decimals), symbol)
	}

	rawStr := raw.String()
	
	// Pad with leading zeros if necessary
	for len(rawStr) <= decimals {
		rawStr = "0" + rawStr
	}

	// Insert decimal point
	insertPos := len(rawStr) - decimals
	wholePart := rawStr[:insertPos]
	decimalPart := rawStr[insertPos:]

	// Add thousand separators to whole part
	wholePart = addThousandSeparators(wholePart)

	return fmt.Sprintf("%s.%s %s", wholePart, decimalPart, symbol)
}

func addThousandSeparators(s string) string {
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

// ValidateAddress checks if a string is a valid Ethereum address
func ValidateAddress(addr string) error {
	addr = strings.TrimPrefix(addr, "0x")
	if len(addr) != 40 {
		return fmt.Errorf("invalid address length: expected 40 hex chars (with or without 0x prefix)")
	}
	_, err := hex.DecodeString(addr)
	if err != nil {
		return fmt.Errorf("invalid address: contains non-hex characters")
	}
	return nil
}

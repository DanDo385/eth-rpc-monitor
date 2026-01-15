package rpc

import (
	"bytes"
	"math/big"
	"testing"
)

func TestFunctionSelector(t *testing.T) {
	selector := FunctionSelector("balanceOf(address)")
	expected := []byte{0x70, 0xa0, 0x82, 0x31}

	if !bytes.Equal(selector, expected) {
		t.Errorf("balanceOf selector: got %x, want %x", selector, expected)
	}
}

func TestFunctionSelectorTransfer(t *testing.T) {
	// Test another common function for validation
	selector := FunctionSelector("transfer(address,uint256)")
	expected := []byte{0xa9, 0x05, 0x9c, 0xbb}

	if !bytes.Equal(selector, expected) {
		t.Errorf("transfer selector: got %x, want %x", selector, expected)
	}
}

func TestEncodeAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"valid with 0x", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", false},
		{"valid without 0x", "d8dA6BF26964aF9D7eEd9e03E53415D37aA96045", false},
		{"too short", "0xd8dA6BF269", true},
		{"invalid hex", "0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EncodeAddress(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodeAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(result) != 32 {
				t.Errorf("EncodeAddress() result length = %d, want 32", len(result))
			}
		})
	}
}

func TestEncodeAddressCorrectPadding(t *testing.T) {
	// Test that the address is correctly right-aligned in the 32-byte output
	result, err := EncodeAddress("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First 12 bytes should be zeros
	for i := 0; i < 12; i++ {
		if result[i] != 0 {
			t.Errorf("expected zero at position %d, got %x", i, result[i])
		}
	}

	// Last 20 bytes should contain the address
	if result[12] != 0xd8 {
		t.Errorf("expected first address byte 0xd8, got %x", result[12])
	}
}

func TestEncodeBalanceOfCalldata(t *testing.T) {
	calldata, err := EncodeBalanceOfCalldata("0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should start with balanceOf selector
	if calldata[:10] != "0x70a08231" {
		t.Errorf("calldata prefix: got %s, want 0x70a08231", calldata[:10])
	}

	// Should be 4 bytes selector + 32 bytes address = 72 hex chars + 0x prefix
	if len(calldata) != 74 {
		t.Errorf("calldata length: got %d, want 74", len(calldata))
	}
}

func TestDecodeUint256(t *testing.T) {
	tests := []struct {
		name    string
		hex     string
		want    string
		wantErr bool
	}{
		{"zero", "0x0", "0", false},
		{"small", "0x64", "100", false},
		{"large", "0x0000000000000000000000000000000000000000000000000000000005f5e100", "100000000", false},
		{"all zeros", "0x0000000000000000000000000000000000000000000000000000000000000000", "0", false},
		{"empty with prefix", "0x", "0", false},
		{"max uint64 equivalent", "0xffffffffffffffff", "18446744073709551615", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := DecodeUint256(tt.hex)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeUint256() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result.String() != tt.want {
				t.Errorf("DecodeUint256() = %s, want %s", result.String(), tt.want)
			}
		})
	}
}

func TestFormatTokenAmount(t *testing.T) {
	tests := []struct {
		name     string
		raw      *big.Int
		decimals int
		symbol   string
		want     string
	}{
		{"zero", big.NewInt(0), 6, "USDC", "0.000000 USDC"},
		{"nil", nil, 6, "USDC", "0.000000 USDC"},
		{"one dollar", big.NewInt(1000000), 6, "USDC", "1.000000 USDC"},
		{"with cents", big.NewInt(1234567), 6, "USDC", "1.234567 USDC"},
		{"large", big.NewInt(1234567890123), 6, "USDC", "1,234,567.890123 USDC"},
		{"sub-dollar", big.NewInt(123456), 6, "USDC", "0.123456 USDC"},
		{"very small", big.NewInt(1), 6, "USDC", "0.000001 USDC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatTokenAmount(tt.raw, tt.decimals, tt.symbol)
			if got != tt.want {
				t.Errorf("FormatTokenAmount() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"valid with 0x", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045", false},
		{"valid without 0x", "d8dA6BF26964aF9D7eEd9e03E53415D37aA96045", false},
		{"too short", "0xd8dA6BF269", true},
		{"too long", "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045aa", true},
		{"invalid hex", "0xZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAddress(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAddress() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddThousandSeparators(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1", "1"},
		{"12", "12"},
		{"123", "123"},
		{"1234", "1,234"},
		{"12345", "12,345"},
		{"123456", "123,456"},
		{"1234567", "1,234,567"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := addThousandSeparators(tt.input)
			if got != tt.want {
				t.Errorf("addThousandSeparators(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

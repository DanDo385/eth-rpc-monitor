package rpc

import (
	"math/big"
	"testing"
)

func TestParseHexUint64_valid(t *testing.T) {
	n, err := ParseHexUint64("0x10")
	if err != nil || n != 16 {
		t.Fatalf("got %d err=%v", n, err)
	}
}

func TestParseHexUint64_invalid(t *testing.T) {
	_, err := ParseHexUint64("0xzz")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseHexBigInt(t *testing.T) {
	v := ParseHexBigInt("0xde0b6b3a7640000") // 1e18 wei
	want, _ := new(big.Int).SetString("1000000000000000000", 10)
	if v == nil || v.Cmp(want) != 0 {
		t.Fatalf("unexpected %v want %v", v, want)
	}
}

func TestFormatNumber(t *testing.T) {
	if got := FormatNumber(21234567); got != "21,234,567" {
		t.Fatalf("got %q", got)
	}
	if got := FormatNumber(12); got != "12" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatGwei_nil(t *testing.T) {
	if got := FormatGwei(nil); got != "—" {
		t.Fatalf("got %q", got)
	}
}

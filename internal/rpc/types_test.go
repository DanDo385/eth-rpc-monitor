package rpc

import (
	"testing"
)

func TestBlock_Parsed(t *testing.T) {
	b := &Block{
		Number:        "0xa",
		Hash:          "0xhash",
		ParentHash:    "0xparent",
		Timestamp:     "0x1",
		GasUsed:       "0x2",
		GasLimit:      "0x3",
		BaseFeePerGas: "",
		Transactions:  []string{"0x1", "0x2"},
	}
	p := b.Parsed()
	if p.Number != 10 || p.TxCount != 2 || p.Timestamp != 1 {
		t.Fatalf("parsed: %+v", p)
	}
	if p.BaseFeePerGas != nil {
		t.Fatal("expected nil base fee")
	}
}

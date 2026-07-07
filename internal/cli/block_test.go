package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// TestNormalizeBlockArg covers every branch of the block-argument normalizer:
// empty/whitespace → "latest", special tags pass through (case-insensitively),
// already-hex pass through, decimal → hex conversion, and invalid input
// returned as-is so the RPC server produces the error.
func TestNormalizeBlockArg(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "latest"},
		{"  ", "latest"},
		{"latest", "latest"},
		{"LATEST", "latest"},
		{"pending", "pending"},
		{"earliest", "earliest"},
		{"19000000", "0x121eac0"},
		{"0xabc", "0xabc"},
		{"not-a-number", "not-a-number"},
	}
	for _, tc := range tests {
		if got := NormalizeBlockArg(tc.in); got != tc.want {
			t.Fatalf("NormalizeBlockArg(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

// TestConvertBlockToJSON verifies hex→decimal conversion, ISO 8601 timestamp
// formatting, and the *float64 base-fee handling for both a London-era block
// (base fee present) and a pre-London block (base fee absent → nil → omitted).
func TestConvertBlockToJSON(t *testing.T) {
	// Block at height 16, timestamp 0x6553b100 (1699983616 → 2023-11-14T17:40:16Z),
	// base fee 1e9 wei (1 gwei).
	block := &rpc.Block{
		Number:        "0x10",
		Hash:          "0xabc",
		ParentHash:    "0xdef",
		Timestamp:     "0x6553b100",
		GasUsed:       "0x64",
		GasLimit:      "0xc8",
		BaseFeePerGas: "0x3b9aca00",
		Transactions:  []string{"0xtx1"},
	}
	got := ConvertBlockToJSON(block)
	if got.Number != 16 {
		t.Fatalf("Number = %d want 16", got.Number)
	}
	if got.GasUsed != 100 || got.GasLimit != 200 {
		t.Fatalf("GasUsed=%d GasLimit=%d want 100/200", got.GasUsed, got.GasLimit)
	}
	if got.Timestamp != "2023-11-14T17:40:16Z" {
		t.Fatalf("Timestamp = %q want ISO 8601", got.Timestamp)
	}
	if got.BaseFeePerGas == nil || *got.BaseFeePerGas != 1.0 {
		t.Fatalf("BaseFeePerGas = %v want 1.0 gwei", got.BaseFeePerGas)
	}
	if len(got.Transactions) != 1 || got.Transactions[0] != "0xtx1" {
		t.Fatalf("Transactions = %v", got.Transactions)
	}

	// Pre-London block: no base fee → pointer stays nil (omitted in JSON).
	preLondon := &rpc.Block{Number: "0x1", Timestamp: "0x0", GasUsed: "0x0", GasLimit: "0x0"}
	if got := ConvertBlockToJSON(preLondon); got.BaseFeePerGas != nil {
		t.Fatalf("BaseFeePerGas = %v want nil for pre-London block", got.BaseFeePerGas)
	}
}

// rpcServer returns an httptest.Server that responds to every JSON-RPC request
// with the given hex block number, then closes. Used to drive SelectFastestProvider
// without hitting real providers.
func rpcServer(hexHeight string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + hexHeight + `"}`))
	}))
}

// TestSelectFastestProvider builds two providers on the same (highest) block
// and a third on a stale block, then asserts the selection picks one of the
// leaders — specifically the lower-latency one (the "fast" server, which has
// no artificial delay, vs the "slow" server that sleeps before responding).
func TestSelectFastestProvider(t *testing.T) {
	fast := rpcServer("0x64")  // height 100, no delay
	stale := rpcServer("0x32") // height 50 — must never be selected
	defer fast.Close()
	defer stale.Close()

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x64"}`))
	}))
	defer slow.Close()

	cfg := &config.Config{
		Defaults: config.Defaults{Timeout: 2 * time.Second},
		Providers: []config.Provider{
			{Name: "fast", URL: fast.URL, Timeout: 2 * time.Second},
			{Name: "slow", URL: slow.URL, Timeout: 2 * time.Second},
			{Name: "stale", URL: stale.URL, Timeout: 2 * time.Second},
		},
	}

	client, err := SelectFastestProvider(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SelectFastestProvider error: %v", err)
	}
	if client == nil {
		t.Fatal("SelectFastestProvider returned nil client")
	}
	if client.Name() == "stale" {
		t.Fatalf("selected stale provider; name=%s", client.Name())
	}
	if client.Name() != "fast" {
		t.Fatalf("expected fast provider, got %s", client.Name())
	}
}

// TestSelectFastestProvider_noResponders asserts the error path when every
// provider points at a dead URL.
func TestSelectFastestProvider_noResponders(t *testing.T) {
	cfg := &config.Config{
		Defaults: config.Defaults{Timeout: 100 * time.Millisecond},
		Providers: []config.Provider{
			{Name: "dead", URL: "http://127.0.0.1:1", Timeout: 100 * time.Millisecond},
		},
	}
	if _, err := SelectFastestProvider(context.Background(), cfg); err == nil {
		t.Fatal("expected error when no providers respond, got nil")
	}
}

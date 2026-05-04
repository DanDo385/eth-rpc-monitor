package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_BlockNumber_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x10"}`))
	}))
	defer srv.Close()

	c := NewClient("t", srv.URL, 2*time.Second)
	n, _, err := c.BlockNumber(context.Background())
	if err != nil || n != 16 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestClient_Call_rpcError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad"}}`))
	}))
	defer srv.Close()

	c := NewClient("t", srv.URL, 2*time.Second)
	_, _, err := c.Call(context.Background(), "eth_blockNumber")
	if err == nil || !strings.Contains(err.Error(), "bad") {
		t.Fatalf("expected rpc error, got %v", err)
	}
}

func TestClient_Call_httpNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`upstream`))
	}))
	defer srv.Close()

	c := NewClient("t", srv.URL, 2*time.Second)
	_, _, err := c.Call(context.Background(), "eth_blockNumber")
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("expected http error, got %v", err)
	}
}

func TestClient_Call_invalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := NewClient("t", srv.URL, 2*time.Second)
	_, _, err := c.Call(context.Background(), "eth_blockNumber")
	if err == nil || !strings.Contains(err.Error(), "decode rpc response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestClient_BlockNumber_badHex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0xzz"}`))
	}))
	defer srv.Close()

	c := NewClient("t", srv.URL, 2*time.Second)
	_, _, err := c.BlockNumber(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse blockNumber") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

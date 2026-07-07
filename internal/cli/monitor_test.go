package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dando385/eth-rpc-monitor/internal/config"
)

// TestFetchAllProviders verifies that one polling cycle collects a result per
// provider, populates block heights from the RPC responses, and marks a dead
// provider with an error (and zero height) instead of crashing.
func TestFetchAllProviders(t *testing.T) {
	ok1 := rpcServer("0x64") // height 100
	ok2 := rpcServer("0xc8") // height 200
	defer ok1.Close()
	defer ok2.Close()

	cfg := &config.Config{
		Defaults: config.Defaults{Timeout: 2 * time.Second},
		Providers: []config.Provider{
			{Name: "ok1", URL: ok1.URL, Timeout: 2 * time.Second},
			{Name: "ok2", URL: ok2.URL, Timeout: 2 * time.Second},
			{Name: "dead", URL: "http://127.0.0.1:1", Timeout: 100 * time.Millisecond},
		},
	}

	results := FetchAllProviders(context.Background(), cfg)
	if len(results) != len(cfg.Providers) {
		t.Fatalf("got %d results want %d", len(results), len(cfg.Providers))
	}

	byName := map[string]struct {
		height uint64
		err    bool
	}{}
	for _, r := range results {
		byName[r.Provider] = struct {
			height uint64
			err    bool
		}{r.BlockHeight, r.Error != nil}
	}

	if r, ok := byName["ok1"]; !ok || r.height != 100 || r.err {
		t.Fatalf("ok1 result = %+v", r)
	}
	if r, ok := byName["ok2"]; !ok || r.height != 200 || r.err {
		t.Fatalf("ok2 result = %+v", r)
	}
	if r, ok := byName["dead"]; !ok || r.height != 0 || !r.err {
		t.Fatalf("dead provider should report error with zero height, got %+v", r)
	}
}

// TestFetchAllProviders_cancelled verifies that a cancelled context aborts the
// cycle promptly rather than blocking on the full per-provider timeout.
func TestFetchAllProviders_cancelled(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request's context is cancelled (i.e. never writes).
		<-r.Context().Done()
	}))
	defer hang.Close()

	cfg := &config.Config{
		Defaults: config.Defaults{Timeout: 5 * time.Second},
		Providers: []config.Provider{
			{Name: "hang", URL: hang.URL, Timeout: 5 * time.Second},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		FetchAllProviders(ctx, cfg)
		close(done)
	}()
	cancel()

	select {
	case <-done:
		// Cycle returned after cancellation — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("FetchAllProviders did not return after context cancellation")
	}
}

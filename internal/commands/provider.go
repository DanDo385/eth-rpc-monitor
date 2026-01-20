package commands

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
	"github.com/dando385/eth-rpc-monitor/internal/rpc"
)

// ExecutorFunc is the function signature for operations executed against a provider.
// It receives a context, RPC client, and provider configuration, and returns a result of type T.
//
// The function should NOT return errors via the error return value for individual provider
// failures. Instead, errors should be tracked within the result type T itself. This ensures
// that failures from one provider don't affect others and all results are collected.
type ExecutorFunc[T any] func(ctx context.Context, client *rpc.Client, provider config.Provider) T

// ExecuteAll runs the given function concurrently against all configured providers.
// It preserves provider order in the results slice and does NOT fail fast - all provider
// operations complete regardless of individual failures.
//
// This function abstracts the common errgroup + mutex pattern used across commands,
// providing a clean, type-safe interface for concurrent provider operations.
//
// Parameters:
//   - ctx: Context for cancellation (passed to each executor function)
//   - cfg: Configuration containing provider list and defaults
//   - pool: Client pool for reusing RPC clients (pass nil to create clients without pooling)
//   - fn: Function to execute for each provider
//
// Returns:
//   - []T: Results in the same order as cfg.Providers
//
// Example:
//
//	results := ExecuteAll(ctx, cfg, pool, func(ctx context.Context, client *rpc.Client, p config.Provider) WatchResult {
//	    height, latency, err := client.BlockNumber(ctx)
//	    return WatchResult{Provider: p.Name, BlockHeight: height, Latency: latency, Error: err}
//	})
func ExecuteAll[T any](ctx context.Context, cfg *config.Config, pool *rpc.ClientPool, fn ExecutorFunc[T]) []T {
	// Pre-allocate results slice to preserve provider order
	results := make([]T, len(cfg.Providers))
	var mu sync.Mutex

	// Use errgroup for concurrent execution with context cancellation support
	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p // Capture loop variables for goroutine

		g.Go(func() error {
			// Get or create client
			var client *rpc.Client
			if pool != nil {
				client = pool.GetOrCreate(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			} else {
				client = rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			}

			// Execute the provided function
			result := fn(gctx, client, p)

			// Thread-safely store result at the correct index
			mu.Lock()
			results[i] = result
			mu.Unlock()

			// Always return nil - we don't fail fast, errors are tracked in results
			return nil
		})
	}

	// Wait for all goroutines to complete
	// Errors are intentionally ignored as they're tracked in individual results
	_ = g.Wait()

	return results
}

// ExecuteAllWithClients is a variant of ExecuteAll that returns a parallel slice
// of clients alongside the results. This is useful when the caller needs to
// retain references to the clients used (e.g., for selectFastestProvider).
//
// Parameters:
//   - ctx: Context for cancellation
//   - cfg: Configuration containing provider list
//   - pool: Client pool for reusing RPC clients (pass nil to create clients without pooling)
//   - fn: Function to execute for each provider, receives and can store the client
//
// Returns:
//   - []T: Results in the same order as cfg.Providers
//   - []*rpc.Client: Clients in the same order as cfg.Providers
func ExecuteAllWithClients[T any](ctx context.Context, cfg *config.Config, pool *rpc.ClientPool, fn ExecutorFunc[T]) ([]T, []*rpc.Client) {
	results := make([]T, len(cfg.Providers))
	clients := make([]*rpc.Client, len(cfg.Providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)

	for i, p := range cfg.Providers {
		i, p := i, p

		g.Go(func() error {
			var client *rpc.Client
			if pool != nil {
				client = pool.GetOrCreate(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			} else {
				client = rpc.NewClient(p.Name, p.URL, p.Timeout, cfg.Defaults.MaxRetries)
			}

			result := fn(gctx, client, p)

			mu.Lock()
			results[i] = result
			clients[i] = client
			mu.Unlock()

			return nil
		})
	}

	_ = g.Wait()

	return results, clients
}

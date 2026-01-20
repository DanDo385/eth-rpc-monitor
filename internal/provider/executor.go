// Package provider contains concurrency helpers for running operations across providers.
//
// Commands frequently need to fan out the same RPC call across all configured providers,
// collect per-provider results, and continue even if some providers fail. This package
// centralizes that pattern to reduce duplication and improve testability.
package provider

import (
	"context"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/dando385/eth-rpc-monitor/internal/config"
)

// Result wraps a provider response with metadata.
type Result[T any] struct {
	ProviderName string
	Index        int
	Value        T
	Err          error
}

// ExecuteAll runs fn concurrently for each provider and collects results.
// Results are returned in provider order (by index), not completion order.
//
// Notes:
//   - This helper does not fail-fast; it always attempts all providers and records
//     per-provider errors in the corresponding Result.
//   - Context cancellation still short-circuits work inside fn via gctx.
func ExecuteAll[T any](
	ctx context.Context,
	providers []config.Provider,
	fn func(ctx context.Context, p config.Provider) (T, error),
) []Result[T] {
	results := make([]Result[T], len(providers))
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	for i, p := range providers {
		i, p := i, p // capture loop vars
		g.Go(func() error {
			val, err := fn(gctx, p)
			mu.Lock()
			results[i] = Result[T]{
				ProviderName: p.Name,
				Index:        i,
				Value:        val,
				Err:          err,
			}
			mu.Unlock()
			return nil // don't fail-fast; collect all results
		})
	}

	_ = g.Wait()
	return results
}

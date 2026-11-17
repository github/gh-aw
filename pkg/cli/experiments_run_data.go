package cli

import (
	"context"
	"sync"

	"github.com/sourcegraph/conc/pool"
)

// loadRunsConcurrently loads one value per run ID with bounded concurrency. The context is passed
// to load for cancellation; tasks never return errors, so it does not trigger error-driven cancellation.
func loadRunsConcurrently[T any](ctx context.Context, runIDs map[string]struct{}, load func(context.Context, string) T) map[string]T {
	result := make(map[string]T, len(runIDs))
	var mu sync.Mutex
	p := pool.New().WithContext(ctx).WithMaxGoroutines(getMaxConcurrentDownloads())
	for runID := range runIDs {
		p.Go(func(ctx context.Context) error {
			data := load(ctx, runID)
			mu.Lock()
			result[runID] = data
			mu.Unlock()
			return nil
		})
	}
	// Tasks always return nil, so Wait cannot fail.
	_ = p.Wait()
	return result
}

package cli

import (
	"context"

	"golang.org/x/sync/errgroup"
)

const updateNetworkConcurrency = 8

// Only independent reads run concurrently; callers apply results and write files afterward.
func runUpdateNetworkJobs(ctx context.Context, jobs []func()) error {
	var group errgroup.Group
	group.SetLimit(updateNetworkConcurrency)
	for _, job := range jobs {
		group.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			job()
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	return ctx.Err()
}

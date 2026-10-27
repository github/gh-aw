//go:build !integration

package cli

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunUpdateNetworkJobsBoundsConcurrency(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2*updateNetworkConcurrency)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var active atomic.Int32
	var completed atomic.Int32
	jobs := make([]func(), 2*updateNetworkConcurrency)
	for i := range jobs {
		jobs[i] = func() {
			if n := active.Add(1); n > updateNetworkConcurrency {
				t.Errorf("concurrent requests = %d, limit = %d", n, updateNetworkConcurrency)
			}
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			active.Add(-1)
			completed.Add(1)
		}
	}
	done := make(chan error, 1)
	go func() { done <- runUpdateNetworkJobs(ctx, jobs) }()
	for range updateNetworkConcurrency {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("independent requests did not start concurrently")
		}
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := completed.Load(); got != int32(len(jobs)) {
		t.Fatalf("completed requests = %d, want %d", got, len(jobs))
	}
}

func TestRunUpdateNetworkJobsHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	jobs := []func(){func() { t.Error("request started after cancellation") }}
	if err := runUpdateNetworkJobs(ctx, jobs); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestCachedActionUpdateDepsCoalescesConcurrentNestedLookups(t *testing.T) {
	t.Parallel()
	var releases, tags atomic.Int32
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	deps := newCachedActionUpdateDeps(actionUpdateDeps{
		runGHReleasesAPI: func(_ context.Context, repo string) ([]byte, error) {
			if repo != "actions/cache" {
				t.Errorf("unexpected release repository: %s", repo)
			}
			releases.Add(1)
			return []byte("v4.2.0\nv4.1.0"), nil
		},
		getActionSHAForTag: func(_ context.Context, repo, tag string) (string, error) {
			if repo != "actions/cache" || tag != "v4.2.0" {
				t.Errorf("unexpected tag lookup: %s@%s", repo, tag)
			}
			tags.Add(1)
			return sha, nil
		},
	})
	var jobs []func()
	for _, action := range []struct{ repo, version string }{
		{"actions/cache", "v4"},
		{"actions/cache/restore", "v4.1.0"},
		{"actions/cache/save", "v4"},
		{"actions/cache", "v4.0.0"},
	} {
		jobs = append(jobs, func() {
			version, gotSHA, err := deps.getLatestRelease(context.Background(), action.repo, action.version, true, false)
			if err != nil || version != "v4.2.0" || gotSHA != sha {
				t.Errorf("unexpected resolution: %s, %s, %v", version, gotSHA, err)
			}
		})
	}
	if err := runUpdateNetworkJobs(context.Background(), jobs); err != nil {
		t.Fatal(err)
	}
	if releases.Load() != 1 || tags.Load() != 1 {
		t.Fatalf("requests = releases:%d tags:%d, want one each", releases.Load(), tags.Load())
	}
}

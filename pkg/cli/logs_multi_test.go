//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schedulerWaitingCount reports how many targets are currently parked waiting
// for a batch turn on scheduler. It exists so tests can observe that a target
// has actually blocked rather than inferring it from timing.
func schedulerWaitingCount(s *logsBatchScheduler) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.waiting
}

func TestLogsTargetOutputDir(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		filepath.Join("logs", "repo-owner-repo", "workflow-daily-report"),
		logsTargetOutputDir("logs", logsWorkflowTarget{
			repoOverride: "owner/repo",
			workflowName: "daily-report",
		}),
	)
}

func TestDownloadWorkflowLogsForTargetsConcurrentAndResilient(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "10")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	outputDirs := make(map[string]string)
	concurrencyLimits := make(map[string]int)
	cachePointers := make(map[string]*cachedLogsJSONLCache)
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		mu.Lock()
		outputDirs[opts.WorkflowName] = opts.OutputDir
		concurrencyLimits[opts.WorkflowName] = opts.maxConcurrentDownloads
		cachePointers[opts.WorkflowName] = opts.cachedJSONLCache
		mu.Unlock()
		started <- struct{}{}
		<-release
		if opts.WorkflowName == "missing" {
			return workflowLogsResult{}, errors.New("workflow not found")
		}
		return workflowLogsResult{
			processedRuns: []ProcessedRun{{
				Run: WorkflowRun{
					DatabaseID:   int64(len(opts.WorkflowName)),
					WorkflowName: opts.WorkflowName,
					CreatedAt:    time.Now(),
					LogsPath:     filepath.Join(opts.OutputDir, "run-1"),
				},
			}},
			artifactFilter: []string{"usage"},
			continuation: &ContinuationData{
				WorkflowName: opts.WorkflowName,
				BeforeRunID:  123,
			},
		}, nil
	}

	cachedJSONL := filepath.Join(tempDir, "logs.jsonl")
	require.NoError(t, os.WriteFile(cachedJSONL, []byte("{\"schema_version\":2,\"kind\":\"run\",\"run\":{\"run_id\":42}}\n"), 0o600))
	done := make(chan error, 1)
	go func() {
		done <- DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
			OutputDir:      filepath.Join(tempDir, "logs"),
			SummaryFile:    "summary.json",
			ArtifactSets:   []string{"usage"},
			SuppressRender: true,
			CachedJSONL:    cachedJSONL,
		}, []logsWorkflowTarget{
			{workflowName: "available", repoOverride: "org/repo-a"},
			{workflowName: "missing", repoOverride: "org/repo-b"},
		}, []error{errors.New("invalid-local: workflow not found")})
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("workflow collectors did not start concurrently")
		}
	}
	close(release)
	require.NoError(t, <-done, "a failed target should not discard successful reports")

	mu.Lock()
	assert.Equal(t, filepath.Join(tempDir, "logs", "repo-org-repo-a", "workflow-available"), outputDirs["available"])
	assert.Equal(t, filepath.Join(tempDir, "logs", "repo-org-repo-b", "workflow-missing"), outputDirs["missing"])
	assert.Equal(t, 5, concurrencyLimits["available"], "total download concurrency should be shared across targets")
	assert.Equal(t, 5, concurrencyLimits["missing"], "total download concurrency should be shared across targets")
	require.NotNil(t, cachePointers["available"])
	assert.Same(t, cachePointers["available"], cachePointers["missing"], "targets should share one in-memory JSONL cache")
	mu.Unlock()

	data, err := os.ReadFile(filepath.Join(tempDir, "logs", "summary.json"))
	require.NoError(t, err)
	var report LogsData
	require.NoError(t, json.Unmarshal(data, &report))
	require.Len(t, report.Runs, 1)
	assert.Equal(t, "available", report.Runs[0].WorkflowName)
	require.Len(t, report.Continuations, 1)
	assert.Equal(t, "org/repo-a", report.Continuations[0].Repository)
	assert.Equal(t, int64(123), report.Continuations[0].BeforeRunID)
}

// TestDownloadWorkflowLogsForTargetsReportsCollectionStatsAcrossTargets verifies
// that DownloadWorkflowLogsForTargets renders one combined collection-stats
// summary reflecting every target's recorded discovered/downloaded/cached
// counts, confirming the shared collectionStats pointer is wired through to
// each concurrent target and rendered once after they finish.
func TestDownloadWorkflowLogsForTargetsReportsCollectionStatsAcrossTargets(t *testing.T) {
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		opts.collectionStats.recordDiscovered(2)
		opts.collectionStats.recordResult(DownloadResult{})
		opts.collectionStats.recordResult(DownloadResult{Cached: true})
		return workflowLogsResult{
			processedRuns: []ProcessedRun{{
				Run: WorkflowRun{
					DatabaseID:   int64(len(opts.WorkflowName)),
					WorkflowName: opts.WorkflowName,
					CreatedAt:    time.Now(),
					LogsPath:     filepath.Join(opts.OutputDir, "run-1"),
				},
			}},
		}, nil
	}

	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	cachedJSONL := filepath.Join(tempDir, "logs.jsonl")
	require.NoError(t, os.WriteFile(cachedJSONL, nil, 0o600))

	_, stderr := captureOutput(t, func() error {
		return DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
			OutputDir:      filepath.Join(tempDir, "logs"),
			SummaryFile:    "summary.json",
			SuppressRender: true,
			CachedJSONL:    cachedJSONL,
		}, []logsWorkflowTarget{
			{workflowName: "alpha", repoOverride: "org/repo-a"},
			{workflowName: "bravo", repoOverride: "org/repo-b"},
		}, nil)
	})

	assert.Contains(t, stderr, "Runs: 4 discovered; reports: 2 downloaded, 2 skipped because cached analyses were reused")
}

func TestDownloadWorkflowLogsForTargetsReturnsErrorWhenAllFail(t *testing.T) {
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })
	collectWorkflowLogsForTarget = func(_ context.Context, _ LogsDownloadOptions) (workflowLogsResult, error) {
		return workflowLogsResult{}, errors.New("access denied")
	}

	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	err = DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
		OutputDir: filepath.Join(tempDir, "logs"),
	}, []logsWorkflowTarget{{workflowName: "private", repoOverride: "org/repo"}}, nil)
	require.ErrorContains(t, err, "access denied")
}

func TestDownloadWorkflowLogsForTargetsUsesOneWallClockTimeout(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "1")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	var calls atomic.Int64
	collectWorkflowLogsForTarget = func(ctx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		calls.Add(1)
		// Targets inherit the shared deadline instead of building a second
		// timeout context, but keep the caller's timeout values so the
		// continuations they emit can be replayed with the same timeout.
		assert.True(t, opts.inheritTimeoutContext)
		assert.Equal(t, 1, opts.TimeoutMinutes)
		<-ctx.Done()
		return workflowLogsResult{timeoutReached: true}, nil
	}

	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	start := time.Now()
	err = DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
		Count:          1,
		OutputDir:      filepath.Join(tempDir, "logs"),
		TimeoutMinutes: 1,
		TimeoutSeconds: 1,
		SuppressRender: true,
	}, []logsWorkflowTarget{
		{workflowName: "first"},
		{workflowName: "second"},
	}, nil)

	require.NoError(t, err)
	assert.Equal(t, int64(2), calls.Load(), "every target should share the same wall-clock timeout")
	assert.Less(t, time.Since(start), 1500*time.Millisecond, "the timeout must bound the entire multi-target operation")
}

func TestCollectLogsTargetsStartsEveryTargetBeforeBatchScheduling(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "1")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	started := make(chan *logsBatchScheduler, 3)
	release := make(chan struct{})
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		started <- opts.batchScheduler
		<-release
		return workflowLogsResult{}, nil
	}

	done := make(chan []logsTargetResult, 1)
	go func() {
		done <- collectLogsTargets(context.Background(), LogsDownloadOptions{
			OutputDir: t.TempDir(),
		}, []logsWorkflowTarget{
			{workflowName: "first"},
			{workflowName: "second"},
			{workflowName: "third"},
		})
	}()

	var scheduler *logsBatchScheduler
	for range 3 {
		select {
		case targetScheduler := <-started:
			require.NotNil(t, targetScheduler)
			if scheduler == nil {
				scheduler = targetScheduler
			} else {
				assert.Same(t, scheduler, targetScheduler)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("all target collectors must start before batch-level scheduling")
		}
	}
	close(release)

	results := <-done
	_, _, _, _, _, errs := mergeLogsTargetResults(results, nil)
	assert.Empty(t, errs)
}

func TestLogsBatchSchedulerDistributesEachRoundAcrossTargets(t *testing.T) {
	ctx := t.Context()
	scheduler := newLogsBatchScheduler(ctx, 3, 2)

	require.NoError(t, scheduler.acquire(ctx, 0))
	require.NoError(t, scheduler.acquire(ctx, 1))

	targetTwoAcquired := make(chan struct{})
	go func() {
		if scheduler.acquire(ctx, 2) == nil {
			close(targetTwoAcquired)
		}
	}()

	scheduler.release(0)
	select {
	case <-targetTwoAcquired:
	case <-time.After(time.Second):
		t.Fatal("the remaining target should receive a slot in the current round")
	}

	nextRoundAcquired := make(chan struct{})
	go func() {
		if scheduler.acquire(ctx, 0) == nil {
			close(nextRoundAcquired)
		}
	}()
	select {
	case <-nextRoundAcquired:
		t.Fatal("a target must not start its next batch before every target completes the current round")
	case <-time.After(25 * time.Millisecond):
	}

	scheduler.release(1)
	scheduler.release(2)
	select {
	case <-nextRoundAcquired:
	case <-time.After(time.Second):
		t.Fatal("the next batch round should start after every target completes the current round")
	}
	scheduler.release(0)
	scheduler.remove(0)
	scheduler.remove(1)
	scheduler.remove(2)
}

func TestLogsBatchSchedulerUnblocksWaitersOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler := newLogsBatchScheduler(ctx, 2, 1)
	// Fill the only concurrency slot so the second target parks in cond.Wait
	// rather than failing the cheap pre-check at the top of acquire.
	require.NoError(t, scheduler.acquire(ctx, 0))

	waitErr := make(chan error, 1)
	go func() { waitErr <- scheduler.acquire(ctx, 1) }()
	// Wait until the target is genuinely parked in cond.Wait, otherwise the
	// cheap ctx pre-check at the top of acquire could satisfy this test without
	// the cancellation broadcast ever waking a blocked waiter.
	require.Eventually(t, func() bool { return schedulerWaitingCount(scheduler) == 1 }, time.Second, time.Millisecond)

	cancel()
	select {
	case err := <-waitErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("acquire must unblock when the context is canceled while waiting for a slot")
	}
	scheduler.release(0)
}

func TestLogsBatchSchedulerAdvancesRoundWhenTargetLeavesMidRound(t *testing.T) {
	ctx := t.Context()
	scheduler := newLogsBatchScheduler(ctx, 3, 3)

	// Targets 1 and 2 complete the current round while target 0 never takes its
	// turn, so the round cannot advance until target 0 leaves.
	require.NoError(t, scheduler.acquire(ctx, 1))
	scheduler.release(1)
	require.NoError(t, scheduler.acquire(ctx, 2))
	scheduler.release(2)

	waitErr := make(chan error, 1)
	go func() { waitErr <- scheduler.acquire(ctx, 1) }()
	require.Eventually(t, func() bool { return schedulerWaitingCount(scheduler) == 1 }, time.Second, time.Millisecond)

	// Target 0 finishes its collection without ever claiming a turn in this
	// round; dropping it must advance the round rather than wedge the waiters.
	scheduler.remove(0)
	select {
	case err := <-waitErr:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("dropping a target that never took its turn must advance the round")
	}
	scheduler.release(1)
	scheduler.remove(1)
	scheduler.remove(2)
}

func TestCollectLogsTargetsUsesGlobalCount(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "2")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	var mu sync.Mutex
	var sharedLimit *logsCountLimit
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		mu.Lock()
		if sharedLimit == nil {
			sharedLimit = opts.countLimit
		} else {
			assert.Same(t, sharedLimit, opts.countLimit)
		}
		mu.Unlock()

		var runs []ProcessedRun
		for range opts.Count {
			if !opts.countLimit.tryAdd() {
				break
			}
			runs = append(runs, ProcessedRun{Run: WorkflowRun{
				DatabaseID:   int64(len(runs) + 1),
				WorkflowName: opts.WorkflowName,
			}})
		}
		return workflowLogsResult{processedRuns: runs}, nil
	}

	results := collectLogsTargets(context.Background(), LogsDownloadOptions{
		Count:     3,
		OutputDir: t.TempDir(),
	}, []logsWorkflowTarget{
		{workflowName: "first"},
		{workflowName: "second"},
	})
	processedRuns, _, _, _, _, errs := mergeLogsTargetResults(results, nil)

	assert.Empty(t, errs)
	assert.Len(t, processedRuns, 3, "count must be shared across all targets")
	require.NotNil(t, sharedLimit)
}

func TestCollectLogsTargetsStopsStartedTargetsOnceGlobalCountReached(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "1")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	var calls atomic.Int64
	// The target that fills the shared count cancels the remaining targets, so
	// the collectors rendezvous before any of them claims a run to keep the
	// call count independent of scheduling order.
	allStarted := make(chan struct{})
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		if calls.Add(1) == 3 {
			close(allStarted)
		}
		select {
		case <-allStarted:
		case <-time.After(5 * time.Second):
		}
		if !opts.countLimit.tryAdd() {
			return countLimitedLogsTargetResult(opts), nil
		}
		return workflowLogsResult{processedRuns: []ProcessedRun{{
			Run: WorkflowRun{DatabaseID: 1, WorkflowName: opts.WorkflowName},
		}}}, nil
	}

	results := collectLogsTargets(context.Background(), LogsDownloadOptions{
		Count:     1,
		OutputDir: t.TempDir(),
	}, []logsWorkflowTarget{
		{workflowName: "first"},
		{workflowName: "second"},
		{workflowName: "third"},
	})
	processedRuns, _, _, _, _, errs := mergeLogsTargetResults(results, nil)

	assert.Empty(t, errs)
	assert.Len(t, processedRuns, 1, "the shared count must be honored across every started target")
	assert.Equal(t, int64(3), calls.Load(), "every target collector starts so batch scheduling can distribute API queries fairly")
}

func TestCollectLogsTargetsClearsQueueWhenNegativeRateLimitReached(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "1")
	originalCollector := collectWorkflowLogsForTarget
	originalFetchRateLimit := fetchRateLimitFunc
	t.Cleanup(func() {
		collectWorkflowLogsForTarget = originalCollector
		fetchRateLimitFunc = originalFetchRateLimit
	})

	var rateLimitCalls atomic.Int64
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		rateLimitCalls.Add(1)
		return rateLimitResource{
			Limit:     15000,
			Remaining: 2000,
			Reset:     time.Now().Add(10 * time.Minute).Unix(),
			Used:      13000,
		}, nil
	}
	var calls atomic.Int64
	// The first target to observe the ceiling cancels the shared context, so the
	// collectors rendezvous before any of them performs the check. Without this
	// barrier the other targets may be canceled before they start and the call
	// count becomes timing dependent.
	allStarted := make(chan struct{})
	collectWorkflowLogsForTarget = func(ctx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		if calls.Add(1) == 3 {
			close(allStarted)
		}
		select {
		case <-allStarted:
		case <-time.After(5 * time.Second):
		}
		assert.Equal(t, -2000, opts.MaxGitHubAPIRateLimit)
		err := opts.rateLimitState.check(ctx, false, opts.MaxGitHubAPIRateLimit, 1)
		if errors.Is(err, context.Canceled) && opts.rateLimitState.isReached() {
			err = errLogsAPIRateLimitReached
		}
		return rateLimitedLogsTargetResult(opts), err
	}

	results := collectLogsTargets(context.Background(), LogsDownloadOptions{
		Count:                 10,
		OutputDir:             t.TempDir(),
		MaxGitHubAPIRateLimit: -2000,
	}, []logsWorkflowTarget{
		{workflowName: "first"},
		{workflowName: "second"},
		{workflowName: "third"},
	})

	assert.Equal(t, int64(3), calls.Load(), "every target collector starts so batch scheduling can distribute API queries fairly")
	assert.Equal(t, int64(1), rateLimitCalls.Load(), "the reached reserve limit must be reused without another API check")
	require.Len(t, results, 3)
	continuationCount := 0
	for _, result := range results {
		require.ErrorIs(t, result.err, errLogsAPIRateLimitReached)
		if result.result.continuation != nil {
			continuationCount++
			assert.Contains(t, result.result.continuation.Message, "GitHub API rate limit ceiling reached")
			assert.Equal(t, -2000, result.result.continuation.MaxGitHubAPIRateLimit)
		}
	}
	_, continuations, _, _, _, errs := mergeLogsTargetResults(results, nil)
	assert.Empty(t, errs, "rate-limit termination must not suppress continuations with a hard error")
	assert.Equal(t, 3, continuationCount, "each target must retain a continuation")
	assert.Len(t, continuations, 3, "each target must retain a continuation after merging")
}

func TestCountLimitedLogsTargetResultPreservesDateRangeContinuation(t *testing.T) {
	result := countLimitedLogsTargetResult(LogsDownloadOptions{
		WorkflowName: "queued",
		Count:        1,
		StartDate:    "2026-09-01",
		BeforeRunID:  123,
	})

	assert.True(t, result.countLimitReached)
	require.NotNil(t, result.continuation)
	assert.Equal(t, "queued", result.continuation.WorkflowName)
	assert.Equal(t, int64(123), result.continuation.BeforeRunID)
}

func TestMergeLogsTargetResultsPropagatesCountLimitReached(t *testing.T) {
	processedRuns, _, _, countLimitReached, _, errs := mergeLogsTargetResults([]logsTargetResult{
		{target: logsWorkflowTarget{workflowName: "limited"}, result: workflowLogsResult{countLimitReached: true}},
		{target: logsWorkflowTarget{workflowName: "complete"}, result: workflowLogsResult{countLimitReached: false}},
	}, nil)

	assert.Empty(t, processedRuns)
	assert.True(t, countLimitReached)
	assert.Empty(t, errs)
}

func TestMergeLogsTargetResultsPreservesPartialRunsFromFailedTarget(t *testing.T) {
	run := ProcessedRun{Run: WorkflowRun{DatabaseID: 42}}
	processedRuns, _, _, _, _, errs := mergeLogsTargetResults([]logsTargetResult{{
		target: logsWorkflowTarget{workflowName: "partial"},
		result: workflowLogsResult{processedRuns: []ProcessedRun{run}},
		err:    errors.New("pagination failed"),
	}}, nil)

	require.Len(t, processedRuns, 1)
	assert.Equal(t, int64(42), processedRuns[0].Run.DatabaseID)
	require.Len(t, errs, 1)
	assert.ErrorContains(t, errs[0], "pagination failed")
}

func TestLogsCountLimitRemaining(t *testing.T) {
	t.Parallel()

	var unlimited *logsCountLimit
	assert.Equal(t, -1, unlimited.remaining(), "no shared limit means unbounded")

	limit := newLogsCountLimit(2)
	assert.Equal(t, 2, limit.remaining())
	require.True(t, limit.tryAdd())
	assert.Equal(t, 1, limit.remaining())
	require.True(t, limit.tryAdd())
	assert.Equal(t, 0, limit.remaining())
	assert.False(t, limit.tryAdd())
	assert.Equal(t, 0, limit.remaining())
}

func TestConcurrentRunDownloadsCancelWorkersAndClearQueueAtSharedCount(t *testing.T) {
	original := processConcurrentRunDownload
	t.Cleanup(func() { processConcurrentRunDownload = original })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limit := newLogsCountLimit(1)
	limit.cancel = cancel

	secondStarted := make(chan struct{})
	secondCanceled := make(chan struct{})
	var started atomic.Int64
	processConcurrentRunDownload = func(ctx context.Context, run WorkflowRun, _ concurrentRunDownloadParams, _ *atomic.Int64, _ *console.ProgressBar) (DownloadResult, error) {
		started.Add(1)
		if run.DatabaseID == 1 {
			<-secondStarted
			return DownloadResult{RunAnalysis: RunAnalysis{Run: run}}, nil
		}
		close(secondStarted)
		<-ctx.Done()
		close(secondCanceled)
		return DownloadResult{RunAnalysis: RunAnalysis{Run: run}, Skipped: true, Error: ctx.Err()}, nil
	}

	runs := []WorkflowRun{
		{DatabaseID: 1},
		{DatabaseID: 2},
		{DatabaseID: 3},
		{DatabaseID: 4},
	}
	results := downloadRunArtifactsConcurrent(ctx, runs, runArtifactsConcurrentOptions{
		outputDir:              t.TempDir(),
		maxRuns:                1,
		maxConcurrentDownloads: 2,
		onResult: func(_ int, result DownloadResult) {
			if !result.Skipped && result.Error == nil {
				limit.tryAdd()
			}
		},
	})

	assert.Equal(t, int64(2), started.Load(), "queued runs must not start after the shared count is reached")
	select {
	case <-secondCanceled:
	default:
		t.Fatal("an in-flight worker did not observe shared count cancellation")
	}
	require.Len(t, results, len(runs))
	require.NoError(t, results[0].Error)
	for _, result := range results[1:] {
		assert.True(t, result.Skipped)
		assert.ErrorIs(t, result.Error, context.Canceled)
	}
}

func TestAppendProcessedWorkflowRunsPreservesNewestPrefixOnCancellation(t *testing.T) {
	originalProcess := processConcurrentRunDownload
	originalBuild := buildLogsProcessedRun
	t.Cleanup(func() {
		processConcurrentRunDownload = originalProcess
		buildLogsProcessedRun = originalBuild
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	limit := newLogsCountLimit(1)
	limit.cancel = cancel

	secondStarted := make(chan struct{})
	var startedMu sync.Mutex
	var started []int64
	processConcurrentRunDownload = func(ctx context.Context, run WorkflowRun, _ concurrentRunDownloadParams, _ *atomic.Int64, _ *console.ProgressBar) (DownloadResult, error) {
		startedMu.Lock()
		started = append(started, run.DatabaseID)
		startedMu.Unlock()
		switch run.DatabaseID {
		case 1:
			<-secondStarted
		case 2:
			close(secondStarted)
		}
		return DownloadResult{RunAnalysis: RunAnalysis{Run: run}}, nil
	}
	buildLogsProcessedRun = func(_ context.Context, result DownloadResult, _, _ bool) ProcessedRun {
		return ProcessedRun{Run: result.Run}
	}

	processed, count, _ := appendProcessedWorkflowRuns(ctx, nil, []WorkflowRun{
		{DatabaseID: 1},
		{DatabaseID: 2},
		{DatabaseID: 3},
	}, 0, processWorkflowRunBatchOptions{
		count:                  1,
		maxConcurrentDownloads: 2,
		countLimit:             limit,
	})

	require.Len(t, processed, 1)
	assert.Equal(t, int64(1), processed[0].Run.DatabaseID, "completion order must not replace the newest API result")
	assert.Equal(t, 1, count)
	startedMu.Lock()
	assert.ElementsMatch(t, []int64{1, 2}, started, "the queued third run must be cleared when the prefix fills the count")
	startedMu.Unlock()
}

func TestConcurrentRunDownloadsRecoverWorkerPanic(t *testing.T) {
	original := processConcurrentRunDownload
	t.Cleanup(func() { processConcurrentRunDownload = original })
	processConcurrentRunDownload = func(context.Context, WorkflowRun, concurrentRunDownloadParams, *atomic.Int64, *console.ProgressBar) (DownloadResult, error) {
		panic("download failed")
	}

	results := downloadRunArtifactsConcurrent(context.Background(), []WorkflowRun{{DatabaseID: 1}}, runArtifactsConcurrentOptions{
		outputDir:              t.TempDir(),
		maxConcurrentDownloads: 1,
	})
	require.Len(t, results, 1)
	assert.True(t, results[0].Skipped)
	require.ErrorContains(t, results[0].Error, "run download panicked: download failed")
}

// TestLogsTargetContinuationPreservesTimeout guards against multi-target
// continuations losing the caller's --timeout: targets inherit the shared
// deadline instead of building their own, but the timeout value itself must
// still be replayable from the emitted continuation parameters.
func TestLogsTargetContinuationPreservesTimeout(t *testing.T) {
	t.Parallel()

	opts := LogsDownloadOptions{
		WorkflowName:          "limited",
		Count:                 5,
		StartDate:             "2026-09-01",
		TimeoutMinutes:        7,
		inheritTimeoutContext: true,
	}

	countLimited := countLimitedLogsTargetResult(opts)
	require.NotNil(t, countLimited.continuation)
	assert.Equal(t, 7, countLimited.continuation.Timeout)

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	queued := queuedLogsTargetResult(opts, ctx)
	require.NotNil(t, queued.continuation)
	assert.Equal(t, 7, queued.continuation.Timeout)
}

// TestRunLogsBatchRoundHoldsSchedulerTurnDuringRateLimitCheck pins the ordering
// that keeps the shared API budget accurate: the rate-limit check must happen
// inside the scheduler turn. If the check ran before acquiring, a target could
// reserve budget, park waiting for a slot while other targets spent quota, and
// then issue its request against stale usage, overshooting the configured
// ceiling.
func TestRunLogsBatchRoundHoldsSchedulerTurnDuringRateLimitCheck(t *testing.T) {
	originalFetchRateLimit := fetchRateLimitFunc
	originalFetch := logsFetchWorkflowRunBatch
	originalProcess := logsProcessWorkflowRunBatch
	t.Cleanup(func() {
		fetchRateLimitFunc = originalFetchRateLimit
		logsFetchWorkflowRunBatch = originalFetch
		logsProcessWorkflowRunBatch = originalProcess
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduler := newLogsBatchScheduler(ctx, 2, 1)
	otherAcquired := make(chan error, 1)
	var turnHeldDuringCheck atomic.Bool

	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		// A competing target must not be able to take a turn while this check
		// is in flight; it has to park until this round's turn is released.
		go func() { otherAcquired <- scheduler.acquire(ctx, 1) }()
		require.Eventually(t, func() bool { return schedulerWaitingCount(scheduler) == 1 }, time.Second, time.Millisecond)
		turnHeldDuringCheck.Store(true)
		return rateLimitResource{Limit: 15000, Remaining: 14000, Reset: time.Now().Add(time.Hour).Unix(), Used: 1000}, nil
	}
	logsFetchWorkflowRunBatch = func(_ context.Context, _ LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		assert.True(t, turnHeldDuringCheck.Load(), "the rate-limit check must precede the batch request inside the same turn")
		assert.Equal(t, 1, schedulerWaitingCount(scheduler), "the competing target must still be parked while this batch runs")
		return workflowRunBatch{runs: []WorkflowRun{{DatabaseID: 1}}, totalFetched: 1, batchSize: 1}, nil
	}
	logsProcessWorkflowRunBatch = func(_ context.Context, batch workflowRunBatch, processedRuns []ProcessedRun, _ processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
		for _, run := range batch.runs {
			processedRuns = append(processedRuns, ProcessedRun{Run: run})
		}
		return processedRuns, len(batch.runs), true, false, false
	}

	state := logsCollectionState{}
	_, err := runLogsBatchRound(&state, logsDownloadRuntime{activeCtx: ctx}, LogsDownloadOptions{
		Count:                 10,
		MaxGitHubAPIRateLimit: 14000,
		rateLimitFirstRequest: true,
		rateLimitState:        newLogsRateLimitState(cancel),
		batchScheduler:        scheduler,
		batchTargetID:         0,
	})
	require.NoError(t, err)
	assert.True(t, turnHeldDuringCheck.Load(), "the rate-limit check must run inside the scheduler turn")

	// Releasing the turn at the end of the round lets the parked target proceed.
	select {
	case err := <-otherAcquired:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("the competing target must receive a turn once the round is released")
	}
}

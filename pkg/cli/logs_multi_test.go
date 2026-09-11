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

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, int64(1), calls.Load(), "queued targets must not receive a fresh timeout")
	assert.Less(t, time.Since(start), 1500*time.Millisecond, "the timeout must bound the entire multi-target operation")
}

func TestCollectLogsTargetsEmitsContinuationForQueuedTarget(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "1")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	// The running target blocks until the shared deadline fires so the queued
	// target never gets a worker slot and must be canceled while still waiting
	// on the semaphore.
	collectWorkflowLogsForTarget = func(ctx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		<-ctx.Done()
		return workflowLogsResult{timeoutReached: true}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results := collectLogsTargets(ctx, LogsDownloadOptions{
		Count:       5,
		OutputDir:   t.TempDir(),
		StartDate:   "2024-01-01",
		BeforeRunID: 999,
	}, []logsWorkflowTarget{
		{workflowName: "first"},
		{workflowName: "second"},
	})

	_, continuations, timeoutReached, _, _, errs := mergeLogsTargetResults(results, nil)
	require.NotEmpty(t, errs, "the queued target should still surface a context error")
	assert.True(t, timeoutReached)
	// The mock only reports timeoutReached (no continuation) for the target that
	// actually ran; the one still waiting on the semaphore when the deadline fires
	// is the only one that goes through queuedLogsTargetResult, so exactly one
	// continuation is expected, regardless of which target won the race for the
	// worker slot.
	require.Len(t, continuations, 1, "the queued target must produce a resumable continuation")
	assert.Equal(t, int64(999), continuations[0].BeforeRunID, "the queued target's continuation must preserve its own resume cursor")
	assert.Equal(t, "2024-01-01", continuations[0].StartDate)
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

func TestCollectLogsTargetsDoesNotStartQueuedTargetsAfterGlobalCountReached(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "1")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	var calls atomic.Int64
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		calls.Add(1)
		if !opts.countLimit.tryAdd() {
			return workflowLogsResult{}, errors.New("shared count reached before first target added a run")
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
	assert.Len(t, processedRuns, 1)
	assert.Equal(t, int64(1), calls.Load(), "queued targets must not start after the shared count is reached")
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

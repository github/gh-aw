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
		assert.Zero(t, opts.TimeoutMinutes)
		assert.Zero(t, opts.TimeoutSeconds)
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
	assert.True(t, sharedLimit.isReached())
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

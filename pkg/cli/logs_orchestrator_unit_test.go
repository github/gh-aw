//go:build !integration

package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsDeadlineExceeded verifies that the helper correctly identifies
// context.DeadlineExceeded and returns false for other cases (including nil error).
func TestIsDeadlineExceeded(t *testing.T) {
	t.Run("deadline exceeded context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		time.Sleep(time.Millisecond) // ensure deadline has fired
		assert.True(t, isDeadlineExceeded(ctx), "expected true for DeadlineExceeded context")
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.False(t, isDeadlineExceeded(ctx), "expected false for cancelled (not deadline) context")
	})

	t.Run("active context", func(t *testing.T) {
		ctx := context.Background()
		assert.False(t, isDeadlineExceeded(ctx), "expected false for active (non-cancelled) context")
	})
}

func TestBuildLogsDownloadContextPrefersSecondTimeout(t *testing.T) {
	before := time.Now()
	ctx, cancel, startTime, timeoutDuration := buildLogsDownloadContext(context.Background(), 5, 55, false)
	defer cancel()

	require.False(t, startTime.IsZero(), "timeout context should record a start time")
	assert.Equal(t, 55*time.Second, timeoutDuration)
	deadline, ok := ctx.Deadline()
	require.True(t, ok, "timeout context should have a deadline")

	wantMin := before.Add(50 * time.Second)
	wantMax := before.Add(60 * time.Second)
	assert.True(t, deadline.After(wantMin) && deadline.Before(wantMax),
		"deadline should use timeoutSeconds instead of timeoutMinutes; got %v from %v", deadline.Sub(before), before)
}

func TestBuildLogsDownloadContextRequiresPositiveMinuteTimeout(t *testing.T) {
	tests := []struct {
		name           string
		timeoutMinutes int
		timeoutSeconds int
	}{
		{name: "zero minutes without seconds", timeoutMinutes: 0, timeoutSeconds: 0},
		{name: "zero minutes ignores seconds", timeoutMinutes: 0, timeoutSeconds: 55},
		{name: "negative minutes ignores seconds", timeoutMinutes: -1, timeoutSeconds: 55},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, startTime, timeoutDuration := buildLogsDownloadContext(context.Background(), tt.timeoutMinutes, tt.timeoutSeconds, false)

			assert.Nil(t, cancel)
			assert.True(t, startTime.IsZero(), "non-positive minute timeout should disable timeout even when seconds are set")
			assert.Zero(t, timeoutDuration)
			_, ok := ctx.Deadline()
			assert.False(t, ok, "non-positive minute timeout should not create a deadline even when seconds are set")
		})
	}
}

// TestNoRunsMessage verifies that the helper returns an informative message
// depending on the start_date filter and timeoutReached flag.
func TestNoRunsMessage(t *testing.T) {
	now := time.Now()
	futureDate := now.AddDate(0, 0, 5).Format("2006-01-02")
	oldDate := now.AddDate(0, 0, -100).Format("2006-01-02")
	recentDate := now.AddDate(0, 0, -5).Format("2006-01-02")
	futureRFC3339 := now.AddDate(1, 0, 0).Format(time.RFC3339)

	tests := []struct {
		name           string
		startDate      string
		timeoutReached bool
		storageReached bool
		wantContains   string
	}{
		{
			name:           "timeout reached",
			startDate:      "",
			timeoutReached: true,
			wantContains:   "Timeout reached",
		},
		{
			name:           "storage limit reached",
			storageReached: true,
			wantContains:   "Storage limit reached",
		},
		{
			name:           "future date (YYYY-MM-DD)",
			startDate:      futureDate,
			timeoutReached: false,
			wantContains:   "is in the future",
		},
		{
			name:           "future date (RFC3339)",
			startDate:      futureRFC3339,
			timeoutReached: false,
			wantContains:   "is in the future",
		},
		{
			name:           "old date beyond retention",
			startDate:      oldDate,
			timeoutReached: false,
			wantContains:   "retention period",
		},
		{
			name:           "recent date within retention",
			startDate:      recentDate,
			timeoutReached: false,
			wantContains:   "No runs found matching",
		},
		{
			name:           "no start date",
			startDate:      "",
			timeoutReached: false,
			wantContains:   "No runs found matching",
		},
		{
			name:           "timeout takes priority over future date",
			startDate:      futureDate,
			timeoutReached: true,
			wantContains:   "Timeout reached",
		},
		{
			name:           "future date message includes the date value",
			startDate:      "2030-01-01",
			timeoutReached: false,
			wantContains:   "2030-01-01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := noRunsMessage(tt.startDate, tt.timeoutReached, tt.storageReached)
			assert.Contains(t, got, tt.wantContains,
				"noRunsMessage(%q, %v) = %q, want to contain %q", tt.startDate, tt.timeoutReached, got, tt.wantContains)
		})
	}
}

// TestParseFilterDate verifies that date strings accepted by the logs flags are
// correctly parsed into time.Time values.
func TestParseFilterDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"YYYY-MM-DD", "2024-01-15", false},
		{"RFC3339", "2024-01-15T10:30:00Z", false},
		{"RFC3339 with offset", "2024-01-15T10:30:00+05:00", false},
		{"invalid", "not-a-date", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFilterDate(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.False(t, got.IsZero(), "expected non-zero time")
			}
		})
	}
}

// TestBuildContinuationIfNeeded exercises the helper that DownloadWorkflowLogs uses
// to emit a pagination cursor when a date-range fetch hits the count limit or times out.
func TestBuildContinuationIfNeeded(t *testing.T) {
	runs := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 3000}},
		{Run: WorkflowRun{DatabaseID: 2999}}, // oldest – used as BeforeRunID cursor
	}

	t.Run("count limit reached emits cursor with correct message and BeforeRunID", func(t *testing.T) {
		c := buildContinuationIfNeeded(runs, false, true, false, continuationOptions{
			workflowName:          "my-workflow",
			startDate:             "2026-06-01",
			endDate:               "2026-06-30",
			engine:                "claude",
			branch:                "main",
			afterRunID:            0,
			count:                 100,
			timeoutMinutes:        3,
			maxGitHubAPIRateLimit: -2000,
			maxStorageMB:          10240,
		})
		require.NotNil(t, c, "expected continuation when countLimitReached=true")
		assert.Equal(t, int64(2999), c.BeforeRunID, "BeforeRunID should be oldest processed run")
		assert.Equal(t, "2026-06-01", c.StartDate)
		assert.Equal(t, "2026-06-30", c.EndDate)
		assert.Equal(t, 100, c.Count)
		assert.Equal(t, -2000, c.MaxGitHubAPIRateLimit)
		assert.Equal(t, 10240, c.MaxStorageMB)
		assert.Contains(t, c.Message, "Count limit reached")
	})

	t.Run("lastFetchedBeforeDate overrides end_date so a resumed request does not replay already-scanned pages", func(t *testing.T) {
		// When many non-matching runs are interspersed across the window, the oldest
		// *matching* run (used for BeforeRunID) can be far newer than where the scan
		// actually reached. The continuation must bound its end_date at the real
		// pagination cursor, not the original request's end_date, or a resumed
		// request restarts from the top of the original window (see github/gh-aw#54110).
		c := buildContinuationIfNeeded(runs, false, true, false, continuationOptions{
			workflowName:          "my-workflow",
			startDate:             "2026-01-01",
			endDate:               "2026-06-30",
			count:                 100,
			timeoutMinutes:        3,
			lastFetchedBeforeDate: "2026-03-15T00:00:00Z",
		})
		require.NotNil(t, c)
		assert.Equal(t, "2026-03-15T00:00:00Z", c.EndDate, "end_date should be the actual scan cursor, not the original request end_date")
		assert.Equal(t, "2026-01-01", c.StartDate)
	})

	t.Run("timeout reached emits cursor with timeout message", func(t *testing.T) {
		c := buildContinuationIfNeeded(runs, true, false, false, continuationOptions{
			workflowName:   "my-workflow",
			startDate:      "2026-06-01",
			endDate:        "",
			engine:         "claude",
			branch:         "",
			afterRunID:     0,
			count:          50,
			timeoutMinutes: 10,
		})
		require.NotNil(t, c, "expected continuation when timeoutReached=true")
		assert.Equal(t, int64(2999), c.BeforeRunID)
		assert.Contains(t, c.Message, "Timeout reached")
	})

	t.Run("storage limit reached emits resumable cursor", func(t *testing.T) {
		c := buildContinuationIfNeeded(runs, false, false, true, continuationOptions{
			workflowName:   "my-workflow",
			count:          50,
			maxStorageMB:   2048,
			pruneOlderRuns: true,
		})
		require.NotNil(t, c)
		assert.Equal(t, int64(2999), c.BeforeRunID)
		assert.Equal(t, 2048, c.MaxStorageMB)
		assert.True(t, c.PruneOlderRuns)
		assert.Contains(t, c.Message, "Storage limit reached")
	})

	t.Run("neither flag set returns nil", func(t *testing.T) {
		c := buildContinuationIfNeeded(runs, false, false, false, continuationOptions{
			workflowName:   "my-workflow",
			startDate:      "2026-06-01",
			endDate:        "",
			engine:         "claude",
			branch:         "",
			afterRunID:     0,
			count:          100,
			timeoutMinutes: 3,
		})
		assert.Nil(t, c, "expected nil when neither timeout nor count limit was reached")
	})

	t.Run("empty processedRuns returns nil even when count limit reached", func(t *testing.T) {
		c := buildContinuationIfNeeded(nil, false, true, false, continuationOptions{
			workflowName:   "my-workflow",
			startDate:      "2026-06-01",
			endDate:        "",
			engine:         "claude",
			branch:         "",
			afterRunID:     0,
			count:          100,
			timeoutMinutes: 3,
		})
		assert.Nil(t, c, "expected nil when no runs were processed")
	})

	t.Run("empty processedRuns returns current cursor when storage blocks progress", func(t *testing.T) {
		c := buildContinuationIfNeeded(nil, false, false, true, continuationOptions{
			workflowName: "my-workflow",
			count:        100,
			maxStorageMB: 2048,
		})
		require.NotNil(t, c)
		assert.Zero(t, c.BeforeRunID)
		assert.Equal(t, 2048, c.MaxStorageMB)
		assert.Contains(t, c.Message, "Storage limit reached")
	})
}

func TestComputeLogsBatchSize(t *testing.T) {
	tests := []struct {
		name            string
		workflowName    string
		count           int
		processedCount  int
		fetchAllInRange bool
		want            int
	}{
		{
			name:           "default batch size for named workflow",
			workflowName:   "logs.yml",
			count:          100,
			processedCount: 0,
			want:           BatchSize,
		},
		{
			name:           "larger default for all workflows",
			count:          100,
			processedCount: 0,
			want:           BatchSizeForAllWorkflows,
		},
		{
			name:           "small remaining count uses buffered batch size",
			workflowName:   "logs.yml",
			count:          10,
			processedCount: 8,
			want:           6,
		},
		{
			name:            "date range keeps default batch size",
			workflowName:    "logs.yml",
			count:           10,
			processedCount:  8,
			fetchAllInRange: true,
			want:            BatchSize,
		},
		{
			name:           "all workflows keep minimum scan size",
			count:          10,
			processedCount: 8,
			want:           BatchSizeForAllWorkflows,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, computeLogsBatchSize(tt.workflowName, tt.count, tt.processedCount, tt.fetchAllInRange))
		})
	}
}

func TestHandleEmptyWorkflowRunBatch(t *testing.T) {
	t.Run("stop when pagination exhausted", func(t *testing.T) {
		cursor, shouldContinue, shouldStop := handleEmptyWorkflowRunBatch(workflowRunBatch{
			totalFetched: 5,
			batchSize:    10,
		}, false)
		assert.Empty(t, cursor)
		assert.False(t, shouldContinue)
		assert.True(t, shouldStop)
	})

	t.Run("advance cursor when more pages may exist", func(t *testing.T) {
		cursorTime := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
		cursor, shouldContinue, shouldStop := handleEmptyWorkflowRunBatch(workflowRunBatch{
			totalFetched:           BatchSize,
			batchSize:              BatchSize,
			oldestFetchedCreatedAt: cursorTime,
		}, false)
		assert.Equal(t, cursorTime.Format(time.RFC3339), cursor)
		assert.True(t, shouldContinue)
		assert.False(t, shouldStop)
	})
}

// TestCollectProcessedWorkflowRunsAccumulatesBatches is a regression test for a bug where
// the batch results were assigned to a loop-scoped copy of processedRuns, so every
// processed run was discarded and `gh aw logs` reported "No workflow runs with artifacts
// found matching the specified criteria" even though artifacts had been downloaded.
func TestCollectProcessedWorkflowRunsAccumulatesBatches(t *testing.T) {
	batches := [][]WorkflowRun{
		{{DatabaseID: 1}, {DatabaseID: 2}},
		{{DatabaseID: 3}},
	}
	fetchCalls := 0

	originalFetch := logsFetchWorkflowRunBatch
	originalProcess := logsProcessWorkflowRunBatch
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch = originalFetch
		logsProcessWorkflowRunBatch = originalProcess
	})

	logsFetchWorkflowRunBatch = func(_ context.Context, _ LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		if fetchCalls >= len(batches) {
			return workflowRunBatch{runs: nil, totalFetched: 0, batchSize: 2}, nil
		}
		runs := batches[fetchCalls]
		fetchCalls++
		// totalFetched == batchSize keeps pagination going after the first batch.
		return workflowRunBatch{runs: runs, totalFetched: len(runs), batchSize: 2}, nil
	}
	logsProcessWorkflowRunBatch = func(_ context.Context, batch workflowRunBatch, processedRuns []ProcessedRun, _ processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
		for _, run := range batch.runs {
			processedRuns = append(processedRuns, ProcessedRun{Run: run})
		}
		return processedRuns, len(batch.runs), true, false, false
	}

	runs, timeoutReached, countLimitReached, _, _, err := collectProcessedWorkflowRuns(
		logsDownloadRuntime{activeCtx: context.Background(), fetchAllInRange: true},
		LogsDownloadOptions{Count: 100, StartDate: "-1d"},
	)
	require.NoError(t, err)
	assert.False(t, timeoutReached)
	assert.False(t, countLimitReached)
	require.Len(t, runs, 3, "runs from every batch should accumulate across iterations")
	assert.Equal(t, int64(1), runs[0].Run.DatabaseID)
	assert.Equal(t, int64(3), runs[2].Run.DatabaseID)
}

// TestFetchAndProcessLogsBatchKeepsCursorWhenStorageLimitReached verifies that
// continuation does not skip unprocessed runs from the interrupted batch.
func TestFetchAndProcessLogsBatchKeepsCursorWhenStorageLimitReached(t *testing.T) {
	originalFetch := logsFetchWorkflowRunBatch
	originalProcess := logsProcessWorkflowRunBatch
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch = originalFetch
		logsProcessWorkflowRunBatch = originalProcess
	})

	storageLimit := newLogsStorageLimit(t.TempDir(), 1, false)
	logsFetchWorkflowRunBatch = func(_ context.Context, _ LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		return workflowRunBatch{
			runs:                   []WorkflowRun{{DatabaseID: 10}, {DatabaseID: 9}},
			totalFetched:           2,
			batchSize:              2,
			oldestFetchedCreatedAt: time.Now().Add(-time.Hour),
		}, nil
	}
	logsProcessWorkflowRunBatch = func(_ context.Context, _ workflowRunBatch, processedRuns []ProcessedRun, _ processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
		storageLimit.reached.Store(true)
		return append(processedRuns, ProcessedRun{Run: WorkflowRun{DatabaseID: 10}}), 1, true, false, true
	}

	state := logsCollectionState{beforeDate: "previous-cursor"}
	stop, err := fetchAndProcessLogsBatch(
		&state,
		logsDownloadRuntime{activeCtx: context.Background(), storageLimit: storageLimit},
		LogsDownloadOptions{Count: 10},
	)

	require.NoError(t, err)
	assert.True(t, stop)
	assert.True(t, state.storageLimitReached)
	assert.Equal(t, "previous-cursor", state.beforeDate)
}

// TestStaleLogsWarning verifies that a warning is only emitted when no explicit
// start_date/end_date was requested and the newest run in the result set is older
// than the staleness threshold. This guards against the "logs" tool silently
// serving stale data without any indication when called with only a count.
func TestStaleLogsWarning(t *testing.T) {
	t.Run("no warning when start date explicitly provided", func(t *testing.T) {
		runs := []ProcessedRun{{Run: WorkflowRun{CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}}}
		assert.Empty(t, staleLogsWarning(runs, "-1d", ""))
	})

	t.Run("no warning when end date explicitly provided", func(t *testing.T) {
		runs := []ProcessedRun{{Run: WorkflowRun{CreatedAt: time.Now().Add(-30 * 24 * time.Hour)}}}
		assert.Empty(t, staleLogsWarning(runs, "", "2024-01-01"))
	})

	t.Run("no warning when no runs", func(t *testing.T) {
		assert.Empty(t, staleLogsWarning(nil, "", ""))
	})

	t.Run("no warning when newest run is recent", func(t *testing.T) {
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: time.Now().Add(-1 * time.Hour)}},
			{Run: WorkflowRun{CreatedAt: time.Now().Add(-40 * 24 * time.Hour)}},
		}
		assert.Empty(t, staleLogsWarning(runs, "", ""))
	})

	t.Run("warns when no dates given and newest run is old", func(t *testing.T) {
		newest := time.Now().Add(-11 * 24 * time.Hour)
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: newest}},
			{Run: WorkflowRun{CreatedAt: newest.Add(-time.Hour)}},
		}
		warning := staleLogsWarning(runs, "", "")
		require.NotEmpty(t, warning)
		assert.Contains(t, warning, "No start_date/end_date was specified")
		assert.Contains(t, warning, "start_date")
		assert.Contains(t, warning, "11 day")
	})
}

// TestCollectProcessedWorkflowRunsIterationLimitSurfacesContinuation is a
// regression test for the bug where hitting MaxIterations during an explicit
// start_date/end_date ("fetchAllInRange") download silently returned whatever
// partial data had accumulated with no continuation cursor, because neither
// timeoutReached nor countLimitReached was ever set. This let callers requesting
// a wide date range (e.g. 90 days) mistake a narrow, possibly-stale slice of
// results for a complete scan of the range (see github/gh-aw#53995).
func TestCollectProcessedWorkflowRunsIterationLimitSurfacesContinuation(t *testing.T) {
	oldFetchRateLimitFunc := fetchRateLimitFunc
	fetchRateLimitFunc = func(context.Context) (rateLimitResource, error) {
		return rateLimitResource{Limit: 5000, Remaining: 5000, Reset: time.Now().Add(time.Hour).Unix()}, nil
	}
	t.Cleanup(func() { fetchRateLimitFunc = oldFetchRateLimitFunc })

	originalFetch := logsFetchWorkflowRunBatch
	originalProcess := logsProcessWorkflowRunBatch
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch = originalFetch
		logsProcessWorkflowRunBatch = originalProcess
	})

	var nextID int64
	// Every batch returns a single matching run and reports totalFetched ==
	// batchSize, so pagination never naturally exhausts the range and never
	// reaches opts.Count either -- the only way out is the MaxIterations cap.
	logsFetchWorkflowRunBatch = func(_ context.Context, _ LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		nextID++
		return workflowRunBatch{
			runs:                   []WorkflowRun{{DatabaseID: nextID}},
			totalFetched:           BatchSize,
			batchSize:              BatchSize,
			oldestFetchedCreatedAt: time.Now().Add(-time.Duration(nextID) * time.Hour),
		}, nil
	}
	logsProcessWorkflowRunBatch = func(_ context.Context, batch workflowRunBatch, processedRuns []ProcessedRun, _ processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
		for _, run := range batch.runs {
			processedRuns = append(processedRuns, ProcessedRun{Run: run})
		}
		return processedRuns, len(batch.runs), true, false, false
	}

	runs, timeoutReached, countLimitReached, _, _, err := collectProcessedWorkflowRuns(
		logsDownloadRuntime{activeCtx: context.Background(), fetchAllInRange: true},
		LogsDownloadOptions{Count: 1000, StartDate: "-90d"},
	)
	require.NoError(t, err)
	assert.False(t, timeoutReached)
	assert.True(t, countLimitReached, "hitting MaxIterations during a date-range scan should surface a continuation cursor")
	assert.Len(t, runs, MaxIterations, "one run should have been collected per iteration up to the cap")

	t.Run("last batch is included when cap is hit", func(t *testing.T) {
		nextID = 0
		runs, _, countLimitReached, _, _, err := collectProcessedWorkflowRuns(
			logsDownloadRuntime{activeCtx: context.Background(), fetchAllInRange: true},
			LogsDownloadOptions{Count: MaxIterations, StartDate: "-90d"},
		)
		require.NoError(t, err)
		assert.True(t, countLimitReached)
		assert.Len(t, runs, MaxIterations, "the final iteration's batch must not be discarded")
	})
}

// TestDateRangeCoverageWarning verifies that a warning is emitted when an
// explicit start_date/end_date window was requested, the result was truncated
// by the count limit (partial=true), and the returned runs span only a small
// slice of the requested window -- guarding against a caller mistaking a
// single busy (and possibly stale) day for a representative sample of a much
// wider requested range (see github/gh-aw#53995).
func TestDateRangeCoverageWarning(t *testing.T) {
	now := time.Now()
	ninetyDaysAgo := now.Add(-90 * 24 * time.Hour).Format(time.RFC3339)

	t.Run("no warning when result is not partial", func(t *testing.T) {
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: now.Add(-89 * 24 * time.Hour)}},
			{Run: WorkflowRun{CreatedAt: now.Add(-89*24*time.Hour - time.Hour)}},
		}
		assert.Empty(t, dateRangeCoverageWarning(runs, ninetyDaysAgo, "", false))
	})

	t.Run("no warning when no start_date was requested", func(t *testing.T) {
		runs := []ProcessedRun{{Run: WorkflowRun{CreatedAt: now}}}
		assert.Empty(t, dateRangeCoverageWarning(runs, "", "", true))
	})

	t.Run("no warning when no runs", func(t *testing.T) {
		assert.Empty(t, dateRangeCoverageWarning(nil, ninetyDaysAgo, "", true))
	})

	t.Run("no warning when returned runs span most of the requested window", func(t *testing.T) {
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: now}},
			{Run: WorkflowRun{CreatedAt: now.Add(-80 * 24 * time.Hour)}},
		}
		assert.Empty(t, dateRangeCoverageWarning(runs, ninetyDaysAgo, "", true))
	})

	t.Run("warns when partial results are all clustered in a narrow window", func(t *testing.T) {
		staleDay := now.Add(-12 * 24 * time.Hour)
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: staleDay}},
			{Run: WorkflowRun{CreatedAt: staleDay.Add(-2 * time.Hour)}},
		}
		warning := dateRangeCoverageWarning(runs, ninetyDaysAgo, "", true)
		require.NotEmpty(t, warning)
		assert.Contains(t, warning, "narrow slice")
		assert.Contains(t, warning, "continuation")
	})

	t.Run("no false-positive warning when only a single run is returned", func(t *testing.T) {
		// A single run has a zero-length covered span (newest.Sub(oldest) == 0),
		// which must not be mistaken for a narrow slice of the requested window.
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: now.Add(-12 * 24 * time.Hour)}},
		}
		assert.Empty(t, dateRangeCoverageWarning(runs, ninetyDaysAgo, "", true))
	})

	t.Run("warns with explicit endDate and narrow coverage", func(t *testing.T) {
		staleDay := now.Add(-88 * 24 * time.Hour)
		runs := []ProcessedRun{
			{Run: WorkflowRun{CreatedAt: staleDay}},
			{Run: WorkflowRun{CreatedAt: staleDay.Add(-time.Hour)}},
		}
		end := now.Add(-1 * 24 * time.Hour).Format(time.RFC3339)
		warning := dateRangeCoverageWarning(runs, ninetyDaysAgo, end, true)
		require.NotEmpty(t, warning)
		assert.Contains(t, warning, "narrow slice")
	})
}

// TestDeriveGradersClusterValue verifies that homogeneous grader outcomes map to
// their status label while heterogeneous outcomes are reported as "mixed".
func TestDeriveGradersClusterValue(t *testing.T) {
	graderResults := func(statuses ...string) map[string]any {
		results := make([]map[string]any, 0, len(statuses))
		for i, status := range statuses {
			results = append(results, map[string]any{
				"id":     fmt.Sprintf("grader-%d", i),
				"status": status,
			})
		}
		return map[string]any{"version": 1, "results": results}
	}

	tests := []struct {
		name     string
		statuses []string
		expected string
	}{
		{name: "all pass", statuses: []string{"pass", "pass"}, expected: "pass"},
		{name: "all fail", statuses: []string{"fail", "fail"}, expected: "fail"},
		{name: "all error", statuses: []string{"error"}, expected: "error"},
		{name: "all unavailable", statuses: []string{"unavailable", "unavailable"}, expected: "unavailable"},
		{name: "pass and fail", statuses: []string{"pass", "fail"}, expected: "mixed"},
		{name: "every status", statuses: []string{"pass", "fail", "error", "unavailable"}, expected: "mixed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runDir := t.TempDir()
			writeGraderFiles(t,
				filepath.Join(runDir, constants.UsageArtifactName.String(), constants.GradersDirName.String()),
				graderResults(tt.statuses...), nil)
			assert.Equal(t, tt.expected, deriveGradersClusterValue(runDir))
		})
	}

	t.Run("no grader artifact", func(t *testing.T) {
		assert.Equal(t, "absent", deriveGradersClusterValue(t.TempDir()))
	})
}

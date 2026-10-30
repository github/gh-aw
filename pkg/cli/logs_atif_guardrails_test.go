//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLogsATIFBatchGuardrails(t *testing.T) {
	originalFetch, originalProcess := logsFetchWorkflowRunBatch, logsProcessWorkflowRunBatch
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch, logsProcessWorkflowRunBatch = originalFetch, originalProcess
	})
	logsFetchWorkflowRunBatch = func(context.Context, LogsDownloadOptions, string, int, bool) (workflowRunBatch, error) {
		return workflowRunBatch{runs: []WorkflowRun{{DatabaseID: 123}}}, nil
	}
	conversionErr := errors.New("malformed unified session")
	for _, test := range []struct {
		name        string
		exportErr   error
		countStop   bool
		rateStop    bool
		wantTimeout bool
		wantFailure bool
		wantCancel  bool
		wantStorage bool
	}{
		{name: "timeout", exportErr: context.DeadlineExceeded, wantTimeout: true},
		{name: "shared count", exportErr: context.Canceled, countStop: true},
		{name: "shared rate", exportErr: context.Canceled, rateStop: true},
		{name: "user cancellation", exportErr: context.Canceled, wantCancel: true},
		{name: "conversion", exportErr: conversionErr, wantFailure: true},
		{name: "timeout and conversion", exportErr: errors.Join(context.DeadlineExceeded, conversionErr), wantTimeout: true, wantFailure: true},
		{name: "count and conversion", exportErr: errors.Join(context.Canceled, conversionErr), countStop: true, wantFailure: true},
		{name: "rate and conversion", exportErr: errors.Join(context.Canceled, conversionErr), rateStop: true, wantFailure: true},
		{name: "storage", exportErr: errLogsStorageLimitReached, wantStorage: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			limit := newLogsCountLimit(1)
			if test.countStop {
				require.True(t, limit.tryAdd())
			}
			rate := newLogsRateLimitState(nil)
			if test.rateStop {
				rate.reached.Store(true)
			}
			logsProcessWorkflowRunBatch = func(_ context.Context, _ workflowRunBatch, runs []ProcessedRun, opts processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
				*opts.atifError = test.exportErr
				return runs, 0, false, false, false
			}
			state := logsCollectionState{
				beforeDate:    "2026-01-01",
				processedRuns: []ProcessedRun{{Run: WorkflowRun{DatabaseID: 124}}},
			}
			stop, err := fetchAndProcessLogsBatch(&state, logsDownloadRuntime{
				activeCtx: t.Context(), fetchAllInRange: true,
			}, LogsDownloadOptions{Count: 10, countLimit: limit, rateLimitState: rate})
			require.True(t, stop)
			require.Equal(t, test.wantTimeout, state.timeoutReached)
			require.Equal(t, test.countStop, state.countLimitReached)
			require.Equal(t, test.wantStorage, state.storageLimitReached)
			require.Equal(t, "2026-01-01", state.beforeDate)
			require.Len(t, state.processedRuns, 1)
			var exportErr *logsATIFExportError
			require.Equal(t, test.wantFailure, errors.As(err, &exportErr))
			require.Equal(t, test.wantFailure, errors.Is(err, conversionErr))
			require.Equal(t, test.wantCancel, errors.Is(err, context.Canceled))
			require.Equal(t, test.rateStop, errors.Is(err, errLogsAPIRateLimitReached))
			if !test.wantFailure && !test.wantCancel && !test.rateStop {
				require.NoError(t, err)
			}
		})
	}
}

func TestLogsATIFUnfinishedExportNotAccepted(t *testing.T) {
	originalBuild := buildLogsProcessedRun
	t.Cleanup(func() { buildLogsProcessedRun = originalBuild })
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "cached"}[cached], func(t *testing.T) {
			root := t.TempDir()
			runDir := resolveAuditOutputDir(root, 123)
			writeSessionTestFile(t, runDir, "usage/aw_session.jsonl", sessionTestHeader+sessionTestAgentEvent)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			limit := newLogsCountLimit(10)
			storage := newLogsStorageLimit(root, 1, false)
			result := DownloadResult{
				RunAnalysis: RunAnalysis{Run: WorkflowRun{DatabaseID: 123, LogsPath: runDir}},
				LogsPath:    runDir,
			}
			if cached {
				result.CachedRun = &RunData{RunID: 123, LogsPath: runDir}
				cancel()
			} else {
				require.NoError(t, storage.reserve(runDir))
				result.storageReserved = true
				buildLogsProcessedRun = func(_ context.Context, result DownloadResult, _, _ bool) ProcessedRun {
					cancel()
					return ProcessedRun{Run: result.Run}
				}
			}
			var exportErr error
			collector := newOrderedLogsRunCollector(ctx, 0, 1, processWorkflowRunBatchOptions{
				outputDir: root, count: 10, countLimit: limit, storageLimit: storage, atifError: &exportErr,
			})
			collector.recordResult(0, result)
			require.ErrorIs(t, exportErr, context.Canceled)
			require.Zero(t, collector.acceptedCount)
			runs, processed := collector.appendAccepted(nil, 0, nil)
			require.Empty(t, runs)
			require.Zero(t, processed)
			require.Equal(t, 10, limit.remaining())
			require.Zero(t, storage.active)
			require.NoFileExists(t, filepath.Join(runDir, "usage", "aw_session.atif"))
		})
	}
}

func TestLogsATIFTimeoutRendersContinuation(t *testing.T) {
	originalFetch, originalProcess := logsFetchWorkflowRunBatch, logsProcessWorkflowRunBatch
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch, logsProcessWorkflowRunBatch = originalFetch, originalProcess
	})
	installSessionTestGH(t, nil)
	for _, malformed := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "timeout and conversion failure"}[malformed], func(t *testing.T) {
			root := t.TempDir()
			logsFetchWorkflowRunBatch = func(context.Context, LogsDownloadOptions, string, int, bool) (workflowRunBatch, error) {
				return workflowRunBatch{runs: []WorkflowRun{{DatabaseID: 123}}}, nil
			}
			conversionErr := errors.New("malformed unified session")
			logsProcessWorkflowRunBatch = func(_ context.Context, _ workflowRunBatch, _ []ProcessedRun, opts processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
				*opts.atifError = context.DeadlineExceeded
				if malformed {
					*opts.atifError = errors.Join(*opts.atifError, conversionErr)
				}
				return []ProcessedRun{{Run: WorkflowRun{DatabaseID: 124}}}, 1, false, false, false
			}
			_, _ = captureOutput(t, func() error {
				err := DownloadWorkflowLogs(t.Context(), LogsDownloadOptions{
					OutputDir: root, SummaryFile: "summary.json", SuppressRender: true,
					JSONOutput: true, RepoOverride: "owner/repo", Count: 10, StartDate: "2026-01-01",
				})
				if malformed {
					require.ErrorIs(t, err, conversionErr)
				} else {
					require.NoError(t, err)
				}
				return nil
			})
			content, err := os.ReadFile(filepath.Join(root, "summary.json"))
			require.NoError(t, err)
			var report LogsData
			require.NoError(t, json.Unmarshal(content, &report))
			require.Len(t, report.Runs, 1)
			require.NotNil(t, report.Continuation)
			require.Equal(t, int64(124), report.Continuation.BeforeRunID)
		})
	}
}

func TestLogsATIFMultiTargetErrorsAfterRendering(t *testing.T) {
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })
	root := t.TempDir()
	installSessionTestGH(t, nil)
	collectWorkflowLogsForTarget = func(ctx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		runDir := resolveAuditOutputDir(opts.OutputDir, 123)
		writeSessionTestFile(t, runDir, "usage/aw_session.jsonl", "invalid\n")
		var exportErr error
		collector := newOrderedLogsRunCollector(ctx, 0, 1, processWorkflowRunBatchOptions{
			outputDir: opts.OutputDir, count: 1, atifError: &exportErr,
		})
		collector.recordResult(0, DownloadResult{CachedRun: &RunData{RunID: 123, LogsPath: runDir}})
		require.Error(t, exportErr)
		runs, _ := collector.appendAccepted(nil, 0, nil)
		return workflowLogsResult{processedRuns: runs}, &logsATIFExportError{err: exportErr}
	}
	_, _ = captureOutput(t, func() error {
		err := DownloadWorkflowLogsForTargets(t.Context(), LogsDownloadOptions{
			OutputDir: root, SummaryFile: "summary.json", SuppressRender: true, JSONOutput: true,
			MaxGitHubAPIRateLimit: 0,
		}, []logsWorkflowTarget{
			{workflowName: "first", repoOverride: "owner/repo"},
			{workflowName: "second", repoOverride: "owner/repo"},
		}, nil)
		require.ErrorContains(t, err, "ATIF export for run 123")
		require.ErrorContains(t, err, "first")
		require.ErrorContains(t, err, "second")
		return nil
	})
	content, err := os.ReadFile(filepath.Join(root, "summary.json"))
	require.NoError(t, err)
	var report LogsData
	require.NoError(t, json.Unmarshal(content, &report))
	require.Len(t, report.Runs, 2)
}

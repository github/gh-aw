//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/stretchr/testify/require"
)

func TestLogsHarborOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--harbor", "--output", "harbor-job"},
		{"--harbor", "--artifacts", "detection", "--graders", "--outcomes", "--engine", "copilot"},
	} {
		cmd := NewLogsCommand()
		require.NoError(t, cmd.ParseFlags(args))
		options, err := loadCommonLogsOptions(cmd)
		require.NoError(t, err)
		require.True(t, options.Harbor)
		for _, set := range []string{"usage", "experiment", "graders", "agent"} {
			require.Contains(t, options.ArtifactSets, set)
		}
		stdin, err := loadStdinLogsOptions(cmd)
		require.NoError(t, err)
		require.True(t, stdin.Harbor)
		require.Equal(t, options.ArtifactSets, stdin.ArtifactSets)
		if len(args) == 3 {
			require.Equal(t, "harbor-job", options.OutputDir)
			require.False(t, options.GradersOnly)
			require.False(t, options.Outcomes)
		} else {
			require.Contains(t, options.ArtifactSets, "detection")
			require.Contains(t, options.ArtifactSets, "outcomes")
			require.True(t, options.GradersOnly)
			require.True(t, options.Outcomes)
			require.Equal(t, "copilot", options.Engine)
		}
	}
	cmd := NewLogsCommand()
	require.Contains(t, cmd.Example, "logs --harbor --output ./my-logs")
	require.Contains(t, cmd.Long, "portable Harbor job to DIR/harbor")
	require.Contains(t, cmd.Long, "does not run graders or evaluate outcomes")
	opts, err := loadCommonLogsOptions(cmd)
	require.NoError(t, err)
	require.False(t, opts.Harbor)
	require.Equal(t, []string{"usage"}, opts.ArtifactSets)
}

func TestLogsHarborArtifactNormalization(t *testing.T) {
	for _, sets := range [][]string{nil, {}, {"all"}, {"usage"}, {"detection", "usage", "experiment"}} {
		original := append([]string(nil), sets...)
		opts := normalizeLogsDownloadOptions(LogsDownloadOptions{Harbor: true, ArtifactSets: sets})
		stdin := normalizeStdinLogsOptions(StdinLogsOptions{Harbor: true, ArtifactSets: sets})
		require.Equal(t, opts.ArtifactSets, stdin.ArtifactSets)
		require.Equal(t, opts, normalizeLogsDownloadOptions(opts), "normalization must be idempotent")
		require.False(t, opts.GradersOnly)
		require.False(t, opts.Outcomes)
		require.False(t, cachedJSONLCanSatisfy(ResolveArtifactFilter(opts.ArtifactSets), false, false, false),
			"compact metadata cannot supply Harbor session artifacts")
		if len(sets) == 0 || sets[0] == "all" {
			require.Equal(t, sets, opts.ArtifactSets, "unfiltered downloads must stay unfiltered")
		} else {
			for _, required := range []string{"usage", "experiment", "graders", "agent"} {
				require.Contains(t, opts.ArtifactSets, required)
			}
		}
		require.Equal(t, original, append([]string(nil), sets...), "caller artifact slice must not change")
	}
}

func TestLogsHarborBypassesCachedMetadata(t *testing.T) {
	root := t.TempDir()
	cache := &cachedLogsJSONLCache{runs: cachedLogsRuns{123: {RunData: RunData{RunID: 123}}}}
	runtime, err := prepareLogsDownload(t.Context(), LogsDownloadOptions{
		Harbor: true, ArtifactSets: []string{"usage"}, OutputDir: root,
		cachedJSONLCache: cache, skipEnsureGitignore: true,
		GradersOnly: true, Engine: "copilot", NoDryRun: true,
	})
	require.NoError(t, err)
	defer cancelLogsDownload(runtime.timeoutCancel)
	require.Nil(t, runtime.cachedRuns, "Harbor must download artifacts instead of reusing metadata-only summaries")
	require.Same(t, cache, runtime.cachedJSONLCache, "run-discovery cache can still be reused")
	require.True(t, runtime.filters.gradersOnly, "explicit grader filter must be preserved")
	require.True(t, runtime.filters.noDryRun)
	require.Equal(t, "copilot", runtime.filters.engine)
}

func TestLogsHarborDiscoveryExportsFinalSelection(t *testing.T) {
	originalFetch, originalProcess, originalExport := logsFetchWorkflowRunBatch, logsProcessWorkflowRunBatch, logsExportHarborJob
	t.Cleanup(func() {
		logsFetchWorkflowRunBatch, logsProcessWorkflowRunBatch, logsExportHarborJob = originalFetch, originalProcess, originalExport
	})
	installSessionTestGH(t, nil)
	root := t.TempDir()
	logsFetchWorkflowRunBatch = func(_ context.Context, opts LogsDownloadOptions, _ string, _ int, _ bool) (workflowRunBatch, error) {
		require.True(t, opts.Harbor)
		require.False(t, opts.GradersOnly)
		return workflowRunBatch{runs: []WorkflowRun{{DatabaseID: 1}, {DatabaseID: 2}}}, nil
	}
	logsProcessWorkflowRunBatch = func(_ context.Context, _ workflowRunBatch, _ []ProcessedRun, _ processWorkflowRunBatchOptions) ([]ProcessedRun, int, bool, bool, bool) {
		return []ProcessedRun{
			{Run: WorkflowRun{DatabaseID: 1, LogsPath: filepath.Join(root, "run-1")}},
			{Run: WorkflowRun{DatabaseID: 2, LogsPath: filepath.Join(root, "run-2")}},
		}, 2, true, false, false
	}
	for _, exportErr := range []error{nil, errors.New("Harbor write failed")} {
		calls := 0
		logsExportHarborJob = func(_ context.Context, outputDir string, runs []ProcessedRun) error {
			calls++
			require.Equal(t, root, outputDir)
			require.Len(t, runs, 1)
			require.Equal(t, int64(1), runs[0].Run.DatabaseID)
			require.Equal(t, filepath.Join(root, "run-1"), runs[0].Run.LogsPath)
			return exportErr
		}
		err := DownloadWorkflowLogs(t.Context(), LogsDownloadOptions{
			Harbor: true, OutputDir: root, Count: 1, SummaryFile: "summary.json",
			SuppressRender: true, RepoOverride: "owner/repo", ArtifactSets: []string{"usage"},
		})
		require.Equal(t, 1, calls)
		require.ErrorIs(t, err, exportErr)
	}
	require.FileExists(t, filepath.Join(root, "summary.json"), "Harbor must preserve the ordinary report")
}

func TestLogsHarborMultiTargetExportsOnce(t *testing.T) {
	originalCollect, originalExport := collectWorkflowLogsForTarget, logsExportHarborJob
	t.Cleanup(func() {
		collectWorkflowLogsForTarget, logsExportHarborJob = originalCollect, originalExport
	})
	installSessionTestGH(t, nil)
	root := t.TempDir()
	targets := []logsWorkflowTarget{
		{workflowName: "first", repoOverride: "owner/first"},
		{workflowName: "second", repoOverride: "owner/second"},
	}
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		createdAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		if opts.WorkflowName == "second" {
			createdAt = createdAt.Add(time.Hour)
		}
		return workflowLogsResult{processedRuns: []ProcessedRun{{
			Run: WorkflowRun{DatabaseID: 123, WorkflowName: opts.WorkflowName, CreatedAt: createdAt,
				LogsPath: filepath.Join(opts.OutputDir, "run-123")},
		}}}, nil
	}
	for _, count := range []int{1, 2} {
		calls := 0
		var exportErr error
		if count == 2 {
			exportErr = errors.New("Harbor export sentinel")
		}
		logsExportHarborJob = func(_ context.Context, dir string, runs []ProcessedRun) error {
			calls++
			require.Equal(t, root, dir)
			require.Len(t, runs, count)
			require.Equal(t, "second", runs[0].Run.WorkflowName)
			require.Equal(t, filepath.Join(logsTargetOutputDir(root, targets[1]), "run-123"), runs[0].Run.LogsPath)
			if count == 2 {
				require.Equal(t, filepath.Join(logsTargetOutputDir(root, targets[0]), "run-123"), runs[1].Run.LogsPath)
			}
			return exportErr
		}
		err := DownloadWorkflowLogsForTargets(t.Context(), LogsDownloadOptions{
			Harbor: true, OutputDir: root, Count: count, RepoOverride: "owner/repo", ArtifactSets: []string{"usage"},
			SummaryFile: "summary.json", SuppressRender: true,
		}, targets, nil)
		require.ErrorIs(t, err, exportErr)
		require.Equal(t, 1, calls, "one job must include the final combined selection")
	}
	require.FileExists(t, filepath.Join(root, "summary.json"))
}

func TestLogsHarborStdinExportsFilteredRuns(t *testing.T) {
	originalProcess, originalExport := processConcurrentRunDownload, logsExportHarborJob
	t.Cleanup(func() {
		processConcurrentRunDownload, logsExportHarborJob = originalProcess, originalExport
	})
	binDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "gh"), []byte(`#!/bin/sh
case "$*" in
  *actions/runs/123*) echo '{"databaseId":123,"workflowName":"test","workflowPath":".github/workflows/test.lock.yml"}' ;;
  *actions/runs/124*) echo '{"databaseId":124,"workflowName":"test","workflowPath":".github/workflows/test.lock.yml"}' ;;
  *) echo '{}' ;;
esac
`), 0o700))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	processConcurrentRunDownload = func(_ context.Context, run WorkflowRun, params concurrentRunDownloadParams, _ *atomic.Int64, _ *console.ProgressBar) (DownloadResult, error) {
		require.Contains(t, params.artifactFilter, "agent")
		require.Contains(t, params.artifactFilter, "graders")
		require.Contains(t, params.artifactFilter, "experiment")
		result := DownloadResult{RunAnalysis: RunAnalysis{Run: run}, LogsPath: filepath.Join(root, "run-123")}
		if run.DatabaseID == 124 {
			result.Skipped = true
		}
		return result, nil
	}
	for _, exportErr := range []error{nil, errors.New("Harbor stdin export failed")} {
		calls := 0
		logsExportHarborJob = func(_ context.Context, outputDir string, runs []ProcessedRun) error {
			calls++
			require.Equal(t, root, outputDir)
			require.Len(t, runs, 1)
			require.Equal(t, int64(123), runs[0].Run.DatabaseID)
			require.Equal(t, filepath.Join(root, "run-123"), runs[0].Run.LogsPath)
			return exportErr
		}
		_, _ = captureOutput(t, func() error {
			err := DownloadWorkflowLogsFromStdin(t.Context(), StdinLogsOptions{
				Harbor: true, OutputDir: root, RepoOverride: "owner/repo", JSONOutput: true,
				SummaryFile: "summary.json", ArtifactSets: []string{"usage"}, RunURLs: []string{"123", "124"},
			})
			require.ErrorIs(t, err, exportErr)
			return nil
		})
		require.Equal(t, 1, calls)
	}
	require.FileExists(t, filepath.Join(root, "summary.json"))
}

func TestLogsHarborStdinEmptySelectionReplacesStaleJob(t *testing.T) {
	originalExport := logsExportHarborJob
	t.Cleanup(func() { logsExportHarborJob = originalExport })
	installSessionTestGH(t, nil)
	for _, test := range []struct {
		name string
		urls []string
	}{
		{name: "no input"},
		{name: "no resolved runs", urls: []string{"not-a-run"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, originalExport(t.Context(), root, nil))
			stale := filepath.Join(root, "harbor", "stale-trial", "result.json")
			writeSessionTestFile(t, root, filepath.Join("harbor", "stale-trial", "result.json"), `{"stale":true}`)
			calls := 0
			logsExportHarborJob = func(ctx context.Context, dir string, runs []ProcessedRun) error {
				calls++
				require.Equal(t, root, dir)
				require.Empty(t, runs)
				return originalExport(ctx, dir, runs)
			}
			_, _ = captureOutput(t, func() error {
				err := DownloadWorkflowLogsFromStdin(t.Context(), StdinLogsOptions{
					Harbor: true, OutputDir: root, RepoOverride: "owner/repo", RunURLs: test.urls,
				})
				require.NoError(t, err)
				return nil
			})
			require.Equal(t, 1, calls)
			require.NoFileExists(t, stale)
			content, err := os.ReadFile(filepath.Join(root, "harbor", "result.json"))
			require.NoError(t, err)
			var result map[string]any
			require.NoError(t, json.Unmarshal(content, &result))
			require.EqualValues(t, 0, result["n_total_trials"])

			exportErr := errors.New("empty job export failed")
			logsExportHarborJob = func(context.Context, string, []ProcessedRun) error { return exportErr }
			_, _ = captureOutput(t, func() error {
				err := DownloadWorkflowLogsFromStdin(t.Context(), StdinLogsOptions{
					Harbor: true, OutputDir: root, RepoOverride: "owner/repo", RunURLs: test.urls,
				})
				require.ErrorIs(t, err, exportErr)
				return nil
			})
		})
	}
}

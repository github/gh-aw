//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		mu.Lock()
		outputDirs[opts.WorkflowName] = opts.OutputDir
		concurrencyLimits[opts.WorkflowName] = opts.maxConcurrentDownloads
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

	done := make(chan error, 1)
	go func() {
		done <- DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
			OutputDir:      filepath.Join(tempDir, "logs"),
			SummaryFile:    "summary.json",
			ArtifactSets:   []string{"usage"},
			SuppressRender: true,
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

func TestMergeLogsTargetResultsPropagatesCountLimitReached(t *testing.T) {
	processedRuns, _, _, countLimitReached, _, errs := mergeLogsTargetResults([]logsTargetResult{
		{target: logsWorkflowTarget{workflowName: "limited"}, result: workflowLogsResult{countLimitReached: true}},
		{target: logsWorkflowTarget{workflowName: "complete"}, result: workflowLogsResult{countLimitReached: false}},
	}, nil)

	assert.Empty(t, processedRuns)
	assert.True(t, countLimitReached)
	assert.Empty(t, errs)
}

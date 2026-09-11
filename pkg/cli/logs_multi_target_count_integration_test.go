//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogsMultiTargetCountIsSharedMaxAcrossTargets is a dedicated regression
// test for https://github.com/githubnext/gh-aw-cao/actions/runs/34634442971/job/103378818854,
// where `gh aw logs --count 1` combined with multiple workflow targets did not
// enforce --count as a shared ceiling across all targets: each target was
// free to download up to `count` runs of its own, so 3 targets with --count 1
// could end up with 3 downloaded runs instead of 1.
//
// It drives the real multi-target entry point, DownloadWorkflowLogsForTargets,
// with 3 concurrent targets and --count 1. Each target's downloader is faked
// (via the collectWorkflowLogsForTarget indirection point also used by the
// existing multi-target unit tests) to simulate a workflow with plenty of
// history: every target believes it has 5 runs available. Each candidate run
// is only "downloaded" (a marker file written to disk and a ProcessedRun
// recorded) if it is admitted by the *shared* logsCountLimit threaded through
// LogsDownloadOptions.countLimit -- the same gate the real per-run download
// loop (appendProcessedWorkflowRuns) consults. If that shared gate were not
// correctly wired to every target, all 3 targets would each download one run,
// producing 3 downloaded runs for a --count of 1.
func TestLogsMultiTargetCountIsSharedMaxAcrossTargets(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "3")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	var downloadedMu sync.Mutex
	var downloadedRunDirs []string

	const availableRunsForTarget = 5
	collectWorkflowLogsForTarget = func(_ context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		require.NotNil(t, opts.countLimit, "multi-target downloads must share a countLimit across targets")
		require.NoError(t, os.MkdirAll(opts.OutputDir, 0o755))

		var processedRuns []ProcessedRun
		for i := 0; i < availableRunsForTarget; i++ {
			if !opts.countLimit.tryAdd() {
				break
			}
			runDir := filepath.Join(opts.OutputDir, "run-"+opts.WorkflowName+"-"+time.Now().Format("150405.000000000"))
			require.NoError(t, os.MkdirAll(runDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(runDir, "marker.txt"), []byte("downloaded"), 0o600))

			downloadedMu.Lock()
			downloadedRunDirs = append(downloadedRunDirs, runDir)
			downloadedMu.Unlock()

			processedRuns = append(processedRuns, ProcessedRun{
				Run: WorkflowRun{
					DatabaseID:   int64(i + 1),
					WorkflowName: opts.WorkflowName,
					CreatedAt:    time.Now(),
					LogsPath:     runDir,
				},
			})
		}
		return workflowLogsResult{processedRuns: processedRuns}, nil
	}

	outputDir := filepath.Join(tempDir, "logs")
	err = DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
		Count:          1,
		OutputDir:      outputDir,
		SummaryFile:    "summary.json",
		SuppressRender: true,
	}, []logsWorkflowTarget{
		{workflowName: "workflow-a", repoOverride: "org/repo-a"},
		{workflowName: "workflow-b", repoOverride: "org/repo-b"},
		{workflowName: "workflow-c", repoOverride: "org/repo-c"},
	}, nil)
	require.NoError(t, err)

	downloadedMu.Lock()
	totalDownloaded := len(downloadedRunDirs)
	dirs := append([]string(nil), downloadedRunDirs...)
	downloadedMu.Unlock()

	assert.Equal(t, 1, totalDownloaded,
		"--count is a shared maximum across all targets, not a per-target maximum; downloaded run dirs: %v", dirs)

	data, err := os.ReadFile(filepath.Join(outputDir, "summary.json"))
	require.NoError(t, err)
	var report LogsData
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Len(t, report.Runs, 1, "the merged report must also respect --count as a shared max across targets")
}

// TestLogsMultiTargetCancelsSiblingsWhenSharedCountLimitReached proves that
// once one target's tryAdd() calls exhaust the shared --count budget, every
// other concurrently running target is interrupted promptly -- instead of
// running to the end of whatever it is currently doing (e.g. downloading an
// over-fetched batch) before it next happens to check the shared limit
// between iterations. Without the cancellation wired into
// logsCountLimit/collectLogsTargets, a sibling target blocked mid-operation
// would only observe the limit on its own schedule, which this test bounds
// with a long simulated in-flight operation and a tight cancellation deadline.
func TestLogsMultiTargetCancelsSiblingsWhenSharedCountLimitReached(t *testing.T) {
	t.Setenv("GH_AW_MAX_CONCURRENT_DOWNLOADS", "3")
	original := collectWorkflowLogsForTarget
	t.Cleanup(func() { collectWorkflowLogsForTarget = original })

	tempDir := t.TempDir()
	originalDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	const simulatedInFlightDuration = 5 * time.Second
	const maxAcceptableCancelLatency = 1 * time.Second

	limitReached := make(chan struct{})
	elapsed := make(chan time.Duration, 2)

	collectWorkflowLogsForTarget = func(ctx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
		require.NoError(t, os.MkdirAll(opts.OutputDir, 0o755))
		if opts.WorkflowName == "first" {
			// Immediately consume the only slot in the shared budget, then let
			// the other targets know the limit has now been hit.
			require.True(t, opts.countLimit.tryAdd())
			close(limitReached)
			return workflowLogsResult{}, nil
		}

		// Simulate a target that is already mid-operation (e.g. downloading an
		// over-fetched batch) at the moment a sibling exhausts the shared
		// budget. It should be interrupted well before simulatedInFlightDuration
		// elapses instead of running to completion.
		<-limitReached
		start := time.Now()
		select {
		case <-ctx.Done():
		case <-time.After(simulatedInFlightDuration):
		}
		elapsed <- time.Since(start)
		return workflowLogsResult{}, nil
	}

	outputDir := filepath.Join(tempDir, "logs")
	err = DownloadWorkflowLogsForTargets(context.Background(), LogsDownloadOptions{
		Count:          1,
		OutputDir:      outputDir,
		SummaryFile:    "",
		SuppressRender: true,
	}, []logsWorkflowTarget{
		{workflowName: "first", repoOverride: "org/repo-a"},
		{workflowName: "second", repoOverride: "org/repo-b"},
		{workflowName: "third", repoOverride: "org/repo-c"},
	}, nil)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		select {
		case d := <-elapsed:
			assert.Less(t, d, maxAcceptableCancelLatency,
				"sibling target should be canceled promptly once the shared --count budget is exhausted, not run to completion")
		case <-time.After(simulatedInFlightDuration + maxAcceptableCancelLatency):
			t.Fatal("timed out waiting for sibling target to report its elapsed cancellation latency")
		}
	}
}

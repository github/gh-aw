//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsOutcomesPrettyUsesStderr(t *testing.T) {
	dir := t.TempDir()
	run := ProcessedRun{Run: WorkflowRun{Attempt: 1, DatabaseID: 42, WorkflowName: "producer", LogsPath: dir}}
	stdout, stderr := captureOutput(t, func() error {
		return renderLogsOutput([]ProcessedRun{run}, renderLogsOutputOptions{
			outputDir: dir,
			format:    "pretty",
			outcomeReports: []OutcomesData{{
				RunID: 42, Workflow: "producer",
				Summary: OutcomeSummary{Accepted: 1, Total: 1},
			}},
		})
	})
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Cross-Run Analysis")
	assert.Contains(t, stderr, "Adoption for producer (run 42): 1 accepted")
}

func TestMultiTargetOutcomesPreserveTimeoutResultsAndCallerCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "download deadline"
		if canceled {
			name = "caller cancellation"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			t.Setenv("GH_AW_ADOPTION_DIR", "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var downloadErr error
			originalCollector := collectWorkflowLogsForTarget
			originalRateLimit := fetchRateLimitForHostFunc
			t.Cleanup(func() {
				collectWorkflowLogsForTarget = originalCollector
				fetchRateLimitForHostFunc = originalRateLimit
			})
			fetchRateLimitForHostFunc = func(context.Context, string) (rateLimitResource, error) {
				return rateLimitResource{Limit: 5000, Remaining: 5000}, nil
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), nil, 0o600))
			collectWorkflowLogsForTarget = func(downloadCtx context.Context, opts LogsDownloadOptions) (workflowLogsResult, error) {
				if canceled {
					cancel()
				} else {
					<-downloadCtx.Done()
					downloadErr = downloadCtx.Err()
				}
				return workflowLogsResult{
					processedRuns: []ProcessedRun{{Run: WorkflowRun{Attempt: 1,
						DatabaseID: 42, WorkflowName: opts.WorkflowName,
						Repository: opts.RepoOverride, LogsPath: dir, CreatedAt: time.Now(),
					}}},
					timeoutReached: !canceled,
					continuation:   &ContinuationData{WorkflowName: opts.WorkflowName, BeforeRunID: 41},
				}, nil
			}
			var err error
			stdout, _ := captureOutput(t, func() error {
				err = DownloadWorkflowLogsForTargets(ctx, LogsDownloadOptions{
					OutputDir: dir, SummaryFile: "summary.json", Outcomes: true,
					TimeoutMinutes: 1, TimeoutSeconds: 1, JSONOutput: true,
				}, []logsWorkflowTarget{{workflowName: "producer", repoOverride: "org/repo"}}, nil)
				return nil
			})
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
				_, statErr := os.Stat(filepath.Join(dir, "summary.json"))
				assert.True(t, os.IsNotExist(statErr))
				return
			}
			require.NoError(t, err)
			require.ErrorIs(t, downloadErr, context.DeadlineExceeded)
			data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
			require.NoError(t, err)
			var report LogsData
			require.NoError(t, json.Unmarshal(data, &report))
			var rendered LogsData
			require.NoError(t, json.Unmarshal([]byte(stdout), &rendered))
			assert.Equal(t, report.Outcomes, rendered.Outcomes)
			assert.Equal(t, report.Continuations, rendered.Continuations)
			require.Len(t, rendered.Runs, 1)
			assert.Equal(t, int64(42), rendered.Runs[0].RunID)
			require.Len(t, report.Runs, 1)
			assert.Equal(t, int64(42), report.Runs[0].RunID)
			require.Len(t, report.Outcomes, 1)
			assert.Equal(t, int64(42), report.Outcomes[0].RunID)
			assert.Empty(t, report.Outcomes[0].EvalError)
			require.Len(t, report.Continuations, 1)
			assert.Equal(t, "org/repo", report.Continuations[0].Repository)
			assert.Equal(t, int64(41), report.Continuations[0].BeforeRunID)
		})
	}
}

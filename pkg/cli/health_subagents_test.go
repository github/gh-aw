//go:build !integration

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func healthSubagentFixture(t *testing.T) ([]WorkflowRun, HealthConfig) {
	t.Helper()
	runs, collection := healthRevisionFixture()
	for i := range runs {
		runs[i].URL = fmt.Sprintf("https://github.com/org/repo/actions/runs/%d", runs[i].DatabaseID)
	}
	return runs, HealthConfig{WorkflowName: "Example", Threshold: 80, Days: 7,
		RepoOverride: "org/repo", LogsDir: t.TempDir(), collection: collection}
}

func cacheHealthSubagentFailure(t *testing.T, run WorkflowRun, config HealthConfig) *RunSummary {
	t.Helper()
	request := SubagentModelRequest{AgentName: "report-formatter", RequestedModel: "kiwi",
		InvocationCount: 2, FailedCount: 2, ReasonCode: modelMismatchReasonSubagentFailed,
		Error: "400 Model \"kiwi\" has no AI credits pricing; private diagnostic detail"}
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: run.DatabaseID, RunAnalysis: RunAnalysis{
		Run: run, TokenUsage: &TokenUsageSummary{
			SubagentModelRequests:  []SubagentModelRequest{request},
			DeclaredSubagentModels: []SubagentModelRequest{request},
		},
	}}
	dir := filepath.Join(config.LogsDir, fmt.Sprintf("run-%d", run.DatabaseID))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, saveRunSummary(dir, summary, false))
	return summary
}

func TestHealthSubagentFailureDegradesGreenCurrentRevision(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	cacheHealthSubagentFailure(t, runs[9], config)
	health := calculateCurrentWorkflowHealth("Example", append(runs, runs[9]), config)
	assert.Equal(t, "subagent_failures_observed", health.Status)
	assert.Equal(t, 5, health.SuccessCount)
	assert.Zero(t, health.FailureCount, "Actions conclusions must remain unchanged")
	assert.InDelta(t, 100, health.SuccessRate, 1e-9)
	assert.False(t, health.BelowThresh)
	assert.Equal(t, 2, health.SubagentFailureCount, "declared models and duplicate runs must not double count")
	assert.Equal(t, 1, health.SubagentFailureRuns)
	require.Len(t, health.SubagentFailures, 1)
	assert.Equal(t, HealthSubagentFailure{RunID: 10, URL: runs[9].URL,
		AgentName: "report-formatter", RequestedModel: "kiwi", FailedCount: 2}, health.SubagentFailures[0])
	require.Len(t, health.Revisions, 1)
	assert.Equal(t, "subagent_failures_observed", health.Revisions[0].Status)
	assert.Equal(t, 2, health.Revisions[0].Current.SubagentFailures)

	summary := CalculateHealthSummary([]WorkflowHealth{health}, "current", 80)
	assert.Zero(t, summary.HealthyWorkflows)
	assert.Zero(t, summary.BelowThreshold)
	assert.Equal(t, 1, summary.DegradedWorkflows)
	assert.Equal(t, 2, summary.SubagentFailureCount)
	assert.Equal(t, 1, summary.SubagentFailureRuns)
}

func TestHealthSubagentFailureOutput(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	cacheHealthSubagentFailure(t, runs[9], config)
	{
		config.JSONOutput = true
		output := captureAuditGroupStdout(t, func() {
			require.NoError(t, displayDetailedHealth(runs, config))
		})
		var report struct {
			SubagentFailureCount int `json:"subagent_failure_count"`
			SubagentFailureRuns  int `json:"subagent_failure_runs"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &report))
		assert.Equal(t, 2, report.SubagentFailureCount)
		assert.Equal(t, 1, report.SubagentFailureRuns)
		assert.Contains(t, output, `"requested_model": "kiwi"`)
		assert.Contains(t, output, runs[9].URL)
		assert.NotContains(t, output, "private diagnostic detail", "raw errors may contain sensitive data")
	}
	config.JSONOutput = false
	output := captureAuditGroupStderr(t, func() {
		require.NoError(t, displayDetailedHealth(runs, config))
	})
	assert.Contains(t, output, "[subagent_failures]")
	assert.Contains(t, output, `agent="report-formatter" requested_model="kiwi"`)
	assert.Contains(t, output, "sub-agent invocation(s) failed")
	assert.Contains(t, output, "Actions success rate does not establish healthy delegation")
	assert.NotContains(t, output, "All evaluated workflows above")
	assert.NotContains(t, output, "is above threshold")
	assert.NotContains(t, output, "private diagnostic detail")
}

func TestHealthSubagentFailuresIgnoreHistoricalAndUnexecutedRuns(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	cacheHealthSubagentFailure(t, runs[4], config)
	unexecuted := runs[9]
	unexecuted.DatabaseID = 11
	unexecuted.Conclusion = "action_required"
	unexecuted.CreatedAt = unexecuted.CreatedAt.Add(time.Hour)
	cacheHealthSubagentFailure(t, unexecuted, config)
	health := calculateCurrentWorkflowHealth("Example", append(runs, unexecuted), config)
	assert.Equal(t, "recovery_observed", health.Status)
	assert.Zero(t, health.SubagentFailureCount)
	assert.Empty(t, health.SubagentFailures)
}

func TestHealthSubagentFailuresRejectStaleCache(t *testing.T) {
	for _, field := range []string{"run-id", "summary-id", "url", "repository", "attempt", "revision", "workflow", "updated", "status", "conclusion", "version"} {
		t.Run(field, func(t *testing.T) {
			runs, config := healthSubagentFixture(t)
			summary := cacheHealthSubagentFailure(t, runs[9], config)
			switch field {
			case "run-id":
				summary.Run.DatabaseID++
			case "summary-id":
				summary.RunID++
			case "url":
				summary.Run.URL = "https://github.example.com/org/repo/actions/runs/10"
			case "repository":
				summary.Run.Repository = "other/repo"
			case "attempt":
				summary.Run.Attempt++
			case "revision":
				summary.Run.HeadSha = "other"
			case "workflow":
				summary.Run.WorkflowPath = ".github/workflows/other.lock.yml"
			case "updated":
				summary.Run.UpdatedAt = summary.Run.UpdatedAt.Add(time.Second)
			case "status":
				summary.Run.Status = "in_progress"
			case "conclusion":
				summary.Run.Conclusion = "failure"
			case "version":
				summary.CLIVersion = "old"
			}
			require.NoError(t, saveRunSummary(filepath.Join(config.LogsDir, "run-10"), summary, false))
			health := calculateCurrentWorkflowHealth("Example", runs, config)
			assert.Equal(t, "recovery_observed", health.Status)
			assert.Zero(t, health.SubagentFailureCount)
		})
	}
}

func TestHealthSubagentFailuresMissingCachePreservesMetrics(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	expected := calculateCurrentWorkflowHealth("Example", runs, config)
	summary := cacheHealthSubagentFailure(t, runs[9], config)
	summary.TokenUsage = nil
	require.NoError(t, saveRunSummary(filepath.Join(config.LogsDir, "run-10"), summary, false))
	assert.Equal(t, expected, calculateCurrentWorkflowHealth("Example", runs, config))
	require.NoError(t, os.WriteFile(filepath.Join(config.LogsDir, "run-10", runSummaryFileName), []byte("{"), 0o600))
	assert.Equal(t, expected, calculateCurrentWorkflowHealth("Example", runs, config))
}

func TestHealthSubagentFailuresKeepIntentionalFailuresOutOfFleetCounts(t *testing.T) {
	health := WorkflowHealth{IntentionalFailure: true, Status: "subagent_failures_observed",
		SuccessRate: 100, SubagentFailureCount: 2, SubagentFailureRuns: 1}
	summary := CalculateHealthSummary([]WorkflowHealth{health}, "current", 80)
	assert.Zero(t, summary.HealthyWorkflows)
	assert.Zero(t, summary.DegradedWorkflows)
	assert.Zero(t, summary.SubagentFailureCount)
	assert.Zero(t, summary.SubagentFailureRuns)
}

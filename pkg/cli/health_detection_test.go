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

func cacheHealthDetectionResult(t *testing.T, run WorkflowRun, config HealthConfig, data string) *RunSummary {
	t.Helper()
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: run.DatabaseID, RunAnalysis: RunAnalysis{Run: run}}
	dir := filepath.Join(config.LogsDir, fmt.Sprintf("run-%d", run.DatabaseID))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "usage"), 0o700))
	require.NoError(t, saveRunSummary(dir, summary, false))
	content := unifiedDetectionTestHeader + unifiedDetectionTestEvent(data)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "usage", "aw_session.jsonl"), []byte(content), 0o600))
	return summary
}

func TestHealthDetectionFailuresDegradeSuccessfulActions(t *testing.T) {
	for _, tt := range []struct {
		name  string
		data  string
		count int
		title string
	}{
		{"warning", `{"jobResult":"success","conclusion":"warning","reason":"parse_error","reasons":["PRIVATE_DETAIL"]}`, 1, "Threat Detection Warning"},
		{"threat", `{"jobResult":"success","conclusion":"warning","reason":"threat_detected","promptInjection":true,"secretLeak":false,"maliciousPatch":true,"reasons":["PRIVATE_DETAIL"]}`, 2, "Threat Detection Warning"},
		{"engine failure", `{"jobResult":"failure","conclusion":"failure","reason":"agent_failure"}`, 1, "Threat Detection Job Failed"},
		{"verdict only", `{"promptInjection":false,"secretLeak":true,"maliciousPatch":false}`, 1, "Security Threat Detected"},
		{"clean", `{"jobResult":"success","conclusion":"success","promptInjection":false,"secretLeak":false,"maliciousPatch":false}`, 0, ""},
		{"skipped", `{"jobResult":"skipped","conclusion":"skipped"}`, 0, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runs, config := healthSubagentFixture(t)
			summary := cacheHealthDetectionResult(t, runs[9], config, tt.data)
			// A cached run's original path is not necessarily the directory used by health.
			summary.Run.LogsPath = "nonexistent-old-path"
			require.NoError(t, saveRunSummary(filepath.Join(config.LogsDir, "run-10"), summary, false))
			health := calculateCurrentWorkflowHealth("Example", append(runs, runs[9]), config)
			assert.Equal(t, 5, health.SuccessCount)
			assert.Zero(t, health.FailureCount)
			assert.InDelta(t, 100, health.SuccessRate, 1e-9)
			assert.False(t, health.BelowThresh)
			require.Len(t, health.DetectionFailures, tt.count)
			if tt.count == 0 {
				assert.Equal(t, "recovery_observed", health.Status)
				assert.Zero(t, health.DetectionFailureRuns)
				return
			}
			assert.Equal(t, "detection_failures_observed", health.Status)
			assert.Equal(t, 1, health.DetectionFailureRuns, "multiple findings and duplicate runs count once")
			assert.Equal(t, tt.title, health.DetectionFailures[0].Title)
			assert.Equal(t, runs[9].DatabaseID, health.DetectionFailures[0].RunID)
			assert.Equal(t, runs[9].URL, health.DetectionFailures[0].URL)
			require.Len(t, health.Revisions, 1)
			assert.Equal(t, "detection_failures_observed", health.Revisions[0].Status)
			assert.Equal(t, 1, health.Revisions[0].Current.DetectionFailureRuns)
			serialized, err := json.Marshal(health)
			require.NoError(t, err)
			assert.NotContains(t, string(serialized), "PRIVATE_DETAIL")
		})
	}
}

func TestHealthDetectionFailuresIgnoreHistoricalAndUnexecutedRuns(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	data := `{"jobResult":"success","conclusion":"warning","reason":"threat_detected"}`
	cacheHealthDetectionResult(t, runs[4], config, data)
	unexecuted := runs[9]
	unexecuted.DatabaseID = 11
	unexecuted.Conclusion = "action_required"
	unexecuted.CreatedAt = unexecuted.CreatedAt.Add(time.Hour)
	cacheHealthDetectionResult(t, unexecuted, config, data)
	health := calculateCurrentWorkflowHealth("Example", append(runs, unexecuted), config)
	assert.Equal(t, "recovery_observed", health.Status)
	assert.Zero(t, health.DetectionFailureRuns)
	assert.Empty(t, health.DetectionFailures)
}

func TestHealthDetectionFailuresFromHistoricalConclusionLogs(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	cacheHealthDetectionResult(t, runs[9], config, `{"jobResult":"success","conclusion":"success"}`)
	dir := filepath.Join(config.LogsDir, "run-10")
	// Older unified sessions lack detection.result, but runner logs retain the outcome.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "usage", "aw_session.jsonl"), []byte(unifiedDetectionTestHeader), 0o600))
	writeConclusionDetectionLog(t, dir, "1_conclusion.txt", conclusionDetectionWarningLog)
	health := calculateCurrentWorkflowHealth("Example", runs, config)
	assert.Equal(t, "detection_failures_observed", health.Status)
	assert.InDelta(t, 100, health.SuccessRate, 1e-9)
	require.Len(t, health.DetectionFailures, 1)
	assert.Equal(t, "Threat Detection Warning", health.DetectionFailures[0].Title)
	assert.Contains(t, health.DetectionFailures[0].Description, "parse_error")
}

func TestHealthDetectionFailuresUseRawJobs(t *testing.T) {
	for _, source := range []string{"missing cache", "matching cache without jobs", "incomplete jobs", "stale cache", "old version"} {
		t.Run(source, func(t *testing.T) {
			runs, config := healthSubagentFixture(t)
			jobs := []JobInfoWithDuration{{JobInfo: JobInfo{Name: "detection", Conclusion: "failure"}}}
			config.collection.jobs = map[healthRunAttempt]*healthJobs{
				{id: runs[9].DatabaseID, attempt: runs[9].Attempt}: {details: jobs, complete: source != "incomplete jobs"},
			}
			if source == "matching cache without jobs" {
				cacheHealthDetectionResult(t, runs[9], config, `{"jobResult":"success","conclusion":"success"}`)
			}
			if source == "old version" || source == "stale cache" {
				summary := cacheHealthDetectionResult(t, runs[9], config, `{"jobResult":"success","conclusion":"warning","reason":"threat_detected"}`)
				if source == "old version" {
					summary.CLIVersion = "old"
					summary.JobDetails = jobs
				} else {
					summary.Run.Attempt++
				}
				require.NoError(t, saveRunSummary(filepath.Join(config.LogsDir, "run-10"), summary, false))
			}
			health := calculateCurrentWorkflowHealth("Example", runs, config)
			if source == "incomplete jobs" {
				assert.Empty(t, health.DetectionFailures)
				return
			}
			assert.Equal(t, 1, health.DetectionFailureRuns)
			require.Len(t, health.DetectionFailures, 1, "incompatible cache artifacts must not add threats")
			assert.Equal(t, AuditFindingDetectionJobFailed, health.DetectionFailures[0].Code)
			assert.Equal(t, "Threat Detection Job Failed", health.DetectionFailures[0].Title)
			assert.InDelta(t, 100, health.SuccessRate, 1e-9)
		})
	}
}

func TestHealthDetectionFailureOutput(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	detection := cacheHealthDetectionResult(t, runs[9], config, `{"jobResult":"success","conclusion":"warning","reason":"threat_detected","promptInjection":true,"secretLeak":false,"maliciousPatch":false,"reasons":["PRIVATE_DETAIL"]}`)
	subagents := cacheHealthSubagentFailure(t, runs[9], config)
	detection.TokenUsage = subagents.TokenUsage
	require.NoError(t, saveRunSummary(filepath.Join(config.LogsDir, "run-10"), detection, false))
	for _, detailed := range []bool{false, true} {
		config.JSONOutput = true
		output := captureAuditGroupStdout(t, func() {
			if detailed {
				require.NoError(t, displayDetailedHealth(runs, config))
			} else {
				require.NoError(t, displayHealthSummary(runs, config))
			}
		})
		var report struct {
			DetectionFailureRuns int `json:"detection_failure_runs"`
		}
		require.NoError(t, json.Unmarshal([]byte(output), &report))
		assert.Equal(t, 1, report.DetectionFailureRuns)
		assert.Contains(t, output, `"detection_failures"`)
		assert.Contains(t, output, `"subagent_failures_observed"`)
		assert.Contains(t, output, string(AuditFindingThreatDetected))
		assert.Contains(t, output, runs[9].URL)
		assert.NotContains(t, output, "PRIVATE_DETAIL")
	}
	config.JSONOutput = false
	output := captureAuditGroupStderr(t, func() {
		require.NoError(t, displayHealthSummary(runs, config))
		require.NoError(t, displayDetailedHealth(runs, config))
	})
	assert.Contains(t, output, "[detection_failures]")
	assert.Contains(t, output, "Threat Detection Warning")
	assert.Contains(t, output, "failed sub-agent invocations")
	assert.Contains(t, output, "prompt injection")
	assert.Contains(t, output, runs[9].URL)
	assert.Contains(t, output, "Actions success rate does not establish successful threat detection")
	assert.NotContains(t, output, "All evaluated workflows above")
	assert.NotContains(t, output, "is above threshold")
	assert.NotContains(t, output, "PRIVATE_DETAIL")
}

func TestHealthDetectionFailureRollups(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	subagents := cacheHealthSubagentFailure(t, runs[9], config)
	detection := cacheHealthDetectionResult(t, runs[9], config, `{"jobResult":"success","conclusion":"warning","reason":"threat_detected"}`)
	detection.TokenUsage = subagents.TokenUsage
	require.NoError(t, saveRunSummary(filepath.Join(config.LogsDir, "run-10"), detection, false))
	health := calculateCurrentWorkflowHealth("Example", runs, config)
	assert.Equal(t, 2, health.SubagentFailureCount)
	assert.Equal(t, 1, health.DetectionFailureRuns)
	assert.Equal(t, "subagent_failures_observed", health.Status)
	require.Len(t, health.Revisions, 1)
	assert.Equal(t, "subagent_failures_observed", health.Revisions[0].Status)
	summary := CalculateHealthSummary([]WorkflowHealth{health}, "current", 80)
	assert.Zero(t, summary.HealthyWorkflows)
	assert.Zero(t, summary.BelowThreshold)
	assert.Zero(t, summary.ObservedFailureWorkflows)
	assert.Equal(t, 1, summary.DegradedWorkflows, "each workflow is counted once across failure types")
	assert.Equal(t, 1, summary.DetectionFailureRuns)
	assert.Equal(t, 2, summary.SubagentFailureCount)
	health.IntentionalFailure = true
	summary = CalculateHealthSummary([]WorkflowHealth{health}, "current", 80)
	assert.Zero(t, summary.DegradedWorkflows)
	assert.Zero(t, summary.DetectionFailureRuns)
	assert.Zero(t, summary.SubagentFailureCount)
	assert.Zero(t, summary.HealthyWorkflows)
	require.Len(t, summary.Workflows[0].DetectionFailures, 2, "intentional failures remain inspectable")
}

func TestHealthDetectionFailuresPreserveInsufficientEvidence(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	run := runs[9]
	cacheHealthDetectionResult(t, run, config, `{"jobResult":"success","conclusion":"warning","reason":"parse_error"}`)
	health := WorkflowHealth{
		WorkflowName: "Example",
		Status:       "insufficient_evidence",
		SuccessRate:  100,
		BelowThresh:  true,
		Revisions: []HealthRevisionStatus{{
			Status:  "insufficient_evidence",
			Current: &HealthRevisionCohort{RunIDs: []int64{run.DatabaseID}},
		}},
	}

	applyHealthDetectionFailures(&health, []WorkflowRun{run}, config)

	assert.Equal(t, "insufficient_evidence", health.Status)
	assert.Equal(t, 1, health.DetectionFailureRuns)
	require.Len(t, health.Revisions, 1)
	assert.Equal(t, "insufficient_evidence", health.Revisions[0].Status)
	assert.Equal(t, 1, health.Revisions[0].Current.DetectionFailureRuns)
	summary := CalculateHealthSummary([]WorkflowHealth{health}, "current", 80)
	assert.Equal(t, 1, summary.InsufficientWorkflows)
	assert.Zero(t, summary.HealthyWorkflows)
	assert.Zero(t, summary.BelowThreshold)
}

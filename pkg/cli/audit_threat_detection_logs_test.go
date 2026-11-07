package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const conclusionDetectionWarningLog = `2026-11-07T11:28:43.0000000Z ##[group]Run actions/github-script@sha
2026-11-07T11:28:43.0000000Z env:
2026-11-07T11:28:43.0000000Z   GH_AW_DETECTION_JOB_RESULT: success
2026-11-07T11:28:43.0000000Z   GH_AW_DETECTION_CONCLUSION: warning
2026-11-07T11:28:43.0000000Z   GH_AW_DETECTION_REASON: parse_error
2026-11-07T11:28:43.0000000Z ##[endgroup]
`

func writeConclusionDetectionLog(t *testing.T, runDir, name, content string) {
	t.Helper()
	dir := filepath.Join(runDir, "workflow-logs")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestAuditReportsHistoricalDetectionParseWarning(t *testing.T) {
	t.Parallel()
	runDir := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader)
	writeConclusionDetectionLog(t, runDir, "1_conclusion.txt", conclusionDetectionWarningLog)
	systemDir := filepath.Join(runDir, "workflow-logs", "conclusion")
	require.NoError(t, os.MkdirAll(systemDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(systemDir, "system.txt"), []byte("Runner system log\n"), 0o600))
	run := WorkflowRun{DatabaseID: 42367827117, LogsPath: runDir, Conclusion: "success"}
	data, _ := buildLocalAuditData(ProcessedRun{Run: run}, LogMetrics{}, nil)
	assert.Equal(t, "success", data.Overview.Conclusion)
	require.Len(t, data.Warnings, 1)
	assert.Contains(t, data.Warnings[0].Message, "parse_error")
	assert.Contains(t, data.Warnings[0].Message, "not a security finding")
	assert.Equal(t, 1, data.Metrics.WarningCount)
	assert.Empty(t, data.Errors)
	var found bool
	for _, finding := range data.KeyFindings {
		assert.NotEqual(t, AuditFindingWorkflowSucceeded, finding.Code)
		assert.NotEqual(t, AuditFindingThreatDetected, finding.Code)
		if finding.Code == AuditFindingDetectionJobFailed {
			found = true
			assert.Equal(t, "tooling", finding.Category)
		}
	}
	assert.True(t, found)
	require.NotEmpty(t, data.Recommendations)
	assert.Contains(t, data.Recommendations[0].Action, "detection")
}

func TestConclusionDetectionUsageResultRequiresRunnerEvidence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, filename, content string
	}{
		{"wrong job", "2_agent.txt", conclusionDetectionWarningLog},
		{"outside header", "1_conclusion.txt", "  GH_AW_DETECTION_JOB_RESULT: success\n  GH_AW_DETECTION_CONCLUSION: warning\n"},
		{"script text", "1_conclusion.txt", "##[group]Run echo hello\n  GH_AW_DETECTION_JOB_RESULT: success\n  GH_AW_DETECTION_CONCLUSION: warning\n##[endgroup]\n"},
		{"invalid enum", "1_conclusion.txt", "##[group]Run script\nenv:\n  GH_AW_DETECTION_JOB_RESULT: success\n  GH_AW_DETECTION_CONCLUSION: PRIVATE_DETAIL\n##[endgroup]\n"},
		{"incomplete block", "1_conclusion.txt", "##[group]Run script\nenv:\n  GH_AW_DETECTION_JOB_RESULT: success\n##[endgroup]\n"},
		{"conflicting blocks", "1_conclusion.txt", conclusionDetectionWarningLog + "##[group]Run script\nenv:\n  GH_AW_DETECTION_JOB_RESULT: success\n  GH_AW_DETECTION_CONCLUSION: success\n##[endgroup]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeConclusionDetectionLog(t, dir, tc.filename, tc.content)
			_, found := readConclusionDetectionUsageResult(dir)
			assert.False(t, found)
		})
	}
}

func TestUnifiedDetectionOutcomeOverridesConclusionLogs(t *testing.T) {
	t.Parallel()
	dir := writeUnifiedDetectionTestSession(t, unifiedDetectionTestHeader+unifiedDetectionTestEvent(`{"jobResult":"success","conclusion":"success","reason":""}`))
	writeConclusionDetectionLog(t, dir, "1_conclusion.txt", conclusionDetectionWarningLog)
	assert.Empty(t, generateThreatDetectionFindings(ProcessedRun{Run: WorkflowRun{LogsPath: dir}}))
}

func TestConclusionDetectionUsageResultFromReusableWorkflow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeConclusionDetectionLog(t, dir, "1_caller _ conclusion.txt", conclusionDetectionWarningLog)
	result, found := readConclusionDetectionUsageResult(dir)
	require.True(t, found)
	assert.Equal(t, "parse_error", result.Reason)
	writeConclusionDetectionLog(t, dir, "2_other _ conclusion.txt", conclusionDetectionWarningLog)
	_, found = readConclusionDetectionUsageResult(dir)
	assert.False(t, found, "do not merge separate invocation outcomes")
}

func TestAuditDetectionIssuesDoNotDoubleCountLogErrors(t *testing.T) {
	t.Parallel()
	data := AuditData{Metrics: MetricsData{ErrorCount: 3}, KeyFindings: []AuditFinding{{
		Code: AuditFindingDetectionJobFailed, Title: "Threat Detection Job Failed", Description: "Detection failed",
	}}}
	appendAuditDetectionIssues(&data)
	assert.Equal(t, 3, data.Metrics.ErrorCount)
	require.Len(t, data.Errors, 1)
}

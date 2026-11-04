package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const protectionSessionHeader = `{"type":"session.format","data":{"version":1},"provenance":{"component":"collector"}}` + "\n"

func protectionSessionRecord(t *testing.T, eventType, component, phase, status string, fallback, staged bool) string {
	t.Helper()
	record := map[string]any{
		"type":       eventType,
		"provenance": map[string]any{"component": component, "phase": phase, "path": "safe-output-outcomes.jsonl:1"},
		"data": map[string]any{
			"type": "push_to_pull_request_branch", "status": status, "fallback_used": fallback, "staged": staged,
			"target_pull_request_number": 123, "url": "https://github.com/org/repo/issues/456",
			"file_protection": map[string]any{"action": "fallback", "source": "protected", "files": []string{".github/***.json", ".github/workflows/test.md", ".github/***.json"}},
		},
	}
	data, err := json.Marshal(record)
	require.NoError(t, err)
	return string(data) + "\n"
}

func TestAuditProtectedFilesDeduplicatesExecutionEvidence(t *testing.T) {
	session := protectionSessionHeader +
		protectionSessionRecord(t, "safe_output.request", "safe_output", "safe_outputs", "success", true, false) +
		protectionSessionRecord(t, "safe_output.result", "agent", "agent", "success", true, false) +
		protectionSessionRecord(t, "safe_output.result", "safe_output", "agent", "success", true, false) +
		protectionSessionRecord(t, "safe_output.result", "safe_output", "safe_outputs", "success", true, false) +
		protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", "success", true, false)
	detections, partial, err := parseAuditProtectedFiles(strings.NewReader(session), 42)
	require.NoError(t, err)
	require.False(t, partial)
	require.Len(t, detections, 1)
	assert.True(t, detections[0].FallbackUsed)
	assert.Equal(t, int64(123), detections[0].TargetPullRequestNumber)
	assert.Equal(t, []string{".github/***.json", ".github/workflows/test.md"}, detections[0].Files)
	assert.Equal(t, int64(42), detections[0].RunID)
}

func TestAuditProtectedFilesPublicationStatus(t *testing.T) {
	for _, status := range []string{"success", "warning", "failed", "skipped", "staged"} {
		t.Run(status, func(t *testing.T) {
			session := protectionSessionHeader + protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", status, true, status == "staged")
			detections, _, err := parseAuditProtectedFiles(strings.NewReader(session), 42)
			require.NoError(t, err)
			require.Len(t, detections, 1)
			assert.Equal(t, status == "success" || status == "warning", detections[0].FallbackUsed)
		})
	}
}

func TestAuditProtectedFilesInvalidEvidence(t *testing.T) {
	valid := protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", "success", true, false)
	for name, session := range map[string]string{
		"missing header":   valid,
		"malformed":        protectionSessionHeader + "{bad",
		"parent traversal": protectionSessionHeader + strings.ReplaceAll(valid, ".github/workflows/test.md", "../test.md"),
		"absolute":         protectionSessionHeader + strings.ReplaceAll(valid, ".github/workflows/test.md", "/test.md"),
	} {
		t.Run(name, func(t *testing.T) {
			_, partial, err := parseAuditProtectedFiles(strings.NewReader(session), 1)
			require.Error(t, err)
			assert.True(t, partial)
			assert.NotContains(t, err.Error(), "../test.md")
		})
	}
	detections, partial, err := parseAuditProtectedFiles(strings.NewReader(protectionSessionHeader+valid+"{bad"), 1)
	require.Error(t, err)
	assert.True(t, partial)
	assert.Len(t, detections, 1)
}

func TestAuditProtectedFilesMissingAndLegacyEvidence(t *testing.T) {
	run := WorkflowRun{DatabaseID: 7, Repository: "org/repo", WorkflowPath: ".github/workflows/pr-finisher.lock.yml", WorkflowName: "Finisher PR #1", LogsPath: t.TempDir()}
	report := readAuditProtectedFiles(run)
	assert.Equal(t, []int64{7}, report.UnavailableRuns)
	require.NoError(t, os.WriteFile(filepath.Join(run.LogsPath, "aw_session.jsonl"), []byte(protectionSessionHeader), 0o600))
	report = readAuditProtectedFiles(run)
	assert.Equal(t, []int64{7}, report.UnavailableRuns)
	assert.False(t, report.Partial)
	require.NoError(t, os.WriteFile(filepath.Join(run.LogsPath, "aw_session.jsonl"), []byte(protectionSessionHeader+protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", "success", true, false)), 0o600))
	report = readAuditProtectedFiles(run)
	assert.Empty(t, report.UnavailableRuns)
	assert.Len(t, report.Files, 2)
}

func TestAuditProtectedFilesWorkflowAggregationAndRendering(t *testing.T) {
	reports := []*AuditProtectedFiles{
		{Workflow: "Finisher PR #1", WorkflowPath: ".github/workflows/finisher.lock.yml", Repository: "org/repo", Files: []string{".github/***.json"}, Detections: []AuditProtectedFileDetection{{RunID: 1}}},
		{Workflow: "Finisher PR #2", WorkflowPath: ".github/workflows/finisher.lock.yml", Repository: "org/repo", Files: []string{".github/***.json"}, UnavailableRuns: []int64{2}, Partial: true},
		{Workflow: "Finisher PR #3", WorkflowPath: ".github/workflows/finisher.lock.yml", Repository: "other/repo", Files: []string{"CODEOWNERS"}},
	}
	merged := mergeAuditProtectedFiles(reports)
	require.Len(t, merged, 2)
	assert.Equal(t, []string{".github/***.json"}, merged[0].Files)
	assert.True(t, merged[0].Partial)
	assert.Equal(t, []int64{2}, merged[0].UnavailableRuns)
	for _, markdown := range []bool{false, true} {
		var output bytes.Buffer
		renderAuditProtectedFiles(&output, merged, markdown)
		assert.Contains(t, output.String(), "absence is not proof")
		assert.Contains(t, output.String(), ".github/")
		assert.NotContains(t, output.String(), "opaque-secret")
	}
	data, err := json.Marshal(merged)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "opaque-secret")
}

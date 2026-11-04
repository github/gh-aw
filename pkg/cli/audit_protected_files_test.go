package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
		assert.Contains(t, output.String(), "Protected-file audit evidence is incomplete")
		assert.NotContains(t, output.String(), "opaque-secret")
	}
	data, err := json.Marshal(merged)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "opaque-secret")
}

func TestAuditProtectedFilesCacheOnlyReplay(t *testing.T) {
	run := WorkflowRun{DatabaseID: 42, WorkflowName: "test"}
	outputDir := t.TempDir()
	detections, partial, err := parseAuditProtectedFiles(strings.NewReader(
		protectionSessionHeader+protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", "success", true, false)), run.DatabaseID)
	require.NoError(t, err)
	require.False(t, partial)
	protection := &AuditProtectedFiles{
		Workflow: "test", Files: detections[0].Files, Detections: detections,
	}
	for _, incomplete := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "partial"}[incomplete], func(t *testing.T) {
			replay := *protection
			replay.Partial = incomplete
			cached := AuditData{SchemaVersion: auditSchemaVersion, ProtectedFiles: &replay}
			processed := processedRunFromCachedData(RunData{RunID: run.DatabaseID, WorkflowName: run.WorkflowName}, &cached, outputDir)

			inputs := processedRunsToCrossRunInputs([]ProcessedRun{processed})
			require.Len(t, inputs, 1)
			assert.Equal(t, &replay, inputs[0].ProtectedFiles)
			report := buildCrossRunAuditReport(inputs)
			require.Len(t, report.ProtectedFiles, 1)
			assert.Equal(t, replay.Files, report.ProtectedFiles[0].Files)
			require.Len(t, report.ProtectedFiles[0].Detections, 1)
			assert.True(t, report.ProtectedFiles[0].Detections[0].FallbackUsed)

			writeLogsAuditFiles([]ProcessedRun{processed}, false)
			data, err := os.ReadFile(auditPath(processed.Run.LogsPath))
			require.NoError(t, err)
			var written AuditData
			require.NoError(t, json.Unmarshal(data, &written))
			assert.Equal(t, &replay, written.ProtectedFiles)
		})
	}
}

func TestAuditProtectedFilesCrossRunFallsBackToSession(t *testing.T) {
	run := WorkflowRun{DatabaseID: 42, WorkflowName: "test", LogsPath: t.TempDir()}
	session := protectionSessionHeader + protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", "success", true, false)
	require.NoError(t, os.WriteFile(filepath.Join(run.LogsPath, "aw_session.jsonl"), []byte(session), 0o600))
	for name, cached := range map[string]*AuditData{
		"no cache":      nil,
		"legacy cache":  {SchemaVersion: auditSchemaVersion},
		"invalid cache": {SchemaVersion: auditSchemaVersion - 1, ProtectedFiles: &AuditProtectedFiles{Files: []string{"stale"}}},
	} {
		t.Run(name, func(t *testing.T) {
			inputs := processedRunsToCrossRunInputs([]ProcessedRun{{Run: run, cachedAudit: cached}})
			require.Len(t, inputs, 1)
			require.Len(t, inputs[0].ProtectedFiles.Detections, 1)
			assert.True(t, inputs[0].ProtectedFiles.Detections[0].FallbackUsed)
			assert.NotContains(t, inputs[0].ProtectedFiles.Files, "stale")
		})
	}
}

func TestAuditProtectedFilesRefreshPreservesKnownEvidence(t *testing.T) {
	recordA := protectionSessionRecord(t, "safe_output.outcome", "safe_output", "safe_outputs", "success", true, false)
	recordB := strings.ReplaceAll(recordA, ".github/workflows/test.md", "CODEOWNERS")
	detections, partial, err := parseAuditProtectedFiles(strings.NewReader(protectionSessionHeader+recordA), 42)
	require.NoError(t, err)
	require.False(t, partial)
	detections[0].EvidencePath = "cached-outcomes.jsonl:7"
	for name, tc := range map[string]struct {
		session    string
		detections int
		partial    bool
	}{
		"header only":       {protectionSessionHeader, 1, true},
		"partial disjoint":  {protectionSessionHeader + recordB + "{bad", 2, true},
		"complete disjoint": {protectionSessionHeader + recordB, 2, true},
		"same decision":     {protectionSessionHeader + recordA, 1, false},
		"complete superset": {protectionSessionHeader + recordA + recordB, 2, false},
	} {
		t.Run(name, func(t *testing.T) {
			run := WorkflowRun{DatabaseID: 42, WorkflowName: "replayed display name", LogsPath: t.TempDir()}
			cached := &AuditProtectedFiles{
				Workflow: "finisher.lock.yml", WorkflowPath: ".github/workflows/finisher.lock.yml", Repository: "org/repo",
				Files: slices.Clone(detections[0].Files), Detections: detections,
				Partial: true, UnavailableRuns: []int64{42, 99, 99},
			}
			before, err := json.Marshal(cached)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(run.LogsPath, "aw_session.jsonl"), []byte(tc.session), 0o600))

			merged := resolveAuditProtectedFiles(run, cached)

			require.Len(t, merged.Detections, tc.detections)
			assert.Equal(t, tc.partial, merged.Partial)
			assert.Equal(t, detections[0], merged.Detections[0])
			assert.Subset(t, merged.Files, cached.Files)
			assert.Equal(t, []int64{99}, merged.UnavailableRuns)
			assert.Equal(t, cached.Workflow, merged.Workflow)
			assert.Equal(t, cached.WorkflowPath, merged.WorkflowPath)
			assert.Equal(t, cached.Repository, merged.Repository)
			if tc.detections == 2 {
				assert.Contains(t, merged.Files, "CODEOWNERS")
			}
			after, err := json.Marshal(cached)
			require.NoError(t, err)
			assert.Equal(t, before, after, "refresh must not mutate shared cached evidence")
		})
	}
}

func TestAuditProtectedFilesDetectionIdentity(t *testing.T) {
	detection := AuditProtectedFileDetection{
		RunID: 42, Operation: "push_to_pull_request_branch", Action: "fallback",
		Source: "protected", Status: "success", Files: []string{"CODEOWNERS", ".github/test.md"},
		FallbackUsed: true, TargetPullRequestNumber: 123, URL: "https://github.com/org/repo/issues/456",
		EvidencePath: "outcomes.jsonl:1",
	}
	key := auditProtectedFileDetectionKey(detection)
	duplicate := detection
	duplicate.Files = []string{".github/test.md", "CODEOWNERS", "CODEOWNERS"}
	duplicate.EvidencePath = "replayed-outcomes.jsonl:7"
	assert.Equal(t, key, auditProtectedFileDetectionKey(duplicate))
	assert.Equal(t, []string{"CODEOWNERS", ".github/test.md"}, detection.Files)

	for _, change := range []func(*AuditProtectedFileDetection){
		func(d *AuditProtectedFileDetection) { d.RunID++ },
		func(d *AuditProtectedFileDetection) { d.Operation = "create_pull_request" },
		func(d *AuditProtectedFileDetection) { d.Action = "deny" },
		func(d *AuditProtectedFileDetection) { d.Source = "allowlist" },
		func(d *AuditProtectedFileDetection) { d.Status = "warning" },
		func(d *AuditProtectedFileDetection) { d.Files = []string{"CODEOWNERS"} },
		func(d *AuditProtectedFileDetection) { d.FallbackUsed = false },
		func(d *AuditProtectedFileDetection) { d.TargetPullRequestNumber++ },
		func(d *AuditProtectedFileDetection) { d.URL = "https://github.com/org/repo/issues/457" },
	} {
		changed := detection
		change(&changed)
		assert.NotEqual(t, key, auditProtectedFileDetectionKey(changed))
	}
}

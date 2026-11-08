//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func durableTestEvent(kind, status string) string {
	record := durableSessionRecord{Engine: "codex", Storage: "artifact", RunID: "200", RunAttempt: "1", Status: status}
	if status == "restored" || status == "resumed" {
		record.ParentRunID = "100"
	}
	if kind == "durable.restore" && status == "restored" {
		record.ArtifactID = "999"
	}
	if kind == "durable.session" {
		record.SessionID = "00000000-0000-4000-8000-000000000001"
	}
	data, _ := json.Marshal(record)
	return `{"type":"` + kind + `","data":` + string(data) + `,"provenance":{"component":"durable","phase":"agent","path":"agent/durable-session.jsonl"}}` + "\n"
}

func TestDurableSessionReports(t *testing.T) {
	run := WorkflowRun{DatabaseID: 200, Attempt: 1}
	for _, tc := range []struct {
		name, content, status string
		started               bool
	}{
		{"resumed", durableTestEvent("durable.restore", "restored") + durableTestEvent("durable.session", "resumed"), "resumed", true},
		{"fresh", durableTestEvent("durable.restore", "fresh") + durableTestEvent("durable.session", "fresh"), "fresh", true},
		{"restored_only", durableTestEvent("durable.restore", "restored"), "restored", false},
		{"fresh_without_start", durableTestEvent("durable.restore", "fresh"), "fresh", false},
		{"restore_failed", durableTestEvent("durable.restore", "failed"), "restore_failed", false},
		{"legacy", "", "unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSessionTestFile(t, dir, "usage/aw_session.jsonl", sessionTestHeader+tc.content)
			report := readDurableSessionReport(dir, run, true)
			require.NotNil(t, report)
			require.Equal(t, tc.status, report.Status)
			require.Equal(t, tc.started, report.Started)
			if report.Started && report.Status == "resumed" {
				require.Equal(t, "100", report.ParentRunID)
				require.Equal(t, "999", report.ArtifactID)
			}
			var rendered bytes.Buffer
			require.NoError(t, renderDurableSession(&rendered, report))
			require.Contains(t, rendered.String(), "status="+tc.status)
		})
	}
}

func TestDurableSessionRejectsUntrustedAndInconsistentEvidence(t *testing.T) {
	restore := durableTestEvent("durable.restore", "restored")
	startup := durableTestEvent("durable.session", "resumed")
	for name, content := range map[string]string{
		"untrusted_provenance": strings.ReplaceAll(restore+startup, `"component":"durable"`, `"component":"agent"`),
		"wrong_attempt":        strings.ReplaceAll(restore+startup, `"runAttempt":"1"`, `"runAttempt":"2"`),
		"wrong_run":            strings.ReplaceAll(restore+startup, `"runId":"200"`, `"runId":"201"`),
		"wrong_parent":         restore + strings.ReplaceAll(startup, `"parentRunId":"100"`, `"parentRunId":"101"`),
		"fresh_after_restore":  restore + durableTestEvent("durable.session", "fresh"),
		"startup_only":         startup,
		"conflicting_restore":  restore + durableTestEvent("durable.restore", "fresh"),
		"invalid_id":           strings.ReplaceAll(restore+startup, `"parentRunId":"100"`, `"parentRunId":"../../secret"`),
		"wrong_phase":          strings.ReplaceAll(restore+startup, `"phase":"agent"`, `"phase":"detection"`),
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeSessionTestFile(t, dir, "aw_session.jsonl", sessionTestHeader+content)
			require.Equal(t, "unavailable", readDurableSessionReport(dir, WorkflowRun{DatabaseID: 200}, true).Status)
		})
	}
	require.Nil(t, readDurableSessionReport(t.TempDir(), WorkflowRun{DatabaseID: 200}, false))
	require.Equal(t, "session_not_cached", readDurableSessionReport(t.TempDir(), WorkflowRun{DatabaseID: 200}, true).Reason)
}

func TestDurableSessionRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "session.jsonl")
	require.NoError(t, os.WriteFile(target, []byte(sessionTestHeader+durableTestEvent("durable.restore", "restored")), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "aw_session.jsonl")))
	require.Equal(t, "invalid_session_source", readDurableSessionReport(dir, WorkflowRun{DatabaseID: 200}, true).Reason)
}

func TestDurableAuditAssemblyAndCacheBackfill(t *testing.T) {
	dir := t.TempDir()
	run := WorkflowRun{DatabaseID: 200, Attempt: 1, LogsPath: dir}
	writeSessionTestFile(t, dir, "usage/aw_session.jsonl", sessionTestHeader+
		durableTestEvent("durable.restore", "restored")+durableTestEvent("durable.session", "resumed"))
	data := assembleAuditData(auditDataInputs{processedRun: ProcessedRun{Run: run}})
	require.NotNil(t, data.DurableSession)
	require.Equal(t, "resumed", data.DurableSession.Status)
	require.NoError(t, writeAuditData(dir, AuditData{Overview: OverviewData{RunID: 200}}))
	cached, ok := loadCachedAuditData(dir, run, "")
	require.True(t, ok)
	require.Equal(t, "resumed", cached.DurableSession.Status)
	encoded, err := json.Marshal(cached)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"durable_session"`)
}

func TestHealthDurableCachedEvidence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run-200")
	writeSessionTestFile(t, dir, "usage/aw_session.jsonl", sessionTestHeader+
		durableTestEvent("durable.restore", "restored")+durableTestEvent("durable.session", "resumed"))
	health := WorkflowHealth{WorkflowName: "continuable", Status: "healthy", SuccessCount: 2}
	runs := []WorkflowRun{{DatabaseID: 200, Attempt: 1}, {DatabaseID: 201, Attempt: 1}}
	applyHealthDurableSessions(&health, runs, HealthConfig{LogsDir: root})
	require.NotNil(t, health.DurableSessions)
	require.Equal(t, 1, health.DurableSessions.ResumedRuns)
	require.Equal(t, 1, health.DurableSessions.UnavailableRuns)
	require.Equal(t, "healthy", health.Status)
	require.Equal(t, 2, health.SuccessCount)
	var output bytes.Buffer
	require.NoError(t, renderHealthDurableSessions(&output, health))
	require.Contains(t, output.String(), "resumed=1")
	require.Contains(t, output.String(), `parent_run_id="100"`)
	require.Contains(t, output.String(), "unavailable=1")
}

func TestHealthDurableRetainsUnavailableEvidenceAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	run := WorkflowRun{DatabaseID: 200, Attempt: 1}
	collection := &HealthCollection{ctx: ctx, jobs: map[healthRunAttempt]*healthJobs{
		{id: 200, attempt: 1}: {details: []JobInfoWithDuration{{JobInfo: JobInfo{
			Name: "agent", Steps: []JobStep{{Name: "Restore durable workflow state"}},
		}}}},
	}}
	health := WorkflowHealth{}
	applyHealthDurableSessions(&health, []WorkflowRun{run}, HealthConfig{LogsDir: t.TempDir(), collection: collection})
	require.NotNil(t, health.DurableSessions)
	require.Equal(t, 1, health.DurableSessions.UnavailableRuns)
	require.Equal(t, "canceled", health.DurableSessions.Runs[0].Session.Reason)
}

func TestDurableAuditLogInputBoundaries(t *testing.T) {
	annotation := `##[error]{"type":"user","message":{"content":[{"type":"tool_result"}]}}`
	require.True(t, isAgentToolResultAnnotation(annotation))
	require.False(t, isAgentToolResultAnnotation(annotation+` {}`))
	require.False(t, isAgentToolResultAnnotation(annotation+` trailing`))
	require.Equal(t, "first\nlast\n", stripGHALogTimestamps("2026-11-08T20:00:00Z first\r\nlast\r\n"))
}

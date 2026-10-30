//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditRetrySummary(t *testing.T) {
	record := func(kind, phase, data string) string {
		return `{"type":"` + kind + `","data":` + data + `,"provenance":{"component":"harness","phase":"` + phase + `","path":"retry.json"}}` + "\n"
	}
	content := sessionTestHeader +
		record("harness.attempt", "agent", `{"attempt":1,"layer":"harness","exitCode":1,"failureClasses":["network"]}`) +
		record("harness.attempt", "agent", `{"attempt":2,"attemptId":"harness:2","layer":"harness","exitCode":null,"failureClasses":[],"outcome":"running","startedAt":1}`) +
		record("harness.attempt", "agent", `{"attempt":2,"attemptId":"harness:2","layer":"harness","exitCode":0,"failureClasses":[],"outcome":"success","strategy":"resume","backoffMs":100,"startedAt":1,"finishedAt":2}`) +
		record("harness.retry", "agent", `{"attemptResults":[{"attempt":1},{"attempt":2},{"attempt":3}],"startupAttemptResults":[{"attempt":1},{"attempt":2}]}`) +
		record("harness.attempt", "detection", `{"attempt":1,"layer":"harness","exitCode":1,"failureClasses":[]}`) +
		record("harness.attempt", "detection", `{"attempt":2,"layer":"harness","exitCode":0,"failureClasses":[]}`)
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		t.Run(relative, func(t *testing.T) {
			root := t.TempDir()
			writeSessionTestFile(t, root, relative, content)
			report := readAuditRetrySummary(root)
			require.NotNil(t, report)
			require.Equal(t, 3, report.AgentRetries)
			require.Equal(t, 1, report.DetectionRetries)
			require.Len(t, report.Attempts, 7)
			require.Equal(t, "success", report.Attempts[1].Outcome)
			require.Equal(t, "resume", report.Attempts[1].Strategy)
			require.InDelta(t, 100, *report.Attempts[1].BackoffMs, 0)

			data := assembleAuditData(auditDataInputs{processedRun: ProcessedRun{Run: WorkflowRun{LogsPath: root}}})
			require.Equal(t, report, data.Retries)
			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"agent_retries":3`)
			require.Contains(t, string(encoded), `"attemptId":"harness:2"`)
			console := captureStderrForGuardPolicyReportTest(func() { renderConsole(data, root) })
			require.Contains(t, console, "retries: agent=3 detection=1 attempts=7")
		})
	}
}

func TestAuditRetriesBackfillCache(t *testing.T) {
	root := t.TempDir()
	run := WorkflowRun{DatabaseID: 123}
	require.NoError(t, writeAuditData(root, AuditData{Overview: OverviewData{RunID: 123}}))
	writeSessionTestFile(t, root, "aw_session.jsonl", sessionTestHeader+
		`{"type":"harness.attempt","data":{"attempt":2,"layer":"harness","exitCode":0,"failureClasses":[]},"provenance":{"component":"harness","phase":"agent","path":"retry.json"}}`+"\n")
	cached, ok := loadCachedAuditData(root, run, "")
	require.True(t, ok)
	require.Equal(t, 1, cached.Retries.AgentRetries)
	require.NoError(t, writeAuditData(root, cached))
	require.NoError(t, os.Remove(filepath.Join(root, "aw_session.jsonl")))
	cached, ok = loadCachedAuditData(root, run, "")
	require.True(t, ok)
	require.Equal(t, 1, cached.Retries.AgentRetries)
}

func TestAuditRetriesMissingAndUntrusted(t *testing.T) {
	require.Nil(t, readAuditRetrySummary(t.TempDir()))
	root := t.TempDir()
	writeSessionTestFile(t, root, "aw_session.jsonl", sessionTestHeader+
		`{"type":"harness.attempt","data":{"attempt":2,"layer":"harness"},"provenance":{"component":"agent","phase":"agent"}}`+"\n")
	require.Nil(t, readAuditRetrySummary(root))
}

func TestAuditRetriesSeparateStartupInvocations(t *testing.T) {
	var events []sessionModelRoutingEvent
	for _, scope := range []string{"startup:1", "startup:2"} {
		for _, attempt := range []int{1, 2} {
			data, err := json.Marshal(AuditRetryAttempt{Attempt: attempt, AttemptID: "harness:" + strconv.Itoa(attempt), StartupAttemptID: scope, Layer: "harness"})
			require.NoError(t, err)
			event := sessionModelRoutingEvent{Type: "harness.attempt", Data: data}
			event.Provenance.Component, event.Provenance.Phase, event.Provenance.Path = "harness", "agent", "retry.json"
			events = append(events, event)
		}
	}
	report := buildAuditRetrySummary(events)
	require.Len(t, report.Attempts, 4)
	require.Equal(t, 2, report.AgentRetries)
	for index := range events {
		var attempt AuditRetryAttempt
		require.NoError(t, json.Unmarshal(events[index].Data, &attempt))
		attempt.AttemptID = attempt.StartupAttemptID + ":" + attempt.AttemptID
		attempt.StartupAttemptID = ""
		events[index].Data, _ = json.Marshal(attempt)
	}
	report = buildAuditRetrySummary(events)
	require.Len(t, report.Attempts, 4)
	require.Equal(t, 2, report.AgentRetries)
}

func TestAuditRetriesRetainDistinctObservedIntervals(t *testing.T) {
	var events []sessionModelRoutingEvent
	for _, start := range []float64{10, 10, 30} {
		data, err := json.Marshal(AuditRetryAttempt{Attempt: 2, AttemptID: "harness:2", Layer: "harness", StartedAt: &start})
		require.NoError(t, err)
		event := sessionModelRoutingEvent{Type: "harness.attempt", Data: data}
		event.Provenance.Component, event.Provenance.Phase, event.Provenance.Path = "harness", "agent", "retry.json"
		events = append(events, event)
	}
	report := buildAuditRetrySummary(events)
	require.Len(t, report.Attempts, 2)
	require.Equal(t, 2, report.AgentRetries)
}

func TestAuditRetriesExposeNativeEngineRetriesSeparately(t *testing.T) {
	root := t.TempDir()
	record := func(attempt, index int, phase string) string {
		return fmt.Sprintf(`{"type":"engine.retry","data":{"sourceEngine":"codex","attempt":%d,"maxRetries":5,"backoffMs":100},"provenance":{"component":"agent","phase":%q,"path":"agent.jsonl","index":%d,"attemptId":"harness:2"}}`+"\n", attempt, phase, index)
	}
	content := sessionTestHeader + record(1, 0, "agent") + record(2, 1, "agent") + record(1, 2, "agent") + record(1, 0, "detection") + record(0, 3, "agent") +
		`{"type":"session.result","data":{"is_error":true},"provenance":{"component":"agent","phase":"agent"}}` + "\n"
	writeSessionTestFile(t, root, "aw_session.jsonl", content)
	report := readAuditRetrySummary(root)
	require.NotNil(t, report)
	require.Zero(t, report.AgentRetries)
	require.Zero(t, report.DetectionRetries)
	require.Equal(t, 3, report.EngineRetries)
	require.Equal(t, 1, report.DetectionEngineRetries)
	require.Len(t, report.EngineRetryHistory, 4)
	require.Equal(t, "harness:2", report.EngineRetryHistory[0].AttemptID)
	require.Equal(t, "codex", report.EngineRetryHistory[0].SourceEngine)
	require.Equal(t, 5, *report.EngineRetryHistory[0].MaxRetries)
	console := captureStderrForGuardPolicyReportTest(func() { renderConsoleRetries(report) })
	require.Contains(t, console, "attempts=0 engine=3 detection_engine=1")
}

func TestAuditRetriesRetainPartialAndLegacyNativeObservations(t *testing.T) {
	root := t.TempDir()
	writeSessionTestFile(t, root, "aw_session.jsonl", sessionTestHeader+
		`{"type":"engine.retry","data":{"sourceEngine":"claude","errors":["retry"]},"provenance":{"component":"agent","phase":"agent","path":"agent.jsonl","index":0}}`+"\n"+
		`{"type":"session.result","data":{"engineRetry":{"attempt":1,"maxRetries":5}},"provenance":{"component":"agent","phase":"agent","path":"agent.jsonl","index":1}}`+"\n")
	report := readAuditRetrySummary(root)
	require.NotNil(t, report)
	require.Equal(t, 2, report.EngineRetries)
	require.Len(t, report.EngineRetryHistory, 2)
	require.Nil(t, report.EngineRetryHistory[0].Attempt)
	require.Equal(t, "claude", report.EngineRetryHistory[0].SourceEngine)
	require.Equal(t, 1, *report.EngineRetryHistory[1].Attempt)
	encoded, err := json.Marshal(report.EngineRetryHistory[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"attempt"`)
}

func TestSessionATIFRetainsRetryEvidence(t *testing.T) {
	content := sessionTestHeader + sessionTestAgentEvent +
		`{"type":"harness.attempt","data":{"attempt":2,"attemptId":"retry-2","exitCode":null,"failureClasses":[],"startedAt":10,"outcome":"interrupted","strategy":"resume","backoffMs":20,"layer":"harness"},"provenance":{"component":"harness","phase":"agent","path":"retry.json","index":1}}` + "\n" +
		`{"type":"harness.retry","data":{"attemptResults":[{"attempt":2,"exitCode":null,"failureClasses":[]}],"startupAttemptResults":[{"attempt":2,"exitCode":0,"failureClasses":[]}]},"provenance":{"component":"harness","phase":"agent","path":"retry.json","index":0}}` + "\n" +
		`{"type":"engine.retry","data":{"sourceEngine":"claude","attempt":1,"maxRetries":5,"backoffMs":100},"provenance":{"component":"agent","phase":"agent","path":"agent.jsonl","index":0,"attemptId":"harness:2"}}` + "\n"
	root := t.TempDir()
	writeSessionTestFile(t, root, "aw_session.jsonl", content)
	require.NoError(t, exportRunATIF(t.Context(), 123, root))
	exported, err := os.ReadFile(filepath.Join(root, "aw_session.atif"))
	require.NoError(t, err)
	var trajectory struct {
		Steps []struct {
			Extra map[string]json.RawMessage `json:"extra"`
		} `json:"steps"`
	}
	require.NoError(t, json.Unmarshal(exported, &trajectory))
	for line := range strings.SplitSeq(strings.TrimSpace(content), "\n") {
		if !strings.Contains(line, `"component":"harness"`) && !strings.Contains(line, `"engine.retry"`) {
			continue
		}
		var want map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &want))
		found := false
		for _, step := range trajectory.Steps {
			var got map[string]any
			if json.Unmarshal(step.Extra["unified_event"], &got) == nil {
				wantJSON, _ := json.Marshal(want)
				gotJSON, _ := json.Marshal(got)
				found = found || bytes.Equal(wantJSON, gotJSON)
			}
		}
		require.True(t, found, "retry event lost in ATIF export: %s", line)
	}
	original, err := os.ReadFile(filepath.Join(root, "aw_session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, content, string(original))
}

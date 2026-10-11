//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const steeringNoticesTokenUsageJSONL = `{"timestamp":"2026-10-10T10:00:00Z","event":"token_usage","request_id":"req-1","purpose":"agent","provider":"copilot","model":"claude-sonnet-5","path":"/v1/messages","status":200,"input_tokens":10,"output_tokens":5,"aic":1.5}
{"timestamp":"2026-10-10T10:00:01Z","event":"token_usage","request_id":"req-2","purpose":"agent","provider":"copilot","model":"claude-sonnet-5","path":"/v1/messages","status":200,"input_tokens":10,"output_tokens":5,"aic":1.5,"steering":{"type":"ai_credit","threshold":80}}
{"timestamp":"2026-10-10T10:00:02Z","event":"token_usage","request_id":"req-3","purpose":"agent","provider":"copilot","model":"claude-sonnet-5","path":"/v1/messages","status":200,"input_tokens":10,"output_tokens":5,"aic":1.5,"steering":{"type":"timeout","threshold":90}}
`

func steeringNoticesSessionEvent(t *testing.T, phase, requestID string, steering any) string {
	t.Helper()
	data := map[string]any{
		"provider":  "copilot",
		"model":     "claude-sonnet-5",
		"path":      "/v1/messages",
		"requestId": requestID,
		"status":    200,
		"aic":       1.5,
		"usage":     map[string]any{"inputTokens": 10, "outputTokens": 5},
	}
	if steering != nil {
		data["steering"] = steering
	}
	path := "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl"
	if phase == "detection" {
		path = "threat-detection/" + path
	}
	event := map[string]any{
		"type":       "firewall.token_usage",
		"timestamp":  "2026-10-10T10:00:00Z",
		"data":       data,
		"provenance": map[string]any{"component": "firewall", "phase": phase, "path": path},
	}
	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	return string(encoded)
}

func writeSteeringNoticesRun(t *testing.T, sessionLines []string) string {
	t.Helper()
	runDir := testutil.TempDir(t, "steering-notices")
	logsDir := filepath.Join(runDir, "sandbox", "firewall", "logs", "api-proxy-logs")
	require.NoError(t, os.MkdirAll(logsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "token-usage.jsonl"), []byte(steeringNoticesTokenUsageJSONL), 0o644))
	if sessionLines != nil {
		usageDir := filepath.Join(runDir, "usage")
		require.NoError(t, os.MkdirAll(usageDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(usageDir, "aw_session.jsonl"), []byte(strings.Join(sessionLines, "\n")+"\n"), 0o644))
	}
	return runDir
}

func TestAnalyzeTokenUsageSteeringNotices(t *testing.T) {
	t.Run("lists notices from unified firewall.token_usage events", func(t *testing.T) {
		runDir := writeSteeringNoticesRun(t, []string{
			steeringNoticesSessionEvent(t, "agent", "req-1", nil),
			steeringNoticesSessionEvent(t, "agent", "req-2", map[string]any{"type": "ai_credit", "threshold": 80}),
			steeringNoticesSessionEvent(t, "agent", "req-3", map[string]any{"type": "timeout", "threshold": 90}),
			steeringNoticesSessionEvent(t, "detection", "det-1", map[string]any{"type": "token", "threshold": 95}),
		})

		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Equal(t, []SteeringNotice{
			{Type: "ai_credit", Threshold: 80, RequestID: "req-2", Phase: "agent"},
			{Type: "timeout", Threshold: 90, RequestID: "req-3", Phase: "agent"},
			{Type: "token", Threshold: 95, RequestID: "det-1", Phase: "detection"},
		}, summary.SteeringNotices)
		assert.Equal(t, 3, summary.TotalRequests, "detection events should not change agent token usage")
	})

	t.Run("no steering fields produce no notices", func(t *testing.T) {
		runDir := writeSteeringNoticesRun(t, []string{
			steeringNoticesSessionEvent(t, "agent", "req-1", nil),
			steeringNoticesSessionEvent(t, "agent", "req-2", nil),
			steeringNoticesSessionEvent(t, "agent", "req-3", nil),
		})

		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Nil(t, summary.SteeringNotices)
		encoded, err := json.Marshal(summary)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "steering_notices")
	})

	t.Run("runs without a unified session list no notices", func(t *testing.T) {
		runDir := writeSteeringNoticesRun(t, nil)

		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Nil(t, summary.SteeringNotices, "notices are read only from the unified session")
	})

	t.Run("ignores malformed or unknown steering values", func(t *testing.T) {
		runDir := writeSteeringNoticesRun(t, []string{
			steeringNoticesSessionEvent(t, "agent", "req-1", "ai_credit"),
			steeringNoticesSessionEvent(t, "agent", "req-2", map[string]any{"type": "unknown", "threshold": 80}),
			steeringNoticesSessionEvent(t, "agent", "req-3", map[string]any{"type": "token", "threshold": "high"}),
		})

		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Nil(t, summary.SteeringNotices)
		assert.Equal(t, 3, summary.TotalRequests, "malformed steering values must not drop token usage")
	})
}

func TestSteeringNoticesInAuditAndLogs(t *testing.T) {
	notices := []SteeringNotice{
		{Type: "ai_credit", Threshold: 80, RequestID: "req-2", Phase: "agent"},
		{Type: "timeout", Threshold: 90, RequestID: "req-3", Phase: "agent"},
	}
	processed := ProcessedRun{
		Run:        WorkflowRun{DatabaseID: 7, WorkflowName: "wf", LogsPath: t.TempDir()},
		TokenUsage: &TokenUsageSummary{SteeringNotices: notices},
	}

	audit, _ := buildLocalAuditData(processed, LogMetrics{}, nil)
	assert.Equal(t, notices, audit.SteeringNotices)

	data := buildLogsData([]ProcessedRun{processed}, t.TempDir(), nil)
	require.Len(t, data.Runs, 1)
	assert.Equal(t, notices, data.Runs[0].SteeringNotices)
	compact := compactLogsData(data)
	require.Len(t, compact.Runs, 1)
	assert.Nil(t, compact.Runs[0].TokenUsageSummary)
	assert.Equal(t, notices, compact.Runs[0].SteeringNotices, "compact logs output should keep delivered notices")

	encoded, err := json.Marshal(data.Runs[0])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"steering_notices":[{"type":"ai_credit","threshold":80,"request_id":"req-2","phase":"agent"}`)

	empty := ProcessedRun{Run: WorkflowRun{DatabaseID: 8, WorkflowName: "wf", LogsPath: t.TempDir()}, TokenUsage: &TokenUsageSummary{}}
	emptyAudit, _ := buildLocalAuditData(empty, LogMetrics{}, nil)
	assert.Nil(t, emptyAudit.SteeringNotices)
	emptyLogs := buildLogsData([]ProcessedRun{empty}, t.TempDir(), nil)
	encoded, err = json.Marshal(emptyLogs.Runs[0])
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "steering_notices")
}

func TestRenderConsoleSteeringNotices(t *testing.T) {
	output := captureStderrForGuardPolicyReportTest(func() {
		renderConsoleSteeringNotices([]SteeringNotice{
			{Type: "ai_credit", Threshold: 80, RequestID: "req-2", Phase: "agent"},
			{Type: "token", Threshold: 95, RequestID: "det-1", Phase: "detection"},
		})
	})
	assert.Contains(t, output, "steering_notices:")
	assert.Contains(t, output, "ai_credit 80% request_id=req-2\n")
	assert.Contains(t, output, "token 95% request_id=det-1 phase=detection")

	assert.Empty(t, captureStderrForGuardPolicyReportTest(func() { renderConsoleSteeringNotices(nil) }))
}

func TestGatewaySteeringEventsIncludeAICreditAndThreshold(t *testing.T) {
	runDir := testutil.TempDir(t, "steering-event-log")
	logsDir := filepath.Join(runDir, "sandbox", "firewall", "logs", "api-proxy-logs")
	require.NoError(t, os.MkdirAll(logsDir, 0o755))
	t.Cleanup(func() { apiProxySteeringLogCache.Delete(filepath.Clean(runDir)) })
	events := strings.Join([]string{
		`{"timestamp":"2026-10-10T10:00:00Z","event":"ai_credit_steering","request_id":"req-2","provider":"copilot","threshold":80,"message":"[AWF AI CREDIT WARNING] 80% of the AI-credit budget used"}`,
		`{"timestamp":"2026-10-10T10:00:01Z","event":"timeout_steering","request_id":"req-3","threshold":90,"message":"[AWF TIME WARNING] 90% of the runtime used"}`,
		`{"timestamp":"2026-10-10T10:00:02Z","event":"token_steering","message":"[AWF TOKEN WARNING] budget nearly exhausted"}`,
		`{"timestamp":"2026-10-10T10:00:03Z","event":"ai_credit_steering","message":"not an AWF warning"}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(events), 0o644))

	got, err := extractGatewaySteeringEvents(runDir)
	require.NoError(t, err)
	assert.Equal(t, []GatewaySteeringEvent{
		{Type: "ai_credit_steering", Message: "[AWF AI CREDIT WARNING] 80% of the AI-credit budget used", Timestamp: "2026-10-10T10:00:00Z", Threshold: 80, RequestID: "req-2"},
		{Type: "timeout_steering", Message: "[AWF TIME WARNING] 90% of the runtime used", Timestamp: "2026-10-10T10:00:01Z", Threshold: 90, RequestID: "req-3"},
		{Type: "token_steering", Message: "[AWF TOKEN WARNING] budget nearly exhausted", Timestamp: "2026-10-10T10:00:02Z"},
	}, got)

	encoded, err := json.Marshal(got[2])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"token_steering","message":"[AWF TOKEN WARNING] budget nearly exhausted","timestamp":"2026-10-10T10:00:02Z"}`, string(encoded), "events without threshold or request id should serialize as before")
}

func TestSteeringEntryToTimelineEventThreshold(t *testing.T) {
	event, ok := steeringEntryToTimelineEvent(proxyEventsEntry{Event: "ai_credit_steering", Message: "[AWF AI CREDIT WARNING] 90%", Threshold: 90})
	require.True(t, ok)
	assert.Equal(t, "credit 90%", event.Status)

	event, ok = steeringEntryToTimelineEvent(proxyEventsEntry{Event: "token_steering", Message: "[AWF TOKEN WARNING] budget"})
	require.True(t, ok)
	assert.Equal(t, "token", event.Status, "events without a threshold keep their existing status")
}

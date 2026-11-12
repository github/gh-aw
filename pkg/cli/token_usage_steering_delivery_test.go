//go:build !integration

package cli

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func steeringDeliverySessionEvent(t *testing.T, phase, requestID string, steering, delivery any) string {
	t.Helper()
	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte(steeringNoticesSessionEvent(t, phase, requestID, steering)), &event))
	if delivery != nil {
		event["data"].(map[string]any)["steeringDelivery"] = delivery
	}
	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	return string(encoded)
}

func TestAnalyzeTokenUsageSteeringDeliveries(t *testing.T) {
	t.Run("lists every delivery with its conversation", func(t *testing.T) {
		runDir := writeSteeringNoticesRun(t, []string{
			steeringNoticesSessionEvent(t, "agent", "req-1", nil),
			steeringDeliverySessionEvent(t, "agent", "req-2", map[string]any{"type": "ai_credit", "threshold": 80},
				map[string]any{"type": "ai_credit", "trigger": "threshold", "threshold": 80, "conversation": "0123456789ab", "first": true}),
			steeringDeliverySessionEvent(t, "agent", "req-3", nil,
				map[string]any{"type": "ai_credit", "trigger": "threshold", "threshold": 80, "conversation": "ba9876543210", "first": false}),
			steeringDeliverySessionEvent(t, "agent", "req-4", nil,
				map[string]any{"type": "timeout", "trigger": "little_left", "threshold": nil, "conversation": nil, "first": true}),
			steeringDeliverySessionEvent(t, "detection", "det-1", nil,
				map[string]any{"type": "ai_credit", "trigger": "little_left", "threshold": nil, "conversation": "fedcba987654", "first": true}),
		})

		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Equal(t, []SteeringNotice{{Type: "ai_credit", Threshold: 80, RequestID: "req-2", Phase: "agent"}}, summary.SteeringNotices,
			"legacy notices stay at most once per threshold")
		assert.Equal(t, []SteeringDelivery{
			{Type: "ai_credit", Trigger: "threshold", Threshold: new(80), Conversation: new("0123456789ab"), First: true, RequestID: "req-2", Phase: "agent"},
			{Type: "ai_credit", Trigger: "threshold", Threshold: new(80), Conversation: new("ba9876543210"), RequestID: "req-3", Phase: "agent"},
			{Type: "timeout", Trigger: "little_left", First: true, RequestID: "req-4", Phase: "agent"},
			{Type: "ai_credit", Trigger: "little_left", Conversation: new("fedcba987654"), First: true, RequestID: "det-1", Phase: "detection"},
		}, summary.SteeringDeliveries)

		audit, _ := buildLocalAuditData(ProcessedRun{
			Run:        WorkflowRun{DatabaseID: 7, WorkflowName: "wf", LogsPath: runDir},
			TokenUsage: summary,
		}, LogMetrics{}, nil)
		assert.Equal(t, summary.SteeringDeliveries, audit.SteeringDeliveries)
		encoded, err := json.Marshal(audit.SteeringDeliveries[2])
		require.NoError(t, err)
		assert.JSONEq(t, `{"type":"timeout","trigger":"little_left","threshold":null,"conversation":null,"first":true,"request_id":"req-4","phase":"agent"}`, string(encoded),
			"a little_left delivery has a null threshold, not 0")

		processed := ProcessedRun{Run: WorkflowRun{DatabaseID: 7, WorkflowName: "wf", LogsPath: runDir}, TokenUsage: summary}
		run := buildRunData(processed, []ProcessedRun{processed}, "", &logsAggregate{engineCounts: make(map[string]int)}, false)
		assert.Equal(t, summary.SteeringDeliveries, run.SteeringDeliveries)
	})

	t.Run("accepts sessions from AWF v0.29.11 and older without deliveries", func(t *testing.T) {
		runDir := writeSteeringNoticesRun(t, []string{
			steeringNoticesSessionEvent(t, "agent", "req-2", map[string]any{"type": "ai_credit", "threshold": 80}),
		})
		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Len(t, summary.SteeringNotices, 1)
		assert.Empty(t, summary.SteeringDeliveries)
	})

	t.Run("drops malformed deliveries without failing the usage record", func(t *testing.T) {
		valid := map[string]any{"type": "ai_credit", "trigger": "threshold", "threshold": 90, "conversation": "0123456789ab", "first": true}
		with := func(key string, value any) map[string]any {
			delivery := maps.Clone(valid)
			if value == nil {
				delete(delivery, key)
			} else {
				delivery[key] = value
			}
			return delivery
		}
		lines := []string{}
		for i, delivery := range []any{
			"ai_credit",
			[]any{},
			with("type", "token"),
			with("trigger", "unknown"),
			with("threshold", 50),
			with("threshold", "90"),
			map[string]any{"type": "ai_credit", "trigger": "threshold", "threshold": nil, "conversation": nil, "first": true},
			map[string]any{"type": "ai_credit", "trigger": "little_left", "threshold": 80, "conversation": nil, "first": true},
			with("conversation", "NOT-A-KEY"),
			with("conversation", nil),
			with("first", "yes"),
			with("first", nil),
			map[string]any{"type": "ai_credit", "trigger": "threshold", "threshold": 90, "conversation": "0123456789ab", "first": nil},
		} {
			lines = append(lines, steeringDeliverySessionEvent(t, "agent", "req-"+string(rune('a'+i)), nil, delivery))
		}
		runDir := writeSteeringNoticesRun(t, lines)
		summary, err := analyzeTokenUsage(runDir, false)
		require.NoError(t, err)
		require.NotNil(t, summary)
		assert.Empty(t, summary.SteeringDeliveries)
		entries, err := readUnifiedTokenUsageEntries(runDir)
		require.NoError(t, err)
		assert.Len(t, entries, len(lines))
	})
}

func TestTokenUsageSteeringDeliveryJSONDecode(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want TokenUsageSteeringDelivery
	}{
		{
			raw:  `{"type":"ai_credit","trigger":"threshold","threshold":95,"conversation":"0123456789ab","first":false}`,
			want: TokenUsageSteeringDelivery{Type: "ai_credit", Trigger: steeringTriggerThreshold, Threshold: 95, Conversation: "0123456789ab"},
		},
		{
			raw:  `{"type":"timeout","trigger":"little_left","threshold":null,"conversation":null,"first":true}`,
			want: TokenUsageSteeringDelivery{Type: "timeout", Trigger: steeringTriggerLittleLeft, First: true},
		},
	} {
		var entry TokenUsageEntry
		require.NoError(t, json.Unmarshal([]byte(`{"event":"token_usage","steering_delivery":`+tt.raw+`}`), &entry))
		require.True(t, entry.SteeringDelivery.valid(), tt.raw)
		assert.Equal(t, &tt.want, entry.SteeringDelivery)
	}
	var entry TokenUsageEntry
	require.NoError(t, json.Unmarshal([]byte(`{"event":"token_usage","request_id":"old","input_tokens":3}`), &entry), "records without steering_delivery decode")
	assert.Nil(t, entry.SteeringDelivery)
	assert.Equal(t, 3, entry.InputTokens)
}

func TestGatewaySteeringEventsLittleLeft(t *testing.T) {
	runDir := writeSteeringNoticesRun(t, []string{
		`{"type":"firewall.steering","timestamp":"2026-10-10T10:00:01Z","data":{"event":"ai_credit_steering","message":"[AWF AI CREDIT WARNING] 247 of 300 AI credits are used","requestId":"req-2","threshold":80,"trigger":"threshold","conversation":"0123456789ab"},"provenance":{"component":"firewall","phase":"agent"}}`,
		`{"type":"firewall.steering","timestamp":"2026-10-10T10:00:02Z","data":{"event":"ai_credit_steering","message":"[AWF AI CREDIT WARNING] little left","requestId":"req-3","threshold":null,"trigger":"little_left","conversation":null},"provenance":{"component":"firewall","phase":"agent"}}`,
		`{"type":"firewall.steering","timestamp":"2026-10-10T10:00:03Z","data":{"event":"timeout_steering","message":"[AWF TIME WARNING] legacy","requestId":"req-4","threshold":90},"provenance":{"component":"firewall","phase":"agent"}}`,
	})
	events, err := extractGatewaySteeringEvents(runDir)
	require.NoError(t, err)
	assert.Equal(t, []GatewaySteeringEvent{
		{Type: aiCreditSteeringEventName, Message: "[AWF AI CREDIT WARNING] 247 of 300 AI credits are used", Timestamp: "2026-10-10T10:00:01Z", RequestID: "req-2", Threshold: 80, Trigger: "threshold", Conversation: "0123456789ab"},
		{Type: aiCreditSteeringEventName, Message: "[AWF AI CREDIT WARNING] little left", Timestamp: "2026-10-10T10:00:02Z", RequestID: "req-3", Trigger: "little_left"},
		{Type: timeoutSteeringEventName, Message: "[AWF TIME WARNING] legacy", Timestamp: "2026-10-10T10:00:03Z", RequestID: "req-4", Threshold: 90},
	}, events)

	littleLeft, ok := steeringEntryToTimelineEvent(proxyEventsEntry{Event: aiCreditSteeringEventName, Message: "[AWF AI CREDIT WARNING] little left", Trigger: json.RawMessage(`"little_left"`), Threshold: 0})
	require.True(t, ok)
	assert.Equal(t, "credit little left", littleLeft.Status)
	threshold, ok := steeringEntryToTimelineEvent(proxyEventsEntry{Event: aiCreditSteeringEventName, Message: "[AWF AI CREDIT WARNING] x", Threshold: 90})
	require.True(t, ok)
	assert.Equal(t, "credit 90%", threshold.Status)
}

func TestRenderConsoleSteeringDeliveries(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderConsoleSteeringDeliveries([]SteeringDelivery{
			{Type: "ai_credit", Trigger: "threshold", Threshold: new(80), Conversation: new("0123456789ab"), First: true, RequestID: "req-2", Phase: "agent"},
			{Type: "timeout", Trigger: "little_left", RequestID: "det-1", Phase: "detection"},
		})
	})
	assert.Contains(t, output, "steering_deliveries:")
	assert.Contains(t, output, "ai_credit 80% conversation=0123456789ab first request_id=req-2\n")
	assert.Contains(t, output, "timeout little left conversation=unknown request_id=det-1 phase=detection\n")
	assert.NotContains(t, output, " 0%")
}

func TestRenderConsoleSteeringOutputEscapesControlCharacters(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderConsoleSteeringDeliveries([]SteeringDelivery{
			{Type: "ai_credit\x1b[2J", Trigger: "threshold", Threshold: new(80), Conversation: new("\x1b]52;c;aGk=\a"), RequestID: "req-\x1b[31mred\x1b[0m", Phase: "det\x1bection"},
		})
		renderConsoleSteeringNotices([]SteeringNotice{
			{Type: "ai_credit\x1b[2J", Threshold: 80, RequestID: "req-\x1b[31mred\x1b[0m", Phase: "det\x1bection"},
		}, false)
	})
	assert.NotContains(t, output, "\x1b")
	assert.NotContains(t, output, "\a")
	assert.Contains(t, output, `request_id=req-'\x1b'[31mred'\x1b'[0m`)
}

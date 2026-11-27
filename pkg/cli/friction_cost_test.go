package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const frictionSummaryJSON = `{
  "schema": "usage-activity-summary/v1",
  "session": {"total_events": 12, "failed_tool_executions": 3},
  "friction": {
    "measurement_state": "statistical",
    "canonical_unit": "aic",
    "sources": ["agent_session", "agent_token_usage", "mcp_gateway"],
    "total_events": 2,
    "total_occurrences": 4,
    "counted_occurrences": 3,
    "suppressed_occurrences": 1,
    "linked_invocations": 1,
    "unattributed_occurrences": 0,
    "cost": {
      "aic": 1.25,
      "tokens": {"input": 200, "output": 40, "cache_read": 10, "cache_write": 2, "reasoning": 4, "total": 256},
      "turns": 1,
      "tool_calls": 3,
      "latency_ms": 550
    },
    "total_run_aic": 2.5,
    "total_run_aic_partial": true,
    "friction_ratio": 0.5,
    "dimension_states": {"aic": "statistical", "tokens": "statistical", "turns": "measured", "tool_calls": "measured", "latency_ms": "measured"},
    "uncertainty": {
      "aic": {"state": "statistical", "method": "mean_invocation_apportionment", "confidence": "low", "relative_error": 0.3, "sample_size": 2, "basis": "mean healthy-invocation cost"},
      "tool_calls": {"state": "measured", "method": "direct_record", "confidence": "high", "relative_error": 0, "sample_size": 3, "basis": "counted friction occurrences"}
    },
    "drivers": [
      {"driver": "mcp_tool_error", "class": "tool_failure", "source": "mcp_gateway", "events": 1, "occurrences": 1, "counted_occurrences": 1, "suppressed_occurrences": 0, "state": "causal", "cost": {"aic": 0.5, "tokens": {"input": 100, "output": 20, "cache_read": 5, "cache_write": 1, "reasoning": 2, "total": 128}, "turns": 0, "tool_calls": 1, "latency_ms": 250}},
      {"driver": "session_tool_failure", "class": "tool_failure", "source": "agent_session", "events": 1, "occurrences": 3, "counted_occurrences": 2, "suppressed_occurrences": 1, "state": "statistical", "cost": {"aic": 0.75, "tokens": {"input": 100, "output": 20, "cache_read": 5, "cache_write": 1, "reasoning": 2, "total": 128}, "turns": 0, "tool_calls": 2, "latency_ms": 0}}
    ],
    "groups": [
      {"group_id": "tool_failure", "primary_source": "mcp_gateway", "event_ids": ["mcp_tool_error:c1", "session_tool_failure:aggregate"], "total_occurrences": 4, "counted_occurrences": 3, "suppressed_occurrences": 1, "rule": "highest-fidelity source owns overlapping occurrences"}
    ],
    "events": [
      {"id": "mcp_tool_error:c1", "driver": "mcp_tool_error", "source": "mcp_gateway", "group_id": "tool_failure", "label": "github/issue_read", "timestamp": "2026-01-01T00:00:01Z", "occurrences": 1, "counted_occurrences": 1, "suppressed_occurrences": 0, "state": "causal", "dimension_states": {"aic": "causal"}, "cost": {"aic": 0.5, "tokens": {"input": 100, "output": 20, "cache_read": 5, "cache_write": 1, "reasoning": 2, "total": 128}, "turns": 0, "tool_calls": 1, "latency_ms": 250}},
      {"id": "session_tool_failure:aggregate", "driver": "session_tool_failure", "source": "agent_session", "group_id": "tool_failure", "label": "agent session tool executions", "occurrences": 3, "counted_occurrences": 2, "suppressed_occurrences": 1, "suppressed_by": "causal-group:tool_failure", "state": "statistical", "dimension_states": {"aic": "statistical"}, "cost": {"aic": 0.75, "tokens": {"input": 100, "output": 20, "cache_read": 5, "cache_write": 1, "reasoning": 2, "total": 128}, "turns": 0, "tool_calls": 2, "latency_ms": 0}}
    ],
    "unmeasured_drivers": [{"driver": "firewall_block", "reason": "source_unavailable:firewall"}]
  }
}`

func writeUsageSummary(t *testing.T, contents string) string {
	t.Helper()
	runDir := t.TempDir()
	activityDir := filepath.Join(runDir, "usage", "activity")
	require.NoError(t, os.MkdirAll(activityDir, 0o755), "mkdir")
	require.NoError(t, os.WriteFile(filepath.Join(activityDir, "summary.json"), []byte(contents), 0o600), "write summary")
	return runDir
}

func TestLoadUsageActivitySummaryDecodesFriction(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	require.NoError(t, err)
	require.NotNil(t, summary.Friction, "expected friction section")
	f := summary.Friction
	assert.Equal(t, FrictionStateStatistical, f.MeasurementState)
	assert.Equal(t, "aic", f.CanonicalUnit)
	assert.Equal(t, 1.25, f.Cost.AIC)
	assert.Equal(t, int64(256), f.Cost.Tokens.Total)
	assert.Equal(t, int64(10), f.Cost.Tokens.CacheRead)
	assert.Equal(t, int64(4), f.Cost.Tokens.Reasoning)
	assert.Equal(t, 1, f.Cost.Turns)
	assert.Equal(t, 3, f.Cost.ToolCalls)
	assert.Equal(t, int64(550), f.Cost.LatencyMS)
	assert.Equal(t, 3, f.CountedOccurrences)
	assert.Equal(t, 1, f.SuppressedOccurrences)
	require.NotNil(t, f.TotalRunAIC)
	assert.Equal(t, 2.5, *f.TotalRunAIC)
	assert.True(t, f.TotalRunAICPartial)
	require.NotNil(t, f.FrictionRatio)
	assert.Equal(t, 0.5, *f.FrictionRatio)
	aic, ok := f.Uncertainty["aic"]
	require.True(t, ok, "expected aic uncertainty")
	require.NotNil(t, aic.RelativeError)
	assert.Equal(t, 0.3, *aic.RelativeError)
	assert.Equal(t, "mean_invocation_apportionment", aic.Method)
	assert.Equal(t, 2, aic.SampleSize)
	require.Len(t, f.Drivers, 2)
	require.Len(t, f.Groups, 1)
	require.Len(t, f.Events, 2)
	assert.Equal(t, "causal-group:tool_failure", f.Events[1].SuppressedBy)
	assert.Equal(t, 3, f.Groups[0].CountedOccurrences)
	require.Len(t, f.UnmeasuredDrivers, 1)
	assert.Equal(t, "source_unavailable:firewall", f.UnmeasuredDrivers[0].Reason)
	assert.False(t, f.Derived, "precomputed friction must not be marked derived")
}

func TestApplyUsageActivitySummaryPrefersPrecomputedFriction(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	require.NoError(t, err)
	result := &DownloadResult{}
	applyUsageActivitySummaryToResult(summary, result, true)
	require.NotNil(t, result.Friction, "expected friction on download result")
	assert.Equal(t, 1.25, result.Friction.Cost.AIC)

	// A derived fallback must never replace a precomputed section.
	derived := deriveFrictionFromLogs(&MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c9", Status: "error"}}}, summary.Session)
	require.NotNil(t, derived, "expected derived fallback for the historical path")
	assert.False(t, result.Friction.Derived, "precomputed friction was overwritten by a derived summary")
}

func TestHistoricalRunsWithoutPrecomputedFrictionDeriveFallback(t *testing.T) {
	legacy := `{"schema":"usage-activity-summary/v1","session":{"total_events":3,"turns":2},"working_set":{"measurement_state":"measured","cumulative_input_tokens":10,"peak_input_tokens":10,"rebuild_excess_tokens":0,"invocations":1}}`
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, legacy))
	require.NoError(t, err)
	assert.Nil(t, summary.Friction, "legacy summary must not synthesize a friction section")
	result := &DownloadResult{}
	applyUsageActivitySummaryToResult(summary, result, true)
	require.NotNil(t, result.Friction, "expected a derived zero-count friction fallback")
	assert.True(t, result.Friction.Derived)
	assert.Equal(t, 0, result.Friction.CountedOccurrences)
	// Every consumer helper must tolerate the nil section.
	assert.Empty(t, frictionDisplayValue(nil))
	assert.Empty(t, frictionSummaryLine(nil))
	assert.Empty(t, frictionRelativeError(nil))
	renderConsoleFriction(nil)
}

func TestHistoricalFrictionFallbackUsesAvailableUsageAndLogs(t *testing.T) {
	summaryJSON := `{"schema":"usage-activity-summary/v1","session":{"failed_tool_executions":1}}`
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, summaryJSON))
	require.NoError(t, err)
	result := &DownloadResult{
		RunAnalysis: RunAnalysis{
			MCPToolUsage: &MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c1", ServerName: "github", ToolName: "issue_read", Status: "error"}}},
		},
	}
	applyUsageActivitySummaryToResult(summary, result, true)
	require.NotNil(t, result.Friction, "expected historical fallback")
	assert.True(t, result.Friction.Derived)
	assert.Equal(t, 2, result.Friction.TotalOccurrences)
	assert.Equal(t, 1, result.Friction.CountedOccurrences)

	result = &DownloadResult{RunAnalysis: RunAnalysis{
		MCPToolUsage: &MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c2", Status: "failed"}}},
	}}
	backfilled := backfillCacheHitIfNeeded(result, t.TempDir(), false)
	assert.True(t, backfilled, "expected no-summary cache backfill")
	require.NotNil(t, result.Friction, "expected no-summary cache fallback")
	assert.True(t, result.Friction.Derived)

	result = &DownloadResult{RunAnalysis: RunAnalysis{MCPToolUsage: &MCPToolUsageData{}}}
	backfilled = backfillCacheHitIfNeeded(result, t.TempDir(), false)
	assert.False(t, backfilled, "empty cache activity should not trigger backfill")
	assert.Nil(t, result.Friction, "empty cache activity should not create friction")
}

func TestDeriveFrictionFromLogsDeduplicatesSessionFailures(t *testing.T) {
	usage := &MCPToolUsageData{
		ToolCalls: []MCPToolCall{
			{ToolCallID: "c1", ServerName: "github", ToolName: "issue_read", Status: "error"},
			{ToolCallID: "c2", ServerName: "github", ToolName: "issue_read", Status: "success"},
			{ToolCallID: "c3", ServerName: "github", ToolName: "list_issues", Status: "failed"},
		},
		Integrity: &IntegrityFilterSummary{TotalFiltered: 2},
	}
	got := deriveFrictionFromLogs(usage, &usageActivitySession{FailedToolExecutions: 3})
	require.NotNil(t, got, "expected derived friction")
	assert.True(t, got.Derived, "derived summary must be marked derived")
	assert.Equal(t, FrictionStateUnavailable, got.MeasurementState, "derived AIC must be unavailable")
	assert.Equal(t, 0.0, got.Cost.AIC, "derived summary must not invent AIC")
	// 2 gateway failures + 2 integrity filters + 3 session failures reported,
	// of which only 1 session failure is in excess of the gateway observations.
	assert.Equal(t, 7, got.TotalOccurrences)
	assert.Equal(t, 5, got.CountedOccurrences)
	assert.Equal(t, 2, got.SuppressedOccurrences)
	assert.Equal(t, 5, got.Cost.ToolCalls)
	drivers := map[string]FrictionDriverSummary{}
	for _, d := range got.Drivers {
		drivers[d.Driver] = d
	}
	assert.Equal(t, 1, drivers["session_tool_failure"].CountedOccurrences)
	assert.Equal(t, 2, drivers["mcp_tool_error"].Occurrences)
}

func TestDeriveFrictionFromLogsReturnsNilWithoutFriction(t *testing.T) {
	assert.Nil(t, deriveFrictionFromLogs(nil, nil))
	clean := &MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c1", Status: "success"}}}
	got := deriveFrictionFromLogs(clean, &usageActivitySession{FailedToolExecutions: 0})
	require.NotNil(t, got, "expected a derived zero for a friction-free run")
	assert.Equal(t, 0, got.CountedOccurrences)
	assert.True(t, got.Derived)
}

func TestIsFrictionToolCallStatus(t *testing.T) {
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"error", true},
		{"ERROR", true},
		{" failed ", true},
		{"failure", true},
		{"success", false},
		{"", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			assert.Equal(t, tc.want, isFrictionToolCallStatus(tc.status))
		})
	}
}

func TestFormatAICValue(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{0.0025, "0.0025"},
		{0.5, "0.5"},
		{1.239, "1.24"},
		{250.4, "250"},
	} {
		assert.Equal(t, tc.want, formatAICValue(tc.in), "formatAICValue(%v)", tc.in)
	}
}

func TestFrictionDisplayValue(t *testing.T) {
	assert.Empty(t, frictionDisplayValue(&FrictionCostSummary{MeasurementState: FrictionStateUnavailable}), "unavailable friction should render empty")
	assert.Equal(t, "2.50", frictionDisplayValue(&FrictionCostSummary{MeasurementState: FrictionStateCausal, Cost: FrictionCost{AIC: 2.5}}))
}

func TestFrictionSummaryLineIncludesEveryMeasuredDimension(t *testing.T) {
	rel := 0.3
	ratio := 0.5
	lower, upper := 1.0, 1.5
	f := &FrictionCostSummary{
		MeasurementState:      FrictionStateStatistical,
		CountedOccurrences:    3,
		SuppressedOccurrences: 1,
		FrictionRatio:         &ratio,
		Cost:                  FrictionCost{AIC: 1.25, Tokens: FrictionTokens{Total: 256}, Turns: 1, ToolCalls: 3, LatencyMS: 550},
		Uncertainty:           map[string]FrictionUncertain{"aic": {State: FrictionStateStatistical, RelativeError: &rel, LowerBound: &lower, UpperBound: &upper}},
	}
	line := frictionSummaryLine(f)
	for _, want := range []string{"occurrences=3", "aic=1.25", "ratio=50.0%", "range=1.00–1.50 AIC", "tokens=256", "turns=1", "tool-calls=3", "latency=550ms", "state=statistical", "±30%", "deduped=1"} {
		assert.Contains(t, line, want)
	}

	unavailable := frictionSummaryLine(&FrictionCostSummary{MeasurementState: FrictionStateUnavailable, TotalOccurrences: 2, CountedOccurrences: 2, Derived: true})
	assert.Contains(t, unavailable, "state=unavailable")
	assert.Contains(t, unavailable, "derived-from-logs")
	assert.NotContains(t, unavailable, "aic=", "unavailable friction must not report AIC")
	assert.Empty(t, frictionSummaryLine(&FrictionCostSummary{MeasurementState: FrictionStateUnavailable}), "friction-free run should render nothing")
}

func TestRenderConsoleFrictionShowsAggregateAndEvents(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	require.NoError(t, err)
	output := captureFrictionStderr(t, func() {
		renderConsoleMetrics(MetricsData{Friction: summary.Friction})
		renderConsoleFriction(summary.Friction)
	})
	for _, want := range []string{
		"friction: occurrences=3",
		"state=statistical",
		"friction_drivers:",
		"source=mcp_gateway driver=mcp_tool_error occurrences=1 counted=1 state=causal aic=0.5 tokens=128 tool-calls=1 latency=250ms",
		"source=agent_session driver=session_tool_failure occurrences=3 counted=2 state=statistical aic=0.75 tokens=128 tool-calls=2",
		"friction_events:",
		"source=mcp_gateway driver=mcp_tool_error: github/issue_read x1 counted=1 state=causal aic=0.5 tokens=128 tool-calls=1 latency=250ms",
		"suppressed-by=causal-group:tool_failure",
	} {
		assert.Contains(t, output, want)
	}
}

func TestRenderConsoleFrictionAttributesCostsToSource(t *testing.T) {
	friction := &FrictionCostSummary{
		Drivers: []FrictionDriverSummary{{
			Driver:             "firewall_block",
			Source:             "firewall",
			Occurrences:        2,
			CountedOccurrences: 2,
			State:              FrictionStateStatistical,
			Cost:               FrictionCost{AIC: 0.25, Tokens: FrictionTokens{Total: 64}},
		}},
		Events: []FrictionEvent{{
			Driver:             "firewall_block",
			Source:             "firewall",
			Label:              "blocked.example",
			Occurrences:        2,
			CountedOccurrences: 2,
			State:              FrictionStateStatistical,
			Cost:               FrictionCost{AIC: 0.25, Tokens: FrictionTokens{Total: 64}},
		}},
	}
	output := captureFrictionStderr(t, func() { renderConsoleFriction(friction) })
	for _, want := range []string{
		"source=firewall driver=firewall_block occurrences=2 counted=2 state=statistical aic=0.25 tokens=64",
		"source=firewall driver=firewall_block: blocked.example x2 counted=2 state=statistical aic=0.25 tokens=64",
	} {
		assert.Contains(t, output, want)
	}
}

func TestRenderConsoleFrictionTruncatesLongEventLists(t *testing.T) {
	friction := &FrictionCostSummary{
		MeasurementState: FrictionStateUnavailable,
		Drivers:          []FrictionDriverSummary{{Driver: "firewall_block", Source: "firewall", Occurrences: 30, CountedOccurrences: 30, State: FrictionStateUnavailable}},
		EventsTruncated:  true,
	}
	for range maxConsoleFrictionEvents + 5 {
		friction.Events = append(friction.Events, FrictionEvent{Driver: "firewall_block", Label: "blocked.example", Occurrences: 1, CountedOccurrences: 1, State: FrictionStateUnavailable})
	}
	output := captureFrictionStderr(t, func() { renderConsoleFriction(friction) })
	assert.Contains(t, output, "... 5 more events", "expected console truncation notice")
	assert.Contains(t, output, "event list truncated during measurement", "expected measurement truncation notice")
}

func TestAuditDataSerializesFriction(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	require.NoError(t, err)
	data := AuditData{Friction: summary.Friction, Metrics: MetricsData{Friction: summary.Friction}}
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	var decoded struct {
		Friction *FrictionCostSummary `json:"friction"`
		Metrics  map[string]any       `json:"metrics"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.NotNil(t, decoded.Friction, "friction round-trip failed")
	assert.Equal(t, 1.25, decoded.Friction.Cost.AIC)
	assert.Len(t, decoded.Friction.Events, 2, "expected event-level detail in audit JSON")
	require.NotEmpty(t, decoded.Friction.Drivers)
	assert.Equal(t, "mcp_gateway", decoded.Friction.Drivers[0].Source, "audit JSON must retain source-attributed driver cost")
	assert.Equal(t, 0.5, decoded.Friction.Drivers[0].Cost.AIC)
	require.NotEmpty(t, decoded.Friction.Events)
	assert.Equal(t, "mcp_gateway", decoded.Friction.Events[0].Source, "audit JSON must retain source-attributed event cost")
	assert.Equal(t, 0.5, decoded.Friction.Events[0].Cost.AIC)
	assert.NotContains(t, decoded.Metrics, "friction", "friction must be serialized once, at the top level")

	// Historical audit payloads omit the key entirely.
	legacy, err := json.Marshal(AuditData{})
	require.NoError(t, err)
	assert.False(t, bytes.Contains(legacy, []byte("\"friction\"")), "friction must be omitted when absent: %s", legacy)
}

func captureFrictionStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err, "pipe")
	os.Stderr = w
	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r)
		done <- buf.String()
	}()
	fn()
	_ = w.Close()
	os.Stderr = original
	return <-done
}

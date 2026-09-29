package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if err := os.MkdirAll(activityDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activityDir, "summary.json"), []byte(contents), 0o600); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	return runDir
}

func TestLoadUsageActivitySummaryDecodesFriction(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	if err != nil {
		t.Fatalf("loadUsageActivitySummary: %v", err)
	}
	if summary.Friction == nil {
		t.Fatal("expected friction section")
	}
	f := summary.Friction
	if f.MeasurementState != FrictionStateStatistical {
		t.Errorf("measurement_state = %q", f.MeasurementState)
	}
	if f.CanonicalUnit != "aic" {
		t.Errorf("canonical_unit = %q", f.CanonicalUnit)
	}
	if f.Cost.AIC != 1.25 {
		t.Errorf("cost.aic = %v", f.Cost.AIC)
	}
	if f.Cost.Tokens.Total != 256 || f.Cost.Tokens.CacheRead != 10 || f.Cost.Tokens.Reasoning != 4 {
		t.Errorf("token classes = %+v", f.Cost.Tokens)
	}
	if f.Cost.Turns != 1 || f.Cost.ToolCalls != 3 || f.Cost.LatencyMS != 550 {
		t.Errorf("dimensions = %+v", f.Cost)
	}
	if f.CountedOccurrences != 3 || f.SuppressedOccurrences != 1 {
		t.Errorf("occurrence accounting = counted %d suppressed %d", f.CountedOccurrences, f.SuppressedOccurrences)
	}
	if f.TotalRunAIC == nil || *f.TotalRunAIC != 2.5 || !f.TotalRunAICPartial || f.FrictionRatio == nil || *f.FrictionRatio != 0.5 {
		t.Errorf("run AIC coverage = total %v partial %v ratio %v", f.TotalRunAIC, f.TotalRunAICPartial, f.FrictionRatio)
	}
	aic, ok := f.Uncertainty["aic"]
	if !ok || aic.RelativeError == nil || *aic.RelativeError != 0.3 || aic.Method != "mean_invocation_apportionment" || aic.SampleSize != 2 {
		t.Errorf("aic uncertainty = %+v", aic)
	}
	if len(f.Drivers) != 2 || len(f.Groups) != 1 || len(f.Events) != 2 {
		t.Fatalf("drivers=%d groups=%d events=%d", len(f.Drivers), len(f.Groups), len(f.Events))
	}
	if f.Events[1].SuppressedBy != "causal-group:tool_failure" {
		t.Errorf("suppressed_by = %q", f.Events[1].SuppressedBy)
	}
	if f.Groups[0].CountedOccurrences != 3 {
		t.Errorf("group counted = %d", f.Groups[0].CountedOccurrences)
	}
	if len(f.UnmeasuredDrivers) != 1 || f.UnmeasuredDrivers[0].Reason != "source_unavailable:firewall" {
		t.Errorf("unmeasured drivers = %+v", f.UnmeasuredDrivers)
	}
	if f.Derived {
		t.Error("precomputed friction must not be marked derived")
	}
}

func TestApplyUsageActivitySummaryPrefersPrecomputedFriction(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	if err != nil {
		t.Fatalf("loadUsageActivitySummary: %v", err)
	}
	result := &DownloadResult{}
	applyUsageActivitySummaryToResult(summary, result, true)
	if result.Friction == nil {
		t.Fatal("expected friction on download result")
	}
	if result.Friction.Cost.AIC != 1.25 {
		t.Errorf("cost.aic = %v", result.Friction.Cost.AIC)
	}

	// A derived fallback must never replace a precomputed section.
	derived := deriveFrictionFromLogs(&MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c9", Status: "error"}}}, summary.Session)
	if derived == nil {
		t.Fatal("expected derived fallback for the historical path")
	}
	if result.Friction.Derived {
		t.Error("precomputed friction was overwritten by a derived summary")
	}
}

func TestHistoricalRunsWithoutFrictionRemainCompatible(t *testing.T) {
	legacy := `{"schema":"usage-activity-summary/v1","session":{"total_events":3,"turns":2},"working_set":{"measurement_state":"measured","cumulative_input_tokens":10,"peak_input_tokens":10,"rebuild_excess_tokens":0,"invocations":1}}`
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, legacy))
	if err != nil {
		t.Fatalf("loadUsageActivitySummary: %v", err)
	}

	if summary.Friction != nil {
		t.Fatal("legacy summary must not synthesize a friction section")
	}
	result := &DownloadResult{}
	applyUsageActivitySummaryToResult(summary, result, true)
	if result.Friction != nil {
		t.Error("legacy runs must leave friction nil")
	}
	// Every consumer helper must tolerate the nil section.
	if got := frictionDisplayValue(nil); got != "" {
		t.Errorf("frictionDisplayValue(nil) = %q", got)
	}
	if got := frictionSummaryLine(nil); got != "" {
		t.Errorf("frictionSummaryLine(nil) = %q", got)
	}
	if got := frictionRelativeError(nil); got != "" {
		t.Errorf("frictionRelativeError(nil) = %q", got)
	}
	renderConsoleFriction(nil)
}

func TestHistoricalFrictionFallbackUsesAvailableUsageAndLogs(t *testing.T) {
	summaryJSON := `{"schema":"usage-activity-summary/v1","session":{"failed_tool_executions":1}}`
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, summaryJSON))
	if err != nil {
		t.Fatalf("loadUsageActivitySummary: %v", err)
	}
	result := &DownloadResult{
		RunAnalysis: RunAnalysis{
			MCPToolUsage: &MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c1", ServerName: "github", ToolName: "issue_read", Status: "error"}}},
		},
	}
	applyUsageActivitySummaryToResult(summary, result, true)
	if result.Friction == nil || !result.Friction.Derived {
		t.Fatalf("expected historical fallback, got %+v", result.Friction)
	}
	if result.Friction.TotalOccurrences != 2 || result.Friction.CountedOccurrences != 1 {
		t.Errorf("fallback occurrences = %d counted = %d", result.Friction.TotalOccurrences, result.Friction.CountedOccurrences)
	}

	result = &DownloadResult{RunAnalysis: RunAnalysis{
		MCPToolUsage: &MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c2", Status: "failed"}}},
	}}
	if !backfillCacheHitIfNeeded(result, t.TempDir(), false) || result.Friction == nil || !result.Friction.Derived {
		t.Fatalf("expected no-summary cache fallback, got %+v", result.Friction)
	}

	result = &DownloadResult{RunAnalysis: RunAnalysis{MCPToolUsage: &MCPToolUsageData{}}}
	if backfillCacheHitIfNeeded(result, t.TempDir(), false) || result.Friction != nil {
		t.Fatalf("empty cache activity should not create friction or trigger backfill: %+v", result.Friction)
	}
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
	if got == nil {
		t.Fatal("expected derived friction")
	}
	if !got.Derived {
		t.Error("derived summary must be marked derived")
	}
	if got.MeasurementState != FrictionStateUnavailable {
		t.Errorf("derived AIC must be unavailable, got %q", got.MeasurementState)
	}
	if got.Cost.AIC != 0 {
		t.Errorf("derived summary must not invent AIC, got %v", got.Cost.AIC)
	}
	// 2 gateway failures + 2 integrity filters + 3 session failures reported,
	// of which only 1 session failure is in excess of the gateway observations.
	if got.TotalOccurrences != 7 {
		t.Errorf("total occurrences = %d, want 7", got.TotalOccurrences)
	}
	if got.CountedOccurrences != 5 {
		t.Errorf("counted occurrences = %d, want 5", got.CountedOccurrences)
	}
	if got.SuppressedOccurrences != 2 {
		t.Errorf("suppressed occurrences = %d, want 2", got.SuppressedOccurrences)
	}
	if got.Cost.ToolCalls != 5 {
		t.Errorf("tool calls = %d, want 5", got.Cost.ToolCalls)
	}
	drivers := map[string]FrictionDriverSummary{}
	for _, d := range got.Drivers {
		drivers[d.Driver] = d
	}
	if drivers["session_tool_failure"].CountedOccurrences != 1 {
		t.Errorf("session counted = %d, want 1", drivers["session_tool_failure"].CountedOccurrences)
	}
	if drivers["mcp_tool_error"].Occurrences != 2 {
		t.Errorf("gateway occurrences = %d, want 2", drivers["mcp_tool_error"].Occurrences)
	}
}

func TestDeriveFrictionFromLogsReturnsNilWithoutFriction(t *testing.T) {
	if got := deriveFrictionFromLogs(nil, nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
	clean := &MCPToolUsageData{ToolCalls: []MCPToolCall{{ToolCallID: "c1", Status: "success"}}}
	if got := deriveFrictionFromLogs(clean, &usageActivitySession{FailedToolExecutions: 0}); got != nil {
		t.Errorf("expected nil for a friction-free run, got %+v", got)
	}
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
		if got := isFrictionToolCallStatus(tc.status); got != tc.want {
			t.Errorf("isFrictionToolCallStatus(%q) = %v, want %v", tc.status, got, tc.want)
		}
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
		if got := formatAICValue(tc.in); got != tc.want {
			t.Errorf("formatAICValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFrictionDisplayValue(t *testing.T) {
	if got := frictionDisplayValue(&FrictionCostSummary{MeasurementState: FrictionStateUnavailable}); got != "" {
		t.Errorf("unavailable friction should render empty, got %q", got)
	}
	got := frictionDisplayValue(&FrictionCostSummary{MeasurementState: FrictionStateCausal, Cost: FrictionCost{AIC: 2.5}})
	if got != "2.50" {
		t.Errorf("frictionDisplayValue = %q, want 2.50", got)
	}
}

func TestFrictionSummaryLineIncludesEveryMeasuredDimension(t *testing.T) {
	rel := 0.3
	f := &FrictionCostSummary{
		MeasurementState:      FrictionStateStatistical,
		CountedOccurrences:    3,
		SuppressedOccurrences: 1,
		Cost:                  FrictionCost{AIC: 1.25, Tokens: FrictionTokens{Total: 256}, Turns: 1, ToolCalls: 3, LatencyMS: 550},
		Uncertainty:           map[string]FrictionUncertain{"aic": {State: FrictionStateStatistical, RelativeError: &rel}},
	}
	line := frictionSummaryLine(f)
	for _, want := range []string{"occurrences=3", "aic=1.25", "tokens=256", "turns=1", "tool-calls=3", "latency=550ms", "state=statistical", "±30%", "deduped=1"} {
		if !strings.Contains(line, want) {
			t.Errorf("friction line %q missing %q", line, want)
		}
	}

	unavailable := frictionSummaryLine(&FrictionCostSummary{MeasurementState: FrictionStateUnavailable, TotalOccurrences: 2, CountedOccurrences: 2, Derived: true})
	if !strings.Contains(unavailable, "state=unavailable") || !strings.Contains(unavailable, "derived-from-logs") {
		t.Errorf("unavailable friction line = %q", unavailable)
	}
	if strings.Contains(unavailable, "aic=") {
		t.Errorf("unavailable friction must not report AIC: %q", unavailable)
	}
	if got := frictionSummaryLine(&FrictionCostSummary{MeasurementState: FrictionStateUnavailable}); got != "" {
		t.Errorf("friction-free run should render nothing, got %q", got)
	}
}

func TestRenderConsoleFrictionShowsAggregateAndEvents(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	if err != nil {
		t.Fatalf("loadUsageActivitySummary: %v", err)
	}
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
		if !strings.Contains(output, want) {
			t.Errorf("console output missing %q:\n%s", want, output)
		}
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
		if !strings.Contains(output, want) {
			t.Errorf("console output missing %q:\n%s", want, output)
		}
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
	if !strings.Contains(output, "... 5 more events") {
		t.Errorf("expected console truncation notice:\n%s", output)
	}
	if !strings.Contains(output, "event list truncated during measurement") {
		t.Errorf("expected measurement truncation notice:\n%s", output)
	}
}

func TestAuditDataSerializesFriction(t *testing.T) {
	summary, err := loadUsageActivitySummary(writeUsageSummary(t, frictionSummaryJSON))
	if err != nil {
		t.Fatalf("loadUsageActivitySummary: %v", err)
	}
	data := AuditData{Friction: summary.Friction, Metrics: MetricsData{Friction: summary.Friction}}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		Friction *FrictionCostSummary `json:"friction"`
		Metrics  map[string]any       `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Friction == nil || decoded.Friction.Cost.AIC != 1.25 {
		t.Fatalf("friction round-trip failed: %+v", decoded.Friction)
	}
	if len(decoded.Friction.Events) != 2 {
		t.Errorf("expected event-level detail in audit JSON, got %d events", len(decoded.Friction.Events))
	}
	if decoded.Friction.Drivers[0].Source != "mcp_gateway" || decoded.Friction.Drivers[0].Cost.AIC != 0.5 {
		t.Errorf("audit JSON must retain source-attributed driver cost: %+v", decoded.Friction.Drivers[0])
	}
	if decoded.Friction.Events[0].Source != "mcp_gateway" || decoded.Friction.Events[0].Cost.AIC != 0.5 {
		t.Errorf("audit JSON must retain source-attributed event cost: %+v", decoded.Friction.Events[0])
	}
	if _, duplicated := decoded.Metrics["friction"]; duplicated {
		t.Error("friction must be serialized once, at the top level")
	}

	// Historical audit payloads omit the key entirely.
	legacy, err := json.Marshal(AuditData{})
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if bytes.Contains(legacy, []byte("\"friction\"")) {
		t.Errorf("friction must be omitted when absent: %s", legacy)
	}
}

func captureFrictionStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
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

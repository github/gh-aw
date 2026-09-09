//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadUsageActivitySummary(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755), "should create usage activity directory")
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"firewall":{
			"total_requests":10,
			"allowed_requests":8,
			"blocked_requests":2,
			"allowed_domains":["api.github.com:443"],
			"blocked_domains":["blocked.example.com:443"],
			"requests_by_domain":{
				"api.github.com:443":{"allowed":8,"blocked":0},
				"blocked.example.com:443":{"allowed":0,"blocked":2}
			}
		},
		"session":{"turns":7},
		"gateway":{
			"total_calls":5,
			"failed_calls":1,
			"tool_calls":[{"tool_call_id":"call-1","timestamp":"2026-09-09T00:00:00Z","server_name":"github","tool_name":"issue_read","request_size":100,"response_size":200,"duration_ms":25,"outcome":"success"}]
		}
	}`), 0o644), "should write usage activity summary")

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err, "loadUsageActivitySummary should parse the primary usage path")
	require.NotNil(t, summary, "summary should not be nil")
	require.NotNil(t, summary.Firewall, "firewall section should be present")
	assert.Equal(t, 10, summary.Firewall.TotalRequests, "firewall total_requests should be parsed from JSON")
	assert.Equal(t, []string{"api.github.com:443"}, summary.Firewall.AllowedDomains, "firewall allowed_domains should be parsed from JSON")
	assert.Equal(t, []string{"blocked.example.com:443"}, summary.Firewall.BlockedDomains, "firewall blocked_domains should be parsed from JSON")
	assert.Len(t, summary.Firewall.RequestsByDomain, 2, "firewall requests_by_domain should be parsed from JSON")
	require.NotNil(t, summary.Session, "session section should be present")
	assert.Equal(t, 7, summary.Session.Turns, "session turns should be parsed from JSON")
	require.NotNil(t, summary.Gateway, "gateway section should be present")
	assert.Equal(t, 5, summary.Gateway.TotalCalls, "gateway total_calls should be parsed from JSON")
	require.Len(t, summary.Gateway.ToolCalls, 1, "gateway tool_calls should be parsed from JSON")
	assert.Equal(t, usageActivityGatewayCall{ToolCallID: "call-1", Timestamp: "2026-09-09T00:00:00Z", ServerName: "github", ToolName: "issue_read", RequestSize: 100, ResponseSize: 200, DurationMS: 25, Outcome: "success"}, summary.Gateway.ToolCalls[0])

	var result DownloadResult
	applyUsageActivitySummaryToResult(summary, &result, true)
	require.NotNil(t, result.MCPToolUsage, "loaded gateway summary should populate MCP usage")
	require.Len(t, result.MCPToolUsage.ToolCalls, 1, "loaded gateway summary should populate MCP call records")
	assert.Equal(t, MCPToolCall{
		ToolCallID: "call-1",
		Timestamp:  "2026-09-09T00:00:00Z",
		ServerName: "github",
		ToolName:   "issue_read",
		InputSize:  100,
		OutputSize: 200,
		Duration:   "25ms",
		Status:     "success",
	}, result.MCPToolUsage.ToolCalls[0])
}

func TestApplyUsageActivitySummaryToResult(t *testing.T) {
	t.Parallel()

	result := DownloadResult{}
	summary := &usageActivitySummary{
		Session: &usageActivitySession{Turns: 4},
		Firewall: &usageActivityFirewall{
			TotalRequests:   12,
			AllowedRequests: 9,
			BlockedRequests: 3,
			AllowedDomains:  []string{"api.github.com:443"},
			BlockedDomains:  []string{"blocked.example.com:443"},
			RequestsByDomain: map[string]DomainRequestStats{
				"api.github.com:443":      {Allowed: 9, Blocked: 0},
				"blocked.example.com:443": {Allowed: 0, Blocked: 3},
			},
		},
		Gateway: &usageActivityGateway{
			TotalCalls:      6,
			FailedCalls:     2,
			TotalInputSize:  120,
			TotalOutputSize: 240,
			MaxInputSize:    70,
			MaxOutputSize:   140,
			Servers: []usageActivityGatewayServer{
				{ServerName: "github", RequestCount: 5, ToolCallCount: 5, FailedCalls: 2, TotalInputSize: 100, TotalOutputSize: 200, AvgDurationMS: 12},
				{ServerName: "playwright", ToolCallCount: 1, FailedCalls: 0},
			},
			Tools: []usageActivityGatewayTool{
				{ServerName: "github", ToolName: "issue_read", CallCount: 5, FailedCalls: 2, TotalInputSize: 100, TotalOutputSize: 200, MaxInputSize: 70, MaxOutputSize: 140, AvgDurationMS: 12, MaxDurationMS: 30},
			},
			ToolCalls: []usageActivityGatewayCall{
				{ToolCallID: "call-1", Timestamp: "2026-09-09T00:00:00Z", ServerName: "github", ToolName: "issue_read", RequestSize: 100, ResponseSize: 200, DurationMS: 25, Outcome: "success"},
			},
		},
		Integrity: &IntegrityFilterSummary{
			TotalFiltered:        2,
			FilteredServerCounts: map[string]int{"github": 2},
			FilteredToolCounts:   map[string]int{"issue_read": 2},
			FilteredReasonCounts: map[string]int{"integrity": 2},
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, true)

	assert.Equal(t, 4, result.Run.Turns, "turns should be backfilled when detailed session artifacts are absent")
	require.NotNil(t, result.FirewallAnalysis, "firewall summary should be backfilled")
	assert.Equal(t, 12, result.FirewallAnalysis.TotalRequests, "firewall total requests should be copied from the summary")
	assert.Equal(t, 9, result.FirewallAnalysis.AllowedRequests, "firewall allowed requests should be copied from the summary")
	assert.Equal(t, 3, result.FirewallAnalysis.BlockedRequests, "firewall blocked requests should be copied from the summary")
	assert.Equal(t, []string{"api.github.com:443"}, result.FirewallAnalysis.AllowedDomains, "allowed domains should be backfilled from the summary")
	assert.Equal(t, []string{"blocked.example.com:443"}, result.FirewallAnalysis.BlockedDomains, "blocked domains should be backfilled from the summary")
	assert.Equal(t, DomainRequestStats{Allowed: 9, Blocked: 0}, result.FirewallAnalysis.RequestsByDomain["api.github.com:443"], "per-domain stats should be backfilled from the summary")
	require.NotNil(t, result.MCPToolUsage, "gateway summary should be backfilled")
	require.Len(t, result.MCPToolUsage.ToolCalls, 1, "usage-summary backfill should include tool call rows")
	assert.Equal(t, "call-1", result.MCPToolUsage.ToolCalls[0].ToolCallID, "opaque tool call ID should be preserved")
	assert.Equal(t, "2026-09-09T00:00:00Z", result.MCPToolUsage.ToolCalls[0].Timestamp, "call timestamp should be preserved")
	assert.Equal(t, "github", result.MCPToolUsage.ToolCalls[0].ServerName, "server name should be preserved")
	assert.Equal(t, "issue_read", result.MCPToolUsage.ToolCalls[0].ToolName, "tool name should be preserved")
	assert.Equal(t, 100, result.MCPToolUsage.ToolCalls[0].InputSize, "request size should be mapped to input size")
	assert.Equal(t, 200, result.MCPToolUsage.ToolCalls[0].OutputSize, "response size should be mapped to output size")
	assert.Equal(t, "25ms", result.MCPToolUsage.ToolCalls[0].Duration, "duration should be formatted")
	assert.Equal(t, "success", result.MCPToolUsage.ToolCalls[0].Status, "outcome should be mapped to status")
	require.Len(t, result.MCPToolUsage.Servers, 2, "gateway servers should be copied from the summary")
	assert.Equal(t, "github", result.MCPToolUsage.Servers[0].ServerName, "server names should be preserved")
	assert.Equal(t, 5, result.MCPToolUsage.Servers[0].RequestCount, "request counts should be preserved")
	assert.Equal(t, 5, result.MCPToolUsage.Servers[0].ToolCallCount, "tool call counts should be preserved")
	assert.Equal(t, 2, result.MCPToolUsage.Servers[0].ErrorCount, "failed call counts should map to server error counts")
	assert.Equal(t, 100, result.MCPToolUsage.Servers[0].TotalInputSize, "server input sizes should be preserved")
	assert.Equal(t, 200, result.MCPToolUsage.Servers[0].TotalOutputSize, "server output sizes should be preserved")
	assert.Equal(t, "12ms", result.MCPToolUsage.Servers[0].AvgDuration, "server average durations should be formatted")
	require.Len(t, result.MCPToolUsage.Summary, 1, "gateway tools should be copied from the summary")
	assert.Equal(t, "issue_read", result.MCPToolUsage.Summary[0].ToolName, "tool names should be preserved")
	assert.Equal(t, 100, result.MCPToolUsage.Summary[0].TotalInputSize, "tool input sizes should be preserved")
	assert.Equal(t, 140, result.MCPToolUsage.Summary[0].MaxOutputSize, "tool maximum output sizes should be preserved")
	assert.Equal(t, "30ms", result.MCPToolUsage.Summary[0].MaxDuration, "tool maximum durations should be formatted")
	require.NotNil(t, result.MCPToolUsage.Integrity, "integrity summary should be backfilled")
	assert.Equal(t, 2, result.MCPToolUsage.Integrity.TotalFiltered, "integrity filtered counts should be preserved")
	assert.Equal(t, map[string]int{"issue_read": 2}, result.MCPToolUsage.Integrity.FilteredToolCounts, "integrity tool counts should be preserved")
}

func TestApplyUsageActivitySummaryBackfillsIntegrityWithoutGateway(t *testing.T) {
	t.Parallel()

	result := DownloadResult{}
	summary := &usageActivitySummary{
		Integrity: &IntegrityFilterSummary{
			TotalFiltered:        1,
			FilteredServerCounts: map[string]int{"github": 1},
			FilteredToolCounts:   map[string]int{"issue_read": 1},
			FilteredReasonCounts: map[string]int{"integrity": 1},
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, true)

	require.NotNil(t, result.MCPToolUsage, "integrity-only summaries should create MCP usage data")
	require.NotNil(t, result.MCPToolUsage.Integrity, "integrity summary should be backfilled without gateway data")
	assert.Equal(t, 1, result.MCPToolUsage.Integrity.TotalFiltered, "integrity count should be preserved")
	assert.Empty(t, result.MCPToolUsage.Servers, "integrity-only summaries should not fabricate servers")
}

func TestApplyUsageActivitySummaryToResultLegacyFirewall(t *testing.T) {
	t.Parallel()

	result := DownloadResult{}
	summary := &usageActivitySummary{
		Firewall: &usageActivityFirewall{
			TotalRequests:   5,
			AllowedRequests: 5,
			BlockedRequests: 0,
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, true)

	require.NotNil(t, result.FirewallAnalysis, "legacy firewall summary should still backfill analysis")
	assert.Equal(t, 5, result.FirewallAnalysis.TotalRequests, "legacy total requests should be copied")
	assert.Equal(t, 5, result.FirewallAnalysis.AllowedRequests, "legacy allowed requests should be copied")
	assert.Equal(t, 0, result.FirewallAnalysis.BlockedRequests, "legacy blocked requests should be copied")
	assert.Empty(t, result.FirewallAnalysis.AllowedDomains, "legacy summaries should backfill empty allowed domains")
	assert.Empty(t, result.FirewallAnalysis.BlockedDomains, "legacy summaries should backfill empty blocked domains")
	require.NotNil(t, result.FirewallAnalysis.RequestsByDomain, "legacy summaries should backfill a non-nil requests map")
	assert.Empty(t, result.FirewallAnalysis.RequestsByDomain, "legacy summaries should backfill an empty requests map")
}

func TestLoadUsageActivitySummaryFallbackPath(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	fallbackPath := filepath.Join(runDir, "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(fallbackPath), 0o755), "should create fallback activity directory")
	require.NoError(t, os.WriteFile(fallbackPath, []byte(`{"schema":"`+usageActivitySummarySchema+`","session":{"turns":3}}`), 0o644), "should write fallback activity summary")

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err, "fallback activity summary should load without error")
	require.NotNil(t, summary, "summary should be loaded from the fallback path")
	require.NotNil(t, summary.Session, "session section should be present in the fallback summary")
	assert.Equal(t, 3, summary.Session.Turns, "session turns should be loaded from the fallback path")
}

func TestLoadUsageActivitySummaryNoFile(t *testing.T) {
	t.Parallel()

	summary, err := loadUsageActivitySummary(t.TempDir())
	require.NoError(t, err, "missing activity summary should not be treated as an error")
	assert.Nil(t, summary, "missing activity summary should return nil")
}

func TestLoadUsageActivitySummaryMalformedPrimaryFallsBack(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	primaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(primaryPath), 0o755), "should create primary activity directory")
	require.NoError(t, os.WriteFile(primaryPath, []byte(`{not valid json`), 0o644), "should write malformed primary summary")

	fallbackPath := filepath.Join(runDir, "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(fallbackPath), 0o755), "should create fallback activity directory")
	require.NoError(t, os.WriteFile(fallbackPath, []byte(`{"schema":"`+usageActivitySummarySchema+`","session":{"turns":5}}`), 0o644), "should write valid fallback summary")

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err, "valid fallback summary should be used when the primary summary is malformed")
	require.NotNil(t, summary, "fallback summary should be returned")
	require.NotNil(t, summary.Session, "session section should be present after fallback")
	assert.Equal(t, 5, summary.Session.Turns, "fallback session turns should be preserved")
}

func TestLoadUsageActivitySummaryRejectsUnsupportedSchema(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755), "should create usage activity directory")
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{"schema":"usage-activity-summary/v2"}`), 0o644), "should write unsupported schema summary")

	summary, err := loadUsageActivitySummary(runDir)
	require.Error(t, err, "unsupported activity summary schema should return an error")
	assert.Nil(t, summary, "unsupported schema should not be returned")
	require.ErrorContains(t, err, "unsupported usage activity summary schema", "schema validation error should explain the mismatch")
}

func TestApplyUsageActivitySummaryDoesNotOverwriteExistingData(t *testing.T) {
	t.Parallel()

	existingFirewall := &FirewallAnalysis{AnalysisBase: AnalysisBase{TotalRequests: 100}}
	existingMCP := &MCPToolUsageData{
		Summary:   []MCPToolSummary{},
		ToolCalls: []MCPToolCall{},
	}
	result := DownloadResult{RunAnalysis: RunAnalysis{
		Run:              WorkflowRun{Turns: 9},
		FirewallAnalysis: existingFirewall,
		MCPToolUsage:     existingMCP,
	},
	}
	summary := &usageActivitySummary{
		Session: &usageActivitySession{Turns: 4},
		Firewall: &usageActivityFirewall{
			TotalRequests:   12,
			AllowedRequests: 9,
			BlockedRequests: 3,
		},
		Gateway: &usageActivityGateway{
			Servers: []usageActivityGatewayServer{{ServerName: "github", ToolCallCount: 5, FailedCalls: 2}},
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, false)

	assert.Equal(t, 9, result.Run.Turns, "existing turns must not be overwritten when detailed artifacts are available")
	assert.Same(t, existingFirewall, result.FirewallAnalysis, "existing firewall analysis must not be replaced")
	assert.Same(t, existingMCP, result.MCPToolUsage, "existing MCP tool usage must not be replaced")
}

func TestApplyUsageActivitySummaryBackfillsMissingMCPMetrics(t *testing.T) {
	t.Parallel()

	existingMCP := &MCPToolUsageData{
		Summary: []MCPToolSummary{{
			ServerName:     "github",
			ToolName:       "issue_read",
			CallCount:      2,
			TotalInputSize: 7,
		}},
		Servers: []MCPServerStats{{
			MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 2},
			TotalInputSize:     7,
		}},
	}
	result := DownloadResult{RunAnalysis: RunAnalysis{MCPToolUsage: existingMCP}}
	summary := &usageActivitySummary{
		Gateway: &usageActivityGateway{
			Servers: []usageActivityGatewayServer{{
				ServerName: "github", TotalInputSize: 100, TotalOutputSize: 200,
				AvgDurationMS: 12,
			}},
			Tools: []usageActivityGatewayTool{{
				ServerName: "github", ToolName: "issue_read", CallCount: 2,
				TotalInputSize: 100, TotalOutputSize: 200, MaxInputSize: 70, MaxOutputSize: 140,
				AvgDurationMS: 12, MaxDurationMS: 30,
			}},
		},
		Integrity: &IntegrityFilterSummary{
			TotalFiltered:        1,
			FilteredServerCounts: map[string]int{"github": 1},
			FilteredToolCounts:   map[string]int{"issue_read": 1},
			FilteredReasonCounts: map[string]int{"integrity": 1},
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, false)

	assert.Same(t, existingMCP, result.MCPToolUsage, "existing detailed usage should retain precedence")
	require.Len(t, existingMCP.Summary, 1)
	assert.Equal(t, 2, existingMCP.Summary[0].CallCount, "call counts must not be duplicated")
	assert.Equal(t, 7, existingMCP.Summary[0].TotalInputSize, "existing non-zero metrics must not be overwritten")
	assert.Equal(t, 200, existingMCP.Summary[0].TotalOutputSize, "missing tool output size should be backfilled")
	assert.Equal(t, 140, existingMCP.Summary[0].MaxOutputSize, "missing tool maximum output size should be backfilled")
	assert.Equal(t, 7, existingMCP.Servers[0].TotalInputSize, "existing server metrics must not be overwritten")
	assert.Equal(t, 200, existingMCP.Servers[0].TotalOutputSize, "missing server output size should be backfilled")
	assert.Equal(t, "12ms", existingMCP.Servers[0].AvgDuration, "missing server duration should be backfilled")
	require.NotNil(t, existingMCP.Integrity, "missing integrity aggregates should be backfilled when detailed events are absent")
	assert.Equal(t, 1, existingMCP.Integrity.TotalFiltered)
}

func TestApplyUsageActivitySummaryBackfillsSafeItemsCount(t *testing.T) {
	t.Parallel()

	result := DownloadResult{}
	summary := &usageActivitySummary{
		SafeOutputs: &usageActivitySafeOutputs{
			TotalItems: 3,
			ItemsByType: map[string]int{
				"create_issue": 2,
				"add_comment":  1,
			},
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, true)

	assert.Equal(t, 3, result.Run.SafeItemsCount, "SafeItemsCount should be backfilled from usage summary safe_outputs.total_items")
}

func TestApplyUsageActivitySummaryDoesNotOverwriteExistingSafeItemsCount(t *testing.T) {
	t.Parallel()

	result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{SafeItemsCount: 5}}}
	summary := &usageActivitySummary{
		SafeOutputs: &usageActivitySafeOutputs{
			TotalItems: 3,
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, true)

	assert.Equal(t, 5, result.Run.SafeItemsCount, "existing SafeItemsCount must not be overwritten by usage summary")
}

func TestApplyUsageActivitySummarySafeItemsCountZeroNotBackfilled(t *testing.T) {
	t.Parallel()

	result := DownloadResult{}
	summary := &usageActivitySummary{
		SafeOutputs: &usageActivitySafeOutputs{
			TotalItems: 0,
		},
	}

	applyUsageActivitySummaryToResult(summary, &result, true)

	assert.Equal(t, 0, result.Run.SafeItemsCount, "zero safe items in summary should not alter SafeItemsCount")
}

func TestLoadUsageActivitySummaryWithSafeOutputs(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"safe_outputs":{
			"total_items":4,
			"items_by_type":{"create_issue":3,"add_comment":1}
		}
	}`), 0o644))

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.NotNil(t, summary.SafeOutputs, "safe_outputs section should be parsed from JSON")
	assert.Equal(t, 4, summary.SafeOutputs.TotalItems, "total_items should be parsed from JSON")
	assert.Equal(t, map[string]int{"create_issue": 3, "add_comment": 1}, summary.SafeOutputs.ItemsByType, "items_by_type should be parsed from JSON")
}

// TestLoadThenApplyUsageActivitySummaryBackfillsSafeItemsCount exercises the processor
// call order: loadUsageActivitySummary followed by applyUsageActivitySummaryToResult,
// verifying that SafeItemsCount is backfilled end-to-end from a summary.json file.
func TestLoadThenApplyUsageActivitySummaryBackfillsSafeItemsCount(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"safe_outputs":{
			"total_items":5,
			"items_by_type":{"create_issue":3,"add_comment":2}
		}
	}`), 0o644))

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err, "loadUsageActivitySummary should succeed")
	require.NotNil(t, summary, "summary should be non-nil")

	result := DownloadResult{}
	applyUsageActivitySummaryToResult(summary, &result, true)

	assert.Equal(t, 5, result.Run.SafeItemsCount, "SafeItemsCount should be backfilled from safe_outputs.total_items through the full load+apply pipeline")
}

// TestExtractThenApplyProcessorOrderingBackfillsSafeItemsCount verifies the processor
// call order: extractCreatedItemsFromManifest runs first (setting SafeItemsCount=0 when
// no manifest exists), then applyUsageActivitySummaryToResult backfills from the summary.
// This test would fail if the order were reversed (apply then extract), because extract
// would unconditionally overwrite the backfilled value with 0.
func TestExtractThenApplyProcessorOrderingBackfillsSafeItemsCount(t *testing.T) {
	t.Parallel()

	// Set up a run directory with a summary but NO manifest file.
	// extractCreatedItemsFromManifest will return nil (→ len=0).
	// applyUsageActivitySummaryToResult must then backfill SafeItemsCount=5.
	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"safe_outputs":{
			"total_items":5,
			"items_by_type":{"create_issue":3,"add_comment":2}
		}
	}`), 0o644))

	// Processor order: extract first, then apply (mirrors logs_run_processor.go).
	result := DownloadResult{}
	result.Run.SafeItemsCount = len(extractCreatedItemsFromManifest(runDir))

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err)
	require.NotNil(t, summary)
	applyUsageActivitySummaryToResult(summary, &result, true)

	assert.Equal(t, 5, result.Run.SafeItemsCount, "SafeItemsCount should be backfilled from summary when no manifest is present")
}

// TestCacheHitBackfillsStaleZeroSafeItemsCount verifies that backfillCacheHitIfNeeded
// re-applies the usage activity backfill when SafeItemsCount is zero in the cached
// run_summary.json. This covers stale cache entries that were saved before the
// safe-outputs backfill was introduced.
func TestCacheHitBackfillsStaleZeroSafeItemsCount(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()

	// Write an activity summary with safe_outputs populated.
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"safe_outputs":{"total_items":4,"items_by_type":{"create_issue":4}}
	}`), 0o644))

	// Simulate a stale cache: SafeItemsCount is 0 (saved before backfill existed).
	result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{SafeItemsCount: 0}}}

	backfillCacheHitIfNeeded(&result, runDir, false)

	assert.Equal(t, 4, result.Run.SafeItemsCount, "cache-hit backfill should heal stale SafeItemsCount=0 from activity summary")
}

// TestCacheHitBackfillsStaleZeroTurns verifies that backfillCacheHitIfNeeded correctly
// re-applies the usage activity backfill when Turns is zero in the cached
// run_summary.json. This covers stale cache entries where the session.turns
// backfill had not yet run.
func TestCacheHitBackfillsStaleZeroTurns(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()

	// Write an activity summary with a session turns count.
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"session":{"turns":34}
	}`), 0o644))

	// Simulate a stale cache: Turns is 0 (saved before turns backfill existed).
	result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{Turns: 0}}}

	backfillCacheHitIfNeeded(&result, runDir, false)

	assert.Equal(t, 34, result.Run.Turns, "cache-hit backfill should heal stale Turns=0 from activity summary")
}

func TestCacheHitBackfillsMissingMCPMetrics(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"gateway":{
			"servers":[{"server_name":"github","total_input_size":100,"total_output_size":200}],
			"tools":[{"server_name":"github","tool_name":"issue_read","call_count":2,"total_input_size":100,"total_output_size":200}]
		}
	}`), 0o644))

	result := DownloadResult{RunAnalysis: RunAnalysis{
		Run:        WorkflowRun{Turns: 3, SafeItemsCount: 1},
		WorkingSet: &WorkingSetMetrics{MeasurementState: "measured"},
		MCPToolUsage: &MCPToolUsageData{
			Summary: []MCPToolSummary{{ServerName: "github", ToolName: "issue_read", CallCount: 2}},
			Servers: []MCPServerStats{{MCPServerStatsBase: MCPServerStatsBase{ServerName: "github", ToolCallCount: 2}}},
		},
	}}

	assert.True(t, backfillCacheHitIfNeeded(&result, runDir, false), "activity summary should be applied on a cache hit")
	assert.Equal(t, 100, result.MCPToolUsage.Summary[0].TotalInputSize)
	assert.Equal(t, 200, result.MCPToolUsage.Servers[0].TotalOutputSize)
}

// TestCacheHitBackfillsStaleZeroTokenUsage verifies that backfillCacheHitIfNeeded
// heals cached run and metrics token counts from a preserved firewall proxy summary.
func TestCacheHitBackfillsStaleZeroTokenUsage(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()

	result := DownloadResult{RunAnalysis: RunAnalysis{
		Run:     WorkflowRun{TokenUsage: 0},
		Metrics: LogMetrics{TokenUsage: 0},
		TokenUsage: &TokenUsageSummary{
			TotalInputTokens:  2000,
			TotalOutputTokens: 1000,
		},
	},
	}

	backfillCacheHitIfNeeded(&result, runDir, false)

	assert.Equal(t, 3000, result.Metrics.TokenUsage, "cache-hit backfill should heal stale Metrics.TokenUsage from firewall token summary")
	assert.Equal(t, 3000, result.Run.TokenUsage, "cache-hit backfill should heal stale Run.TokenUsage from firewall token summary")
}

// TestCacheHitDoesNotOverwriteNonZeroValues verifies that backfillCacheHitIfNeeded
// is a no-op when both Run.Turns and Run.SafeItemsCount are already non-zero.
func TestCacheHitDoesNotOverwriteNonZeroValues(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()

	// Write an activity summary with different values than what is in the cache.
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"session":{"turns":99},
		"safe_outputs":{"total_items":99}
	}`), 0o644))

	// Cache has non-zero values — the backfill guard should be a no-op.
	result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{Turns: 14, SafeItemsCount: 5}}}

	backfillCacheHitIfNeeded(&result, runDir, false)

	// Guard condition is false (both >0), so neither value should change.
	assert.Equal(t, 14, result.Run.Turns, "non-zero cached Turns must not be overwritten by the cache-hit backfill guard")
	assert.Equal(t, 5, result.Run.SafeItemsCount, "non-zero cached SafeItemsCount must not be overwritten by the cache-hit backfill guard")
}

// TestCacheHitBackfillsPartialZeroTurns verifies that backfillCacheHitIfNeeded
// triggers on the || condition when only Turns is zero, backfills Turns from the
// activity summary, and leaves the non-zero SafeItemsCount untouched.
func TestCacheHitBackfillsPartialZeroTurns(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()

	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"session":{"turns":34},
		"safe_outputs":{"total_items":99}
	}`), 0o644))

	// Turns=0 triggers the guard; SafeItemsCount=5 is already non-zero.
	result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{Turns: 0, SafeItemsCount: 5}}}

	backfillCacheHitIfNeeded(&result, runDir, false)

	assert.Equal(t, 34, result.Run.Turns, "stale Turns=0 must be backfilled when SafeItemsCount is non-zero")
	assert.Equal(t, 5, result.Run.SafeItemsCount, "non-zero SafeItemsCount must not be overwritten when only Turns triggers the guard")
}

// TestCacheHitBackfillsPartialZeroSafeItemsCount verifies that backfillCacheHitIfNeeded
// triggers on the || condition when only SafeItemsCount is zero, backfills SafeItemsCount
// from the activity summary, and leaves the non-zero Turns untouched.
func TestCacheHitBackfillsPartialZeroSafeItemsCount(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()

	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"session":{"turns":99},
		"safe_outputs":{"total_items":4}
	}`), 0o644))

	// SafeItemsCount=0 triggers the guard; Turns=14 is already non-zero.
	result := DownloadResult{RunAnalysis: RunAnalysis{Run: WorkflowRun{Turns: 14, SafeItemsCount: 0}}}

	backfillCacheHitIfNeeded(&result, runDir, false)

	assert.Equal(t, 14, result.Run.Turns, "non-zero Turns must not be overwritten when only SafeItemsCount triggers the guard")
	assert.Equal(t, 4, result.Run.SafeItemsCount, "stale SafeItemsCount=0 must be backfilled when Turns is non-zero")
}

// TestMetricsTurnsZeroDoesNotOverwriteBackfilledTurns verifies that
// applyMetricsTurnsToRun preserves backfilled run.Turns when metrics.Turns is 0.
// This is the case for usage-only artifact downloads where no events.jsonl/.log
// files exist, so extractLogMetrics returns Turns=0.
func TestMetricsTurnsZeroDoesNotOverwriteBackfilledTurns(t *testing.T) {
	t.Parallel()

	// Simulate a result where the backfill set Run.Turns=34 but Metrics.Turns=0
	// because only the usage artifact was downloaded (no log files).
	run := WorkflowRun{Turns: 34}
	metrics := LogMetrics{Turns: 0}

	applyMetricsTurnsToRun(&run, metrics)

	assert.Equal(t, 34, run.Turns, "backfilled Turns must be preserved when Metrics.Turns is 0 (usage-only download)")
}

// TestMetricsTurnsNonZeroOverridesBackfilledTurns verifies that when full log
// artifacts are present (Metrics.Turns > 0), applyMetricsTurnsToRun uses the more
// precise log-derived count over the backfilled session.turns value.
func TestMetricsTurnsNonZeroOverridesBackfilledTurns(t *testing.T) {
	t.Parallel()

	run := WorkflowRun{Turns: 34}    // backfilled from session.turns
	metrics := LogMetrics{Turns: 36} // from events.jsonl (more precise)

	applyMetricsTurnsToRun(&run, metrics)

	assert.Equal(t, 36, run.Turns, "log-derived Metrics.Turns must override backfilled value when non-zero")
}

func TestUsageActivitySummaryBackfillsWorkingSet(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	summaryPath := filepath.Join(runDir, "usage", "activity", "summary.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(summaryPath), 0o755))
	require.NoError(t, os.WriteFile(summaryPath, []byte(`{
		"schema":"`+usageActivitySummarySchema+`",
		"working_set":{
			"measurement_state":"measured",
			"rebuild_factor":3.9017857142857144,
			"cumulative_input_tokens":874000,
			"peak_input_tokens":224000,
			"rebuild_excess_tokens":650000,
			"invocations":5
		}
	}`), 0o644))

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err)
	result := DownloadResult{}
	applyUsageActivitySummaryToResult(summary, &result, true)

	require.NotNil(t, result.WorkingSet)
	assert.Equal(t, "measured", result.WorkingSet.MeasurementState)
	require.NotNil(t, result.WorkingSet.RebuildFactor)
	assert.InDelta(t, 3.9017857142857144, *result.WorkingSet.RebuildFactor, 1e-12)
	assert.EqualValues(t, 874000, result.WorkingSet.CumulativeInputTokens)
	assert.Equal(t, 5, result.WorkingSet.Invocations)
}

func TestWSRFDisplayValue(t *testing.T) {
	t.Parallel()

	factor := 3.9017857142857144
	assert.Equal(t, "3.90", wsrfDisplayValue(&WorkingSetMetrics{RebuildFactor: &factor}))
	assert.Empty(t, wsrfDisplayValue(&WorkingSetMetrics{MeasurementState: "unavailable"}))
	assert.Empty(t, wsrfDisplayValue(nil))
}

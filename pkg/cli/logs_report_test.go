//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

func TestNormalizeJobNamePreservesPeriods(t *testing.T) {
	t.Parallel()

	if got := normalizeJobName(" Worker.V1 "); got != "worker.v1" {
		t.Errorf("normalizeJobName() = %q, want %q; periods are not separators in log job-name matching", got, "worker.v1")
	}
}

func TestToolUsageSummariesShareStatsBase(t *testing.T) {
	t.Parallel()

	for _, typ := range []reflect.Type{
		reflect.TypeFor[ToolUsageSummary](),
		reflect.TypeFor[MCPToolSummary](),
	} {
		field, ok := typ.FieldByName("ToolUsageStatsBase")
		if !ok || !field.Anonymous {
			t.Fatalf("%s must embed ToolUsageStatsBase", typ.Name())
		}
	}
}

func TestToolUsageSummaryJSONSchemas(t *testing.T) {
	t.Parallel()

	stats := ToolUsageStatsBase{
		ToolName:      "github.issue_read",
		CallCount:     3,
		MaxOutputSize: 1024,
		MaxDuration:   "2s",
	}

	genericData, err := json.Marshal(ToolUsageSummary{ToolUsageStatsBase: stats, Runs: 2})
	if err != nil {
		t.Fatalf("failed to marshal generic tool summary: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(genericData, &generic); err != nil {
		t.Fatalf("failed to decode generic tool summary: %v", err)
	}
	if generic["name"] != stats.ToolName || generic["total_calls"] != float64(stats.CallCount) || generic["runs"] != float64(2) {
		t.Fatalf("unexpected generic tool summary JSON: %s", genericData)
	}
	if _, ok := generic["tool_name"]; ok {
		t.Fatalf("generic tool summary must preserve its legacy JSON schema: %s", genericData)
	}
	var decodedGeneric ToolUsageSummary
	if err := json.Unmarshal(genericData, &decodedGeneric); err != nil {
		t.Fatalf("failed to unmarshal generic tool summary: %v", err)
	}
	if decodedGeneric.ToolUsageStatsBase != stats || decodedGeneric.Runs != 2 {
		t.Fatalf("unexpected generic tool summary round trip: %+v", decodedGeneric)
	}

	mcpSummary := MCPToolSummary{
		ServerName:    "github",
		ToolName:      stats.ToolName,
		CallCount:     stats.CallCount,
		MaxOutputSize: stats.MaxOutputSize,
		MaxDuration:   stats.MaxDuration,
	}
	mcpSummary.syncBaseFromFields()
	mcpData, err := json.Marshal(mcpSummary)
	if err != nil {
		t.Fatalf("failed to marshal MCP tool summary: %v", err)
	}
	var mcp map[string]any
	if err := json.Unmarshal(mcpData, &mcp); err != nil {
		t.Fatalf("failed to decode MCP tool summary: %v", err)
	}
	if mcp["server_name"] != "github" || mcp["tool_name"] != stats.ToolName || mcp["call_count"] != float64(stats.CallCount) {
		t.Fatalf("unexpected MCP tool summary JSON: %s", mcpData)
	}
}

func TestToolUsageSummaryUnmarshalJSONCompatibility(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "legacy generic schema",
			raw:  `{"name":"github.issue_read","total_calls":3,"runs":2,"max_output_size":1024,"max_duration":"2s"}`,
		},
		{
			name: "mcp style schema",
			raw:  `{"tool_name":"github.issue_read","call_count":3,"runs":2,"max_output_size":1024,"max_duration":"2s"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var summary ToolUsageSummary
			if err := json.Unmarshal([]byte(tc.raw), &summary); err != nil {
				t.Fatalf("failed to unmarshal %s schema: %v", tc.name, err)
			}
			if summary.Name != "github.issue_read" || summary.TotalCalls != 3 {
				t.Fatalf("unexpected compatibility fields: %+v", summary)
			}
			base := summary.ToolUsageStatsBase
			if base.ToolName != summary.Name || base.CallCount != summary.TotalCalls {
				t.Fatalf("base fields drifted from compatibility fields: %+v", summary)
			}
		})
	}
}

// TestRenderLogsConsoleUnified tests the unified console rendering
func TestRenderLogsConsoleUnified(t *testing.T) {
	t.Parallel()
	// Create test data
	data := LogsData{
		Summary: LogsSummary{
			TotalRuns:         2,
			TotalDuration:     "10m30s",
			TotalTurns:        8,
			TotalErrors:       1,
			TotalWarnings:     3,
			TotalMissingTools: 2,
		},
		Runs: []RunData{
			{
				RunID:            12345,
				WorkflowName:     "test-workflow",
				Agent:            "claude",
				Status:           "completed",
				Duration:         "5m30s",
				TokenUsage:       1000,
				Turns:            3,
				ErrorCount:       0,
				WarningCount:     2,
				MissingToolCount: 1,
				CreatedAt:        time.Now(),
				LogsPath:         "/tmp/logs/12345",
			},
		},
		ToolUsage: []ToolUsageSummary{
			{
				Name:          "github-mcp-server",
				TotalCalls:    1500,
				MaxOutputSize: 2500000,
				MaxDuration:   "1m30s",
				Runs:          5,
			},
			{
				Name:          "playwright",
				TotalCalls:    500,
				MaxOutputSize: 512000,
				MaxDuration:   "45s",
				Runs:          3,
			},
		},
		MissingTools: []MissingToolSummary{
			{
				Tool: "terraform",
				AggregatedSummaryBase: AggregatedSummaryBase{
					Count:              5,
					Workflows:          []string{"workflow-a", "workflow-b", "workflow-c"},
					WorkflowsDisplay:   "workflow-a, workflow-b, workflow-c",
					FirstReason:        "Infrastructure automation needed",
					FirstReasonDisplay: "Infrastructure automation needed",
				},
			},
			{
				Tool: "kubectl",
				AggregatedSummaryBase: AggregatedSummaryBase{
					Count:              3,
					Workflows:          []string{"k8s-deploy"},
					WorkflowsDisplay:   "k8s-deploy",
					FirstReason:        "K8s management required",
					FirstReasonDisplay: "K8s management required",
				},
			},
		},
		MCPFailures: []MCPFailureSummary{
			{
				ServerName: "github-mcp-server",
				AggregatedSummaryBase: AggregatedSummaryBase{
					Count:            2,
					Workflows:        []string{"workflow-a", "workflow-b"},
					WorkflowsDisplay: "workflow-a, workflow-b",
				},
			},
			{
				ServerName: "playwright",
				AggregatedSummaryBase: AggregatedSummaryBase{
					Count:            1,
					Workflows:        []string{"browser-test"},
					WorkflowsDisplay: "browser-test",
				},
			},
		},
		LogsLocation: "/tmp/logs",
	}

	// Test unified rendering - should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("renderLogsConsoleToWriter panicked: %v", r)
		}
	}()

	var buf bytes.Buffer
	renderLogsConsoleToWriter(&buf, data)
	renderLogsConsoleToWriter(&buf, data)
}

func TestRenderLogsConsoleMCPFailureSchema(t *testing.T) {
	var buf bytes.Buffer
	renderLogsConsoleToWriter(&buf, LogsData{
		MCPFailures: []MCPFailureSummary{{
			ServerName: "github-mcp-server",
			AggregatedSummaryBase: AggregatedSummaryBase{
				Count:              2,
				WorkflowsDisplay:   "workflow-a, workflow-b",
				FirstReasonDisplay: "not part of the MCP failure schema",
			},
		}},
	})

	output := buf.String()
	for _, expected := range []string{"MCP Server Failures", "Server", "Failures", "Workflows"} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q in MCP failure console output: %s", expected, output)
		}
	}
	for _, unexpected := range []string{"Occurrences", "First Reason"} {
		if strings.Contains(output, unexpected) {
			t.Errorf("unexpected %q in MCP failure console output: %s", unexpected, output)
		}
	}
}

// TestBuildToolUsageSummaryPopulatesDisplay tests that buildToolUsageSummary works correctly
func TestBuildToolUsageSummaryPopulatesDisplay(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				LogsPath: "/tmp/test-logs",
			},
		},
	}

	result := buildToolUsageSummary(processedRuns)

	// The result should be a valid slice (nil or empty is fine when no tools)
	_ = result
}

// TestBuildMissingToolsSummaryPopulatesDisplay tests that display fields are populated
func TestBuildMissingToolsSummaryPopulatesDisplay(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "test-workflow",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "terraform",
					Reason: "Infrastructure automation needed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "test-workflow",
						RunID:        12345,
					},
				},
			},
		},
	}

	result := buildMissingToolsSummary(processedRuns)

	if len(result) != 1 {
		t.Errorf("Expected 1 missing tool summary, got %d", len(result))
	}

	if len(result) > 0 {
		if result[0].WorkflowsDisplay == "" {
			t.Error("WorkflowsDisplay not populated")
		}
		if result[0].FirstReasonDisplay == "" {
			t.Error("FirstReasonDisplay not populated")
		}
	}
}

// TestBuildMCPFailuresSummaryPopulatesDisplay tests that display fields are populated
func TestBuildMCPFailuresSummaryPopulatesDisplay(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "test-workflow",
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "github-mcp-server",
					ReportProvenance: ReportProvenance{
						WorkflowName: "test-workflow",
						RunID:        12345,
					},
				},
			},
		},
	}

	result := buildMCPFailuresSummary(processedRuns)

	if len(result) != 1 {
		t.Errorf("Expected 1 MCP failure summary, got %d", len(result))
	}

	if len(result) > 0 {
		if result[0].WorkflowsDisplay == "" {
			t.Error("WorkflowsDisplay not populated")
		}
	}
}

// TestAddUniqueWorkflow tests the workflow deduplication helper
func TestAddUniqueWorkflow(t *testing.T) {
	tests := []struct {
		name      string
		workflows []string
		workflow  string
		expected  []string
	}{
		{
			name:      "add to empty list",
			workflows: []string{},
			workflow:  "workflow-a",
			expected:  []string{"workflow-a"},
		},
		{
			name:      "add new workflow",
			workflows: []string{"workflow-a", "workflow-b"},
			workflow:  "workflow-c",
			expected:  []string{"workflow-a", "workflow-b", "workflow-c"},
		},
		{
			name:      "duplicate workflow at beginning",
			workflows: []string{"workflow-a", "workflow-b", "workflow-c"},
			workflow:  "workflow-a",
			expected:  []string{"workflow-a", "workflow-b", "workflow-c"},
		},
		{
			name:      "duplicate workflow in middle",
			workflows: []string{"workflow-a", "workflow-b", "workflow-c"},
			workflow:  "workflow-b",
			expected:  []string{"workflow-a", "workflow-b", "workflow-c"},
		},
		{
			name:      "duplicate workflow at end",
			workflows: []string{"workflow-a", "workflow-b", "workflow-c"},
			workflow:  "workflow-c",
			expected:  []string{"workflow-a", "workflow-b", "workflow-c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sliceutil.MergeUnique(tt.workflows, tt.workflow)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected length %d, got %d", len(tt.expected), len(result))
			}
			for i, wf := range result {
				if wf != tt.expected[i] {
					t.Errorf("Expected workflow[%d] = %s, got %s", i, tt.expected[i], wf)
				}
			}
		})
	}
}

// TestBuildMissingToolsSummaryDeduplication tests that workflow deduplication works correctly
func TestBuildMissingToolsSummaryDeduplication(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "terraform",
					Reason: "First reason",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-a",
						RunID:        12345,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "terraform",
					Reason: "Second reason",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-b",
						RunID:        12346,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "terraform",
					Reason: "Third reason from workflow-a",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-a",
						RunID:        12347,
					},
				},
			},
		},
	}

	result := buildMissingToolsSummary(processedRuns)

	if len(result) != 1 {
		t.Errorf("Expected 1 missing tool summary, got %d", len(result))
	}

	if len(result) > 0 {
		summary := result[0]

		// Should have 3 total occurrences
		if summary.Count != 3 {
			t.Errorf("Expected count = 3, got %d", summary.Count)
		}

		// Should have only 2 unique workflows (workflow-a and workflow-b)
		if len(summary.Workflows) != 2 {
			t.Errorf("Expected 2 unique workflows, got %d", len(summary.Workflows))
		}

		// Should have 3 run IDs
		if len(summary.RunIDs) != 3 {
			t.Errorf("Expected 3 run IDs, got %d", len(summary.RunIDs))
		}

		// First reason should be preserved
		if summary.FirstReason != "First reason" {
			t.Errorf("Expected FirstReason = 'First reason', got '%s'", summary.FirstReason)
		}
	}
}

// TestBuildMCPFailuresSummaryDeduplication tests that workflow deduplication works correctly
func TestBuildMCPFailuresSummaryDeduplication(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "github-mcp-server",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-a",
						RunID:        12345,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "github-mcp-server",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-b",
						RunID:        12346,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "github-mcp-server",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-a",
						RunID:        12347,
					},
				},
			},
		},
	}

	result := buildMCPFailuresSummary(processedRuns)

	if len(result) != 1 {
		t.Errorf("Expected 1 MCP failure summary, got %d", len(result))
	}

	if len(result) > 0 {
		summary := result[0]

		// Should have 3 total occurrences
		if summary.Count != 3 {
			t.Errorf("Expected count = 3, got %d", summary.Count)
		}

		// Should have only 2 unique workflows (workflow-a and workflow-b)
		if len(summary.Workflows) != 2 {
			t.Errorf("Expected 2 unique workflows, got %d", len(summary.Workflows))
		}

		// Should have 3 run IDs
		if len(summary.RunIDs) != 3 {
			t.Errorf("Expected 3 run IDs, got %d", len(summary.RunIDs))
		}
	}
}

// TestAggregateSummaryItems tests the generic aggregation helper function
func TestAggregateSummaryItems(t *testing.T) {
	// Test with MissingToolReport data using the generic helper
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "docker",
					Reason: "Container operations needed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-a",
						RunID:        1001,
					},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "docker",
					Reason: "Container build needed",
					ReportProvenance: ReportProvenance{
						WorkflowName: "workflow-b",
						RunID:        1002,
					},
				},
			},
		},
	}

	// Use the generic aggregation helper directly
	result := aggregateSummaryItems(
		processedRuns,
		func(pr ProcessedRun) []MissingToolReport {
			return pr.MissingTools
		},
		func(tool MissingToolReport) string {
			return tool.Tool
		},
		func(tool MissingToolReport) *MissingToolSummary {
			return &MissingToolSummary{
				Tool: tool.Tool,
				AggregatedSummaryBase: AggregatedSummaryBase{
					Count:       1,
					Workflows:   []string{tool.WorkflowName},
					FirstReason: tool.Reason,
					RunIDs:      []int64{tool.RunID},
				},
			}
		},
		func(summary *MissingToolSummary, tool MissingToolReport) {
			summary.Count++
			summary.Workflows = sliceutil.MergeUnique(summary.Workflows, tool.WorkflowName)
			summary.RunIDs = append(summary.RunIDs, tool.RunID)
		},
		func(summary *MissingToolSummary) {
			summary.WorkflowsDisplay = "test-display"
		},
	)

	// Verify aggregation worked correctly
	if len(result) != 1 {
		t.Errorf("Expected 1 aggregated summary, got %d", len(result))
		return
	}

	summary := result[0]

	// Verify count aggregation
	if summary.Count != 2 {
		t.Errorf("Expected count = 2, got %d", summary.Count)
	}

	// Verify workflow deduplication
	if len(summary.Workflows) != 2 {
		t.Errorf("Expected 2 unique workflows, got %d", len(summary.Workflows))
	}

	// Verify run IDs collected
	if len(summary.RunIDs) != 2 {
		t.Errorf("Expected 2 run IDs, got %d", len(summary.RunIDs))
	}

	// Verify first reason preserved
	if summary.FirstReason != "Container operations needed" {
		t.Errorf("Expected FirstReason = 'Container operations needed', got '%s'", summary.FirstReason)
	}

	// Verify finalize was called
	if summary.WorkflowsDisplay != "test-display" {
		t.Errorf("Expected WorkflowsDisplay = 'test-display', got '%s'", summary.WorkflowsDisplay)
	}
}

// TestAggregateDomainStats tests the shared domain aggregation helper
func TestAggregateDomainStats(t *testing.T) {
	t.Run("aggregates domains correctly", func(t *testing.T) {
		processedRuns := []ProcessedRun{
			{
				AccessAnalysis: &DomainAnalysis{
					AnalysisBase: AnalysisBase{
						DomainBuckets: DomainBuckets{
							AllowedDomains: []string{"example.com", "api.github.com"},
							BlockedDomains: []string{"blocked.com"},
						},
						TotalRequests:   10,
						AllowedRequests: 8,
						BlockedRequests: 2,
					},
				},
			},
			{
				AccessAnalysis: &DomainAnalysis{
					AnalysisBase: AnalysisBase{
						DomainBuckets: DomainBuckets{
							AllowedDomains: []string{"api.github.com", "docs.github.com"},
							BlockedDomains: []string{"spam.com"},
						},
						TotalRequests:   5,
						AllowedRequests: 4,
						BlockedRequests: 1,
					},
				},
			},
		}

		agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
			if pr.AccessAnalysis == nil {
				return nil, nil, 0, 0, 0, false
			}
			return pr.AccessAnalysis.AllowedDomains,
				pr.AccessAnalysis.BlockedDomains,
				pr.AccessAnalysis.TotalRequests,
				pr.AccessAnalysis.AllowedRequests,
				pr.AccessAnalysis.BlockedRequests,
				true
		})

		if agg.totalRequests != 15 {
			t.Errorf("Expected totalRequests = 15, got %d", agg.totalRequests)
		}
		if agg.allowedCount != 12 {
			t.Errorf("Expected allowedCount = 12, got %d", agg.allowedCount)
		}
		if agg.blockedCount != 3 {
			t.Errorf("Expected blockedCount = 3, got %d", agg.blockedCount)
		}

		// Check unique domains
		if len(agg.allAllowedDomains) != 3 {
			t.Errorf("Expected 3 unique allowed domains, got %d", len(agg.allAllowedDomains))
		}
		if len(agg.allBlockedDomains) != 2 {
			t.Errorf("Expected 2 unique blocked domains, got %d", len(agg.allBlockedDomains))
		}

		// Verify specific domains
		if !setutil.Contains(agg.allAllowedDomains, "example.com") {
			t.Error("Expected example.com in allowed domains")
		}
		if !setutil.Contains(agg.allAllowedDomains, "api.github.com") {
			t.Error("Expected api.github.com in allowed domains")
		}
		if !setutil.Contains(agg.allBlockedDomains, "blocked.com") {
			t.Error("Expected blocked.com in blocked domains")
		}
	})

	t.Run("handles nil analysis", func(t *testing.T) {
		processedRuns := []ProcessedRun{
			{
				AccessAnalysis: nil,
			},
			{
				AccessAnalysis: &DomainAnalysis{
					AnalysisBase: AnalysisBase{
						DomainBuckets: DomainBuckets{
							AllowedDomains: []string{"example.com"},
						},
						TotalRequests:   5,
						AllowedRequests: 5,
						BlockedRequests: 0,
					},
				},
			},
		}

		agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
			if pr.AccessAnalysis == nil {
				return nil, nil, 0, 0, 0, false
			}
			return pr.AccessAnalysis.AllowedDomains,
				pr.AccessAnalysis.BlockedDomains,
				pr.AccessAnalysis.TotalRequests,
				pr.AccessAnalysis.AllowedRequests,
				pr.AccessAnalysis.BlockedRequests,
				true
		})

		if agg.totalRequests != 5 {
			t.Errorf("Expected totalRequests = 5, got %d", agg.totalRequests)
		}
		if len(agg.allAllowedDomains) != 1 {
			t.Errorf("Expected 1 allowed domain, got %d", len(agg.allAllowedDomains))
		}
	})

	t.Run("handles empty runs", func(t *testing.T) {
		processedRuns := []ProcessedRun{}

		agg := aggregateDomainStats(processedRuns, func(pr *ProcessedRun) ([]string, []string, int, int, int, bool) {
			return nil, nil, 0, 0, 0, false
		})

		if agg.totalRequests != 0 {
			t.Errorf("Expected totalRequests = 0, got %d", agg.totalRequests)
		}
		if len(agg.allAllowedDomains) != 0 {
			t.Errorf("Expected 0 allowed domains, got %d", len(agg.allAllowedDomains))
		}
	})
}

// TestConvertDomainsToSortedSlices tests the domain conversion helper
func TestConvertDomainsToSortedSlices(t *testing.T) {
	t.Run("converts and sorts domains", func(t *testing.T) {
		allowedMap := map[string]struct {
		}{
			"z.com": {},
			"a.com": {},
			"m.com": {},
		}
		deniedMap := map[string]struct {
		}{
			"y.com": {},
			"b.com": {},
		}

		allowed, denied := convertDomainsToSortedSlices(allowedMap, deniedMap)

		// Check sorted order
		expectedAllowed := []string{"a.com", "m.com", "z.com"}
		if len(allowed) != len(expectedAllowed) {
			t.Errorf("Expected %d allowed domains, got %d", len(expectedAllowed), len(allowed))
		}
		for i, domain := range expectedAllowed {
			if allowed[i] != domain {
				t.Errorf("Expected allowed[%d] = %s, got %s", i, domain, allowed[i])
			}
		}

		expectedDenied := []string{"b.com", "y.com"}
		if len(denied) != len(expectedDenied) {
			t.Errorf("Expected %d blocked domains, got %d", len(expectedDenied), len(denied))
		}
		for i, domain := range expectedDenied {
			if denied[i] != domain {
				t.Errorf("Expected denied[%d] = %s, got %s", i, domain, denied[i])
			}
		}
	})

	t.Run("handles empty maps", func(t *testing.T) {
		allowedMap := map[string]struct {
		}{}
		deniedMap := map[string]struct {
		}{}

		allowed, denied := convertDomainsToSortedSlices(allowedMap, deniedMap)

		if len(allowed) != 0 {
			t.Errorf("Expected 0 allowed domains, got %d", len(allowed))
		}
		if len(denied) != 0 {
			t.Errorf("Expected 0 blocked domains, got %d", len(denied))
		}
	})
}

// TestBuildAccessLogSummaryWithSharedHelper tests access log summary with shared helper
func TestBuildAccessLogSummaryWithSharedHelper(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			AccessAnalysis: &DomainAnalysis{
				AnalysisBase: AnalysisBase{
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"example.com", "api.github.com"},
						BlockedDomains: []string{"blocked.com"},
					},
					TotalRequests:   10,
					AllowedRequests: 8,
					BlockedRequests: 2,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			AccessAnalysis: &DomainAnalysis{
				AnalysisBase: AnalysisBase{
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"docs.github.com"},
						BlockedDomains: []string{},
					},
					TotalRequests:   5,
					AllowedRequests: 5,
					BlockedRequests: 0,
				},
			},
		},
	}

	summary := buildAccessLogSummary(processedRuns)

	if summary == nil {
		t.Fatal("Expected non-nil summary")
	}

	if summary.TotalRequests != 15 {
		t.Errorf("Expected TotalRequests = 15, got %d", summary.TotalRequests)
	}
	if summary.AllowedRequests != 13 {
		t.Errorf("Expected AllowedRequests = 13, got %d", summary.AllowedRequests)
	}
	if summary.BlockedRequests != 2 {
		t.Errorf("Expected BlockedRequests = 2, got %d", summary.BlockedRequests)
	}

	// Check sorted domains
	expectedAllowed := []string{"api.github.com", "docs.github.com", "example.com"}
	if len(summary.AllowedDomains) != len(expectedAllowed) {
		t.Errorf("Expected %d allowed domains, got %d", len(expectedAllowed), len(summary.AllowedDomains))
	}
	for i, domain := range expectedAllowed {
		if summary.AllowedDomains[i] != domain {
			t.Errorf("Expected AllowedDomains[%d] = %s, got %s", i, domain, summary.AllowedDomains[i])
		}
	}

	if len(summary.BlockedDomains) != 1 || summary.BlockedDomains[0] != "blocked.com" {
		t.Errorf("Expected BlockedDomains = [blocked.com], got %v", summary.BlockedDomains)
	}

	// Check by workflow
	if len(summary.ByWorkflow) != 2 {
		t.Errorf("Expected 2 workflows, got %d", len(summary.ByWorkflow))
	}
}

func TestAccessLogSummaryJSONUsesEmbeddedBaseFields(t *testing.T) {
	summary := AccessLogSummary{
		FirewallSummaryBase: FirewallSummaryBase{
			TotalRequests:   3,
			AllowedRequests: 2,
			BlockedRequests: 1,
			AllowedDomains:  []string{"example.com"},
			BlockedDomains:  []string{"blocked.com"},
		},
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if got["allowed_requests"] != float64(2) {
		t.Fatalf("expected allowed_requests = 2, got %v", got["allowed_requests"])
	}
	if got["blocked_requests"] != float64(1) {
		t.Fatalf("expected blocked_requests = 1, got %v", got["blocked_requests"])
	}
	if _, ok := got["allowed_count"]; ok {
		t.Fatalf("did not expect legacy allowed_count field in %s", string(data))
	}
	if _, ok := got["blocked_count"]; ok {
		t.Fatalf("did not expect legacy blocked_count field in %s", string(data))
	}
}

// TestRunDataJSONIncludesZeroTokenUsageAndAIC is a regression guard for the bug where
// TokenUsage/AIC used `omitempty` and silently dropped out of the JSON output whenever
// a run genuinely recorded zero tokens, making "field absent" indistinguishable from
// "no tokens recorded". Marshaling a zero-metric RunData must still surface both keys.
func TestRunDataJSONIncludesZeroTokenUsageAndAIC(t *testing.T) {
	run := RunData{
		RunID:        1,
		WorkflowName: "wf-1",
		WorkflowPath: ".github/workflows/wf-1.md",
		Status:       "completed",
		TokenUsage:   0,
		AIC:          0,
	}

	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	tokenUsage, ok := got["token_usage"]
	if !ok {
		t.Fatalf("expected token_usage key to be present in %s", string(data))
	}
	if tokenUsage != float64(0) {
		t.Fatalf("expected token_usage = 0, got %v", tokenUsage)
	}

	aic, ok := got["aic"]
	if !ok {
		t.Fatalf("expected aic key to be present in %s", string(data))
	}
	if aic != float64(0) {
		t.Fatalf("expected aic = 0, got %v", aic)
	}
}

// TestBuildFirewallLogSummaryWithSharedHelper tests firewall log summary with shared helper
func TestBuildFirewallLogSummaryWithSharedHelper(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			FirewallAnalysis: &FirewallAnalysis{
				AnalysisBase: AnalysisBase{
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"example.com"},
						BlockedDomains: []string{"blocked.com"},
					},
					TotalRequests:   10,
					AllowedRequests: 8,
					BlockedRequests: 2,
				},
				RequestsByDomain: map[string]DomainRequestStats{
					"example.com": {Allowed: 8, Blocked: 0},
					"blocked.com": {Allowed: 0, Blocked: 2},
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			FirewallAnalysis: &FirewallAnalysis{
				AnalysisBase: AnalysisBase{
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"example.com", "api.github.com"},
						BlockedDomains: []string{},
					},
					TotalRequests:   5,
					AllowedRequests: 5,
					BlockedRequests: 0,
				},
				RequestsByDomain: map[string]DomainRequestStats{
					"example.com":    {Allowed: 3, Blocked: 0},
					"api.github.com": {Allowed: 2, Blocked: 0},
				},
			},
		},
	}

	summary := buildFirewallLogSummary(processedRuns)

	if summary == nil {
		t.Fatal("Expected non-nil summary")
	}

	if summary.TotalRequests != 15 {
		t.Errorf("Expected TotalRequests = 15, got %d", summary.TotalRequests)
	}
	if summary.AllowedRequests != 13 {
		t.Errorf("Expected AllowedRequests = 13, got %d", summary.AllowedRequests)
	}
	if summary.BlockedRequests != 2 {
		t.Errorf("Expected BlockedRequests = 2, got %d", summary.BlockedRequests)
	}

	// Check RequestsByDomain aggregation (firewall-specific)
	if stats, ok := summary.RequestsByDomain["example.com"]; !ok {
		t.Error("Expected example.com in RequestsByDomain")
	} else {
		if stats.Allowed != 11 {
			t.Errorf("Expected example.com Allowed = 11, got %d", stats.Allowed)
		}
		if stats.Blocked != 0 {
			t.Errorf("Expected example.com Denied = 0, got %d", stats.Blocked)
		}
	}

	if stats, ok := summary.RequestsByDomain["blocked.com"]; !ok {
		t.Error("Expected blocked.com in RequestsByDomain")
	} else {
		if stats.Allowed != 0 {
			t.Errorf("Expected blocked.com Allowed = 0, got %d", stats.Allowed)
		}
		if stats.Blocked != 2 {
			t.Errorf("Expected blocked.com Denied = 2, got %d", stats.Blocked)
		}
	}
}

// TestBuildLogsDataIncludesDateFields tests that RunData includes all date fields
func TestBuildLogsDataIncludesDateFields(t *testing.T) {
	// Create test times
	createdAt := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	startedAt := time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC)
	updatedAt := time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC)

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   12345,
				WorkflowName: "test-workflow",
				CreatedAt:    createdAt,
				StartedAt:    startedAt,
				UpdatedAt:    updatedAt,
				Duration:     5 * time.Minute,
			},
		},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)

	if len(data.Runs) != 1 {
		t.Fatalf("Expected 1 run, got %d", len(data.Runs))
	}

	run := data.Runs[0]

	// Verify all date fields are populated
	if run.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
	if !run.CreatedAt.Equal(createdAt) {
		t.Errorf("Expected CreatedAt = %v, got %v", createdAt, run.CreatedAt)
	}

	if run.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}
	if !run.StartedAt.Equal(startedAt) {
		t.Errorf("Expected StartedAt = %v, got %v", startedAt, run.StartedAt)
	}

	if run.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
	if !run.UpdatedAt.Equal(updatedAt) {
		t.Errorf("Expected UpdatedAt = %v, got %v", updatedAt, run.UpdatedAt)
	}
}

// TestDeriveRunClassification tests the classification mapping helper.
func TestDeriveRunClassification(t *testing.T) {
	tests := []struct {
		name       string
		comparison *AuditComparisonData
		want       string
	}{
		{
			name:       "nil comparison returns unclassified",
			comparison: nil,
			want:       "unclassified",
		},
		{
			name:       "no baseline found returns baseline",
			comparison: &AuditComparisonData{BaselineFound: false},
			want:       "baseline",
		},
		{
			name: "nil classification with baseline returns unclassified",
			comparison: &AuditComparisonData{
				BaselineFound:  true,
				Classification: nil,
			},
			want: "unclassified",
		},
		{
			name: "risky label returns risky",
			comparison: &AuditComparisonData{
				BaselineFound:  true,
				Classification: &AuditComparisonClassification{Label: "risky"},
			},
			want: "risky",
		},
		{
			name: "stable label returns normal",
			comparison: &AuditComparisonData{
				BaselineFound:  true,
				Classification: &AuditComparisonClassification{Label: "stable"},
			},
			want: "normal",
		},
		{
			name: "changed label returns normal",
			comparison: &AuditComparisonData{
				BaselineFound:  true,
				Classification: &AuditComparisonClassification{Label: "changed"},
			},
			want: "normal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveRunClassification(tt.comparison)
			if got != tt.want {
				t.Errorf("deriveRunClassification() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuildLogsDataEngineCountsFromAwInfo verifies that engine_counts in the summary
// is populated from aw_info.json data (the authoritative engine source), not from
// lock file string matching.
func TestBuildLogsDataEngineCountsFromAwInfo(t *testing.T) {
	createRunDir := func(engineID string) string {
		dir := t.TempDir()
		awInfo := `{"engine_id":"` + engineID + `","engine_name":"Test","workflow_name":"test","created_at":"2024-01-01T00:00:00Z"}`
		if err := os.WriteFile(filepath.Join(dir, "aw_info.json"), []byte(awInfo), 0600); err != nil {
			t.Fatalf("Failed to write aw_info.json: %v", err)
		}
		return dir
	}

	claudeDir := createRunDir("claude")
	claudeDir2 := createRunDir("claude")
	copilotDir := createRunDir("copilot")

	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf-claude-1", LogsPath: claudeDir}},
		{Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf-claude-2", LogsPath: claudeDir2}},
		{Run: WorkflowRun{DatabaseID: 3, WorkflowName: "wf-copilot", LogsPath: copilotDir}},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)

	if data.Summary.EngineCounts == nil {
		t.Fatal("EngineCounts should not be nil when runs have aw_info.json")
	}
	if got := data.Summary.EngineCounts["claude"]; got != 2 {
		t.Errorf("Expected 2 claude runs, got %d", got)
	}
	if got := data.Summary.EngineCounts["copilot"]; got != 1 {
		t.Errorf("Expected 1 copilot run, got %d", got)
	}
	// Verify individual RunData.Agent fields also reflect the engine from aw_info.json
	agentsByID := make(map[int64]string)
	for _, run := range data.Runs {
		agentsByID[run.RunID] = run.Agent
	}
	if agentsByID[1] != "claude" {
		t.Errorf("Run 1: expected agent=claude, got %q", agentsByID[1])
	}
	if agentsByID[2] != "claude" {
		t.Errorf("Run 2: expected agent=claude, got %q", agentsByID[2])
	}
	if agentsByID[3] != "copilot" {
		t.Errorf("Run 3: expected agent=copilot, got %q", agentsByID[3])
	}
}

func TestBuildLogsDataAggregatesSteeringEvents(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf-1"},
			TokenUsage: &TokenUsageSummary{
				TotalSteeringEvents: 2,
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf-2"},
			TokenUsage: &TokenUsageSummary{
				TotalSteeringEvents: 3,
			},
		},
		{
			Run:        WorkflowRun{DatabaseID: 3, WorkflowName: "wf-3"},
			TokenUsage: nil,
		},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)
	if data.Summary.TotalSteeringEvents != 5 {
		t.Errorf("Expected TotalSteeringEvents = 5, got %d", data.Summary.TotalSteeringEvents)
	}
}

func TestBuildLogsDataAggregatesAIC(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf-1"},
			TokenUsage: &TokenUsageSummary{
				TotalAIC: 1.25,
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf-2"},
			TokenUsage: &TokenUsageSummary{
				TotalAIC: 0.75,
			},
		},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)
	if data.Summary.TotalAIC != 2.0 {
		t.Fatalf("Expected TotalAIC = 2.0, got %v", data.Summary.TotalAIC)
	}
	if data.Runs[0].AIC != 1.25 {
		t.Fatalf("Expected run AIC = 1.25, got %v", data.Runs[0].AIC)
	}
}

// TestBuildLogsDataAggregatesTokensFromRunTokenUsage verifies that TotalTokens is
// populated from Run.TokenUsage. For AWF-based engines (Claude, Codex, Gemini) the
// run processor backfills Run.TokenUsage from the firewall proxy
// (TotalInputTokens + TotalOutputTokens) when event logs return 0.  This test
// confirms that buildLogsData faithfully aggregates whatever value ends up in
// Run.TokenUsage so that the fleet-wide token total is surfaced in the summary.
func TestBuildLogsDataAggregatesTokensFromRunTokenUsage(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			// Run.TokenUsage is populated (either directly from events or via the
			// firewall-proxy backfill in logs_run_processor.go).
			Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf-1", TokenUsage: 3000},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  2000,
				TotalOutputTokens: 1000,
				TotalAIC:          1.5,
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf-2", TokenUsage: 2000},
			TokenUsage: &TokenUsageSummary{
				TotalInputTokens:  1500,
				TotalOutputTokens: 500,
				TotalAIC:          0.5,
			},
		},
		{
			// Run with no token data at all (e.g., run that did not emit telemetry).
			Run:        WorkflowRun{DatabaseID: 3, WorkflowName: "wf-3", TokenUsage: 0},
			TokenUsage: nil,
		},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)
	if data.Summary.TotalTokens != 5000 {
		t.Fatalf("Expected TotalTokens = 5000, got %d", data.Summary.TotalTokens)
	}
	if data.Runs[0].TokenUsage != 3000 {
		t.Fatalf("Expected run[0].TokenUsage = 3000, got %d", data.Runs[0].TokenUsage)
	}
	if data.Runs[1].TokenUsage != 2000 {
		t.Fatalf("Expected run[1].TokenUsage = 2000, got %d", data.Runs[1].TokenUsage)
	}
	if data.Runs[2].TokenUsage != 0 {
		t.Fatalf("Expected run[2].TokenUsage = 0, got %d", data.Runs[2].TokenUsage)
	}
}

// TestBuildLogsDataDriverExitFailureClassification verifies that buildLogsData correctly
// classifies failed runs as driver_exit (zero turns, TurnsAvailable) vs agent_logic
// (non-zero turns) and accumulates the rollup counts in LogsSummary.
func TestBuildLogsDataDriverExitFailureClassification(t *testing.T) {
	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf", Conclusion: "success", Turns: 4, TurnsAvailable: true}},
		// driver-exit: failed, agent never ran, artifact metrics confirmed 0 turns
		{Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true}},
		{Run: WorkflowRun{DatabaseID: 3, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true}},
		// agent-logic: failed, agent ran
		{Run: WorkflowRun{DatabaseID: 4, WorkflowName: "wf", Conclusion: "failure", Turns: 3, TurnsAvailable: true}},
		// safe_outputs failed after successful agent execution, so this is not a driver exit.
		{
			Run: WorkflowRun{DatabaseID: 5, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true},
			JobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "agent", Conclusion: "success"}},
				{JobInfo: JobInfo{Name: "safe_outputs", Conclusion: "failure"}},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 6, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true},
			JobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "agent", Conclusion: "success"}},
				{JobInfo: JobInfo{Name: "safe outputs", Conclusion: "failure"}},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 7, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true},
			JobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "agent", Conclusion: "success"}},
				{JobInfo: JobInfo{Name: "safe-outputs", Conclusion: "failure"}},
			},
		},
		{
			Run: WorkflowRun{DatabaseID: 8, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true},
			JobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "  AGENT  ", Conclusion: "success"}},
				{JobInfo: JobInfo{Name: "  Safe Outputs  ", Conclusion: "failure"}},
			},
		},
		{
			// success run should never be classified as a failure kind.
			Run: WorkflowRun{DatabaseID: 9, WorkflowName: "wf", Conclusion: "success", Turns: 0, TurnsAvailable: true},
			JobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "agent", Conclusion: "success"}},
				{JobInfo: JobInfo{Name: "safe_outputs", Conclusion: "failure"}},
			},
		},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)

	if data.Summary.TotalDriverExitFailures != 2 {
		t.Errorf("Expected TotalDriverExitFailures = 2, got %d", data.Summary.TotalDriverExitFailures)
	}
	if data.Summary.TotalAgentLogicFailures != 5 {
		t.Errorf("Expected TotalAgentLogicFailures = 5, got %d", data.Summary.TotalAgentLogicFailures)
	}

	// Verify per-run FailureKind
	byID := make(map[int64]RunData)
	for _, r := range data.Runs {
		byID[r.RunID] = r
	}
	if byID[1].FailureKind != "" {
		t.Errorf("run 1 (success): expected empty FailureKind, got %q", byID[1].FailureKind)
	}
	if byID[2].FailureKind != "driver_exit" {
		t.Errorf("run 2 (failure, 0 turns): expected FailureKind=driver_exit, got %q", byID[2].FailureKind)
	}
	if byID[3].FailureKind != "driver_exit" {
		t.Errorf("run 3 (failure, 0 turns): expected FailureKind=driver_exit, got %q", byID[3].FailureKind)
	}
	if byID[4].FailureKind != "agent_logic" {
		t.Errorf("run 4 (failure, 3 turns): expected FailureKind=agent_logic, got %q", byID[4].FailureKind)
	}
	if byID[5].FailureKind != "agent_logic" {
		t.Errorf("run 5 (safe_outputs failure after successful agent): expected FailureKind=agent_logic, got %q", byID[5].FailureKind)
	}
	if byID[6].FailureKind != "agent_logic" {
		t.Errorf("run 6 (safe outputs failure after successful agent): expected FailureKind=agent_logic, got %q", byID[6].FailureKind)
	}
	if byID[7].FailureKind != "agent_logic" {
		t.Errorf("run 7 (safe-outputs failure after successful agent): expected FailureKind=agent_logic, got %q", byID[7].FailureKind)
	}
	if byID[8].FailureKind != "agent_logic" {
		t.Errorf("run 8 (spaced/cased safe outputs failure after successful agent): expected FailureKind=agent_logic, got %q", byID[8].FailureKind)
	}
	if byID[9].FailureKind != "" {
		t.Errorf("run 9 (success with safe_outputs failure metadata): expected empty FailureKind, got %q", byID[9].FailureKind)
	}
}

func TestIsSafeOutputsFailureAfterSuccessfulAgentIgnoresCancelled(t *testing.T) {
	jobDetails := []JobInfoWithDuration{
		{JobInfo: JobInfo{Name: "agent", Conclusion: "success"}},
		{JobInfo: JobInfo{Name: "safe_outputs", Conclusion: "cancelled"}},
	}

	if isSafeOutputsFailureAfterSuccessfulAgent(jobDetails) {
		t.Fatal("expected cancelled safe_outputs to not classify as safe_outputs failure")
	}
}

// TestBuildLogsDataNoArtifactsFailureUnclassified verifies that failed runs whose
// artifact download returned ErrNoArtifacts (TurnsAvailable=false, Turns=0) are left
// unclassified rather than mislabelled as driver_exit.
func TestBuildLogsDataNoArtifactsFailureUnclassified(t *testing.T) {
	processedRuns := []ProcessedRun{
		// ErrNoArtifacts path: TurnsAvailable=false, Turns=0 — agent activity unknown
		{Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: false}},
		// Normal driver-exit: TurnsAvailable=true confirms the zero
		{Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf", Conclusion: "failure", Turns: 0, TurnsAvailable: true}},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)

	if data.Summary.TotalDriverExitFailures != 1 {
		t.Errorf("Expected TotalDriverExitFailures = 1, got %d", data.Summary.TotalDriverExitFailures)
	}
	if data.Summary.TotalAgentLogicFailures != 0 {
		t.Errorf("Expected TotalAgentLogicFailures = 0, got %d", data.Summary.TotalAgentLogicFailures)
	}

	byID := make(map[int64]RunData)
	for _, r := range data.Runs {
		byID[r.RunID] = r
	}
	if byID[1].FailureKind != "" {
		t.Errorf("run 1 (no artifacts): expected empty FailureKind, got %q", byID[1].FailureKind)
	}
	if byID[2].FailureKind != "driver_exit" {
		t.Errorf("run 2 (driver exit): expected FailureKind=driver_exit, got %q", byID[2].FailureKind)
	}
}

// TestBuildLogsDataNoFailuresProducesZeroDriverExitCount verifies that zero-failure
// runs do not populate the driver-exit or agent-logic counters.
func TestBuildLogsDataNoFailuresProducesZeroDriverExitCount(t *testing.T) {
	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{DatabaseID: 1, WorkflowName: "wf", Conclusion: "success", Turns: 5}},
		{Run: WorkflowRun{DatabaseID: 2, WorkflowName: "wf", Conclusion: "success", Turns: 3}},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)

	if data.Summary.TotalDriverExitFailures != 0 {
		t.Errorf("Expected TotalDriverExitFailures = 0, got %d", data.Summary.TotalDriverExitFailures)
	}
	if data.Summary.TotalAgentLogicFailures != 0 {
		t.Errorf("Expected TotalAgentLogicFailures = 0, got %d", data.Summary.TotalAgentLogicFailures)
	}
}

// TestBuildLogsDataIntentionalFailure verifies that buildLogsData correctly marks
// runs[].intentional_failure for workflows tagged with features.intentional-failure: true
// and accumulates the count in summary.intentional_failure_runs.
func TestBuildLogsDataIntentionalFailure(t *testing.T) {
	// Set up a temp dir with a real workflow file so IsIntentionalFailure can read it.
	tempDir := t.TempDir()
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("failed to create workflows dir: %v", err)
	}
	t.Chdir(tempDir)

	// Intentional-failure workflow.
	intentionalMD := filepath.Join(workflowsDir, "credit-guardrail.md")
	if err := os.WriteFile(intentionalMD, []byte("---\nfeatures:\n  intentional-failure: true\n---\n"), 0644); err != nil {
		t.Fatalf("failed to write intentional workflow file: %v", err)
	}

	processedRuns := []ProcessedRun{
		{Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "Credit Guardrail",
			WorkflowPath: ".github/workflows/credit-guardrail.lock.yml",
			Conclusion:   "failure",
		}},
		{Run: WorkflowRun{
			DatabaseID:   2,
			WorkflowName: "Normal Workflow",
			WorkflowPath: ".github/workflows/normal-workflow.lock.yml",
			Conclusion:   "success",
		}},
	}

	data := buildLogsData(processedRuns, "/tmp/logs", nil)

	byID := make(map[int64]RunData)
	for _, r := range data.Runs {
		byID[r.RunID] = r
	}

	if !byID[1].IntentionalFailure {
		t.Error("run 1 (credit-guardrail): expected IntentionalFailure=true, got false")
	}
	if byID[2].IntentionalFailure {
		t.Error("run 2 (normal-workflow): expected IntentionalFailure=false, got true")
	}
	if data.Summary.IntentionalFailureRuns != 1 {
		t.Errorf("expected IntentionalFailureRuns=1, got %d", data.Summary.IntentionalFailureRuns)
	}
}

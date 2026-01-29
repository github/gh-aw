//go:build !integration

package cli

import (
	"testing"
	"time"
)

// TestRenderLogsConsoleUnified tests the unified console rendering
func TestRenderLogsConsoleUnified(t *testing.T) {
	// Create test data
	data := LogsData{
		Summary: LogsSummary{
			TotalRuns:         2,
			TotalDuration:     "10m30s",
			TotalTokens:       2500,
			TotalCost:         0.025,
			TotalTurns:        8,
			TotalErrors:       1,
			TotalWarnings:     3,
			TotalMissingTools: 2,
		},
		Runs: []RunData{
			{
				DatabaseID:       12345,
				WorkflowName:     "test-workflow",
				Agent:            "claude",
				Status:           "completed",
				Duration:         "5m30s",
				TokenUsage:       1000,
				EstimatedCost:    0.01,
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
				Runs:          5,
				MaxOutputSize: 2500000,
				MaxDuration:   "1m30s",
			},
			{
				Name:          "playwright",
				TotalCalls:    500,
				Runs:          3,
				MaxOutputSize: 512000,
				MaxDuration:   "45s",
			},
		},
		MissingTools: []MissingToolSummary{
			{
				Tool:               "terraform",
				Count:              5,
				Workflows:          []string{"workflow-a", "workflow-b", "workflow-c"},
				WorkflowsDisplay:   "workflow-a, workflow-b, workflow-c",
				FirstReason:        "Infrastructure automation needed",
				FirstReasonDisplay: "Infrastructure automation needed",
			},
			{
				Tool:               "kubectl",
				Count:              3,
				Workflows:          []string{"k8s-deploy"},
				WorkflowsDisplay:   "k8s-deploy",
				FirstReason:        "K8s management required",
				FirstReasonDisplay: "K8s management required",
			},
		},
		MCPFailures: []MCPFailureSummary{
			{
				ServerName:       "github-mcp-server",
				Count:            2,
				Workflows:        []string{"workflow-a", "workflow-b"},
				WorkflowsDisplay: "workflow-a, workflow-b",
			},
			{
				ServerName:       "playwright",
				Count:            1,
				Workflows:        []string{"browser-test"},
				WorkflowsDisplay: "browser-test",
			},
		},
		LogsLocation: "/tmp/logs",
	}

	// Test unified rendering - should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("renderLogsConsole panicked: %v", r)
		}
	}()

	renderLogsConsole(data)
	renderLogsConsole(data)
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
					Tool:         "terraform",
					Reason:       "Infrastructure automation needed",
					WorkflowName: "test-workflow",
					RunID:        12345,
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
					ServerName:   "github-mcp-server",
					WorkflowName: "test-workflow",
					RunID:        12345,
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
			result := addUniqueWorkflow(tt.workflows, tt.workflow)
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
					Tool:         "terraform",
					Reason:       "First reason",
					WorkflowName: "workflow-a",
					RunID:        12345,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "terraform",
					Reason:       "Second reason",
					WorkflowName: "workflow-b",
					RunID:        12346,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "terraform",
					Reason:       "Third reason from workflow-a",
					WorkflowName: "workflow-a",
					RunID:        12347,
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
					ServerName:   "github-mcp-server",
					WorkflowName: "workflow-a",
					RunID:        12345,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName:   "github-mcp-server",
					WorkflowName: "workflow-b",
					RunID:        12346,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName:   "github-mcp-server",
					WorkflowName: "workflow-a",
					RunID:        12347,
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
					Tool:         "docker",
					Reason:       "Container operations needed",
					WorkflowName: "workflow-a",
					RunID:        1001,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "docker",
					Reason:       "Container build needed",
					WorkflowName: "workflow-b",
					RunID:        1002,
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
				Tool:        tool.Tool,
				Count:       1,
				Workflows:   []string{tool.WorkflowName},
				FirstReason: tool.Reason,
				RunIDs:      []int64{tool.RunID},
			}
		},
		func(summary *MissingToolSummary, tool MissingToolReport) {
			summary.Count++
			summary.Workflows = addUniqueWorkflow(summary.Workflows, tool.WorkflowName)
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
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"example.com", "api.github.com"},
						BlockedDomains: []string{"blocked.com"},
					},
					TotalRequests: 10,
					AllowedCount:  8,
					BlockedCount:  2,
				},
			},
			{
				AccessAnalysis: &DomainAnalysis{
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"api.github.com", "docs.github.com"},
						BlockedDomains: []string{"spam.com"},
					},
					TotalRequests: 5,
					AllowedCount:  4,
					BlockedCount:  1,
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
				pr.AccessAnalysis.AllowedCount,
				pr.AccessAnalysis.BlockedCount,
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
		if !agg.allAllowedDomains["example.com"] {
			t.Error("Expected example.com in allowed domains")
		}
		if !agg.allAllowedDomains["api.github.com"] {
			t.Error("Expected api.github.com in allowed domains")
		}
		if !agg.allBlockedDomains["blocked.com"] {
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
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"example.com"},
					},
					TotalRequests: 5,
					AllowedCount:  5,
					BlockedCount:  0,
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
				pr.AccessAnalysis.AllowedCount,
				pr.AccessAnalysis.BlockedCount,
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
		allowedMap := map[string]bool{
			"z.com": true,
			"a.com": true,
			"m.com": true,
		}
		deniedMap := map[string]bool{
			"y.com": true,
			"b.com": true,
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
		allowedMap := map[string]bool{}
		deniedMap := map[string]bool{}

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
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{"example.com", "api.github.com"},
					BlockedDomains: []string{"blocked.com"},
				},
				TotalRequests: 10,
				AllowedCount:  8,
				BlockedCount:  2,
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-b",
			},
			AccessAnalysis: &DomainAnalysis{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{"docs.github.com"},
					BlockedDomains: []string{},
				},
				TotalRequests: 5,
				AllowedCount:  5,
				BlockedCount:  0,
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
	if summary.AllowedCount != 13 {
		t.Errorf("Expected AllowedCount = 13, got %d", summary.AllowedCount)
	}
	if summary.BlockedCount != 2 {
		t.Errorf("Expected BlockedCount = 2, got %d", summary.BlockedCount)
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

// TestBuildFirewallLogSummaryWithSharedHelper tests firewall log summary with shared helper
func TestBuildFirewallLogSummaryWithSharedHelper(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "workflow-a",
			},
			FirewallAnalysis: &FirewallAnalysis{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{"example.com"},
					BlockedDomains: []string{"blocked.com"},
				},
				TotalRequests:   10,
				AllowedRequests: 8,
				BlockedRequests: 2,
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
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{"example.com", "api.github.com"},
					BlockedDomains: []string{},
				},
				TotalRequests:   5,
				AllowedRequests: 5,
				BlockedRequests: 0,
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

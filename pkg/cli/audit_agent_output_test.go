//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hasFindingByCategory(findings []AuditFinding, category string) bool {
	for _, finding := range findings {
		if finding.Category == category {
			return true
		}
	}

	return false
}

// TestKeyFindingsGeneration verifies key findings are generated correctly
func TestKeyFindingsGeneration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		run           WorkflowRun
		metrics       MetricsData
		errors        []ValidationIssue
		mcpFailures   []MCPFailureReport
		missingTools  []MissingToolReport
		expectedCount int
		hasFailure    bool
		hasTooling    bool
	}{
		{
			name: "Failed workflow with errors",
			run: WorkflowRun{
				DatabaseID:   123,
				WorkflowName: "Test",
				Conclusion:   "failure",
				Duration:     5 * time.Minute,
			},
			metrics: MetricsData{
				ErrorCount: 3,
				TokenUsage: 1000,
			},
			errors: []ValidationIssue{
				{Type: "error", Message: "Test error 1"},
				{Type: "error", Message: "Test error 2"},
				{Type: "error", Message: "Test error 3"},
			},
			expectedCount: 1, // only failure finding (3 errors doesn't trigger "multiple errors")
			hasFailure:    true,
		},
		{
			name: "High token workflow",
			run: WorkflowRun{
				DatabaseID:   124,
				WorkflowName: "Expensive",
				Conclusion:   "success",
				Duration:     10 * time.Minute,
			},
			metrics: MetricsData{
				TokenUsage: 100000,
			},
			expectedCount: 2, // high tokens + success
		},
		{
			name: "MCP failures",
			run: WorkflowRun{
				DatabaseID:   125,
				WorkflowName: "MCP Test",
				Conclusion:   "failure",
			},
			mcpFailures: []MCPFailureReport{
				{ServerName: "test-server", Status: "failed"},
			},
			expectedCount: 2, // failure + mcp failure
			hasTooling:    true,
		},
		{
			name: "Missing tools",
			run: WorkflowRun{
				DatabaseID:   126,
				WorkflowName: "Tool Test",
				Conclusion:   "success",
			},
			missingTools: []MissingToolReport{
				{Tool: "missing_tool_1"},
				{Tool: "missing_tool_2"},
			},
			expectedCount: 2, // missing tools + success
			hasTooling:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			processedRun := ProcessedRun{
				Run:          tt.run,
				MCPFailures:  tt.mcpFailures,
				MissingTools: tt.missingTools,
			}

			findings := generateFindings(processedRun, tt.metrics, tt.errors)

			assert.GreaterOrEqual(t, len(findings), tt.expectedCount,
				"Expected at least %d findings for scenario %q, got %d", tt.expectedCount, tt.name, len(findings))

			// Verify expected categories
			if tt.hasFailure {
				var failureFinding *AuditFinding
				for _, finding := range findings {
					if finding.Category == "error" && strings.Contains(finding.Title, "Failed") {
						failureFinding = &finding
						break
					}
				}
				require.NotNil(t, failureFinding,
					"Expected an error finding with 'Failed' in title for scenario %q", tt.name)
				assert.Equal(t, scanfindings.SeverityCritical, failureFinding.Severity,
					"Expected critical severity for failure finding in scenario %q", tt.name)
			}

			if tt.hasTooling {
				assert.True(t, hasFindingByCategory(findings, "tooling"),
					"Expected at least one tooling finding for scenario %q", tt.name)
			}
		})
	}
}

// TestRecommendationsGeneration verifies recommendations are generated correctly
func TestRecommendationsGeneration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		run              WorkflowRun
		metrics          MetricsData
		findings         []AuditFinding
		mcpFailures      []MCPFailureReport
		missingTools     []MissingToolReport
		expectedMinCount int
		hasHighPriority  bool
	}{
		{
			name: "Critical failure",
			run: WorkflowRun{
				Conclusion: "failure",
			},
			findings: []AuditFinding{
				{Severity: "critical", Category: "error"},
			},
			expectedMinCount: 1,
			hasHighPriority:  true,
		},
		{
			name: "High cost with many turns",
			run: WorkflowRun{
				Conclusion: "success",
			},
			metrics: MetricsData{
				Turns: 15,
			},
			findings: []AuditFinding{
				{Severity: "high", Category: "cost", Title: "High Cost"},
				{Severity: "medium", Category: "performance", Title: "Many Iterations"},
			},
			expectedMinCount: 2,
		},
		{
			name: "Missing tools",
			run: WorkflowRun{
				Conclusion: "success",
			},
			missingTools: []MissingToolReport{
				{Tool: "required_tool", Reason: "Not configured"},
			},
			expectedMinCount: 1,
		},
		{
			name: "MCP failures",
			run: WorkflowRun{
				Conclusion: "failure",
			},
			mcpFailures: []MCPFailureReport{
				{ServerName: "critical-server", Status: "failed"},
			},
			expectedMinCount: 2, // MCP failure + general failure
			hasHighPriority:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			processedRun := ProcessedRun{
				Run:          tt.run,
				MCPFailures:  tt.mcpFailures,
				MissingTools: tt.missingTools,
			}

			recommendations := generateRecommendations(processedRun, tt.metrics, tt.findings)

			assert.GreaterOrEqual(t, len(recommendations), tt.expectedMinCount,
				"Expected at least %d recommendations for scenario %q, got %d",
				tt.expectedMinCount, tt.name, len(recommendations))

			if tt.hasHighPriority {
				assert.Condition(t, func() bool {
					for _, rec := range recommendations {
						if rec.Priority == "high" {
							return true
						}
					}
					return false
				}, "Expected at least one high priority recommendation for scenario %q", tt.name)
			}

			// Verify all recommendations have required fields
			for _, rec := range recommendations {
				assert.NotEmpty(t, rec.Action,
					"Recommendation action should be set for scenario %q", tt.name)
				assert.NotEmpty(t, rec.Reason,
					"Recommendation reason should be set for scenario %q", tt.name)
				assert.NotEmpty(t, rec.Priority,
					"Recommendation priority should be set for scenario %q", tt.name)
			}
		})
	}
}

// TestPerformanceMetricsGeneration verifies performance metrics are calculated correctly
func TestPerformanceMetricsGeneration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		run                   WorkflowRun
		metrics               MetricsData
		toolUsage             []ToolUsageInfo
		firewallAnalysis      *FirewallAnalysis
		expectTokensPerMin    bool
		expectMostUsedTool    bool
		expectNetworkRequests bool
	}{
		{
			name: "Basic tokens per minute",
			run: WorkflowRun{
				Duration: 10 * time.Minute,
			},
			metrics: MetricsData{
				TokenUsage: 5000,
			},
			expectTokensPerMin: true,
		},
		{
			name: "Tokens per minute with short run",
			run: WorkflowRun{
				Duration: 5 * time.Minute,
			},
			metrics: MetricsData{
				TokenUsage: 10000,
			},
			expectTokensPerMin: true,
		},
		{
			name: "With tool usage",
			run: WorkflowRun{
				Duration: 5 * time.Minute,
			},
			toolUsage: []ToolUsageInfo{
				{Name: "bash", CallCount: 10, MaxDuration: "2s"},
				{Name: "github_issue_read", CallCount: 5, MaxDuration: "1s"},
			},
			expectMostUsedTool: true,
		},
		{
			name: "With firewall analysis",
			run: WorkflowRun{
				Duration: 5 * time.Minute,
			},
			firewallAnalysis: &FirewallAnalysis{
				AnalysisBase: AnalysisBase{TotalRequests: 25},
			},
			expectNetworkRequests: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			processedRun := ProcessedRun{
				Run:              tt.run,
				FirewallAnalysis: tt.firewallAnalysis,
			}

			pm := generatePerformanceMetrics(processedRun, tt.metrics, tt.toolUsage)

			require.NotNil(t, pm, "Expected performance metrics to be generated for scenario %q", tt.name)

			if tt.expectTokensPerMin {
				assert.Positive(t, pm.TokensPerMinute,
					"Expected positive tokens per minute for scenario %q", tt.name)
			}

			if tt.expectMostUsedTool {
				assert.NotEmpty(t, pm.MostUsedTool,
					"Expected most used tool to be populated for scenario %q", tt.name)
			}

			if tt.expectNetworkRequests {
				assert.Positive(t, pm.NetworkRequests,
					"Expected network request count to be positive for scenario %q", tt.name)
			}
		})
	}
}

// TestAuditDataJSONStructure verifies the JSON structure includes all new fields
func TestAuditDataJSONStructure(t *testing.T) {
	t.Parallel()
	// Create comprehensive audit data
	run := WorkflowRun{
		DatabaseID:   123456,
		WorkflowName: "Test Workflow",
		Status:       "completed",
		Conclusion:   "failure",
		CreatedAt:    time.Now(),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/org/repo/actions/runs/123456",
		TokenUsage:   5000,
		Turns:        8,
		ErrorCount:   2,
		WarningCount: 1,
		Duration:     5 * time.Minute,
	}

	metrics := LogMetrics{
		TokenUsage: 5000,
		Turns:      8,
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "bash", CallCount: 5, MaxDuration: 2 * time.Second},
		},
	}

	processedRun := ProcessedRun{
		Run: run,
		MissingTools: []MissingToolReport{
			{Tool: "missing_tool", Reason: "Not configured"},
		},
		MCPFailures: []MCPFailureReport{
			{ServerName: "test-server", Status: "failed"},
		},
		GatewaySteeringEvents: []GatewaySteeringEvent{
			{Type: tokenSteeringEventName, Message: "[AWF TOKEN WARNING] You are running out of AI Credits."},
		},
		JobDetails: []JobInfoWithDuration{
			{JobInfo: JobInfo{Name: "test", Conclusion: "failure"}},
		},
	}

	// Build audit data
	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

	// Marshal to JSON
	jsonBytes, err := json.MarshalIndent(auditData, "", "  ")
	require.NoError(t, err, "Failed to marshal audit data to JSON")

	jsonStr := string(jsonBytes)

	// Verify all new fields are present
	// Note: "errors" and "warnings" fields are omitempty and will not appear in JSON
	// since error/warning extraction was removed from buildAuditData
	expectedFields := []string{
		"key_findings",
		"recommendations",
		"performance_metrics",
		"overview",
		"metrics",
		"jobs",
		"downloaded_files",
		"missing_tools",
		"mcp_failures",
		"gateway_steering_events",
		"tool_usage",
	}

	for _, field := range expectedFields {
		assert.Contains(t, jsonStr, fmt.Sprintf(`"%s"`, field),
			"JSON output should include expected field %q", field)
	}

	// Verify key findings structure
	assert.Contains(t, jsonStr, `"category"`, "Key findings should include category field")
	assert.Contains(t, jsonStr, `"severity"`, "Key findings should include severity field")

	// Verify recommendations structure
	assert.Contains(t, jsonStr, `"priority"`, "Recommendations should include priority field")
	assert.Contains(t, jsonStr, `"action"`, "Recommendations should include action field")

	// Parse back to verify structure
	var parsed AuditData
	require.NoError(t, json.Unmarshal(jsonBytes, &parsed), "Failed to parse JSON back to AuditData")

	// Verify parsed data has expected content
	assert.NotEmpty(t, parsed.KeyFindings, "Expected key findings to be present after JSON round-trip")
	assert.NotEmpty(t, parsed.Recommendations, "Expected recommendations to be present after JSON round-trip")
	assert.NotNil(t, parsed.PerformanceMetrics, "Expected performance metrics to be present after JSON round-trip")
}

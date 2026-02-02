//go:build !integration

package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
)

// TestKeyFindingsGeneration verifies key findings are generated correctly
func TestKeyFindingsGeneration(t *testing.T) {
	tests := []struct {
		name          string
		run           WorkflowRun
		metrics       MetricsData
		errors        []ErrorInfo
		warnings      []ErrorInfo
		mcpFailures   []MCPFailureReport
		missingTools  []MissingToolReport
		expectedCount int
		hasFailure    bool
		hasCost       bool
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
			errors: []ErrorInfo{
				{Type: "error", Message: "Test error 1"},
				{Type: "error", Message: "Test error 2"},
				{Type: "error", Message: "Test error 3"},
			},
			expectedCount: 1, // only failure finding (3 errors doesn't trigger "multiple errors")
			hasFailure:    true,
		},
		{
			name: "High cost workflow",
			run: WorkflowRun{
				DatabaseID:   124,
				WorkflowName: "Expensive",
				Conclusion:   "success",
				Duration:     10 * time.Minute,
			},
			metrics: MetricsData{
				EstimatedCost: 1.5,
				TokenUsage:    100000,
			},
			expectedCount: 3, // high cost + high tokens + success
			hasCost:       true,
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
			processedRun := ProcessedRun{
				Run:          tt.run,
				MCPFailures:  tt.mcpFailures,
				MissingTools: tt.missingTools,
			}

			findings := generateFindings(processedRun, tt.metrics, tt.errors, tt.warnings)

			if len(findings) < tt.expectedCount {
				t.Errorf("Expected at least %d findings, got %d", tt.expectedCount, len(findings))
			}

			// Verify expected categories
			if tt.hasFailure {
				found := false
				for _, finding := range findings {
					if finding.Category == "error" && strings.Contains(finding.Title, "Failed") {
						found = true
						if finding.Severity != "critical" {
							t.Errorf("Expected critical severity for failure, got %s", finding.Severity)
						}
						break
					}
				}
				if !found {
					t.Error("Expected failure finding but didn't find one")
				}
			}

			if tt.hasCost {
				found := false
				for _, finding := range findings {
					if finding.Category == "cost" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected cost finding but didn't find one")
				}
			}

			if tt.hasTooling {
				found := false
				for _, finding := range findings {
					if finding.Category == "tooling" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected tooling finding but didn't find one")
				}
			}
		})
	}
}

// TestRecommendationsGeneration verifies recommendations are generated correctly
func TestRecommendationsGeneration(t *testing.T) {
	tests := []struct {
		name             string
		run              WorkflowRun
		metrics          MetricsData
		findings         []Finding
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
			findings: []Finding{
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
				EstimatedCost: 1.0,
				Turns:         15,
			},
			findings: []Finding{
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
			processedRun := ProcessedRun{
				Run:          tt.run,
				MCPFailures:  tt.mcpFailures,
				MissingTools: tt.missingTools,
			}

			recommendations := generateRecommendations(processedRun, tt.metrics, tt.findings)

			if len(recommendations) < tt.expectedMinCount {
				t.Errorf("Expected at least %d recommendations, got %d", tt.expectedMinCount, len(recommendations))
			}

			if tt.hasHighPriority {
				found := false
				for _, rec := range recommendations {
					if rec.Priority == "high" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Expected high priority recommendation but didn't find one")
				}
			}

			// Verify all recommendations have required fields
			for _, rec := range recommendations {
				if rec.Action == "" {
					t.Error("Recommendation missing action")
				}
				if rec.Reason == "" {
					t.Error("Recommendation missing reason")
				}
				if rec.Priority == "" {
					t.Error("Recommendation missing priority")
				}
			}
		})
	}
}

// TestFailureAnalysisGeneration verifies failure analysis is generated correctly
func TestFailureAnalysisGeneration(t *testing.T) {
	tests := []struct {
		name               string
		run                WorkflowRun
		errors             []ErrorInfo
		jobDetails         []JobInfoWithDuration
		mcpFailures        []MCPFailureReport
		expectedRootCause  string
		expectedFailedJobs int
	}{
		{
			name: "Simple failure with error",
			run: WorkflowRun{
				Conclusion: "failure",
			},
			errors: []ErrorInfo{
				{Message: "Connection timeout occurred"},
			},
			expectedRootCause: "Operation timeout",
		},
		{
			name: "Permission denied error",
			run: WorkflowRun{
				Conclusion: "failure",
			},
			errors: []ErrorInfo{
				{Message: "Permission denied for resource"},
			},
			expectedRootCause: "Permission denied",
		},
		{
			name: "MCP server failure",
			run: WorkflowRun{
				Conclusion: "failure",
			},
			mcpFailures: []MCPFailureReport{
				{ServerName: "github-mcp", Status: "failed"},
			},
			expectedRootCause: "MCP server failure: github-mcp",
		},
		{
			name: "Failed jobs",
			run: WorkflowRun{
				Conclusion: "failure",
			},
			jobDetails: []JobInfoWithDuration{
				{JobInfo: JobInfo{Name: "test-job", Conclusion: "failure"}},
				{JobInfo: JobInfo{Name: "build-job", Conclusion: "success"}},
			},
			expectedFailedJobs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processedRun := ProcessedRun{
				Run:         tt.run,
				MCPFailures: tt.mcpFailures,
				JobDetails:  tt.jobDetails,
			}

			analysis := generateFailureAnalysis(processedRun, tt.errors)

			if analysis == nil {
				t.Fatal("Expected failure analysis but got nil")
			}

			if analysis.PrimaryFailure != tt.run.Conclusion {
				t.Errorf("Expected primary failure %s, got %s", tt.run.Conclusion, analysis.PrimaryFailure)
			}

			if tt.expectedRootCause != "" {
				if analysis.RootCause != tt.expectedRootCause {
					t.Errorf("Expected root cause '%s', got '%s'", tt.expectedRootCause, analysis.RootCause)
				}
			}

			if tt.expectedFailedJobs > 0 {
				if len(analysis.FailedJobs) != tt.expectedFailedJobs {
					t.Errorf("Expected %d failed jobs, got %d", tt.expectedFailedJobs, len(analysis.FailedJobs))
				}
			}
		})
	}
}

// TestPerformanceMetricsGeneration verifies performance metrics are calculated correctly
func TestPerformanceMetricsGeneration(t *testing.T) {
	tests := []struct {
		name                   string
		run                    WorkflowRun
		metrics                MetricsData
		toolUsage              []ToolUsageInfo
		firewallAnalysis       *FirewallAnalysis
		expectedCostEfficiency string
		expectTokensPerMin     bool
		expectMostUsedTool     bool
		expectNetworkRequests  bool
	}{
		{
			name: "Excellent cost efficiency",
			run: WorkflowRun{
				Duration: 10 * time.Minute,
			},
			metrics: MetricsData{
				EstimatedCost: 0.05,
				TokenUsage:    5000,
			},
			expectedCostEfficiency: "excellent",
			expectTokensPerMin:     true,
		},
		{
			name: "Poor cost efficiency",
			run: WorkflowRun{
				Duration: 5 * time.Minute,
			},
			metrics: MetricsData{
				EstimatedCost: 1.0,
				TokenUsage:    10000,
			},
			expectedCostEfficiency: "poor",
			expectTokensPerMin:     true,
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
				TotalRequests: 25,
			},
			expectNetworkRequests: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processedRun := ProcessedRun{
				Run:              tt.run,
				FirewallAnalysis: tt.firewallAnalysis,
			}

			pm := generatePerformanceMetrics(processedRun, tt.metrics, tt.toolUsage)

			if pm == nil {
				t.Fatal("Expected performance metrics but got nil")
			}

			if tt.expectedCostEfficiency != "" {
				if pm.CostEfficiency != tt.expectedCostEfficiency {
					t.Errorf("Expected cost efficiency '%s', got '%s'", tt.expectedCostEfficiency, pm.CostEfficiency)
				}
			}

			if tt.expectTokensPerMin {
				if pm.TokensPerMinute <= 0 {
					t.Error("Expected positive tokens per minute")
				}
			}

			if tt.expectMostUsedTool {
				if pm.MostUsedTool == "" {
					t.Error("Expected most used tool to be set")
				}
			}

			if tt.expectNetworkRequests {
				if pm.NetworkRequests <= 0 {
					t.Error("Expected network requests count")
				}
			}
		})
	}
}

// TestAuditDataJSONStructure verifies the JSON structure includes all new fields
func TestAuditDataJSONStructure(t *testing.T) {
	// Create comprehensive audit data
	run := WorkflowRun{
		DatabaseID:    123456,
		WorkflowName:  "Test Workflow",
		Status:        "completed",
		Conclusion:    "failure",
		CreatedAt:     time.Now(),
		Event:         "push",
		HeadBranch:    "main",
		URL:           "https://github.com/org/repo/actions/runs/123456",
		TokenUsage:    5000,
		EstimatedCost: 0.5,
		Turns:         8,
		ErrorCount:    2,
		WarningCount:  1,
		Duration:      5 * time.Minute,
	}

	metrics := LogMetrics{
		TokenUsage:    5000,
		EstimatedCost: 0.5,
		Turns:         8,
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
		JobDetails: []JobInfoWithDuration{
			{JobInfo: JobInfo{Name: "test", Conclusion: "failure"}},
		},
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, nil)

	// Marshal to JSON
	jsonBytes, err := json.MarshalIndent(auditData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal audit data to JSON: %v", err)
	}

	jsonStr := string(jsonBytes)

	// Verify all new fields are present
	// Note: "errors" and "warnings" fields are omitempty and will not appear in JSON
	// since error/warning extraction was removed from buildAuditData
	expectedFields := []string{
		"key_findings",
		"recommendations",
		"failure_analysis",
		"performance_metrics",
		"overview",
		"metrics",
		"jobs",
		"downloaded_files",
		"missing_tools",
		"mcp_failures",
		"tool_usage",
	}

	for _, field := range expectedFields {
		if !strings.Contains(jsonStr, fmt.Sprintf(`"%s"`, field)) {
			t.Errorf("JSON output missing expected field: %s", field)
		}
	}

	// Verify key findings structure
	if !strings.Contains(jsonStr, `"category"`) {
		t.Error("Key findings missing category field")
	}
	if !strings.Contains(jsonStr, `"severity"`) {
		t.Error("Key findings missing severity field")
	}

	// Verify recommendations structure
	if !strings.Contains(jsonStr, `"priority"`) {
		t.Error("Recommendations missing priority field")
	}
	if !strings.Contains(jsonStr, `"action"`) {
		t.Error("Recommendations missing action field")
	}

	// Verify failure analysis structure
	if !strings.Contains(jsonStr, `"primary_failure"`) {
		t.Error("Failure analysis missing primary_failure field")
	}

	// Verify performance metrics structure
	if !strings.Contains(jsonStr, `"cost_efficiency"`) {
		t.Error("Performance metrics missing cost_efficiency field")
	}

	// Parse back to verify structure
	var parsed AuditData
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to parse JSON back to AuditData: %v", err)
	}

	// Verify parsed data has expected content
	if len(parsed.KeyFindings) == 0 {
		t.Error("Expected key findings but got none")
	}
	if len(parsed.Recommendations) == 0 {
		t.Error("Expected recommendations but got none")
	}
	if parsed.FailureAnalysis == nil {
		t.Error("Expected failure analysis but got nil")
	}
	if parsed.PerformanceMetrics == nil {
		t.Error("Expected performance metrics but got nil")
	}
}

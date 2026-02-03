//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestProcessedRun creates a test ProcessedRun with customizable parameters
func createTestProcessedRun(opts ...func(*ProcessedRun)) ProcessedRun {
	run := WorkflowRun{
		DatabaseID:    123456,
		WorkflowName:  "Test Workflow",
		Status:        "completed",
		Conclusion:    "success",
		CreatedAt:     time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		StartedAt:     time.Date(2024, 1, 1, 10, 0, 30, 0, time.UTC),
		UpdatedAt:     time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC),
		Duration:      4*time.Minute + 30*time.Second,
		Event:         "push",
		HeadBranch:    "main",
		URL:           "https://github.com/org/repo/actions/runs/123456",
		TokenUsage:    1500,
		EstimatedCost: 0.025,
		Turns:         5,
		ErrorCount:    0,
		WarningCount:  0,
		LogsPath:      "/tmp/test-logs",
	}

	processedRun := ProcessedRun{
		Run: run,
	}

	for _, opt := range opts {
		opt(&processedRun)
	}

	return processedRun
}

// assertFindingExists checks if a finding with the given category and severity exists
func assertFindingExists(t *testing.T, findings []Finding, category, severity string, msgAndArgs ...any) {
	t.Helper()
	for _, f := range findings {
		if f.Category == category && f.Severity == severity {
			return // Found it!
		}
	}
	assert.Fail(t, "Finding not found", "category=%s, severity=%s", category, severity)
}

// findFindingByCategory returns the first finding with the given category, or nil if not found
func findFindingByCategory(findings []Finding, category string) *Finding {
	for _, f := range findings {
		if f.Category == category {
			return &f
		}
	}
	return nil
}

// assertFindingContains checks if a finding with the given category exists and title contains expected text
func assertFindingContains(t *testing.T, findings []Finding, category, titleContains string, msgAndArgs ...any) {
	t.Helper()
	for _, f := range findings {
		if f.Category == category && strings.Contains(f.Title, titleContains) {
			return // Found it!
		}
	}
	assert.Fail(t, "Finding not found", "category=%s, title containing '%s'", category, titleContains)
}

// assertRecommendationExists checks if a recommendation with the given priority and action text exists
func assertRecommendationExists(t *testing.T, recs []Recommendation, priority, actionContains string, msgAndArgs ...any) {
	t.Helper()
	for _, r := range recs {
		if r.Priority == priority && strings.Contains(r.Action, actionContains) {
			return // Found it!
		}
	}
	assert.Fail(t, "Recommendation not found", "priority=%s, action containing '%s'", priority, actionContains)
}

func TestGenerateFindings(t *testing.T) {
	tests := []struct {
		name          string
		processedRun  ProcessedRun
		metrics       MetricsData
		errors        []ErrorInfo
		warnings      []ErrorInfo
		expectedCount int
		checkFindings func(t *testing.T, findings []Finding)
	}{
		{
			name: "successful workflow with no issues",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "success"
				return pr
			}(),
			metrics: MetricsData{
				TokenUsage:    1000,
				EstimatedCost: 0.01,
				Turns:         3,
				ErrorCount:    0,
				WarningCount:  0,
			},
			errors:        []ErrorInfo{},
			warnings:      []ErrorInfo{},
			expectedCount: 1, // Should have success finding
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingExists(t, findings, "success", "info",
					"Successful workflow should generate a success finding")
			},
		},
		{
			name: "failed workflow",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			metrics: MetricsData{
				TokenUsage:    1000,
				EstimatedCost: 0.01,
				Turns:         3,
				ErrorCount:    2,
				WarningCount:  0,
			},
			errors:        []ErrorInfo{{Type: "error", Message: "Test error"}},
			warnings:      []ErrorInfo{},
			expectedCount: 1, // Should have failure finding
			checkFindings: func(t *testing.T, findings []Finding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Failed workflow should generate an error finding")
				assert.Equal(t, "critical", finding.Severity, "Error finding should have critical severity")
				assert.Contains(t, finding.Title, "Failed", "Error finding should have 'Failed' in title")
			},
		},
		{
			name: "timed out workflow",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "timed_out"
				return pr
			}(),
			metrics: MetricsData{
				Turns: 20,
			},
			errors:        []ErrorInfo{},
			warnings:      []ErrorInfo{},
			expectedCount: 1, // Timeout finding
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "performance", "Timeout",
					"Timed out workflow should generate a timeout finding")
			},
		},
		{
			name: "high cost workflow",
			processedRun: func() ProcessedRun {
				return createTestProcessedRun()
			}(),
			metrics: MetricsData{
				EstimatedCost: 1.50, // > 1.0 threshold
				Turns:         5,
			},
			errors:        []ErrorInfo{},
			warnings:      []ErrorInfo{},
			expectedCount: 1, // High cost finding
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingExists(t, findings, "cost", "high",
					"High cost workflow should generate a high cost finding")
			},
		},
		{
			name: "moderate cost workflow",
			processedRun: func() ProcessedRun {
				return createTestProcessedRun()
			}(),
			metrics: MetricsData{
				EstimatedCost: 0.75, // Between 0.5 and 1.0
				Turns:         5,
			},
			errors:        []ErrorInfo{},
			warnings:      []ErrorInfo{},
			expectedCount: 1, // Moderate cost finding
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingExists(t, findings, "cost", "medium",
					"Moderate cost workflow should generate a medium cost finding")
			},
		},
		{
			name: "high token usage",
			processedRun: func() ProcessedRun {
				return createTestProcessedRun()
			}(),
			metrics: MetricsData{
				TokenUsage: 60000, // > 50000 threshold
				Turns:      5,
			},
			errors:   []ErrorInfo{},
			warnings: []ErrorInfo{},
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "performance", "Token Usage",
					"High token usage should generate a performance finding")
			},
		},
		{
			name: "many iterations",
			processedRun: func() ProcessedRun {
				return createTestProcessedRun()
			}(),
			metrics: MetricsData{
				Turns: 15, // > 10 threshold
			},
			errors:   []ErrorInfo{},
			warnings: []ErrorInfo{},
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "performance", "Iterations",
					"Many iterations should generate a performance finding")
			},
		},
		{
			name: "multiple errors",
			processedRun: func() ProcessedRun {
				return createTestProcessedRun()
			}(),
			metrics: MetricsData{
				Turns:      5,
				ErrorCount: 10,
			},
			errors: []ErrorInfo{
				{Type: "error", Message: "Error 1"},
				{Type: "error", Message: "Error 2"},
				{Type: "error", Message: "Error 3"},
				{Type: "error", Message: "Error 4"},
				{Type: "error", Message: "Error 5"},
				{Type: "error", Message: "Error 6"},
			},
			warnings: []ErrorInfo{},
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "error", "Multiple Errors",
					"Multiple errors should generate an error finding")
			},
		},
		{
			name: "MCP server failures",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.MCPFailures = []MCPFailureReport{
					{ServerName: "test-server", Status: "failed"},
				}
				return pr
			}(),
			metrics: MetricsData{
				Turns: 5,
			},
			errors:   []ErrorInfo{},
			warnings: []ErrorInfo{},
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "tooling", "MCP Server",
					"MCP server failures should generate a tooling finding")
			},
		},
		{
			name: "missing tools",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.MissingTools = []MissingToolReport{
					{Tool: "tool1", Reason: "Not available"},
					{Tool: "tool2", Reason: "Not configured"},
				}
				return pr
			}(),
			metrics: MetricsData{
				Turns: 5,
			},
			errors:   []ErrorInfo{},
			warnings: []ErrorInfo{},
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "tooling", "Tools Not Available",
					"Missing tools should generate a tooling finding")
			},
		},
		{
			name: "firewall blocked requests",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.FirewallAnalysis = &FirewallAnalysis{
					TotalRequests:   10,
					BlockedRequests: 5,
					AllowedRequests: 5,
				}
				return pr
			}(),
			metrics: MetricsData{
				Turns: 5,
			},
			errors:   []ErrorInfo{},
			warnings: []ErrorInfo{},
			checkFindings: func(t *testing.T, findings []Finding) {
				assertFindingContains(t, findings, "network", "Blocked",
					"Firewall blocked requests should generate a network finding")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := generateFindings(tt.processedRun, tt.metrics, tt.errors, tt.warnings)

			if tt.expectedCount > 0 {
				assert.GreaterOrEqual(t, len(findings), tt.expectedCount,
					"Should generate at least %d finding(s) for %s", tt.expectedCount, tt.name)
			}

			if tt.checkFindings != nil {
				tt.checkFindings(t, findings)
			}
		})
	}
}

func TestGenerateRecommendations(t *testing.T) {
	tests := []struct {
		name                 string
		processedRun         ProcessedRun
		metrics              MetricsData
		findings             []Finding
		expectedMinCount     int
		checkRecommendations func(t *testing.T, recs []Recommendation)
	}{
		{
			name: "failed workflow generates review recommendation",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			metrics:          MetricsData{},
			findings:         []Finding{},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				assertRecommendationExists(t, recs, "high", "error logs",
					"Failed workflow should generate high priority review recommendation")
			},
		},
		{
			name:         "critical findings generate review recommendation",
			processedRun: createTestProcessedRun(),
			metrics:      MetricsData{},
			findings: []Finding{
				{Category: "error", Severity: "critical", Title: "Test Critical"},
			},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				assertRecommendationExists(t, recs, "high", "error logs",
					"Critical findings should generate high priority review recommendation")
			},
		},
		{
			name:         "high cost findings generate optimization recommendation",
			processedRun: createTestProcessedRun(),
			metrics:      MetricsData{EstimatedCost: 1.5},
			findings: []Finding{
				{Category: "cost", Severity: "high", Title: "High Cost"},
			},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				found := false
				for _, r := range recs {
					if strings.Contains(r.Action, "Optimize") || strings.Contains(r.Action, "prompt") {
						found = true
						break
					}
				}
				assert.True(t, found, "High cost findings should generate optimization recommendation")
			},
		},
		{
			name: "missing tools generate add tools recommendation",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.MissingTools = []MissingToolReport{
					{Tool: "missing_tool", Reason: "Not available"},
				}
				return pr
			}(),
			metrics:          MetricsData{},
			findings:         []Finding{},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				found := false
				for _, r := range recs {
					if strings.Contains(r.Action, "missing tools") || strings.Contains(r.Action, "Add") {
						found = true
						break
					}
				}
				assert.True(t, found, "Missing tools should generate add tools recommendation")
			},
		},
		{
			name: "MCP failures generate fix recommendation",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.MCPFailures = []MCPFailureReport{
					{ServerName: "test-server", Status: "failed"},
				}
				return pr
			}(),
			metrics:          MetricsData{},
			findings:         []Finding{},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				found := false
				for _, r := range recs {
					if strings.Contains(r.Action, "MCP") || strings.Contains(r.Action, "server") {
						found = true
						break
					}
				}
				assert.True(t, found, "MCP failures should generate fix recommendation")
			},
		},
		{
			name: "many firewall blocks generate network review recommendation",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.FirewallAnalysis = &FirewallAnalysis{
					BlockedRequests: 15, // > 10 threshold
				}
				return pr
			}(),
			metrics:          MetricsData{},
			findings:         []Finding{},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				found := false
				for _, r := range recs {
					if strings.Contains(r.Action, "network") || strings.Contains(r.Action, "Review") {
						found = true
						break
					}
				}
				assert.True(t, found, "Many firewall blocks should generate network review recommendation")
			},
		},
		{
			name: "successful workflow with no issues gets monitoring recommendation",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "success"
				return pr
			}(),
			metrics:          MetricsData{},
			findings:         []Finding{},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				assertRecommendationExists(t, recs, "low", "Monitor",
					"Successful workflow should generate low priority monitoring recommendation")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs := generateRecommendations(tt.processedRun, tt.metrics, tt.findings)

			assert.GreaterOrEqual(t, len(recs), tt.expectedMinCount,
				"Should generate at least %d recommendation(s) for %s", tt.expectedMinCount, tt.name)

			if tt.checkRecommendations != nil {
				tt.checkRecommendations(t, recs)
			}
		})
	}
}

func TestGenerateFailureAnalysis(t *testing.T) {
	tests := []struct {
		name          string
		processedRun  ProcessedRun
		errors        []ErrorInfo
		checkAnalysis func(t *testing.T, analysis *FailureAnalysis)
	}{
		{
			name: "basic failure analysis",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "failure", analysis.PrimaryFailure,
					"Primary failure should match workflow conclusion")
			},
		},
		{
			name: "failure with failed jobs",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				pr.JobDetails = []JobInfoWithDuration{
					{JobInfo: JobInfo{Name: "build", Conclusion: "success"}},
					{JobInfo: JobInfo{Name: "test", Conclusion: "failure"}},
					{JobInfo: JobInfo{Name: "deploy", Conclusion: "cancelled"}},
				}
				return pr
			}(),
			errors: []ErrorInfo{},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Len(t, analysis.FailedJobs, 2,
					"Should have 2 failed jobs (failure and cancelled)")
			},
		},
		{
			name: "failure with single error",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{
				{Type: "error", Message: "Build failed"},
			},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "Build failed", analysis.ErrorSummary,
					"Error summary should contain single error message")
			},
		},
		{
			name: "failure with multiple errors",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{
				{Type: "error", Message: "First error"},
				{Type: "error", Message: "Second error"},
				{Type: "error", Message: "Third error"},
			},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Contains(t, analysis.ErrorSummary, "3 errors",
					"Error summary should indicate multiple errors")
				assert.Contains(t, analysis.ErrorSummary, "First error",
					"Error summary should include first error")
			},
		},
		{
			name: "failure with MCP server failure root cause",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				pr.MCPFailures = []MCPFailureReport{
					{ServerName: "github-mcp", Status: "failed"},
				}
				return pr
			}(),
			errors: []ErrorInfo{},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Contains(t, analysis.RootCause, "MCP server failure",
					"Root cause should identify MCP server failure")
			},
		},
		{
			name: "failure with timeout error pattern",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{
				{Type: "error", Message: "Connection timeout after 30s"},
			},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "Operation timeout", analysis.RootCause,
					"Root cause should identify timeout")
			},
		},
		{
			name: "failure with permission error pattern",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{
				{Type: "error", Message: "Permission blocked: cannot access file"},
			},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "Permission denied", analysis.RootCause,
					"Root cause should identify permission issue")
			},
		},
		{
			name: "failure with not found error pattern",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{
				{Type: "error", Message: "File not found: test.txt"},
			},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "Resource not found", analysis.RootCause,
					"Root cause should identify missing resource")
			},
		},
		{
			name: "failure with authentication error pattern",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			errors: []ErrorInfo{
				{Type: "error", Message: "Authentication failed for user"},
			},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "Authentication failure", analysis.RootCause,
					"Root cause should identify authentication issue")
			},
		},
		{
			name: "unknown failure with no errors",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = ""
				return pr
			}(),
			errors: []ErrorInfo{},
			checkAnalysis: func(t *testing.T, analysis *FailureAnalysis) {
				assert.Equal(t, "unknown", analysis.PrimaryFailure,
					"Primary failure should be 'unknown' for empty conclusion")
				assert.Contains(t, analysis.ErrorSummary, "No specific errors",
					"Error summary should indicate no errors found")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := generateFailureAnalysis(tt.processedRun, tt.errors)

			require.NotNil(t, analysis, "Failure analysis should be generated for failed workflow")

			if tt.checkAnalysis != nil {
				tt.checkAnalysis(t, analysis)
			}
		})
	}
}

func TestGeneratePerformanceMetrics(t *testing.T) {
	tests := []struct {
		name         string
		processedRun ProcessedRun
		metrics      MetricsData
		toolUsage    []ToolUsageInfo
		checkMetrics func(t *testing.T, pm *PerformanceMetrics)
	}{
		{
			name: "tokens per minute calculation",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Duration = 2 * time.Minute
				return pr
			}(),
			metrics: MetricsData{
				TokenUsage: 1000,
			},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.InDelta(t, 500.0, pm.TokensPerMinute, 0.01,
					"Tokens per minute should be 1000 tokens / 2 minutes = 500")
			},
		},
		{
			name: "cost efficiency - excellent",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Duration = 10 * time.Minute
				return pr
			}(),
			metrics: MetricsData{
				EstimatedCost: 0.05, // $0.005/min < $0.01 threshold
			},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.Equal(t, "excellent", pm.CostEfficiency,
					"Cost efficiency should be excellent for low cost per minute")
			},
		},
		{
			name: "cost efficiency - good",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Duration = 10 * time.Minute
				return pr
			}(),
			metrics: MetricsData{
				EstimatedCost: 0.25, // $0.025/min
			},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.Equal(t, "good", pm.CostEfficiency,
					"Cost efficiency should be good for moderate cost per minute")
			},
		},
		{
			name: "cost efficiency - moderate",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Duration = 10 * time.Minute
				return pr
			}(),
			metrics: MetricsData{
				EstimatedCost: 0.75, // $0.075/min
			},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.Equal(t, "moderate", pm.CostEfficiency,
					"Cost efficiency should be moderate for higher cost per minute")
			},
		},
		{
			name: "cost efficiency - poor",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Duration = 10 * time.Minute
				return pr
			}(),
			metrics: MetricsData{
				EstimatedCost: 1.50, // $0.15/min
			},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.Equal(t, "poor", pm.CostEfficiency,
					"Cost efficiency should be poor for high cost per minute")
			},
		},
		{
			name:         "most used tool",
			processedRun: createTestProcessedRun(),
			metrics:      MetricsData{},
			toolUsage: []ToolUsageInfo{
				{Name: "bash", CallCount: 5},
				{Name: "github_issue_read", CallCount: 10},
				{Name: "file_edit", CallCount: 3},
			},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.Contains(t, pm.MostUsedTool, "github_issue_read",
					"Most used tool should be github_issue_read")
				assert.Contains(t, pm.MostUsedTool, "10 calls",
					"Most used tool should show 10 calls")
			},
		},
		{
			name:         "average tool duration",
			processedRun: createTestProcessedRun(),
			metrics:      MetricsData{},
			toolUsage: []ToolUsageInfo{
				{Name: "bash", CallCount: 5, MaxDuration: "1s"},
				{Name: "github_issue_read", CallCount: 10, MaxDuration: "3s"},
			},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.NotEmpty(t, pm.AvgToolDuration,
					"Average tool duration should be calculated")
			},
		},
		{
			name: "network requests from firewall",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.FirewallAnalysis = &FirewallAnalysis{
					TotalRequests: 50,
				}
				return pr
			}(),
			metrics:   MetricsData{},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.Equal(t, 50, pm.NetworkRequests,
					"Network requests should match firewall analysis total")
			},
		},
		{
			name: "zero duration doesn't calculate tokens per minute",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Duration = 0
				return pr
			}(),
			metrics: MetricsData{
				TokenUsage: 1000,
			},
			toolUsage: []ToolUsageInfo{},
			checkMetrics: func(t *testing.T, pm *PerformanceMetrics) {
				assert.InDelta(t, 0.0, pm.TokensPerMinute, 0.01,
					"Tokens per minute should be 0 for zero duration")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := generatePerformanceMetrics(tt.processedRun, tt.metrics, tt.toolUsage)

			require.NotNil(t, pm, "Performance metrics should be generated")

			if tt.checkMetrics != nil {
				tt.checkMetrics(t, pm)
			}
		})
	}
}

func TestBuildAuditDataComplete(t *testing.T) {
	// Create a comprehensive test with all data filled in
	tmpDir := testutil.TempDir(t, "audit-data-test-*")

	// Create test files in the temp directory
	testFiles := map[string]string{
		"aw_info.json": `{"engine":"copilot"}`,
		"output.log":   "Test log content",
	}
	for filename, content := range testFiles {
		err := os.WriteFile(tmpDir+"/"+filename, []byte(content), 0644)
		require.NoError(t, err, "Failed to create test file %s", filename)
	}

	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:    12345,
			WorkflowName:  "Complete Test Workflow",
			Status:        "completed",
			Conclusion:    "failure",
			CreatedAt:     time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			StartedAt:     time.Date(2024, 1, 1, 10, 0, 30, 0, time.UTC),
			UpdatedAt:     time.Date(2024, 1, 1, 10, 10, 0, 0, time.UTC),
			Duration:      9*time.Minute + 30*time.Second,
			Event:         "pull_request",
			HeadBranch:    "feature-branch",
			URL:           "https://github.com/test/repo/actions/runs/12345",
			TokenUsage:    25000,
			EstimatedCost: 0.75,
			Turns:         8,
			ErrorCount:    3,
			WarningCount:  2,
			LogsPath:      tmpDir,
		},
		JobDetails: []JobInfoWithDuration{
			{JobInfo: JobInfo{Name: "build", Status: "completed", Conclusion: "success"}, Duration: 2 * time.Minute},
			{JobInfo: JobInfo{Name: "test", Status: "completed", Conclusion: "failure"}, Duration: 5 * time.Minute},
		},
		MissingTools: []MissingToolReport{
			{Tool: "special_tool", Reason: "Not configured"},
		},
		MCPFailures: []MCPFailureReport{
			{ServerName: "test-mcp", Status: "connection_error"},
		},
		FirewallAnalysis: &FirewallAnalysis{
			DomainBuckets: DomainBuckets{
				AllowedDomains: []string{"api.github.com"},
				BlockedDomains: []string{"blocked.example.com"},
			},
			TotalRequests:   15,
			AllowedRequests: 10,
			BlockedRequests: 5,
			RequestsByDomain: map[string]DomainRequestStats{
				"api.github.com":      {Allowed: 10, Blocked: 0},
				"blocked.example.com": {Allowed: 0, Blocked: 5},
			},
		},
		RedactedDomainsAnalysis: &RedactedDomainsAnalysis{
			TotalDomains: 2,
			Domains:      []string{"secret.example.com", "internal.test.com"},
		},
	}

	metrics := workflow.LogMetrics{
		TokenUsage:    25000,
		EstimatedCost: 0.75,
		Turns:         8,
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "bash", CallCount: 15, MaxInputSize: 500, MaxOutputSize: 2000, MaxDuration: 5 * time.Second},
			{Name: "github_issue_read", CallCount: 8, MaxInputSize: 100, MaxOutputSize: 5000, MaxDuration: 2 * time.Second},
		},
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, nil)

	// Verify overview
	t.Run("Overview", func(t *testing.T) {
		assert.Equal(t, int64(12345), auditData.Overview.RunID,
			"RunID should match workflow run database ID")
		assert.Equal(t, "Complete Test Workflow", auditData.Overview.WorkflowName,
			"Workflow name should match")
		assert.Equal(t, "completed", auditData.Overview.Status,
			"Status should match")
		assert.Equal(t, "failure", auditData.Overview.Conclusion,
			"Conclusion should match")
	})

	// Verify metrics
	t.Run("Metrics", func(t *testing.T) {
		assert.Equal(t, 25000, auditData.Metrics.TokenUsage,
			"Token usage should match")
		assert.Equal(t, 3, auditData.Metrics.ErrorCount,
			"Error count should match")
		assert.Equal(t, 2, auditData.Metrics.WarningCount,
			"Warning count should match")
	})

	// Verify jobs
	t.Run("Jobs", func(t *testing.T) {
		assert.Len(t, auditData.Jobs, 2,
			"Should have 2 jobs")
	})

	// Verify tool usage
	t.Run("ToolUsage", func(t *testing.T) {
		assert.Len(t, auditData.ToolUsage, 2,
			"Should have 2 tool usage entries")
	})

	// Verify findings are generated
	t.Run("Findings", func(t *testing.T) {
		assert.NotEmpty(t, auditData.KeyFindings,
			"Should generate at least one finding")
		// Should have failure finding since conclusion is "failure"
		hasFailureFinding := false
		for _, f := range auditData.KeyFindings {
			if f.Severity == "critical" && f.Category == "error" {
				hasFailureFinding = true
				break
			}
		}
		assert.True(t, hasFailureFinding,
			"Failed workflow should generate a critical error finding")
	})

	// Verify recommendations are generated
	t.Run("Recommendations", func(t *testing.T) {
		assert.NotEmpty(t, auditData.Recommendations,
			"Should generate at least one recommendation")
	})

	// Verify failure analysis is generated
	t.Run("FailureAnalysis", func(t *testing.T) {
		assert.NotNil(t, auditData.FailureAnalysis,
			"Failed workflow should generate failure analysis")
	})

	// Verify performance metrics are generated
	t.Run("PerformanceMetrics", func(t *testing.T) {
		assert.NotNil(t, auditData.PerformanceMetrics,
			"Should generate performance metrics")
	})

	// Verify firewall analysis is passed through
	t.Run("FirewallAnalysis", func(t *testing.T) {
		require.NotNil(t, auditData.FirewallAnalysis,
			"Should include firewall analysis")
		assert.Equal(t, 15, auditData.FirewallAnalysis.TotalRequests,
			"Total requests should match")
	})

	// Verify redacted domains are passed through
	t.Run("RedactedDomainsAnalysis", func(t *testing.T) {
		require.NotNil(t, auditData.RedactedDomainsAnalysis,
			"Should include redacted domains analysis")
		assert.Equal(t, 2, auditData.RedactedDomainsAnalysis.TotalDomains,
			"Total redacted domains should match")
	})

	// Verify MCP failures are passed through
	t.Run("MCPFailures", func(t *testing.T) {
		assert.Len(t, auditData.MCPFailures, 1,
			"Should have 1 MCP failure")
	})

	// Verify missing tools are passed through
	t.Run("MissingTools", func(t *testing.T) {
		assert.Len(t, auditData.MissingTools, 1,
			"Should have 1 missing tool")
	})
}

func TestBuildAuditDataMinimal(t *testing.T) {
	// Test with minimal/empty data
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "Minimal",
			Status:       "completed",
			Conclusion:   "success",
			LogsPath:     "/nonexistent",
		},
	}

	metrics := workflow.LogMetrics{}

	auditData := buildAuditData(processedRun, metrics, nil)

	// Should still produce valid data
	assert.Equal(t, int64(1), auditData.Overview.RunID,
		"RunID should match even with minimal data")

	// Empty slices should be nil or empty, not cause panics
	// We just want to ensure no panics occur accessing these fields
	_ = auditData.Jobs
}

func TestRenderJSONComplete(t *testing.T) {
	auditData := AuditData{
		Overview: OverviewData{
			RunID:        99999,
			WorkflowName: "JSON Test",
			Status:       "completed",
			Conclusion:   "success",
			Event:        "push",
			Branch:       "main",
			URL:          "https://github.com/test/repo/actions/runs/99999",
		},
		Metrics: MetricsData{
			TokenUsage:    5000,
			EstimatedCost: 0.10,
			Turns:         4,
			ErrorCount:    1,
			WarningCount:  2,
		},
		KeyFindings: []Finding{
			{Category: "success", Severity: "info", Title: "Test Finding", Description: "Test description"},
		},
		Recommendations: []Recommendation{
			{Priority: "low", Action: "Monitor", Reason: "Test reason"},
		},
		Jobs: []JobData{
			{Name: "test-job", Status: "completed", Conclusion: "success", Duration: "1m30s"},
		},
		DownloadedFiles: []FileInfo{
			{Path: "test.log", Size: 1024, Description: "Test log"},
		},
		Errors: []ErrorInfo{
			{Type: "error", Message: "Test error"},
		},
		Warnings: []ErrorInfo{
			{Type: "warning", Message: "Test warning 1"},
			{Type: "warning", Message: "Test warning 2"},
		},
		ToolUsage: []ToolUsageInfo{
			{Name: "bash", CallCount: 5, MaxInputSize: 100, MaxOutputSize: 500},
		},
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := renderJSON(auditData)
	w.Close()

	// Read output
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	require.NoError(t, err, "renderJSON should not fail")

	jsonOutput := buf.String()

	// Verify valid JSON
	var parsed AuditData
	err = json.Unmarshal([]byte(jsonOutput), &parsed)
	require.NoError(t, err, "Should produce valid JSON output")

	// Verify all fields
	assert.Equal(t, int64(99999), parsed.Overview.RunID,
		"RunID should be preserved in JSON")
	assert.Len(t, parsed.KeyFindings, 1,
		"Findings should be preserved in JSON")
	assert.Len(t, parsed.Recommendations, 1,
		"Recommendations should be preserved in JSON")
	assert.Len(t, parsed.Jobs, 1,
		"Jobs should be preserved in JSON")
	assert.Len(t, parsed.Errors, 1,
		"Errors should be preserved in JSON")
	assert.Len(t, parsed.Warnings, 2,
		"Warnings should be preserved in JSON")
}

func TestToolUsageAggregation(t *testing.T) {
	// Test that tool usage is properly aggregated with prettified names
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "Tool Test",
			Status:       "completed",
			Conclusion:   "success",
			LogsPath:     "/tmp/test",
		},
	}

	// Simulate multiple calls to the same tool with different raw names
	metrics := workflow.LogMetrics{
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "github_mcp_server_issue_read", CallCount: 5, MaxInputSize: 100, MaxOutputSize: 500, MaxDuration: 2 * time.Second},
			{Name: "github_mcp_server_issue_read", CallCount: 3, MaxInputSize: 200, MaxOutputSize: 800, MaxDuration: 3 * time.Second},
			{Name: "bash", CallCount: 10, MaxInputSize: 50, MaxOutputSize: 100, MaxDuration: 1 * time.Second},
		},
	}

	auditData := buildAuditData(processedRun, metrics, nil)

	// Tool usage should be aggregated
	// The exact aggregation depends on workflow.PrettifyToolName behavior
	assert.NotEmpty(t, auditData.ToolUsage,
		"Should have tool usage data")

	// Check that bash is present
	bashFound := false
	for _, tool := range auditData.ToolUsage {
		if strings.Contains(strings.ToLower(tool.Name), "bash") {
			bashFound = true
			assert.Equal(t, 10, tool.CallCount,
				"Bash call count should match")
		}
	}
	assert.True(t, bashFound,
		"Bash should be present in tool usage")
}

func TestExtractDownloadedFilesEmpty(t *testing.T) {
	// Test with nonexistent directory
	files := extractDownloadedFiles("/nonexistent/path")
	assert.Empty(t, files,
		"Should return empty slice for nonexistent path")

	// Test with empty directory
	tmpDir := testutil.TempDir(t, "empty-dir-*")
	files = extractDownloadedFiles(tmpDir)
	assert.Empty(t, files,
		"Should return empty slice for empty directory")
}

func TestFindingSeverityOrdering(t *testing.T) {
	// Test that findings are generated with proper severity levels
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "Severity Test",
			Status:       "completed",
			Conclusion:   "failure",
			Duration:     5 * time.Minute,
		},
		MCPFailures: []MCPFailureReport{
			{ServerName: "test-mcp", Status: "failed"},
		},
	}

	metrics := MetricsData{
		ErrorCount:    5,
		EstimatedCost: 2.0, // High cost
		Turns:         15,  // Many turns
	}

	errors := []ErrorInfo{
		{Type: "error", Message: "Error 1"},
		{Type: "error", Message: "Error 2"},
		{Type: "error", Message: "Error 3"},
		{Type: "error", Message: "Error 4"},
		{Type: "error", Message: "Error 5"},
		{Type: "error", Message: "Error 6"},
	}

	findings := generateFindings(processedRun, metrics, errors, []ErrorInfo{})

	// Should have critical, high, and medium findings
	severityCounts := make(map[string]int)
	for _, f := range findings {
		severityCounts[f.Severity]++
	}

	assert.NotZero(t, severityCounts["critical"],
		"Failed workflow should generate at least one critical finding")
	assert.NotZero(t, severityCounts["high"],
		"Should generate at least one high severity finding")
}

func TestRecommendationPriorityOrdering(t *testing.T) {
	// Test that recommendations are generated with proper priorities
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "Priority Test",
			Status:       "completed",
			Conclusion:   "failure",
		},
		MCPFailures: []MCPFailureReport{
			{ServerName: "test-mcp", Status: "failed"},
		},
		MissingTools: []MissingToolReport{
			{Tool: "missing", Reason: "Not available"},
		},
		FirewallAnalysis: &FirewallAnalysis{
			BlockedRequests: 20, // Many blocked requests
		},
	}

	metrics := MetricsData{
		EstimatedCost: 1.5,
	}

	findings := []Finding{
		{Category: "error", Severity: "critical", Title: "Critical"},
		{Category: "cost", Severity: "high", Title: "High Cost"},
	}

	recs := generateRecommendations(processedRun, metrics, findings)

	// Should have high priority recommendations
	priorityCounts := make(map[string]int)
	for _, r := range recs {
		priorityCounts[r.Priority]++
	}

	assert.NotZero(t, priorityCounts["high"],
		"Should generate at least one high priority recommendation")
}

func TestDescribeFileAdditionalPatterns(t *testing.T) {
	// Test file description for additional file patterns not covered in audit_test.go
	tests := []struct {
		filename    string
		description string
	}{
		{"unknown_file", ""}, // Unknown file with no extension
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := describeFile(tt.filename)
			assert.Equal(t, tt.description, result,
				"File description should match expected value")
		})
	}
}

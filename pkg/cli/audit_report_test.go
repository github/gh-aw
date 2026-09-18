//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestProcessedRun creates a test ProcessedRun with customizable parameters
func createTestProcessedRun(opts ...func(*ProcessedRun)) ProcessedRun {
	run := WorkflowRun{
		DatabaseID:   123456,
		WorkflowName: "Test Workflow",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		StartedAt:    time.Date(2024, 1, 1, 10, 0, 30, 0, time.UTC),
		UpdatedAt:    time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC),
		Duration:     4*time.Minute + 30*time.Second,
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/org/repo/actions/runs/123456",
		TokenUsage:   1500,
		Turns:        5,
		ErrorCount:   0,
		WarningCount: 0,
		LogsPath:     "/tmp/test-logs",
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
func assertFindingExists(t *testing.T, findings []AuditFinding, category, severity string, msgAndArgs ...any) {
	t.Helper()
	for _, f := range findings {
		if f.Category == category && f.Severity.String() == severity {
			return // Found it!
		}
	}
	assert.Fail(t, "Finding not found", "category=%s, severity=%s", category, severity)
}

// findFindingByCategory returns the first finding with the given category, or nil if not found
func findFindingByCategory(findings []AuditFinding, category string) *AuditFinding {
	for _, f := range findings {
		if f.Category == category {
			return &f
		}
	}
	return nil
}

// assertFindingContains checks if a finding with the given category exists and title contains expected text
func assertFindingContains(t *testing.T, findings []AuditFinding, category, titleContains string, msgAndArgs ...any) {
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
	t.Parallel()
	tests := []struct {
		name          string
		processedRun  ProcessedRun
		metrics       MetricsData
		errors        []ValidationIssue
		expectedCount int
		checkFindings func(t *testing.T, findings []AuditFinding)
	}{
		{
			name: "successful workflow with no issues",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "success"
				return pr
			}(),
			metrics: MetricsData{
				TokenUsage:   1000,
				Turns:        3,
				ErrorCount:   0,
				WarningCount: 0,
			},
			errors:        []ValidationIssue{},
			expectedCount: 1, // Should have success finding
			checkFindings: func(t *testing.T, findings []AuditFinding) {
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
				TokenUsage:   1000,
				Turns:        3,
				ErrorCount:   2,
				WarningCount: 0,
			},
			errors:        []ValidationIssue{{Type: "error", Message: "Test error"}},
			expectedCount: 1, // Should have failure finding
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Failed workflow should generate an error finding")
				assert.Equal(t, scanfindings.SeverityCritical, finding.Severity, "Error finding should have critical severity")
				assert.Contains(t, finding.Title, "Failed", "Error finding should have 'Failed' in title")
				assert.Contains(t, finding.Description, "Test error", "Error finding description should include the first error message")
			},
		},
		{
			name: "failed workflow with very long error message truncates in description",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			metrics:       MetricsData{ErrorCount: 1},
			errors:        []ValidationIssue{{Type: "step_failure", Message: strings.Repeat("x", 500)}},
			expectedCount: 1,
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Should generate an error finding")
				assert.LessOrEqual(t, len(finding.Description), 300, "Description should be truncated for long error messages")
				assert.True(t, strings.HasSuffix(finding.Description, "..."), "Truncated description should end with ellipsis")
			},
		},
		{
			name: "failed workflow without errors surfaces no message in description",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			metrics: MetricsData{
				ErrorCount: 1,
			},
			errors:        []ValidationIssue{},
			expectedCount: 1,
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Failed workflow should generate an error finding")
				assert.Equal(t, scanfindings.SeverityCritical, finding.Severity, "Error finding should have critical severity")
				assert.Equal(t, "Workflow 'Test Workflow' failed with 1 error(s)", finding.Description,
					"Description should match standard format without error message suffix when no errors available")
			},
		},
		{
			name: "failed workflow uses actual error count not stale metrics.ErrorCount",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			metrics: MetricsData{
				ErrorCount: 0, // metrics are wrong / stale — should not be used when errors slice is populated
			},
			errors: []ValidationIssue{
				{Type: "step_failure", Message: "##[error]Process completed with exit code 1."},
				{Type: "step_failure", Message: "##[error]Process completed with exit code 1."},
			},
			expectedCount: 1,
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Failed workflow should generate an error finding")
				assert.Equal(t, scanfindings.SeverityCritical, finding.Severity, "Error finding should have critical severity")
				assert.Contains(t, finding.Description, "2 error(s)",
					"Description should reflect the actual number of errors, not metrics.ErrorCount")
				assert.NotContains(t, finding.Description, "0 error(s)",
					"Description must not show 0 errors when the errors slice is non-empty")
			},
		},
		{
			name: "failed workflow with zero errors and no error details uses pre-activation message",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				return pr
			}(),
			metrics: MetricsData{
				ErrorCount: 0, // no errors extracted — logs were not available
			},
			errors:        []ValidationIssue{},
			expectedCount: 1,
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Failed workflow should still generate an error finding")
				assert.Equal(t, scanfindings.SeverityCritical, finding.Severity, "Error finding should have critical severity")
				assert.Contains(t, finding.Description, "before agent activation",
					"Description should indicate pre-activation failure when no logs are available")
				assert.Contains(t, finding.Description, "no error logs",
					"Description should mention that no error logs were available")
				assert.NotContains(t, finding.Description, "0 error(s)",
					"Description must not produce the contradictory 'failed with 0 error(s)' message")
			},
		},
		{
			name: "failed workflow with agent job failure and no telemetry uses post-activation message",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.Run.Conclusion = "failure"
				pr.JobDetails = []JobInfoWithDuration{
					{
						JobInfo: JobInfo{
							Name:       "activation",
							Status:     "completed",
							Conclusion: "success",
						},
						Duration: 37 * time.Second,
					},
					{
						JobInfo: JobInfo{
							Name:       "agent",
							Status:     "completed",
							Conclusion: "failure",
						},
						Duration: 2 * time.Minute,
					},
				}
				return pr
			}(),
			metrics: MetricsData{
				ErrorCount: 0,
			},
			errors:        []ValidationIssue{},
			expectedCount: 1,
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				finding := findFindingByCategory(findings, "error")
				require.NotNil(t, finding, "Failed workflow should still generate an error finding")
				assert.Contains(t, finding.Description, "after agent activation",
					"Description should indicate post-activation failure when agent job ran")
				assert.Contains(t, finding.Description, "ran for 2.0m before failing",
					"Description should include agent job runtime before failure")
				assert.Contains(t, finding.Description, "no agent telemetry was available to analyze",
					"Description should explain why no error details were available")
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
			errors:        []ValidationIssue{},
			expectedCount: 1, // Timeout finding
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				assertFindingContains(t, findings, "performance", "Timeout",
					"Timed out workflow should generate a timeout finding")
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
			errors: []ValidationIssue{},
			checkFindings: func(t *testing.T, findings []AuditFinding) {
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
			errors: []ValidationIssue{},
			checkFindings: func(t *testing.T, findings []AuditFinding) {
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
			errors: []ValidationIssue{
				{Type: "error", Message: "Error 1"},
				{Type: "error", Message: "Error 2"},
				{Type: "error", Message: "Error 3"},
				{Type: "error", Message: "Error 4"},
				{Type: "error", Message: "Error 5"},
				{Type: "error", Message: "Error 6"},
			},
			checkFindings: func(t *testing.T, findings []AuditFinding) {
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
			errors: []ValidationIssue{},
			checkFindings: func(t *testing.T, findings []AuditFinding) {
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
			errors: []ValidationIssue{},
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				assertFindingContains(t, findings, "tooling", "Tools Not Available",
					"Missing tools should generate a tooling finding")
			},
		},
		{
			name: "firewall blocked requests",
			processedRun: func() ProcessedRun {
				pr := createTestProcessedRun()
				pr.FirewallAnalysis = &FirewallAnalysis{
					AnalysisBase: AnalysisBase{
						TotalRequests:   10,
						BlockedRequests: 5,
						AllowedRequests: 5,
					},
				}
				return pr
			}(),
			metrics: MetricsData{
				Turns: 5,
			},
			errors: []ValidationIssue{},
			checkFindings: func(t *testing.T, findings []AuditFinding) {
				assertFindingContains(t, findings, "network", "Blocked",
					"Firewall blocked requests should generate a network finding")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := generateFindings(tt.processedRun, tt.metrics, tt.errors)

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

func TestBuildBlockedNetworkFindingDescription(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		blockedRequests int
		blockedDomains  []string
		expected        string
	}{
		{
			name:            "fallback to blocked request count",
			blockedRequests: 3,
			expected:        "3 network request(s) were blocked by firewall",
		},
		{
			name:            "single blocked domain",
			blockedRequests: 1,
			blockedDomains:  []string{"example.com"},
			expected:        "Agent attempted to access blocked domain: example.com",
		},
		{
			name:            "few blocked domains",
			blockedRequests: 2,
			blockedDomains:  []string{"example.com", "api.example.com"},
			expected:        "Agent attempted to access blocked domains: example.com, api.example.com",
		},
		{
			name:            "many blocked domains",
			blockedRequests: 4,
			blockedDomains:  []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"},
			expected:        "Agent attempted to access 4 blocked domains, including: a.example.com, b.example.com, c.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, buildBlockedNetworkFindingDescription(tt.blockedRequests, tt.blockedDomains))
		})
	}
}

func TestGenerateRecommendations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		processedRun         ProcessedRun
		metrics              MetricsData
		findings             []AuditFinding
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
			findings:         []AuditFinding{},
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
			findings: []AuditFinding{
				{Category: "error", Severity: "critical", Title: "Test Critical"},
			},
			expectedMinCount: 1,
			checkRecommendations: func(t *testing.T, recs []Recommendation) {
				assertRecommendationExists(t, recs, "high", "error logs",
					"Critical findings should generate high priority review recommendation")
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
			findings:         []AuditFinding{},
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
			findings:         []AuditFinding{},
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
					AnalysisBase: AnalysisBase{BlockedRequests: 15}, // > 10 threshold
				}
				return pr
			}(),
			metrics:          MetricsData{},
			findings:         []AuditFinding{},
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
			findings:         []AuditFinding{},
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

func TestGeneratePerformanceMetrics(t *testing.T) {
	t.Parallel()
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
					AnalysisBase: AnalysisBase{TotalRequests: 50},
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
	t.Parallel()
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
			DatabaseID:   12345,
			WorkflowName: "Complete Test Workflow",
			Status:       "completed",
			Conclusion:   "failure",
			CreatedAt:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			StartedAt:    time.Date(2024, 1, 1, 10, 0, 30, 0, time.UTC),
			UpdatedAt:    time.Date(2024, 1, 1, 10, 10, 0, 0, time.UTC),
			Duration:     9*time.Minute + 30*time.Second,
			Event:        "pull_request",
			HeadBranch:   "feature-branch",
			URL:          "https://github.com/test/repo/actions/runs/12345",
			TokenUsage:   25000,
			Turns:        8,
			ErrorCount:   3,
			WarningCount: 2,
			LogsPath:     tmpDir,
		},
		JobDetails: []JobInfoWithDuration{
			{
				JobInfo: JobInfo{
					Name:       "build",
					Status:     "completed",
					Conclusion: "success",
					Steps: []JobStep{
						{Name: "Set up job", Status: "completed", Conclusion: "success"},
						{Name: "Compile", Status: "completed", Conclusion: "success"},
					},
				},
				Duration: 2 * time.Minute,
			},
			{
				JobInfo: JobInfo{
					Name:       "test",
					Status:     "completed",
					Conclusion: "failure",
					Steps: []JobStep{
						{Name: "Set up job", Status: "completed", Conclusion: "success"},
						{Name: "Run tests", Status: "completed", Conclusion: "failure"},
					},
				},
				Duration: 5 * time.Minute,
			},
		},
		MissingTools: []MissingToolReport{
			{Tool: "special_tool", Reason: "Not configured"},
		},
		MCPFailures: []MCPFailureReport{
			{ServerName: "test-mcp", Status: "connection_error"},
		},
		FirewallAnalysis: &FirewallAnalysis{
			AnalysisBase: AnalysisBase{
				DomainBuckets: DomainBuckets{
					AllowedDomains: []string{"api.github.com"},
					BlockedDomains: []string{"blocked.example.com"},
				},
				TotalRequests:   15,
				AllowedRequests: 10,
				BlockedRequests: 5,
			},
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
		TokenUsage: 25000,
		Turns:      8,
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "bash", CallCount: 15, MaxInputSize: 500, MaxOutputSize: 2000, MaxDuration: 5 * time.Second},
			{Name: "github_issue_read", CallCount: 8, MaxInputSize: 100, MaxOutputSize: 5000, MaxDuration: 2 * time.Second},
		},
	}

	// Build audit data
	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

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
		assert.Len(t, auditData.Jobs[1].Steps, 2,
			"Should preserve step details for each job")
		assert.Equal(t, "Run tests", auditData.Jobs[1].Steps[1].Name,
			"Should preserve step names")
		assert.Equal(t, "failure", auditData.Jobs[1].Steps[1].Conclusion,
			"Should preserve step conclusions")
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
	t.Parallel()
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

	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

	// Should still produce valid data
	assert.Equal(t, int64(1), auditData.Overview.RunID,
		"RunID should match even with minimal data")

	// Empty slices should be nil or empty, not cause panics
	// We just want to ensure no panics occur accessing these fields
	_ = auditData.Jobs
}

func TestBuildAuditDataFallbackMetricsWithoutAwInfo(t *testing.T) {
	t.Parallel()
	tmpDir := testutil.TempDir(t, "audit-fallback-*")
	logContent := `{"type":"result","subtype":"success","num_turns":7,"usage":{"input_tokens":100,"output_tokens":200}}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "agent-stdio.log"), []byte(logContent), 0o644))

	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   42,
			WorkflowName: "Fallback Metrics Workflow",
			Status:       "completed",
			Conclusion:   "success",
			LogsPath:     tmpDir,
			TokenUsage:   0,
			Turns:        0,
		},
		TokenUsage: &TokenUsageSummary{
			TotalInputTokens:      5944,
			TotalOutputTokens:     8698,
			TotalCacheReadTokens:  1170605,
			TotalCacheWriteTokens: 86049,
		},
	}

	auditData := buildAuditData(context.Background(), processedRun, workflow.LogMetrics{}, nil)
	assert.Equal(t, 14642, auditData.Metrics.TokenUsage, "token usage should fall back to input+output from agent usage summary")
	assert.Equal(t, 7, auditData.Metrics.Turns, "turns should fall back to inferred value from agent log")
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
			TokenUsage:   5000,
			Turns:        4,
			ErrorCount:   1,
			WarningCount: 2,
		},
		KeyFindings: []AuditFinding{
			{Category: "success", Severity: "info", Title: "Test Finding", Description: "Test description"},
		},
		Recommendations: []Recommendation{
			{Priority: "low", Action: "Monitor", Reason: "Test reason"},
		},
		Jobs: []JobData{
			{
				Name:       "test-job",
				Status:     "completed",
				Conclusion: "success",
				Duration:   "1m30s",
				Steps: []JobStepData{
					{Name: "Set up job", Status: "completed", Conclusion: "success"},
					{Name: "Run agent", Status: "completed", Conclusion: "success"},
				},
			},
		},
		DownloadedFiles: []FileInfo{
			{Path: "test.log", Size: 1024, Description: "Test log"},
		},
		Errors: []ValidationIssue{
			{Type: "error", Message: "Test error"},
		},
		Warnings: []ValidationIssue{
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
	assert.Len(t, parsed.Jobs[0].Steps, 2,
		"Job steps should be preserved in JSON")
	assert.Equal(t, "Run agent", parsed.Jobs[0].Steps[1].Name,
		"Job step names should be preserved in JSON")
	assert.Len(t, parsed.Errors, 1,
		"Errors should be preserved in JSON")
	assert.Len(t, parsed.Warnings, 2,
		"Warnings should be preserved in JSON")
}

func TestRenderConsoleExperimentsSorted(t *testing.T) {
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	renderConsole(AuditData{
		Overview: OverviewData{
			RunID:        7,
			WorkflowName: "Sort Experiments",
			Conclusion:   "success",
			Duration:     "1m0s",
			URL:          "https://example.test/runs/7",
		},
		Metrics: MetricsData{},
		Experiments: &ExperimentData{
			Assignments: map[string]string{
				"zeta":  "b",
				"alpha": "a",
			},
		},
	}, "/tmp/experiments")

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stderr = oldStderr

	assert.Contains(t, buf.String(), "  experiments: alpha=a zeta=b\n")
}

func TestToolUsageAggregation(t *testing.T) {
	t.Parallel()
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

	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

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
	t.Parallel()
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
	t.Parallel()
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
		ErrorCount: 5,
		Turns:      15, // Many turns
	}

	errors := []ValidationIssue{
		{Type: "error", Message: "Error 1"},
		{Type: "error", Message: "Error 2"},
		{Type: "error", Message: "Error 3"},
		{Type: "error", Message: "Error 4"},
		{Type: "error", Message: "Error 5"},
		{Type: "error", Message: "Error 6"},
	}

	findings := generateFindings(processedRun, metrics, errors)

	// Should have critical, high, and medium findings
	severityCounts := make(map[string]int)
	for _, f := range findings {
		severityCounts[f.Severity.String()]++
	}

	assert.NotZero(t, severityCounts["critical"],
		"Failed workflow should generate at least one critical finding")
	assert.NotZero(t, severityCounts["high"],
		"Should generate at least one high severity finding")
}

func TestRecommendationPriorityOrdering(t *testing.T) {
	t.Parallel()
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
			AnalysisBase: AnalysisBase{BlockedRequests: 20}, // Many blocked requests
		},
	}

	metrics := MetricsData{}

	findings := []AuditFinding{
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
	t.Parallel()
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

func TestExtractCreatedItemsFromManifest(t *testing.T) {
	t.Parallel()
	t.Run("returns nil for empty logsPath", func(t *testing.T) {
		items := extractCreatedItemsFromManifest("")
		assert.Nil(t, items, "should return nil for empty logsPath")
	})

	t.Run("returns nil when manifest file does not exist", func(t *testing.T) {
		dir := t.TempDir()
		items := extractCreatedItemsFromManifest(dir)
		assert.Nil(t, items, "should return nil when file does not exist")
	})

	t.Run("parses valid JSONL manifest", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"type":"create_issue","url":"https://github.com/owner/repo/issues/1","number":1,"repo":"owner/repo","timestamp":"2024-01-01T00:00:00Z"}
{"type":"add_comment","url":"https://github.com/owner/repo/issues/1#issuecomment-999","timestamp":"2024-01-01T00:01:00Z"}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0600))

		items := extractCreatedItemsFromManifest(dir)
		require.Len(t, items, 2, "should parse 2 items from manifest")
		assert.Equal(t, "create_issue", items[0].Type)
		assert.Equal(t, "https://github.com/owner/repo/issues/1", items[0].URL)
		assert.Equal(t, 1, items[0].Number)
		assert.Equal(t, "owner/repo", items[0].Repo)
		assert.Equal(t, "add_comment", items[1].Type)
	})

	t.Run("includes entries without URL (modification types like add_labels)", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"type":"add_labels","number":20875,"timestamp":"2024-01-01T00:00:00Z"}
{"type":"create_issue","url":"https://github.com/owner/repo/issues/2","timestamp":"2024-01-01T00:01:00Z"}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0600))

		items := extractCreatedItemsFromManifest(dir)
		require.Len(t, items, 2, "should include both entries: one without URL and one with URL")
		assert.Equal(t, "add_labels", items[0].Type)
		assert.Empty(t, items[0].URL)
		assert.Equal(t, 20875, items[0].Number)
		assert.Equal(t, "https://github.com/owner/repo/issues/2", items[1].URL)
	})

	t.Run("parses execution metadata snapshots", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"type":"update_issue","number":7,"repo":"owner/repo","before_state":{"title":"Before","labels":["triage"]},"after_state":{"title":"After","labels":["triage","done"]},"timestamp":"2024-01-01T00:00:00Z"}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0600))

		items := extractCreatedItemsFromManifest(dir)
		require.Len(t, items, 1)
		require.Equal(t, "update_issue", items[0].Type)
		require.Equal(t, "Before", items[0].BeforeState["title"])
		require.Equal(t, "After", items[0].AfterState["title"])
		require.Equal(t, []any{"triage"}, items[0].BeforeState["labels"])
		require.Equal(t, []any{"triage", "done"}, items[0].AfterState["labels"])
	})

	t.Run("skips entries without type field", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"url":"https://github.com/owner/repo/issues/1","timestamp":"2024-01-01T00:00:00Z"}
{"type":"add_comment","url":"https://github.com/owner/repo/issues/1#issuecomment-1","timestamp":"2024-01-01T00:02:00Z"}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0600))

		items := extractCreatedItemsFromManifest(dir)
		require.Len(t, items, 1, "should skip entry without type and parse 1 valid one")
		assert.Equal(t, "add_comment", items[0].Type)
	})

	t.Run("skips invalid JSON lines", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"type":"create_issue","url":"https://github.com/owner/repo/issues/1","timestamp":"2024-01-01T00:00:00Z"}
not-valid-json
{"type":"add_comment","url":"https://github.com/owner/repo/issues/1#issuecomment-1","timestamp":"2024-01-01T00:02:00Z"}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0600))

		items := extractCreatedItemsFromManifest(dir)
		require.Len(t, items, 2, "should skip invalid JSON lines and parse 2 valid ones")
	})

	t.Run("returns nil for empty manifest file", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(""), 0600))

		items := extractCreatedItemsFromManifest(dir)
		assert.Nil(t, items, "should return nil for empty manifest")
	})
}

func TestResolveCreatedItems(t *testing.T) {
	t.Parallel()

	t.Run("falls back to cached safe outputs when manifest is absent", func(t *testing.T) {
		dir := t.TempDir()
		cached := []CreatedItemReport{{Type: "create_issue", Number: 42, Repo: "owner/repo"}}

		items := resolveCreatedItems(dir, cached)

		require.Len(t, items, 1)
		assert.Equal(t, "create_issue", items[0].Type)
		assert.Equal(t, 42, items[0].Number)
	})

	t.Run("prefers the on-disk manifest over cached safe outputs when both are present", func(t *testing.T) {
		dir := t.TempDir()
		content := `{"type":"add_comment","number":7,"repo":"owner/repo","timestamp":"2024-01-01T00:00:00Z"}
`
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0600))
		cached := []CreatedItemReport{{Type: "create_issue", Number: 42, Repo: "owner/repo"}}

		items := resolveCreatedItems(dir, cached)

		require.Len(t, items, 1)
		assert.Equal(t, "add_comment", items[0].Type)
	})

	t.Run("returns nil when neither manifest nor cached safe outputs are available", func(t *testing.T) {
		dir := t.TempDir()
		items := resolveCreatedItems(dir, nil)
		assert.Nil(t, items)
	})

	t.Run("does not fall back to cached safe outputs when manifest exists but is empty", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(""), 0600))
		cached := []CreatedItemReport{{Type: "create_issue", Number: 42, Repo: "owner/repo"}}

		items := resolveCreatedItems(dir, cached)

		assert.Nil(t, items)
	})
}

func TestParseStepFilename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		filename     string
		expectedNum  int
		expectedName string
	}{
		{
			name:         "typical step filename",
			filename:     "12_Validate lockdown mode requirements.txt",
			expectedNum:  12,
			expectedName: "Validate lockdown mode requirements",
		},
		{
			name:         "single digit step",
			filename:     "1_Set up job.txt",
			expectedNum:  1,
			expectedName: "Set up job",
		},
		{
			name:         "step with underscores in name",
			filename:     "5_Run_shell_script.txt",
			expectedNum:  5,
			expectedName: "Run_shell_script",
		},
		{
			name:         "no underscore separator",
			filename:     "nomatch.txt",
			expectedNum:  0,
			expectedName: "nomatch",
		},
		{
			name:         "non-numeric prefix",
			filename:     "abc_Step name.txt",
			expectedNum:  0,
			expectedName: "abc_Step name",
		},
		{
			name:         "leading underscore only",
			filename:     "_Step name.txt",
			expectedNum:  0,
			expectedName: "_Step name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			num, name := parseStepFilename(tt.filename)
			assert.Equal(t, tt.expectedNum, num, "Step number should match")
			assert.Equal(t, tt.expectedName, name, "Step name should match")
		})
	}
}

func TestStripGHALogTimestamps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "typical GHA log line with timestamp",
			input:    "2024-01-15T10:30:45.1234567Z This is the log message",
			expected: "This is the log message",
		},
		{
			name:     "short fractional seconds",
			input:    "2024-01-15T10:30:45.123Z Short fractional message",
			expected: "Short fractional message",
		},
		{
			name:     "no fractional seconds",
			input:    "2024-01-15T10:30:45Z No fractional message",
			expected: "No fractional message",
		},
		{
			name:     "multiple lines with timestamps",
			input:    "2024-01-15T10:30:45.1234567Z Line one\n2024-01-15T10:30:46.9876543Z Line two",
			expected: "Line one\nLine two",
		},
		{
			name:     "line without timestamp unchanged",
			input:    "This line has no timestamp",
			expected: "This line has no timestamp",
		},
		{
			name:     "mixed lines",
			input:    "2024-01-15T10:30:45.1234567Z Timestamped\nNot timestamped",
			expected: "Timestamped\nNot timestamped",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripGHALogTimestamps(tt.input)
			assert.Equal(t, tt.expected, result, "Stripped content should match expected")
		})
	}
}

func TestExtractPreAgentStepErrors(t *testing.T) {
	t.Parallel()
	t.Run("falls back to agent-stdio.log excerpt when agent ran and no ##[error] annotation exists", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		// Create agent-stdio.log to indicate agent ran
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("INFO: startup\nERROR: request failed with status 500"), 0600))
		// Create workflow-logs with a step log that has content
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "activation")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "5_Generate agentic run info.txt"), []byte("Error: lockdown failed"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should return fallback error context when agent-stdio.log exists")
		require.Len(t, errors, 1, "Should return one fallback error info")
		assert.Equal(t, "agent_failure", errors[0].Type, "Error type should indicate agent failure excerpt")
		assert.Equal(t, "agent-stdio.log", errors[0].File, "File should point to agent-stdio.log")
		assert.Contains(t, errors[0].Message, "ERROR: request failed with status 500", "Should include concrete failure from agent-stdio.log")
	})

	t.Run("prefers ##[error] workflow annotations over agent-stdio fallback when agent ran", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("ERROR: generic fallback message"), 0600))
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "activation")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "5_Generate agentic run info.txt"),
			[]byte("2026-02-23T23:46:10.9523559Z ##[error]Lockdown mode is enabled but no token configured"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should return ##[error] annotations when present")
		require.Len(t, errors, 1, "Should return one error from workflow annotation")
		assert.Equal(t, "step_failure", errors[0].Type, "Should keep step_failure type for ##[error] annotations")
		assert.Equal(t, "activation/Generate agentic run info", errors[0].File, "Should reference failing workflow step")
		assert.Contains(t, errors[0].Message, "Lockdown mode is enabled", "Should include actionable ##[error] text")
	})

	t.Run("ignores annotated tool results and surfaces the runner failure", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("agent output"), 0600))
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "agent")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		toolResult := `{"type":"user","message":{"content":[{"type":"tool_result","content":"raw Go source"}]}}`
		logContent := "2026-08-13T04:03:11Z ##[error]" + toolResult + "\n" +
			"2026-08-13T04:11:07Z ##[error]The action 'Execute Claude Code CLI' has timed out after 15 minutes."
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "10_Execute Claude Code CLI.txt"), []byte(logContent), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.Len(t, errors, 1)
		assert.Contains(t, errors[0].Message, "timed out after 15 minutes")
		assert.NotContains(t, errors[0].Message, "raw Go source")
	})

	t.Run("preserves mixed tool result annotations", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "agent")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		mixedContent := `{"type":"user","message":{"content":[{"type":"tool_result","content":"raw Go source"},{"type":"text","text":"runner failure"}]}}`
		logContent := "2026-08-13T04:03:11Z ##[error]" + mixedContent
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "10_Execute Claude Code CLI.txt"), []byte(logContent), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.Len(t, errors, 1)
		assert.Contains(t, errors[0].Message, "runner failure")
	})

	t.Run("returns nil when workflow-logs directory missing", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		// No agent-stdio.log and no workflow-logs directory
		errors := extractPreAgentStepErrors(dir)
		assert.Nil(t, errors, "Should return nil when no workflow-logs directory")
	})

	t.Run("emits agent_failure fallback when agent-stdio.log exists but workflow-logs is missing", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		// agent-stdio.log present but no workflow-logs directory at all
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("ERROR: connection refused\nfatal: server unreachable"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should emit agent_failure fallback even when workflow-logs is absent")
		require.Len(t, errors, 1)
		assert.Equal(t, "agent_failure", errors[0].Type)
		assert.Equal(t, "agent-stdio.log", errors[0].File)
		assert.Contains(t, errors[0].Message, "ERROR: connection refused")
	})

	t.Run("returns nil when workflow-logs cannot be read even if agent-stdio exists", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "agent-stdio.log"), []byte("ERROR: should not be used"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-logs"), []byte("not a directory"), 0600))

		errors := extractPreAgentStepErrors(dir)
		assert.Nil(t, errors, "Should preserve early return when workflow-logs exists but cannot be scanned")
	})

	t.Run("extracts error from last step log file", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		// No agent-stdio.log
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "activation")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Create multiple step logs - last one should be selected
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "1_Set up job.txt"), []byte("Setup complete"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "4_Check workflow lock file.txt"), []byte("Timestamp check complete"), 0600))
		lockdownErr := "Lockdown mode is enabled (lockdown: true) but no custom GitHub token is configured.\n\nPlease configure one of the following as a repository secret:\n  - GH_AW_GITHUB_TOKEN (recommended)"
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "5_Generate agentic run info.txt"), []byte(lockdownErr), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should return errors from step logs")
		require.Len(t, errors, 1, "Should return exactly one error info")
		assert.Equal(t, "step_failure", errors[0].Type, "Error type should be step_failure")
		assert.Equal(t, "activation/Generate agentic run info", errors[0].File, "File should include job and step name")
		assert.Contains(t, errors[0].Message, "Lockdown mode is enabled", "Message should contain lockdown error text")
		assert.Contains(t, errors[0].Message, "GH_AW_GITHUB_TOKEN", "Message should contain token suggestion")
	})

	t.Run("strips GHA timestamps from step log content", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "agent")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Step log with GHA-style timestamps
		logContent := "2024-01-15T10:30:45.1234567Z Error: something went wrong\n2024-01-15T10:30:46.9876543Z Please check your configuration"
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "3_Run validation.txt"), []byte(logContent), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should extract errors from step log")
		assert.Contains(t, errors[0].Message, "Error: something went wrong", "Should strip timestamps")
		assert.NotContains(t, errors[0].Message, "2024-01-15T", "Should not contain timestamp prefix")
	})

	t.Run("truncates long step log content", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "agent")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Create a very long log message
		longContent := strings.Repeat("This is a very long error message. ", 100)
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "7_Long step.txt"), []byte(longContent), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should extract errors from long step log")
		assert.LessOrEqual(t, len(errors[0].Message), 1503, "Message should be truncated to maxMessageLen (1500) + len(\"...\") (3)")
		assert.True(t, strings.HasSuffix(errors[0].Message, "..."), "Truncated message should end with ellipsis")
	})

	t.Run("prioritizes ##[error] annotations over last step fallback", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "activation")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Step 3 has a ##[error] annotation (the real failure)
		lockdownLog := "2026-02-23T23:46:10.9523559Z ##[error]Lockdown mode is enabled (lockdown: true) but no custom GitHub token is configured.\n2026-02-23T23:46:10.9523560Z Please configure GH_AW_GITHUB_TOKEN"
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "3_Generate agentic run info.txt"), []byte(lockdownLog), 0600))
		// Step 15 is the "Complete job" step with unrelated cleanup content (higher step number)
		completeJobLog := "2026-02-23T23:46:13.5790741Z Evaluate and set job outputs\n2026-02-23T23:46:13.5790742Z Set output 'checkout_pr_success'\n2026-02-23T23:46:13.5790743Z Set output 'has_patch'"
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "15_Complete job.txt"), []byte(completeJobLog), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should return errors from ##[error] annotations")
		require.Len(t, errors, 1, "Should return one error for the step with ##[error]")
		assert.Equal(t, "activation/Generate agentic run info", errors[0].File, "Should reference the step with ##[error], not Complete job")
		assert.Contains(t, errors[0].Message, "Lockdown mode is enabled", "Message should contain the actual ##[error] annotation content")
		assert.NotContains(t, errors[0].Message, "Evaluate and set job outputs", "Message should not contain Complete job cleanup content")
		assert.NotContains(t, errors[0].Message, "2026-02-23T", "Should strip GHA timestamps from ##[error] lines")
	})

	t.Run("returns ##[error] annotations from multiple steps", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "agent")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Two steps each with ##[error] annotations
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "3_Step A.txt"),
			[]byte("2024-01-01T00:00:01Z ##[error]First error"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "5_Step B.txt"),
			[]byte("2024-01-01T00:00:02Z ##[error]Second error"), 0600))
		// Higher-numbered step with no ##[error]
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "10_Complete job.txt"),
			[]byte("Cleanup content"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should return errors")
		assert.Len(t, errors, 2, "Should return one ValidationIssue per step with ##[error] annotations")
		// All returned errors should be from steps with ##[error], not the cleanup step
		for _, e := range errors {
			assert.NotEqual(t, "agent/Complete job", e.File, "Should not include cleanup step in errors")
		}
	})

	t.Run("falls back to last step when no ##[error] annotations exist", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "agent")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Step 3 has non-annotated error content
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "3_Some step.txt"), []byte("Some content"), 0600))
		// Step 7 is the last step and has the actual failure (no ##[error] prefix)
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "7_Failing step.txt"), []byte("Error: installation failed"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should fall back to last step content")
		require.Len(t, errors, 1, "Fallback should return one error")
		assert.Equal(t, "agent/Failing step", errors[0].File, "Fallback should use the last step")
		assert.Contains(t, errors[0].Message, "Error: installation failed", "Fallback should use last step content")
	})

	t.Run("builds error summary from step errors in buildAuditData", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		// No agent-stdio.log
		workflowLogsDir := filepath.Join(dir, "workflow-logs", "activation")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "5_Generate agentic run info.txt"),
			[]byte("Lockdown mode is enabled but no token configured"), 0600))

		run := WorkflowRun{
			DatabaseID:   123,
			Conclusion:   "failure",
			LogsPath:     dir,
			WorkflowName: "Test Workflow",
		}
		processedRun := ProcessedRun{Run: run}
		metrics := LogMetrics{}

		data := buildAuditData(context.Background(), processedRun, metrics, nil)

		require.NotEmpty(t, data.Errors, "Should have errors extracted from step logs for failed run")
		assert.Contains(t, data.Errors[0].Message, "Lockdown mode is enabled",
			"Error should contain the actual error from the step log")
	})

	t.Run("extracts ##[error] from flat job log files", func(t *testing.T) {
		// GitHub Actions log zips may use a flat structure where each job is a single file
		// at the root of workflow-logs/ (e.g., 3_activation.txt) rather than a subdirectory.
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Flat job log with ##[error] annotation
		lockdownLog := "2026-02-23T23:46:10.9523559Z ##[error]Lockdown mode is enabled (lockdown: true) but no custom GitHub token is configured.\n2026-02-23T23:46:10.9523560Z Please configure GH_AW_GITHUB_TOKEN"
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "3_activation.txt"), []byte(lockdownLog), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should extract errors from flat job log")
		require.Len(t, errors, 1, "Should return one error info for the flat job log")
		assert.Equal(t, "step_failure", errors[0].Type, "Error type should be step_failure")
		assert.Equal(t, "activation", errors[0].File, "File should be the job name from flat log")
		assert.Contains(t, errors[0].Message, "Lockdown mode is enabled", "Message should contain the ##[error] content")
		assert.NotContains(t, errors[0].Message, "2026-02-23T", "Should strip GHA timestamps")
	})

	t.Run("falls back to last flat job log when no ##[error] in flat files", func(t *testing.T) {
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "1_setup.txt"), []byte("Setup output"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "3_activation.txt"), []byte("Error: lockdown check failed"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should fall back to last flat job log content")
		require.Len(t, errors, 1, "Fallback should return one error")
		assert.Equal(t, "activation", errors[0].File, "Fallback should use the last flat log (highest number)")
		assert.Contains(t, errors[0].Message, "Error: lockdown check failed", "Should use last flat log content")
	})

	t.Run("flat job logs and subdirectory step logs are both scanned", func(t *testing.T) {
		// Mixed structure: some jobs as flat files, some as subdirectories
		dir := testutil.TempDir(t, "audit-step-*")
		workflowLogsDir := filepath.Join(dir, "workflow-logs")
		require.NoError(t, os.MkdirAll(workflowLogsDir, 0755))
		// Flat job log with ##[error]
		require.NoError(t, os.WriteFile(filepath.Join(workflowLogsDir, "2_activation.txt"),
			[]byte("2024-01-01T00:00:01Z ##[error]Flat job error"), 0600))
		// Subdirectory job with ##[error] in a step
		agentDir := filepath.Join(workflowLogsDir, "agent")
		require.NoError(t, os.MkdirAll(agentDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(agentDir, "5_Run agent.txt"),
			[]byte("2024-01-01T00:00:02Z ##[error]Subdirectory step error"), 0600))

		errors := extractPreAgentStepErrors(dir)
		require.NotNil(t, errors, "Should extract errors from both flat and subdirectory logs")
		assert.Len(t, errors, 2, "Should return errors from both flat and subdirectory sources")
		files := []string{errors[0].File, errors[1].File}
		assert.Contains(t, files, "activation", "Should include flat job log error")
		assert.Contains(t, files, "agent/Run agent", "Should include subdirectory step error")
	})
}

func TestBuildAuditDataActionMinutes(t *testing.T) {
	t.Parallel()
	// Verify that action_minutes is populated from run.Duration even when
	// token/turn metrics are zero (e.g. Codex runs that exit early).
	// math.Ceil should be applied, and a pre-set run.ActionMinutes should take precedence.
	t.Run("derived from Duration with ceil", func(t *testing.T) {
		processedRun := ProcessedRun{
			Run: WorkflowRun{
				DatabaseID:   42,
				WorkflowName: "Codex Test",
				Status:       "completed",
				Conclusion:   "failure",
				Duration:     6*time.Minute + 30*time.Second, // 6.5 minutes → ceil = 7
				TokenUsage:   0,
				Turns:        0,
				ErrorCount:   1,
				WarningCount: 0,
			},
		}

		metrics := workflow.LogMetrics{}
		auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

		assert.InDelta(t, 7.0, auditData.Metrics.ActionMinutes, 0.01,
			"ActionMinutes should be ceil of duration minutes (6.5m → 7)")
		assert.Equal(t, 0, auditData.Metrics.TokenUsage,
			"TokenUsage should be 0 when no log metrics available")
		assert.Equal(t, 0, auditData.Metrics.Turns,
			"Turns should be 0 when no log metrics available")
	})

	t.Run("uses pre-set ActionMinutes when available", func(t *testing.T) {
		processedRun := ProcessedRun{
			Run: WorkflowRun{
				DatabaseID:    43,
				WorkflowName:  "Pre-set Test",
				Status:        "completed",
				Conclusion:    "success",
				Duration:      6 * time.Minute,
				ActionMinutes: 8.0, // pre-set by orchestrator, takes precedence
			},
		}

		metrics := workflow.LogMetrics{}
		auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

		assert.InDelta(t, 8.0, auditData.Metrics.ActionMinutes, 0.01,
			"Pre-set ActionMinutes should take precedence over Duration")
	})
}

func TestGenerateFindingsFirewallWithBlockedDomains(t *testing.T) {
	t.Parallel()
	// A single blocked domain should produce a finding naming the domain.
	pr := createTestProcessedRun()
	fw := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			TotalRequests:   1,
			BlockedRequests: 1,
			AllowedRequests: 0,
		},
		RequestsByDomain: map[string]DomainRequestStats{},
	}
	fw.SetBlockedDomains([]string{"chatgpt.com"})
	pr.FirewallAnalysis = fw

	findings := generateFindings(pr, MetricsData{}, nil)

	var networkFinding *AuditFinding
	for i := range findings {
		if findings[i].Category == "network" {
			networkFinding = &findings[i]
			break
		}
	}
	require.NotNil(t, networkFinding, "Should generate a network finding when firewall blocks a domain")
	assert.Contains(t, networkFinding.Description, "chatgpt.com",
		"Finding description should name the blocked domain")
}

func TestGenerateRecommendationsFirewallSingleBlock(t *testing.T) {
	t.Parallel()
	// Even a single blocked request (threshold changed from >10 to >0)
	// should generate a recommendation with the domain name in the example.
	pr := createTestProcessedRun()
	fw := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			TotalRequests:   1,
			BlockedRequests: 1,
			AllowedRequests: 0,
		},
		RequestsByDomain: map[string]DomainRequestStats{},
	}
	fw.SetBlockedDomains([]string{"chatgpt.com"})
	pr.FirewallAnalysis = fw

	findings := []AuditFinding{{Category: "network", Severity: "medium", Title: "Blocked Network Requests"}}
	recs := generateRecommendations(pr, MetricsData{}, findings)

	var networkRec *Recommendation
	for i := range recs {
		if strings.Contains(recs[i].Action, "network") || strings.Contains(recs[i].Action, "blocked") {
			networkRec = &recs[i]
			break
		}
	}
	require.NotNil(t, networkRec, "Should generate a network recommendation for any firewall block")
	assert.Contains(t, networkRec.Example, "chatgpt.com",
		"Recommendation example should include the blocked domain name")
}

func TestGenerateRecommendationsFiltersDashPlaceholder(t *testing.T) {
	t.Parallel()
	// Defense-in-depth: even if "-" somehow appears in BlockedDomains (e.g. from
	// extractFirewallFromAgentLog or a future code path), the recommendation must
	// not include it as an allow-list entry.  In practice, parseFirewallLog now
	// replaces "-" with the unknownDomain sentinel, but other ingestion paths may
	// still produce "-" entries.
	pr := createTestProcessedRun()
	fw := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			TotalRequests:   1,
			BlockedRequests: 1,
			AllowedRequests: 0,
		},
		RequestsByDomain: map[string]DomainRequestStats{"-": {Blocked: 1}},
	}
	fw.SetBlockedDomains([]string{"-"})
	pr.FirewallAnalysis = fw

	findings := []AuditFinding{{Category: "network", Severity: "medium", Title: "Blocked Network Requests"}}
	recs := generateRecommendations(pr, MetricsData{}, findings)

	var networkRec *Recommendation
	for i := range recs {
		if strings.Contains(recs[i].Action, "network") || strings.Contains(recs[i].Action, "blocked") {
			networkRec = &recs[i]
			break
		}
	}
	require.NotNil(t, networkRec, "Should still generate a network recommendation when blocked requests exist")
	assert.NotContains(t, networkRec.Example, "    - -",
		"Recommendation example should not include the \"-\" placeholder as an allow-list entry")
}

func TestGenerateRecommendationsFiltersUnknownSentinel(t *testing.T) {
	t.Parallel()
	// When blocked domains only contain the unknownDomain sentinel (from iptables drops
	// where no destination info is available), the recommendation should not include the
	// sentinel in the allow-list example.
	pr := createTestProcessedRun()
	fw := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			TotalRequests:   1,
			BlockedRequests: 1,
			AllowedRequests: 0,
		},
		RequestsByDomain: map[string]DomainRequestStats{unknownDomain: {Blocked: 1}},
	}
	fw.SetBlockedDomains([]string{unknownDomain})
	pr.FirewallAnalysis = fw

	findings := []AuditFinding{{Category: "network", Severity: "medium", Title: "Blocked Network Requests"}}
	recs := generateRecommendations(pr, MetricsData{}, findings)

	var networkRec *Recommendation
	for i := range recs {
		if strings.Contains(recs[i].Action, "network") || strings.Contains(recs[i].Action, "blocked") {
			networkRec = &recs[i]
			break
		}
	}
	require.NotNil(t, networkRec, "Should still generate a network recommendation when blocked requests exist")
	assert.NotContains(t, networkRec.Example, unknownDomain,
		"Recommendation example should not include the unknownDomain sentinel as an allow-list entry")
}

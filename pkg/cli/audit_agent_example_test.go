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
)

// TestAgentFriendlyOutputExample demonstrates the new agent-friendly output format
func TestAgentFriendlyOutputExample(t *testing.T) {
	// Create a realistic workflow run scenario
	run := WorkflowRun{
		DatabaseID:    987654,
		WorkflowName:  "weekly-research",
		Status:        "completed",
		Conclusion:    "success",
		CreatedAt:     time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		StartedAt:     time.Date(2024, 1, 15, 10, 1, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2024, 1, 15, 10, 15, 30, 0, time.UTC),
		Duration:      14*time.Minute + 30*time.Second,
		Event:         "schedule",
		HeadBranch:    "main",
		URL:           "https://github.com/org/repo/actions/runs/987654",
		TokenUsage:    45000,
		EstimatedCost: 0.18,
		Turns:         12,
		ErrorCount:    0,
		WarningCount:  2,
		LogsPath:      testutil.TempDir(t, "test-*"),
	}

	metrics := LogMetrics{
		TokenUsage:    45000,
		EstimatedCost: 0.18,
		Turns:         12,
		ToolCalls: []workflow.ToolCallInfo{
			{
				Name:          "github_search_repositories",
				CallCount:     8,
				MaxInputSize:  512,
				MaxOutputSize: 4096,
				MaxDuration:   3 * time.Second,
			},
			{
				Name:          "web_search",
				CallCount:     5,
				MaxInputSize:  256,
				MaxOutputSize: 2048,
				MaxDuration:   2 * time.Second,
			},
			{
				Name:          "bash_echo",
				CallCount:     3,
				MaxInputSize:  128,
				MaxOutputSize: 256,
				MaxDuration:   500 * time.Millisecond,
			},
		},
	}

	firewallAnalysis := &FirewallAnalysis{
		DomainBuckets: DomainBuckets{
			AllowedDomains: []string{
				"api.github.com:443",
				"search.brave.com:443",
				"npmjs.org:443",
			},
			BlockedDomains: []string{
				"tracking.example.com:443",
			},
		},
		TotalRequests:   42,
		AllowedRequests: 40,
		BlockedRequests: 2,
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":       {Allowed: 25, Blocked: 0},
			"search.brave.com:443":     {Allowed: 10, Blocked: 0},
			"npmjs.org:443":            {Allowed: 5, Blocked: 0},
			"tracking.example.com:443": {Allowed: 0, Blocked: 2},
		},
	}

	processedRun := ProcessedRun{
		Run:              run,
		FirewallAnalysis: firewallAnalysis,
		MissingTools:     []MissingToolReport{},
		MCPFailures:      []MCPFailureReport{},
		JobDetails: []JobInfoWithDuration{
			{
				JobInfo: JobInfo{
					Name:        "research",
					Status:      "completed",
					Conclusion:  "success",
					StartedAt:   run.StartedAt,
					CompletedAt: run.UpdatedAt,
				},
				Duration: run.Duration,
			},
		},
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, nil)

	// Test JSON output
	t.Run("JSON Output", func(t *testing.T) {
		jsonBytes, err := json.MarshalIndent(auditData, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal JSON: %v", err)
		}

		// Verify key sections exist
		jsonStr := string(jsonBytes)
		if !strings.Contains(jsonStr, `"key_findings"`) {
			t.Error("JSON missing key_findings")
		}
		if !strings.Contains(jsonStr, `"recommendations"`) {
			t.Error("JSON missing recommendations")
		}
		if !strings.Contains(jsonStr, `"performance_metrics"`) {
			t.Error("JSON missing performance_metrics")
		}

		// Print sample JSON for documentation
		t.Logf("Sample JSON Output:\n%s", string(jsonBytes))
	})

	// Test console output
	t.Run("Console Output", func(t *testing.T) {
		// Capture console output - renderConsole now writes to stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		renderConsole(auditData, run.LogsPath)

		w.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		os.Stderr = oldStderr

		output := buf.String()

		// Verify key sections - now without markdown-style headers
		expectedSections := []string{
			"Workflow Run Audit Report",
			"Overview",
			"Key Findings",
			"Recommendations",
			"Performance Metrics",
			"Metrics",
			"Jobs",
			"Firewall Analysis",
			"Tool Usage",
		}

		for _, section := range expectedSections {
			if !strings.Contains(output, section) {
				t.Errorf("Console output missing section: %s", section)
			}
		}

		// Verify emojis and visual indicators
		if !strings.Contains(output, "✅") {
			t.Error("Console output should contain success indicator ✅")
		}

		// Print sample console output for documentation
		t.Logf("Sample Console Output:\n%s", output)
	})

	// Verify key findings quality
	t.Run("Key Findings Quality", func(t *testing.T) {
		if len(auditData.KeyFindings) == 0 {
			t.Error("Expected key findings to be generated")
		}

		// Should have findings for high token usage and many turns
		hasPerformanceFinding := false
		for _, finding := range auditData.KeyFindings {
			if finding.Category == "performance" {
				hasPerformanceFinding = true
			}
			// All findings should have impact
			if finding.Impact == "" && finding.Severity != "info" {
				t.Errorf("Finding '%s' missing impact", finding.Title)
			}
		}

		if !hasPerformanceFinding {
			t.Error("Expected performance finding for high token usage")
		}
	})

	// Verify recommendations quality
	t.Run("Recommendations Quality", func(t *testing.T) {
		if len(auditData.Recommendations) == 0 {
			t.Error("Expected recommendations to be generated")
		}

		for _, rec := range auditData.Recommendations {
			// All recommendations should have action, reason, and priority
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

	// Verify performance metrics
	t.Run("Performance Metrics Quality", func(t *testing.T) {
		if auditData.PerformanceMetrics == nil {
			t.Fatal("Expected performance metrics to be generated")
		}

		pm := auditData.PerformanceMetrics

		if pm.TokensPerMinute <= 0 {
			t.Error("Expected tokens per minute to be calculated")
		}

		if pm.CostEfficiency == "" {
			t.Error("Expected cost efficiency to be set")
		}

		if pm.MostUsedTool == "" {
			t.Error("Expected most used tool to be identified")
		}

		if pm.NetworkRequests != 42 {
			t.Errorf("Expected 42 network requests, got %d", pm.NetworkRequests)
		}

		// Verify cost efficiency calculation
		// Cost: $0.18, Duration: 14.5 minutes = $0.0124/min → "good"
		if pm.CostEfficiency != "good" {
			t.Errorf("Expected 'good' cost efficiency, got '%s'", pm.CostEfficiency)
		}
	})
}

// TestAgentFriendlyOutputFailureScenario tests output for a failed workflow
func TestAgentFriendlyOutputFailureScenario(t *testing.T) {
	// Create a failed workflow scenario
	run := WorkflowRun{
		DatabaseID:    111222,
		WorkflowName:  "ci-build",
		Status:        "completed",
		Conclusion:    "failure",
		CreatedAt:     time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
		Duration:      3*time.Minute + 45*time.Second,
		Event:         "push",
		HeadBranch:    "feature-branch",
		URL:           "https://github.com/org/repo/actions/runs/111222",
		TokenUsage:    8000,
		EstimatedCost: 0.03,
		Turns:         4,
		ErrorCount:    3,
		WarningCount:  1,
		LogsPath:      testutil.TempDir(t, "test-*"),
	}

	metrics := LogMetrics{
		TokenUsage:    8000,
		EstimatedCost: 0.03,
		Turns:         4,
	}

	processedRun := ProcessedRun{
		Run: run,
		MCPFailures: []MCPFailureReport{
			{
				ServerName: "build-tools",
				Status:     "connection_failed",
			},
		},
		JobDetails: []JobInfoWithDuration{
			{
				JobInfo: JobInfo{
					Name:       "build",
					Status:     "completed",
					Conclusion: "failure",
				},
				Duration: run.Duration,
			},
		},
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, nil)

	// Test failure analysis
	t.Run("Failure Analysis", func(t *testing.T) {
		if auditData.FailureAnalysis == nil {
			t.Fatal("Expected failure analysis for failed workflow")
		}

		fa := auditData.FailureAnalysis

		if fa.PrimaryFailure != "failure" {
			t.Errorf("Expected primary failure 'failure', got '%s'", fa.PrimaryFailure)
		}

		if len(fa.FailedJobs) == 0 {
			t.Error("Expected failed jobs to be listed")
		}

		if fa.RootCause == "" {
			t.Error("Expected root cause to be identified")
		}

		// Should identify timeout as root cause
		if !strings.Contains(fa.RootCause, "timeout") && !strings.Contains(fa.RootCause, "MCP server") {
			t.Errorf("Expected timeout or MCP failure as root cause, got: %s", fa.RootCause)
		}
	})

	// Test key findings for failure
	t.Run("Failure Findings", func(t *testing.T) {
		if len(auditData.KeyFindings) == 0 {
			t.Error("Expected key findings for failed workflow")
		}

		// Should have critical failure finding
		hasCritical := false
		hasMCPFailure := false
		for _, finding := range auditData.KeyFindings {
			if finding.Severity == "critical" && strings.Contains(finding.Title, "Failed") {
				hasCritical = true
			}
			if finding.Category == "tooling" && strings.Contains(finding.Description, "MCP") {
				hasMCPFailure = true
			}
		}

		if !hasCritical {
			t.Error("Expected critical failure finding")
		}
		if !hasMCPFailure {
			t.Error("Expected MCP failure finding")
		}
	})

	// Test recommendations for failure
	t.Run("Failure Recommendations", func(t *testing.T) {
		if len(auditData.Recommendations) == 0 {
			t.Error("Expected recommendations for failed workflow")
		}

		// Should have high priority recommendations
		hasHighPriority := false
		for _, rec := range auditData.Recommendations {
			if rec.Priority == "high" {
				hasHighPriority = true
				// High priority recommendations should mention review or fix
				if !strings.Contains(strings.ToLower(rec.Action), "review") &&
					!strings.Contains(strings.ToLower(rec.Action), "fix") {
					t.Errorf("High priority recommendation should mention review or fix: %s", rec.Action)
				}
			}
		}

		if !hasHighPriority {
			t.Error("Expected high priority recommendations for failure")
		}
	})

	// Test JSON output for failure
	t.Run("JSON Output for Failure", func(t *testing.T) {
		jsonBytes, err := json.MarshalIndent(auditData, "", "  ")
		if err != nil {
			t.Fatalf("Failed to marshal JSON: %v", err)
		}

		jsonStr := string(jsonBytes)

		// Verify failure analysis is included
		if !strings.Contains(jsonStr, `"failure_analysis"`) {
			t.Error("JSON missing failure_analysis for failed workflow")
		}
		if !strings.Contains(jsonStr, `"primary_failure"`) {
			t.Error("JSON missing primary_failure field")
		}
		if !strings.Contains(jsonStr, `"root_cause"`) {
			t.Error("JSON missing root_cause field")
		}

		// Print for documentation
		t.Logf("Failure Scenario JSON Output:\n%s", string(jsonBytes))
	})
}

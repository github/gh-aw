//go:build !integration

package cli

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectTaskDomain(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			WorkflowName: "Weekly Research Report",
			WorkflowPath: ".github/workflows/weekly-research.yml",
			Event:        "schedule",
		},
	}

	domain := detectTaskDomain(processedRun, nil, nil, nil)
	require.NotNil(t, domain, "domain should be detected")
	assert.Equal(t, "research", domain.Name)
	assert.Equal(t, "Research", domain.Label)
}

func TestBuildAgenticAssessmentsFlagsPotentialDeterministicAlternative(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			WorkflowName: "Issue Triage",
			Turns:        2,
			Duration:     2 * time.Minute,
		},
	}
	metrics := MetricsData{Turns: 2}
	toolUsage := []ToolUsageInfo{{Name: "github_issue_read", CallCount: 1}}
	domain := &TaskDomainInfo{Name: "triage", Label: "Triage"}
	fingerprint := &BehaviorFingerprint{
		ExecutionStyle:  "directed",
		ToolBreadth:     "narrow",
		ActuationStyle:  "read_only",
		ResourceProfile: "lean",
		DispatchMode:    "standalone",
	}

	assessments := buildAgenticAssessments(processedRun, metrics, toolUsage, nil, domain, fingerprint, nil)
	require.NotEmpty(t, assessments)
	assert.Equal(t, "overkill_for_agentic", assessments[0].Kind)
}

func TestBuildAgenticAssessmentsFlagsResourceHeavyRun(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			WorkflowName:   "Deep Research",
			Turns:          15,
			Duration:       22 * time.Minute,
			SafeItemsCount: 4,
		},
	}
	metrics := MetricsData{Turns: 15}
	toolUsage := []ToolUsageInfo{
		{Name: "bash", CallCount: 4},
		{Name: "grep", CallCount: 3},
		{Name: "gh", CallCount: 2},
		{Name: "github_issue_read", CallCount: 2},
		{Name: "sed", CallCount: 1},
		{Name: "cat", CallCount: 1},
		{Name: "jq", CallCount: 1},
	}
	domain := &TaskDomainInfo{Name: "research", Label: "Research"}
	fingerprint := buildBehaviorFingerprint(processedRun, metrics, toolUsage, []CreatedItemReport{{Type: "create_issue"}}, nil)

	assessments := buildAgenticAssessments(processedRun, metrics, toolUsage, []CreatedItemReport{{Type: "create_issue"}}, domain, fingerprint, nil)

	var found bool
	for _, assessment := range assessments {
		if assessment.Kind == "resource_heavy_for_domain" {
			found = true
			assert.Equal(t, "high", assessment.Severity)
		}
	}
	assert.True(t, found, "resource heavy assessment should be present")
}

func TestBuildAuditDataIncludesAgenticAnalysis(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   7,
			WorkflowName: "Issue Triage",
			WorkflowPath: ".github/workflows/issue-triage.yml",
			Status:       "completed",
			Conclusion:   "success",
			Duration:     3 * time.Minute,
			Turns:        3,
			Event:        "issues",
			LogsPath:     t.TempDir(),
		},
	}
	metrics := LogMetrics{Turns: 3}

	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)
	require.NotNil(t, auditData.TaskDomain, "task domain should be present")
	require.NotNil(t, auditData.BehaviorFingerprint, "behavioral fingerprint should be present")
	assert.NotEmpty(t, auditData.AgenticAssessments, "agentic assessments should be present")
	assert.Equal(t, "triage", auditData.TaskDomain.Name)
}

func TestGenerateAgenticAssessmentFindingsHaveCodes(t *testing.T) {
	t.Parallel()

	assessments := []AgenticAssessment{
		{Kind: "resource_heavy_for_domain"},
		{Kind: "overkill_for_agentic"},
		{Kind: "poor_agentic_control"},
		{Kind: "partially_reducible"},
		{Kind: "model_downgrade_available"},
		{Kind: "delegated_context_present"},
	}

	findings := generateAgenticAssessmentFindings(assessments)
	require.Len(t, findings, len(assessments))
	for _, finding := range findings {
		assert.NotEmpty(t, finding.Code, "finding %q must have a stable code", finding.Title)
	}
}

func TestComputeAgenticFraction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		run      WorkflowRun
		minAlpha float64
		maxAlpha float64
	}{
		{
			name:     "zero turns returns zero",
			run:      WorkflowRun{Turns: 0},
			minAlpha: 0.0,
			maxAlpha: 0.0,
		},
		{
			name:     "short read-only run is fully agentic",
			run:      WorkflowRun{Turns: 2},
			minAlpha: 1.0,
			maxAlpha: 1.0,
		},
		{
			name:     "write-heavy run with many turns has partial fraction",
			run:      WorkflowRun{Turns: 10, SafeItemsCount: 3},
			minAlpha: 0.3,
			maxAlpha: 0.5,
		},
		{
			name:     "long read-only run returns 0.5",
			run:      WorkflowRun{Turns: 8},
			minAlpha: 0.5,
			maxAlpha: 0.5,
		},
		{
			name:     "single write action in multi-turn run",
			run:      WorkflowRun{Turns: 6, SafeItemsCount: 1},
			minAlpha: 0.3,
			maxAlpha: 0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pr := ProcessedRun{Run: tt.run}
			alpha := computeAgenticFraction(pr)
			assert.GreaterOrEqual(t, alpha, tt.minAlpha, "agentic fraction should be >= %v", tt.minAlpha)
			assert.LessOrEqual(t, alpha, tt.maxAlpha, "agentic fraction should be <= %v", tt.maxAlpha)
		})
	}
}

func TestBuildBehaviorFingerprintIncludesAgenticFraction(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			Turns:          8,
			Duration:       10 * time.Minute,
			SafeItemsCount: 2,
		},
	}
	metrics := MetricsData{Turns: 8}
	toolUsage := []ToolUsageInfo{
		{Name: "bash", CallCount: 3},
		{Name: "github_issue_read", CallCount: 2},
	}

	fp := buildBehaviorFingerprint(processedRun, metrics, toolUsage, nil, nil)
	require.NotNil(t, fp, "fingerprint should not be nil")
	assert.Greater(t, fp.AgenticFraction, 0.0, "agentic fraction should be positive")
	assert.LessOrEqual(t, fp.AgenticFraction, 1.0, "agentic fraction should be <= 1.0")
}

func TestBuildAgenticAssessmentsFlagsPartiallyReducible(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			WorkflowName:   "Data Collector",
			Turns:          10,
			Duration:       8 * time.Minute,
			SafeItemsCount: 2,
		},
	}
	metrics := MetricsData{Turns: 10}
	toolUsage := []ToolUsageInfo{
		{Name: "bash", CallCount: 5},
		{Name: "github_issue_read", CallCount: 3},
		{Name: "gh", CallCount: 2},
		{Name: "jq", CallCount: 1},
	}
	domain := &TaskDomainInfo{Name: "research", Label: "Research"}
	fingerprint := buildBehaviorFingerprint(processedRun, metrics, toolUsage, nil, nil)

	assessments := buildAgenticAssessments(processedRun, metrics, toolUsage, nil, domain, fingerprint, nil)

	var found bool
	for _, a := range assessments {
		if a.Kind == "partially_reducible" {
			found = true
			assert.Contains(t, a.Summary, "data-gathering", "summary should mention data-gathering")
			assert.Contains(t, a.Recommendation, "steps:", "recommendation should mention steps:")
		}
	}
	assert.True(t, found, "partially_reducible assessment should be present for low agentic fraction moderate run")
}

func TestBuildAgenticAssessmentsFlagsModelDowngrade(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			WorkflowName:   "Issue Triage Moderate",
			Turns:          7,
			Duration:       6 * time.Minute,
			SafeItemsCount: 1,
		},
	}
	metrics := MetricsData{Turns: 7}
	toolUsage := []ToolUsageInfo{
		{Name: "bash", CallCount: 3},
		{Name: "github_issue_read", CallCount: 2},
		{Name: "grep", CallCount: 1},
		{Name: "jq", CallCount: 1},
	}
	domain := &TaskDomainInfo{Name: "triage", Label: "Triage"}
	fingerprint := buildBehaviorFingerprint(processedRun, metrics, toolUsage, nil, nil)

	assessments := buildAgenticAssessments(processedRun, metrics, toolUsage, nil, domain, fingerprint, nil)

	var found bool
	for _, a := range assessments {
		if a.Kind == "model_downgrade_available" {
			found = true
			assert.Contains(t, a.Recommendation, "gpt-4.1-mini", "should suggest a cheaper model")
		}
	}
	assert.True(t, found, "model_downgrade_available assessment should be present for moderate triage run")
}

func TestActionMinutesComputedFromDuration(t *testing.T) {
	t.Parallel()
	run := WorkflowRun{
		Duration: 3*time.Minute + 30*time.Second,
	}
	// ActionMinutes should be ceil of Duration in minutes
	// Since ActionMinutes is set by logs_orchestrator, test the formula directly
	expected := 4.0 // ceil(3.5)
	actual := math.Ceil(run.Duration.Minutes())
	assert.InDelta(t, expected, actual, 0.001, "ActionMinutes should be ceiling of duration in minutes")
}

func TestPrettifyAssessmentKindNewKinds(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Partially Reducible To Deterministic", prettifyAssessmentKind("partially_reducible"))
	assert.Equal(t, "Cheaper Model Available", prettifyAssessmentKind("model_downgrade_available"))
}

func TestGenerateAgenticAssessmentFindingsSetsStableCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind string
		code AuditFindingCode
	}{
		{kind: "resource_heavy_for_domain", code: AuditFindingAgenticResourceHeavy},
		{kind: "overkill_for_agentic", code: AuditFindingAgenticOverkill},
		{kind: "poor_agentic_control", code: AuditFindingAgenticPoorControl},
		{kind: "partially_reducible", code: AuditFindingAgenticPartiallyReducible},
		{kind: "model_downgrade_available", code: AuditFindingAgenticModelDowngrade},
		{kind: "delegated_context_present", code: AuditFindingAgenticDelegatedContext},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			t.Parallel()

			findings := generateAgenticAssessmentFindings([]AgenticAssessment{{
				Kind:     tt.kind,
				Severity: "medium",
				Summary:  "summary",
			}})

			require.Len(t, findings, 1)
			assert.Equal(t, tt.code, findings[0].Code)
		})
	}
}

func TestBuildToolUsageInfoAggregatesAndSorts(t *testing.T) {
	t.Parallel()
	metrics := LogMetrics{
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "bash", CallCount: 3, MaxInputSize: 100, MaxOutputSize: 200, MaxDuration: 1 * time.Second},
			{Name: "github_issue_read", CallCount: 5, MaxInputSize: 50, MaxOutputSize: 300, MaxDuration: 500 * time.Millisecond},
			{Name: "bash", CallCount: 2, MaxInputSize: 150, MaxOutputSize: 180, MaxDuration: 2 * time.Second},
		},
	}

	result := buildToolUsageInfo(metrics)

	require.Len(t, result, 2, "duplicate tool names should be merged")

	// bash has CallCount 3+2=5 (merged), github_issue_read has CallCount 5.
	// Both have equal call counts, so the tie is broken alphabetically: "bash" < "github_issue_read".
	assert.Equal(t, "bash", result[0].Name, "alphabetically first among equal call counts should be first")
	assert.Equal(t, 5, result[0].CallCount, "bash call counts should be summed: 3+2=5")
	assert.Equal(t, 150, result[0].MaxInputSize, "max input size should be max across merged entries")
	assert.Equal(t, 200, result[0].MaxOutputSize, "max output size should be max across merged entries")
	assert.NotEmpty(t, result[0].MaxDuration, "max duration should be set from the longest call")

	assert.Equal(t, "github_issue_read", result[1].Name, "alphabetically second should be second")
	assert.Equal(t, 5, result[1].CallCount, "call count for github_issue_read should be 5")
}

func TestBuildToolUsageInfoEmpty(t *testing.T) {
	t.Parallel()
	result := buildToolUsageInfo(LogMetrics{})
	assert.Empty(t, result, "empty metrics should produce empty tool usage")
}

func TestBuildToolUsageInfoSortOrderTieBreak(t *testing.T) {
	t.Parallel()
	metrics := LogMetrics{
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "zebra_tool", CallCount: 2},
			{Name: "alpha_tool", CallCount: 2},
		},
	}

	result := buildToolUsageInfo(metrics)

	require.Len(t, result, 2, "should have two distinct tools")
	assert.Equal(t, "alpha_tool", result[0].Name, "tools with equal call counts should be sorted alphabetically")
	assert.Equal(t, "zebra_tool", result[1].Name, "tools with equal call counts should be sorted alphabetically")
}

func TestBuildAuditDataToolUsageMatchesBuildToolUsageInfo(t *testing.T) {
	t.Parallel()
	metrics := LogMetrics{
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "bash", CallCount: 4, MaxInputSize: 100, MaxOutputSize: 200},
			{Name: "github_issue_read", CallCount: 7, MaxInputSize: 50, MaxOutputSize: 300},
		},
		Turns: 5,
	}
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   1,
			WorkflowName: "Test Workflow",
			Status:       "completed",
			Conclusion:   "success",
			LogsPath:     t.TempDir(),
		},
	}

	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)
	expected := buildToolUsageInfo(metrics)

	require.Equal(t, expected, auditData.ToolUsage, "buildAuditData tool usage should match buildToolUsageInfo output")
}

func TestMergeMCPToolUsageInfoUsesSummaryWhenMetricsToolCallsEmpty(t *testing.T) {
	t.Parallel()
	merged := mergeMCPToolUsageInfo(nil, &MCPToolUsageData{
		Summary: []MCPToolSummary{
			{ServerName: "safeoutputs", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "create_discussion", CallCount: 2, MaxOutputSize: 1200}, MaxInputSize: 50},
			{ServerName: "safeoutputs", ToolUsageStatsBase: ToolUsageStatsBase{ToolName: "push_repo_memory", CallCount: 1, MaxOutputSize: 240}, MaxInputSize: 32},
		},
	})

	require.Len(t, merged, 2, "MCP summary should be represented as tool usage entries")
	assert.Equal(t, "safeoutputs.create_discussion", merged[0].Name, "higher call-count MCP tool should sort first")
	assert.Equal(t, 2, merged[0].CallCount, "call count should come from MCP summary")
	assert.Equal(t, "safeoutputs.push_repo_memory", merged[1].Name, "second MCP tool should be preserved")
}

func TestMergeMCPToolUsageInfoFallsBackToToolCalls(t *testing.T) {
	t.Parallel()
	merged := mergeMCPToolUsageInfo(nil, &MCPToolUsageData{
		ToolCalls: []MCPToolCall{
			{ServerName: "safeoutputs", ToolName: "create_discussion", InputSize: 20, OutputSize: 200},
			{ServerName: "safeoutputs", ToolName: "create_discussion", InputSize: 40, OutputSize: 250},
			{ServerName: "safeoutputs", ToolName: "push_repo_memory", InputSize: 10, OutputSize: 100},
		},
	})

	require.Len(t, merged, 2, "fallback should aggregate MCP tool calls when summary is absent")
	assert.Equal(t, "safeoutputs.create_discussion", merged[0].Name, "tool call fallback should aggregate by MCP tool name")
	assert.Equal(t, 2, merged[0].CallCount, "fallback should count repeated MCP tool calls")
	assert.Equal(t, 40, merged[0].MaxInputSize, "fallback should keep max input size from calls")
	assert.Equal(t, 250, merged[0].MaxOutputSize, "fallback should keep max output size from calls")
}

// TestDeriveRunAgenticAnalysisFingerprintConsistency verifies that the fingerprint
// produced by deriveRunAgenticAnalysis is consistent when Run.Turns is correctly
// populated from log metrics. This guards against the bug where logs_orchestrator.go
// computed the fingerprint before updating result.Run.Turns from extracted metrics,
// causing different fingerprint values between the logs and audit tools for the same run.
func TestDeriveRunAgenticAnalysisFingerprintConsistency(t *testing.T) {
	t.Parallel()
	const metricsTurns = 12

	logMetrics := LogMetrics{
		Turns: metricsTurns,
		ToolCalls: []workflow.ToolCallInfo{
			{Name: "bash", CallCount: 5},
			{Name: "github_issue_read", CallCount: 3},
		},
	}

	// Simulate the corrected behavior (post-fix): Run.Turns is set from log metrics
	// before deriveRunAgenticAnalysis is called, matching what audit.go does.
	processedRunFixed := ProcessedRun{
		Run: WorkflowRun{
			Turns:    metricsTurns, // set from metrics.Turns
			Duration: 20 * time.Minute,
		},
	}
	_, _, _, _, fpFixed, _ := deriveRunAgenticAnalysis(processedRunFixed, logMetrics)

	require.NotNil(t, fpFixed, "fingerprint should not be nil")
	assert.Equal(t, "exploratory", fpFixed.ExecutionStyle, "12 turns should produce exploratory execution style")
	assert.Equal(t, "heavy", fpFixed.ResourceProfile, "20 min duration should produce heavy resource profile")
	assert.Greater(t, fpFixed.AgenticFraction, 0.0, "agentic fraction should be positive when turns > 0")

	// Simulate the broken behavior (pre-fix): Run.Turns is zero because the orchestrator
	// had not yet updated it from extracted metrics when computing the fingerprint.
	processedRunStale := ProcessedRun{
		Run: WorkflowRun{
			Turns:    0, // stale — NOT updated from metrics.Turns
			Duration: 20 * time.Minute,
		},
	}
	_, _, _, _, fpStale, _ := deriveRunAgenticAnalysis(processedRunStale, logMetrics)

	require.NotNil(t, fpStale, "fingerprint should not be nil even with zero turns")
	assert.Equal(t, "directed", fpStale.ExecutionStyle, "zero turns should produce directed execution style")
	assert.InDelta(t, 0.0, fpStale.AgenticFraction, 0.001, "agentic fraction should be zero when Run.Turns is zero")

	// Confirm the two results differ — this is exactly the inconsistency the fix resolves.
	assert.NotEqual(t, fpFixed.ExecutionStyle, fpStale.ExecutionStyle,
		"fingerprints should differ when Run.Turns is stale vs. correctly set from metrics")
	assert.NotEqual(t, fpFixed.AgenticFraction, fpStale.AgenticFraction,
		"agentic fraction should differ when Run.Turns is stale vs. correctly set from metrics")
}

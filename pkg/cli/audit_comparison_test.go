//go:build !integration

package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAuditComparison_NoBaseline(t *testing.T) {
	t.Parallel()
	comparison := buildAuditComparison("success", auditComparisonSnapshot{Turns: 4, Posture: "read_only"}, nil, nil)
	require.NotNil(t, comparison, "comparison should still be returned when no baseline exists")
	assert.False(t, comparison.BaselineFound, "baseline should be marked unavailable")
	assert.Nil(t, comparison.Baseline, "baseline details should be omitted")
	assert.Nil(t, comparison.Delta, "delta should be omitted when no baseline exists")
	assert.Nil(t, comparison.Classification, "classification should be omitted when no baseline exists")
}

func TestBuildAuditComparison_RiskyChange(t *testing.T) {
	t.Parallel()
	baselineRun := &WorkflowRun{
		DatabaseID:   100,
		WorkflowName: "triage",
		Conclusion:   "success",
		CreatedAt:    time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
	}

	comparison := buildAuditComparison(
		"success",
		auditComparisonSnapshot{Turns: 11, Posture: "write_capable", BlockedRequests: 7, MCPFailures: []string{"github"}},
		baselineRun,
		&auditComparisonSnapshot{Turns: 4, Posture: "read_only", BlockedRequests: 0},
	)

	require.NotNil(t, comparison, "comparison should be built")
	require.True(t, comparison.BaselineFound, "baseline should be marked available")
	require.NotNil(t, comparison.Delta, "delta should be present")
	require.NotNil(t, comparison.Classification, "classification should be present")
	require.NotNil(t, comparison.Recommendation, "recommendation should be present")

	assert.Equal(t, "risky", comparison.Classification.Label, "write-capable transition should be risky")
	assert.Contains(t, comparison.Classification.ReasonCodes, "turns_increase")
	assert.Contains(t, comparison.Classification.ReasonCodes, "posture_changed")
	assert.Contains(t, comparison.Classification.ReasonCodes, "blocked_requests_increase")
	assert.Contains(t, comparison.Classification.ReasonCodes, "new_mcp_failure")
	assert.Equal(t, 4, comparison.Delta.Turns.Before)
	assert.Equal(t, 11, comparison.Delta.Turns.After)
	assert.Equal(t, "read_only", comparison.Delta.Posture.Before)
	assert.Equal(t, "write_capable", comparison.Delta.Posture.After)
	assert.True(t, comparison.Delta.MCPFailure.NewlyPresent, "new MCP failure should be marked")
	assert.Contains(t, comparison.Recommendation.Action, "write-capable", "recommendation should address the risky posture change")
}

func TestBuildAuditComparison_StableRun(t *testing.T) {
	t.Parallel()
	baselineRun := &WorkflowRun{DatabaseID: 99, WorkflowName: "triage", Conclusion: "success", CreatedAt: time.Now().Add(-time.Hour)}
	comparison := buildAuditComparison(
		"success",
		auditComparisonSnapshot{Turns: 4, Posture: "read_only", BlockedRequests: 0},
		baselineRun,
		&auditComparisonSnapshot{Turns: 4, Posture: "read_only", BlockedRequests: 0},
	)

	require.NotNil(t, comparison.Classification, "classification should be present")
	assert.Equal(t, "stable", comparison.Classification.Label, "unchanged runs should be stable")
	assert.Empty(t, comparison.Classification.ReasonCodes, "stable runs should have no reason codes")
	assert.Contains(t, comparison.Recommendation.Action, "No action needed", "stable runs should produce a no-op recommendation")
}

func TestBuildAuditComparison_FailedRunIsRisky(t *testing.T) {
	t.Parallel()
	baselineRun := &WorkflowRun{DatabaseID: 101, WorkflowName: "triage", Conclusion: "success", CreatedAt: time.Now().Add(-time.Hour)}
	comparison := buildAuditComparison(
		"failure",
		auditComparisonSnapshot{Turns: 4, Posture: "read_only", BlockedRequests: 0},
		baselineRun,
		&auditComparisonSnapshot{Turns: 4, Posture: "read_only", BlockedRequests: 0},
	)

	require.NotNil(t, comparison.Classification, "classification should be present")
	require.NotNil(t, comparison.Recommendation, "recommendation should be present")
	assert.Equal(t, "risky", comparison.Classification.Label, "failed runs should not be classified as stable")
	assert.Contains(t, comparison.Classification.ReasonCodes, "run_unsuccessful", "failed runs should record their non-success conclusion")
	assert.Contains(t, comparison.Recommendation.Action, "Investigate failure", "recommendation should direct the reader to investigate the failure")
	assert.NotContains(t, comparison.Recommendation.Action, "No action needed", "failed runs should not get a no-op recommendation")
}

func TestSelectAuditComparisonBaselinePrefersCohortMatchOverRecency(t *testing.T) {
	t.Parallel()
	current := ProcessedRun{
		Run: WorkflowRun{
			Event: "issues",
		},
		TaskDomain: &TaskDomainInfo{Name: "triage", Label: "Triage"},
		BehaviorFingerprint: &BehaviorFingerprint{
			ExecutionStyle:  "directed",
			ToolBreadth:     "narrow",
			ActuationStyle:  "read_only",
			ResourceProfile: "lean",
			DispatchMode:    "standalone",
		},
	}

	candidates := []auditComparisonCandidate{
		{
			Run: WorkflowRun{
				DatabaseID: 200,
				CreatedAt:  time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC),
				Event:      "push",
			},
			TaskDomain: &TaskDomainInfo{Name: "release_ops", Label: "Release / Ops"},
			BehaviorFingerprint: &BehaviorFingerprint{
				ExecutionStyle:  "adaptive",
				ToolBreadth:     "moderate",
				ActuationStyle:  "selective_write",
				ResourceProfile: "moderate",
				DispatchMode:    "standalone",
			},
		},
		{
			Run: WorkflowRun{
				DatabaseID: 150,
				CreatedAt:  time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC),
				Event:      "issues",
			},
			TaskDomain: &TaskDomainInfo{Name: "triage", Label: "Triage"},
			BehaviorFingerprint: &BehaviorFingerprint{
				ExecutionStyle:  "directed",
				ToolBreadth:     "narrow",
				ActuationStyle:  "read_only",
				ResourceProfile: "lean",
				DispatchMode:    "standalone",
			},
		},
	}

	selected := selectAuditComparisonBaseline(current, candidates)
	require.NotNil(t, selected, "baseline should be selected")
	assert.Equal(t, int64(150), selected.Run.DatabaseID, "cohort-matching run should beat the more recent but behaviorally different run")
	assert.Equal(t, "cohort_match", selected.Selection)
	assert.Contains(t, selected.MatchedOn, "task_domain")
	assert.Contains(t, selected.MatchedOn, "resource_profile")
	assert.Positive(t, selected.Score, "cohort match should have a positive score")
}

func TestScoreAuditComparisonCandidateFallsBackToLatestSuccess(t *testing.T) {
	t.Parallel()
	current := ProcessedRun{Run: WorkflowRun{Event: "issues"}}
	candidate := auditComparisonCandidate{
		Run: WorkflowRun{DatabaseID: 300, CreatedAt: time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC), Event: "push"},
	}

	scoreAuditComparisonCandidate(current, &candidate)

	assert.Equal(t, 0, candidate.Score)
	assert.Equal(t, "latest_success", candidate.Selection)
	assert.Nil(t, candidate.MatchedOn)
}

func TestRecommendAuditComparisonAction(t *testing.T) {
	t.Parallel()

	newlyPresentMCPFailure := &AuditComparisonMCPFailureDelta{NewlyPresent: true}

	tests := []struct {
		name              string
		label             string
		currentConclusion string
		delta             *AuditComparisonDelta
		expectedContains  string
	}{
		{
			name:              "non-success failure conclusion",
			label:             "changed",
			currentConclusion: "failure",
			delta:             &AuditComparisonDelta{},
			expectedContains:  "Investigate failure; run concluded with errors",
		},
		{
			name:              "non-success non-failure conclusion",
			label:             "changed",
			currentConclusion: "action_required",
			delta:             &AuditComparisonDelta{},
			expectedContains:  "Investigate the action required conclusion",
		},
		{
			name:              "nil delta with success conclusion",
			label:             "changed",
			currentConclusion: "success",
			delta:             nil,
			expectedContains:  "No action needed",
		},
		{
			name:              "stable label with success conclusion",
			label:             "stable",
			currentConclusion: "success",
			delta:             &AuditComparisonDelta{},
			expectedContains:  "No action needed",
		},
		{
			name:              "empty conclusion treated as success-equivalent, stable label",
			label:             "stable",
			currentConclusion: "",
			delta:             &AuditComparisonDelta{},
			expectedContains:  "No action needed",
		},
		{
			name:              "posture read_only to write_capable",
			label:             "risky",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				Posture: AuditComparisonStringDelta{Before: "read_only", After: "write_capable"},
			},
			expectedContains: "Review first-time write-capable behavior",
		},
		{
			name:              "newly present MCP failure",
			label:             "risky",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				MCPFailure: newlyPresentMCPFailure,
			},
			expectedContains: "Inspect the new MCP failure",
		},
		{
			name:              "MCP failure present but not newly present falls through",
			label:             "changed",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				MCPFailure: &AuditComparisonMCPFailureDelta{NewlyPresent: false},
			},
			expectedContains: "Review the behavior change",
		},
		{
			name:              "blocked requests increased",
			label:             "risky",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				BlockedRequests: AuditComparisonIntDelta{Before: 1, After: 3},
			},
			expectedContains: "Review network policy changes",
		},
		{
			name:              "turns increased",
			label:             "changed",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				Turns: AuditComparisonIntDelta{Before: 2, After: 5},
			},
			expectedContains: "Compare prompt or task-shape changes",
		},
		{
			name:              "routing change",
			label:             "changed",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				ModelRouting: &AuditComparisonRouteDelta{Changed: true},
			},
			expectedContains: "Review the model-routing change",
		},
		{
			name:              "fallback default review message",
			label:             "changed",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				Turns:           AuditComparisonIntDelta{Before: 5, After: 3},
				BlockedRequests: AuditComparisonIntDelta{Before: 3, After: 1},
			},
			expectedContains: "Review the behavior change against the selected successful baseline",
		},
		{
			name:              "priority order: posture change wins over MCP failure",
			label:             "risky",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				Posture:      AuditComparisonStringDelta{Before: "read_only", After: "write_capable"},
				MCPFailure:   newlyPresentMCPFailure,
				ModelRouting: &AuditComparisonRouteDelta{Changed: true},
			},
			expectedContains: "Review first-time write-capable behavior",
		},
		{
			name:              "priority order: MCP failure wins over blocked requests",
			label:             "risky",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				MCPFailure:      newlyPresentMCPFailure,
				BlockedRequests: AuditComparisonIntDelta{Before: 1, After: 5},
				ModelRouting:    &AuditComparisonRouteDelta{Changed: true},
			},
			expectedContains: "Inspect the new MCP failure",
		},
		{
			name:              "priority order: blocked requests wins over turns",
			label:             "risky",
			currentConclusion: "success",
			delta: &AuditComparisonDelta{
				BlockedRequests: AuditComparisonIntDelta{Before: 1, After: 5},
				Turns:           AuditComparisonIntDelta{Before: 2, After: 8},
				ModelRouting:    &AuditComparisonRouteDelta{Changed: true},
			},
			expectedContains: "Review network policy changes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := recommendAuditComparisonAction(tt.label, tt.currentConclusion, tt.delta)
			assert.Contains(t, result, tt.expectedContains)
		})
	}
}

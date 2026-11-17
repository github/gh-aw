//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOutcomeSummaryAICPerAcceptedOutcome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		accepted int
		aic      float64
		expected float64
	}{
		{"accepted share of full run cost", 2, 12, 6},
		{"no accepted adoption", 0, 12, 0},
		{"missing cost", 2, 0, 0},
		{"invalid cost", 2, -1, 0},
		{"not a number", 2, math.NaN(), 0},
		{"infinite cost", 2, math.Inf(1), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			summary := OutcomeSummary{Accepted: tc.accepted}
			summary.addAIC(tc.aic)
			assert.InDelta(t, tc.expected, summary.CostPerAcceptedOutcome, 1e-9)
			data, err := json.Marshal(summary)
			require.NoError(t, err)
			if tc.expected == 0 {
				assert.NotContains(t, string(data), "cost_per_adopted_output")
			}
		})
	}
}

func TestLoadOutcomeAIC(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		cached    bool
		agent     bool
		detection bool
		expected  float64
	}{
		{"missing usage", false, false, false, 0},
		{"missing usage with agent-only cache", true, false, false, 0},
		{"agent usage", false, true, false, 1.25},
		{"agent and detection usage", false, true, true, 3.75},
		{"agent and detection usage with agent-only cache", true, true, true, 3.75},
		{"detection usage with agent-only cache", true, false, true, 2.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.cached {
				require.NoError(t, saveRunSummary(dir, &RunSummary{
					CLIVersion: GetVersion(),
					RunAnalysis: RunAnalysis{
						TokenUsage: &TokenUsageSummary{TotalAIC: 1.25},
					},
				}, false))
			}
			require.NoError(t, os.Mkdir(filepath.Join(dir, "usage"), 0o700))
			if tc.agent {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "usage", "agent_usage.jsonl"), []byte("{\"aic\":1.25}\n"), 0o600))
			}
			if tc.detection {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "usage", "detection_usage.jsonl"), []byte("{\"usage\":{\"ai_credits\":2.5}}\n"), 0o600))
			}

			aic, err := loadOutcomeAIC(dir)
			require.NoError(t, err)
			assert.InDelta(t, tc.expected, aic, 1e-9)
		})
	}
}

func TestAuditLeavesOutcomeEvaluationToOutcomesCommand(t *testing.T) {
	original := workflowOutcomeGHAPIGet
	t.Cleanup(func() { workflowOutcomeGHAPIGet = original })
	workflowOutcomeGHAPIGet = func(_ context.Context, _, _ string) (map[string]any, error) {
		t.Fatal("audit must not query adoption evidence")
		return nil, nil
	}
	dir := t.TempDir()
	content := "{\"type\":\"dispatch_workflow\",\"repo\":\"acme/repo\",\"metadata\":{\"run_id\":2}}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, safeOutputItemsManifestFilename), []byte(content), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "usage"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "usage", "detection_usage.jsonl"), []byte("{\"aic\":2.5}\n"), 0o600))
	data := buildAuditData(context.Background(), ProcessedRun{
		Run:        WorkflowRun{DatabaseID: 1, LogsPath: dir},
		TokenUsage: &TokenUsageSummary{TotalAIC: 1.25},
	}, LogMetrics{}, nil)
	require.Len(t, data.CreatedItems, 1)
	assert.Empty(t, data.Outcomes)
	assert.Nil(t, data.OutcomeSummary)
	assert.InDelta(t, 1.25, data.Metrics.AIC, 1e-9, "audit metrics remain agent-only")
}

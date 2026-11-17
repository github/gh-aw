//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunOutcomesSupplementaryAIC(t *testing.T) {
	oldGet, oldLabels := outcomeEvidenceGHAPIGet, objectiveMappingGHAPIGetArray
	t.Cleanup(func() {
		outcomeEvidenceGHAPIGet, objectiveMappingGHAPIGetArray = oldGet, oldLabels
	})
	outcomeEvidenceGHAPIGet = func(_ context.Context, _, _ string) (map[string]any, error) {
		return map[string]any{"milestone": map[string]any{"number": float64(2)}}, nil
	}
	objectiveMappingGHAPIGetArray = func(_ context.Context, _, _ string) ([]map[string]any, error) {
		return nil, nil
	}

	for _, tc := range []struct {
		name    string
		usage   string
		cost    float64
		warning string
	}{
		{"full run usage", "full", 1.875, ""},
		{"missing usage", "missing", 0, ""},
		{"malformed agent usage", "malformed", 0, "failed to parse agent usage file"},
		{"unreadable usage stream", "unreadable", 0, "error reading token usage file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			runDir := filepath.Join(dir, "run-42")
			usageDir := filepath.Join(runDir, "usage")
			require.NoError(t, os.MkdirAll(usageDir, 0o700))
			cached := &RunSummary{
				CLIVersion: GetVersion(),
				RunAnalysis: RunAnalysis{
					TokenUsage: &TokenUsageSummary{TotalAIC: 1.25},
				},
			}
			require.NoError(t, saveRunSummary(runDir, cached, false))
			item := "{\"type\":\"assign_milestone\",\"number\":1,\"repo\":\"acme/repo\",\"metadata\":{\"milestone_number\":2},\"timestamp\":\"2026-10-01T00:00:00Z\"}\n"
			manifest := item + strings.Replace(item, `"number":1`, `"number":2`, 1)
			require.NoError(t, os.WriteFile(filepath.Join(runDir, safeOutputItemsManifestFilename), []byte(manifest), 0o600))

			switch tc.usage {
			case "full":
				require.NoError(t, os.WriteFile(filepath.Join(usageDir, "agent_usage.jsonl"), []byte("{\"aic\":1.25}\n"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(usageDir, "detection_usage.jsonl"), []byte("{\"aic\":2.5}\n"), 0o600))
			case "malformed":
				require.NoError(t, os.WriteFile(filepath.Join(runDir, "agent_usage.json"), []byte("{"), 0o600))
			case "unreadable":
				// A directory-backed stream fails to read even when tests run as root.
				require.NoError(t, os.Symlink(usageDir, filepath.Join(usageDir, "agent_usage.jsonl")))
			}

			outcomesDir := filepath.Join(dir, "adoption")
			var runErr error
			var reports []OutcomesData
			stderr := testutil.CaptureStderr(t, func() {
				reports, runErr = collectLogsLifecycles(context.Background(), []ProcessedRun{{Run: WorkflowRun{
					DatabaseID: 42, Attempt: 1, WorkflowPath: ".github/workflows/test.lock.yml", Repository: "acme/repo", LogsPath: runDir,
				}}}, outcomesDir, false, true)
			})
			require.NoError(t, runErr)
			require.Len(t, reports, 1)
			data := reports[0]
			output, err := json.Marshal(data)
			require.NoError(t, err)
			require.Len(t, data.Items, 2)
			assert.Equal(t, 2, data.Summary.Accepted)
			assert.InDelta(t, tc.cost, data.Summary.CostPerAcceptedOutcome, 1e-9)
			if tc.cost == 0 {
				assert.NotContains(t, string(output), "cost_per_adopted_output")
			}
			if tc.warning != "" {
				assert.Contains(t, stderr, "Failed to compute adoption AI Credits; cost per accepted adoption omitted")
				assert.Contains(t, stderr, tc.warning)
			} else {
				assert.NotContains(t, stderr, "Failed to compute adoption AI Credits")
			}

			jsonl, err := os.ReadFile(filepath.Join(outcomesDir, "adoption-42.jsonl"))
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(jsonl)), "\n")
			require.Len(t, lines, 2)
			for _, line := range lines {
				var report map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &report))
				assert.Equal(t, string(OutcomeStatusAccepted), report["adoption_status"])
				assert.InDelta(t, 42, report["run_id"], 0)
				assert.NotEmpty(t, report["item_id"])
				assert.InDelta(t, 2, report["run_item_count"], 0)
			}
			assert.NotEqual(t, data.Items[0].ItemID, data.Items[1].ItemID)

			summary, ok := loadRunSummary(runDir, false)
			require.True(t, ok)
			require.NotNil(t, summary.TokenUsage)
			assert.InDelta(t, 1.25, summary.TokenUsage.TotalAIC, 1e-9, "agent-only cached metrics remain unchanged")
		})
	}
}

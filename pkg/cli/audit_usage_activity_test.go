//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyUsageSummaryToAuditResultsBackfillsMCPPayloadMetrics(t *testing.T) {
	t.Parallel()

	results := auditAnalysisResults{}
	applyUsageSummaryToAuditResults(&usageActivitySummary{
		Gateway: &usageActivityGateway{
			Tools: []usageActivityGatewayTool{{
				ServerName:      "github",
				ToolName:        "issue_read",
				CallCount:       2,
				TotalInputSize:  300,
				TotalOutputSize: 900,
				AvgInputSize:    150,
				AvgOutputSize:   450,
				MaxInputSize:    200,
				MaxOutputSize:   600,
			}},
			Servers: []usageActivityGatewayServer{{
				ServerName:      "github",
				ToolCallCount:   2,
				TotalInputSize:  300,
				TotalOutputSize: 900,
				AvgInputSize:    150,
				AvgOutputSize:   450,
				MaxInputSize:    200,
				MaxOutputSize:   600,
			}},
		},
	}, &results)

	require.NotNil(t, results.mcpToolUsage)
	require.Len(t, results.mcpToolUsage.Summary, 1)
	assert.Equal(t, 150, results.mcpToolUsage.Summary[0].AvgInputSize)
	assert.Equal(t, 450, results.mcpToolUsage.Summary[0].AvgOutputSize)
	assert.Equal(t, 600, results.mcpToolUsage.Summary[0].MaxOutputSize)
	require.Len(t, results.mcpToolUsage.Servers, 1)
	assert.Equal(t, 200, results.mcpToolUsage.Servers[0].MaxInputSize)
}

func TestAuditLedgerActivityFromUsageSummary(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	activityDir := filepath.Join(runDir, "usage", "activity")
	require.NoError(t, os.MkdirAll(activityDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(activityDir, "summary.json"), []byte(`{"schema":"usage-activity-summary/v1","ledger":{"transactions_added":3}}`), 0o600))

	summary, err := loadUsageActivitySummary(runDir)
	require.NoError(t, err)
	results := auditAnalysisResults{}
	applyUsageSummaryToAuditResults(summary, &results)
	run := WorkflowRun{DatabaseID: 42}
	processed := buildProcessedAuditRun(run, results)
	require.Equal(t, 3, processed.Ledger.TransactionsAdded)
	require.Equal(t, 3, buildAuditRunSummary(run, processed, results).Ledger.TransactionsAdded)

	data := buildAuditData(context.Background(), processed, workflow.LogMetrics{}, nil)
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"ledger":{"transactions_added":3}`)
	assert.Contains(t, captureFrictionStderr(t, func() { renderConsole(data, runDir) }), "ledger: transactions_added=3")
}

func TestAuditLedgerActivityMissingVersusZero(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		ledger *LedgerActivity
		want   string
	}{
		{name: "missing", want: `"ledger"`},
		{name: "zero", ledger: &LedgerActivity{}, want: `"ledger":{"transactions_added":0}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			results := auditAnalysisResults{}
			applyUsageSummaryToAuditResults(&usageActivitySummary{Ledger: test.ledger}, &results)
			raw, err := json.Marshal(buildAuditRunSummary(WorkflowRun{}, buildProcessedAuditRun(WorkflowRun{}, results), results))
			require.NoError(t, err)
			if test.ledger == nil {
				assert.NotContains(t, string(raw), test.want)
			} else {
				assert.Contains(t, string(raw), test.want)
			}
		})
	}
}

func TestCachedAuditBackfillsLedgerActivity(t *testing.T) {
	runDir := t.TempDir()
	activityDir := filepath.Join(runDir, "usage", "activity")
	require.NoError(t, os.MkdirAll(activityDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(activityDir, "summary.json"), []byte(`{"schema":"usage-activity-summary/v1","ledger":{"transactions_added":2}}`), 0o600))
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}
	require.NoError(t, writeAuditData(runDir, AuditData{
		CacheSource: auditCacheSourceFull,
		Overview:    buildAuditOverview(run, nil),
	}))

	processed := processedRunFromSummary(&RunSummary{RunAnalysis: RunAnalysis{Run: run}}, runDir)
	require.NotNil(t, processed.Ledger)
	stdout, _ := captureOutput(t, func() error {
		return renderAuditReport(context.Background(), processed, LogMetrics{}, nil, AuditOptions{
			OutputDir:  runDir,
			JSONOutput: true,
		})
	})
	assert.Contains(t, stdout, `"ledger": {`)
	assert.Contains(t, stdout, `"transactions_added": 2`)
}

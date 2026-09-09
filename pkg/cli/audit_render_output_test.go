//go:build !integration

package cli

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderAuditOutputConsole(t *testing.T) {
	auditData := AuditData{Overview: OverviewData{RunID: 4242, WorkflowName: "Test Workflow"}}

	var err error
	output := testutil.CaptureStderr(t, func() {
		err = renderAuditOutput(auditData, t.TempDir(), false, false)
	})
	require.NoError(t, err)
	assert.Contains(t, output, "Test Workflow")
}

func TestRenderAuditReportReusesCompleteCache(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}
	require.NoError(t, writeAuditData(runDir, AuditData{
		CacheSource: auditCacheSourceFull,
		Overview:    buildAuditOverview(run, nil),
		KeyFindings: []AuditFinding{{Title: "cache marker"}},
	}))

	stdout, _ := captureOutput(t, func() error {
		return renderAuditReport(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, AuditOptions{
			OutputDir:  runDir,
			JSONOutput: true,
		})
	})
	assert.Contains(t, stdout, "cache marker")
}

func TestRenderConsoleTokenUsageWarnings(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderConsoleTokenUsage(&TokenUsageSummary{
			TotalRequests: 1,
			Warnings:      []string{"fallback accounting was used"},
		})
	})

	assert.Contains(t, output, "token_usage_warnings:")
	assert.Contains(t, output, "fallback accounting was used")
}

func TestRenderAuditCompletion(t *testing.T) {
	outputDir := t.TempDir()

	t.Run("json output suppresses completion message", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			renderAuditCompletion(outputDir, true)
		})
		assert.Empty(t, output)
	})

	t.Run("console output reports the log directory", func(t *testing.T) {
		output := testutil.CaptureStderr(t, func() {
			renderAuditCompletion(outputDir, false)
		})
		assert.Contains(t, output, "Audit complete")
		assert.Contains(t, output, outputDir)
	})
}

func TestParseAuditLogsIfRequestedSkipsWhenParseDisabled(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		parseAuditLogsIfRequested(1, t.TempDir(), AuditOptions{Parse: false})
	})
	assert.Empty(t, output)
}

func TestParseAgentLogIfRequestedWithoutEngine(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		parseAgentLogIfRequested(1, t.TempDir(), true)
	})
	assert.Contains(t, output, "No engine detected")
}

func TestRenderAuditGatewayMetricsWithoutLogs(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderAuditGatewayMetrics(t.TempDir(), false)
	})
	assert.Empty(t, output)
}

func TestRenderAuditUnifiedTimelineWithoutEvents(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderAuditUnifiedTimeline(t.TempDir(), false)
	})
	assert.Empty(t, output)
}

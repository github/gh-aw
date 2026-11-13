//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestRenderAuditReportSetsSchemaVersionOnFreshJSONOutput(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}

	stdout, _ := captureOutput(t, func() error {
		return renderAuditReport(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, AuditOptions{
			OutputDir:  runDir,
			JSONOutput: true,
		})
	})
	var auditData AuditData
	require.NoError(t, json.Unmarshal([]byte(stdout), &auditData))
	assert.Equal(t, auditSchemaVersion, auditData.SchemaVersion)
}

func TestBuildRenderedAuditDataSkipsBaseline(t *testing.T) {
	runDir := t.TempDir()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   42,
			WorkflowPath: ".github/workflows/test.lock.yml",
		},
	}

	auditData, err := buildRenderedAuditData(context.Background(), processedRun, LogMetrics{}, nil, runDir, AuditOptions{
		NoBaseline: true,
	})
	require.NoError(t, err)

	require.NotNil(t, auditData.Comparison)
	assert.False(t, auditData.Comparison.BaselineFound)
}

func TestBuildRenderedAuditDataFromCacheSkipsBaseline(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{
		DatabaseID:   42,
		WorkflowPath: ".github/workflows/test.lock.yml",
		LogsPath:     runDir,
	}
	require.NoError(t, writeAuditData(runDir, AuditData{
		CacheSource: auditCacheSourceLogs,
		Overview:    buildAuditOverview(run, nil),
	}))

	auditData, err := buildRenderedAuditDataFromCache(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, runDir, AuditOptions{
		NoBaseline: true,
	})
	require.NoError(t, err)

	require.NotNil(t, auditData.Comparison)
	assert.False(t, auditData.Comparison.BaselineFound)
}

func TestRenderAuditReportDropsCachedComparisonWithNoBaseline(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}
	require.NoError(t, writeAuditData(runDir, AuditData{
		CacheSource: auditCacheSourceFull,
		Overview:    buildAuditOverview(run, nil),
		Comparison:  &AuditComparisonData{BaselineFound: true, Baseline: &AuditComparisonBaseline{RunID: 41}},
	}))

	stdout, _ := captureOutput(t, func() error {
		return renderAuditReport(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, AuditOptions{
			OutputDir:  runDir,
			JSONOutput: true,
			NoBaseline: true,
		})
	})

	var auditData AuditData
	require.NoError(t, json.Unmarshal([]byte(stdout), &auditData))
	require.NotNil(t, auditData.Comparison)
	assert.False(t, auditData.Comparison.BaselineFound)
	assert.Nil(t, auditData.Comparison.Baseline)

	// The cached entry must stay intact for later default audits.
	cached, ok := loadCachedAuditData(runDir, run, auditCacheSourceFull)
	require.True(t, ok)
	require.NotNil(t, cached.Comparison)
	assert.True(t, cached.Comparison.BaselineFound)
}

func TestRenderAuditReportDoesNotCacheNoBaselineResult(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}

	_, _ = captureOutput(t, func() error {
		return renderAuditReport(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, AuditOptions{
			OutputDir:  runDir,
			JSONOutput: true,
			NoBaseline: true,
		})
	})

	_, ok := loadCachedAuditData(runDir, run, auditCacheSourceFull)
	assert.False(t, ok, "opt-out result must not be persisted as a full cache entry")
}

func TestRenderAuditReportGroupParsesLogs(t *testing.T) {
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success", LogsPath: runDir}

	_, stderr := captureOutput(t, func() error {
		return renderAuditReport(context.Background(), ProcessedRun{Run: run}, LogMetrics{}, nil, AuditOptions{
			OutputDir: runDir,
			Verbose:   true,
			Parse:     true,
			Group:     true,
		})
	})

	assert.Contains(t, stderr, "No engine detected")
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

func TestRenderConsoleGatewaySteeringEvents(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderConsoleGatewaySteeringEvents([]GatewaySteeringEvent{{
			Type:      tokenSteeringEventName,
			Message:   "[AWF TOKEN WARNING] You are running out of AI Credits.",
			Timestamp: "2026-09-23T12:00:00Z",
		}})
	})

	assert.Contains(t, output, "gateway_steering_events:")
	assert.Contains(t, output, tokenSteeringEventName)
	assert.Contains(t, output, "running out of AI Credits")
	assert.Contains(t, output, "2026-09-23T12:00:00Z")
}

func TestRenderConsoleGatewaySteeringEventsEscapesControlChars(t *testing.T) {
	output := testutil.CaptureStderr(t, func() {
		renderConsoleGatewaySteeringEvents([]GatewaySteeringEvent{{
			Type:         tokenSteeringEventName + "\x1b[2J",
			Message:      "budget \x1b]52;c;aGk=\a low \x1b[31mred\x1b[0m",
			Conversation: "conv-\x1b[1m",
			Timestamp:    "2026-09-23T12:00:00Z\x1b[K",
		}})
	})

	assert.NotContains(t, output, "\x1b")
	assert.NotContains(t, output, "\a")
	assert.Contains(t, output, `low '\x1b'[31mred'\x1b'[0m`)
	assert.Contains(t, output, `conversation=conv-'\x1b'[1m`)
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

func TestReportParseOutcome(t *testing.T) {
	tests := []struct {
		name           string
		warnLabel      string
		successLabel   string
		outputFileName string
		err            error
		verbose        bool
		createOutput   bool
		wantOutput     string
	}{
		{
			name:           "agent success uses distinct success label",
			warnLabel:      "agent log",
			successLabel:   "log",
			outputFileName: "log.md",
			createOutput:   true,
			wantOutput:     "Parsed log for run 42 →",
		},
		{
			name:           "firewall success",
			warnLabel:      "firewall logs",
			successLabel:   "firewall logs",
			outputFileName: "firewall.md",
			createOutput:   true,
			wantOutput:     "Parsed firewall logs for run 42 →",
		},
		{
			name:           "missing output file is silent",
			warnLabel:      "firewall logs",
			successLabel:   "firewall logs",
			outputFileName: "firewall.md",
		},
		{
			name:           "verbose error uses warning label",
			warnLabel:      "agent log",
			successLabel:   "log",
			outputFileName: "log.md",
			err:            errors.New("parse failed"),
			verbose:        true,
			wantOutput:     "Failed to parse agent log for run 42: parse failed",
		},
		{
			name:           "nonverbose error is silent",
			warnLabel:      "firewall logs",
			successLabel:   "firewall logs",
			outputFileName: "firewall.md",
			err:            errors.New("parse failed"),
			wantOutput:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runOutputDir := t.TempDir()
			if tt.createOutput {
				require.NoError(t, os.WriteFile(filepath.Join(runOutputDir, tt.outputFileName), nil, 0o600))
			}

			output := testutil.CaptureStderr(t, func() {
				reportParseOutcome(42, runOutputDir, tt.warnLabel, tt.successLabel, tt.outputFileName, tt.verbose, tt.err)
			})

			if tt.wantOutput == "" {
				assert.Empty(t, output)
			} else {
				assert.Contains(t, output, tt.wantOutput)
				if tt.createOutput {
					assert.Contains(t, output, filepath.Join(runOutputDir, tt.outputFileName))
				}
			}
		})
	}
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

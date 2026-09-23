//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsPermissionError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "Nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "Authentication required error",
			err:      errors.New("authentication required"),
			expected: true,
		},
		{
			name:     "Exit status 4 error",
			err:      errors.New("exit status 4"),
			expected: true,
		},
		{
			name:     "GitHub CLI authentication error",
			err:      errors.New("GitHub CLI authentication required"),
			expected: true,
		},
		{
			name:     "Permission denied error",
			err:      errors.New("permission denied"),
			expected: true,
		},
		{
			name:     "GH_TOKEN error",
			err:      errors.New("GH_TOKEN environment variable not set"),
			expected: true,
		},
		{
			name:     "Not logged into any GitHub hosts error",
			err:      errors.New("not logged into any GitHub hosts"),
			expected: true,
		},
		{
			name:     "GitHub Actions workflow auth error",
			err:      errors.New("To use GitHub CLI in a GitHub Actions workflow, set the GH_TOKEN environment variable"),
			expected: true,
		},
		{
			name:     "gh auth login message",
			err:      errors.New("run gh auth login to authenticate"),
			expected: true,
		},
		{
			name:     "Other error",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPermissionError(tt.err)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsPermissionErrorStr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		s        string
		expected bool
	}{
		{
			name:     "Combined message with exit status 4",
			s:        "exit status 4 not logged into any GitHub hosts",
			expected: true,
		},
		{
			name:     "Output only contains gh auth login",
			s:        "Run gh auth login to proceed",
			expected: true,
		},
		{
			name:     "GitHub CLI authentication marker",
			s:        "GitHub CLI authentication token is missing",
			expected: true,
		},
		{
			name:     "Empty string",
			s:        "",
			expected: false,
		},
		{
			name:     "Unrelated combined message",
			s:        "exit status 1 unknown field",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPermissionErrorStr(tt.s)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestProcessedRunFromSummaryBackfillsTurnsFromMetrics(t *testing.T) {
	t.Parallel()
	summary := &RunSummary{RunAnalysis: RunAnalysis{
		Run:     WorkflowRun{DatabaseID: 123, Turns: 0},
		Metrics: LogMetrics{Turns: 34},
	},
	}

	processed := processedRunFromSummary(summary, "/tmp/run-output")

	assert.Equal(t, 34, processed.Run.Turns, "run turns should backfill from summary metrics when run turns are missing")
	assert.Equal(t, "/tmp/run-output", processed.Run.LogsPath, "logs path should be set from the current run output directory")
}

func TestProcessedRunFromSummaryPreservesExistingTurns(t *testing.T) {
	t.Parallel()
	summary := &RunSummary{RunAnalysis: RunAnalysis{
		Run:     WorkflowRun{DatabaseID: 456, Turns: 7},
		Metrics: LogMetrics{Turns: 34},
	},
	}

	processed := processedRunFromSummary(summary, "/tmp/run-output")

	assert.Equal(t, 7, processed.Run.Turns, "existing run turns should not be overwritten by summary metrics")
}

func TestProcessedRunFromSummaryBothTurnsZero(t *testing.T) {
	t.Parallel()
	summary := &RunSummary{RunAnalysis: RunAnalysis{
		Run:     WorkflowRun{DatabaseID: 789, Turns: 0},
		Metrics: LogMetrics{Turns: 0},
	},
	}

	processed := processedRunFromSummary(summary, "/tmp/run-output")

	assert.Equal(t, 0, processed.Run.Turns, "run turns should remain zero when neither Run.Turns nor Metrics.Turns is available")
}

func TestProcessedRunFromSummaryBackfillsGatewaySteeringEvents(t *testing.T) {
	t.Parallel()
	runOutputDir := testutil.TempDir(t, "processed-run-summary-steering")
	logsDir := filepath.Join(runOutputDir, "sandbox", "firewall", "audit", "api-proxy-logs")
	require.NoError(t, os.MkdirAll(logsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(
		`{"timestamp":"2026-09-23T12:00:00Z","event":"token_steering","message":"[AWF TOKEN WARNING] You are running out of AI Credits."}`+"\n",
	), 0o644))
	summary := &RunSummary{RunAnalysis: RunAnalysis{
		Run: WorkflowRun{DatabaseID: 790},
	}}

	processed := processedRunFromSummary(summary, runOutputDir)

	require.Len(t, processed.GatewaySteeringEvents, 1)
	assert.Equal(t, tokenSteeringEventName, processed.GatewaySteeringEvents[0].Type)
}

func TestBuildAuditData(t *testing.T) {
	t.Parallel()
	// Create test data
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
		ErrorCount:   2,
		WarningCount: 1,
		LogsPath:     testutil.TempDir(t, "test-*"),
	}

	metrics := LogMetrics{
		TokenUsage: 1500,
		Turns:      5,
		ToolCalls: []workflow.ToolCallInfo{
			{
				Name:          "github_issue_read",
				CallCount:     3,
				MaxInputSize:  256,
				MaxOutputSize: 1024,
				MaxDuration:   2 * time.Second,
			},
		},
	}

	missingTools := []MissingToolReport{
		{
			Tool:         "missing_tool",
			Reason:       "Tool not available",
			Alternatives: "use alternative_tool instead",
			ReportProvenance: ReportProvenance{
				Timestamp: "2024-01-01T10:00:00Z",
			},
		},
	}

	mcpFailures := []MCPFailureReport{
		{
			ServerName: "test-server",
			Status:     "failed",
		},
	}

	processedRun := ProcessedRun{
		Run:          run,
		MissingTools: missingTools,
		MCPFailures:  mcpFailures,
	}
	require.NoError(t, os.WriteFile(filepath.Join(run.LogsPath, safeOutputItemsManifestFilename), []byte("{\"type\":\"create_issue\",\"repo\":\"github/gh-aw\",\"number\":17,\"temporaryId\":\"aw_alpha\",\"timestamp\":\"2024-01-01T10:00:00Z\"}\n{\"type\":\"add_comment\",\"repo\":\"github/gh-aw\",\"number\":17,\"timestamp\":\"2024-01-01T10:01:00Z\"}\n"), 0o600), "should write safe output manifest")
	require.NoError(t, os.WriteFile(filepath.Join(run.LogsPath, constants.TemporaryIdMapFilename.String()), []byte("{\"aw_alpha\":{\"repo\":\"github/gh-aw\",\"number\":17}}"), 0o600), "should write temporary ID map")

	// Build audit data
	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)
	auditData.Comparison = &AuditComparisonData{BaselineFound: false}

	// Verify overview
	if auditData.Overview.RunID != 123456 {
		t.Errorf("Expected run ID 123456, got %d", auditData.Overview.RunID)
	}
	if auditData.Overview.WorkflowName != "Test Workflow" {
		t.Errorf("Expected workflow name 'Test Workflow', got %s", auditData.Overview.WorkflowName)
	}
	if auditData.Overview.Status != "completed" {
		t.Errorf("Expected status 'completed', got %s", auditData.Overview.Status)
	}
	// LogsPath should be set and preserved as-is (absolute path, resolved in AuditWorkflowRun via filepath.Abs)
	if auditData.Overview.LogsPath == "" {
		t.Error("Expected logs path to be set")
	}
	if auditData.Overview.LogsPath != run.LogsPath {
		t.Errorf("Expected logs path %q, got %q", run.LogsPath, auditData.Overview.LogsPath)
	}

	// Verify metrics
	if auditData.Metrics.TokenUsage != 1500 {
		t.Errorf("Expected token usage 1500, got %d", auditData.Metrics.TokenUsage)
	}
	if auditData.Metrics.ErrorCount != 2 {
		t.Errorf("Expected error count 2, got %d", auditData.Metrics.ErrorCount)
	}
	if auditData.Metrics.WarningCount != 1 {
		t.Errorf("Expected warning count 1, got %d", auditData.Metrics.WarningCount)
	}
	if auditData.SafeOutputSummary == nil {
		t.Fatal("Expected safe output summary to be set")
	}
	if auditData.SafeOutputSummary.TemporaryIDMapStatus != temporaryIDMapStatusLoaded {
		t.Errorf("Expected temp map status %q, got %q", temporaryIDMapStatusLoaded, auditData.SafeOutputSummary.TemporaryIDMapStatus)
	}
	if auditData.SafeOutputSummary.TemporaryIDMappings != 1 {
		t.Errorf("Expected temp ID mappings 1, got %d", auditData.SafeOutputSummary.TemporaryIDMappings)
	}
	if auditData.SafeOutputSummary.ChainedTargetCount != 1 {
		t.Errorf("Expected chained target count 1, got %d", auditData.SafeOutputSummary.ChainedTargetCount)
	}

	if auditData.Comparison == nil {
		t.Error("Expected comparison field to be assignable on audit data")
	}

	// Note: Error and warning extraction was removed from buildAuditData
	// The error/warning counts in metrics are preserved but individual error/warning
	// extraction via pattern matching is no longer performed
	// if len(auditData.Errors) != 2 {
	// 	t.Errorf("Expected 2 errors, got %d", len(auditData.Errors))
	// }
	// if len(auditData.Warnings) != 1 {
	// 	t.Errorf("Expected 1 warning, got %d", len(auditData.Warnings))
	// }

	// Verify tool usage
	if len(auditData.ToolUsage) != 1 {
		t.Errorf("Expected 1 tool usage entry, got %d", len(auditData.ToolUsage))
	}

	// Verify missing tools
	if len(auditData.MissingTools) != 1 {
		t.Errorf("Expected 1 missing tool, got %d", len(auditData.MissingTools))
	}

	// Verify MCP failures
	if len(auditData.MCPFailures) != 1 {
		t.Errorf("Expected 1 MCP failure, got %d", len(auditData.MCPFailures))
	}
}

func TestBuildAuditDataCountsFailedWorkflowWithoutTelemetryAsError(t *testing.T) {
	t.Parallel()
	processedRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:   123456,
			WorkflowName: "Failed Workflow",
			Status:       "completed",
			Conclusion:   "failure",
			LogsPath:     testutil.TempDir(t, "test-*"),
		},
	}

	auditData := buildAuditData(context.Background(), processedRun, LogMetrics{}, nil)

	assert.Equal(t, 1, auditData.Metrics.ErrorCount, "failed workflow should contribute at least one error")
}

func TestApplyAuditMetricsCountsWorkflowFailureWithoutTelemetry(t *testing.T) {
	t.Parallel()
	run := WorkflowRun{
		Conclusion: "failure",
	}

	updated := applyAuditMetrics(run, auditAnalysisResults{})

	assert.Equal(t, 1, updated.ErrorCount, "failed workflow should contribute at least one error")
}

func TestDescribeFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		filename    string
		description string
	}{
		{"aw_info.json", "Engine configuration and workflow metadata"},
		{"safe_output.jsonl", "Safe outputs from workflow execution"},
		{"agent_output.json", "Validated safe outputs"},
		{"aw.patch", "Git patch of changes made during execution"},
		{"agent-stdio.log", "Agent standard output/error logs"},
		{"log.md", "Human-readable agent session summary"},
		{"firewall.md", "Firewall log analysis report"},
		{"run_summary.json", "Cached summary of workflow run analysis"},
		{"prompt.txt", "Input prompt for AI agent"},
		{"random.log", "Log file"},
		{"unknown.txt", "Text file"},
		{"data.json", "JSON data file"},
		{"output.jsonl", "JSON Lines data file"},
		{"changes.patch", "Git patch file"},
		{"notes.md", "Markdown documentation"},
		{"agent_output", "Directory containing log files"},
		{"firewall-logs", "Directory containing log files"},
		{"squid-logs", "Directory containing log files"},
		{"aw-prompts", "Directory containing AI prompts"},
		{"somedir/", "Directory"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			result := describeFile(tt.filename)
			if result != tt.description {
				t.Errorf("Expected description '%s', got '%s'", tt.description, result)
			}
		})
	}
}

func TestRenderJSON(t *testing.T) {
	// Create test audit data
	auditData := AuditData{
		Overview: OverviewData{
			RunID:        123456,
			WorkflowName: "Test Workflow",
			Status:       "completed",
			Conclusion:   "success",
			Event:        "push",
			Branch:       "main",
			URL:          "https://github.com/org/repo/actions/runs/123456",
		},
		Metrics: MetricsData{
			TokenUsage:   1500,
			Turns:        5,
			ErrorCount:   1,
			WarningCount: 1,
		},
		Jobs: []JobData{
			{
				Name:       "test-job",
				Status:     "completed",
				Conclusion: "success",
				Duration:   "2m30s",
			},
		},
		DownloadedFiles: []FileInfo{
			{
				Path:        "aw_info.json",
				Size:        1024,
				Description: "Engine configuration and workflow metadata",
			},
		},
		MissingTools: []MissingToolReport{
			{
				Tool:   "missing_tool",
				Reason: "Tool not available",
			},
		},
		Errors: []ValidationIssue{
			{
				File:    "agent.log",
				Line:    42,
				Type:    "error",
				Message: "Test error",
			},
		},
		Warnings: []ValidationIssue{
			{
				File:    "agent.log",
				Line:    50,
				Type:    "warning",
				Message: "Test warning",
			},
		},
	}

	// Render to JSON
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := renderJSON(auditData)
	w.Close()

	// Read the output
	var buf strings.Builder
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("renderJSON failed: %v", err)
	}

	jsonOutput := buf.String()

	// Verify it's valid JSON
	var parsed AuditData
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify key fields
	if parsed.Overview.RunID != 123456 {
		t.Errorf("Expected run ID 123456, got %d", parsed.Overview.RunID)
	}
	if parsed.Metrics.TokenUsage != 1500 {
		t.Errorf("Expected token usage 1500, got %d", parsed.Metrics.TokenUsage)
	}
	if len(parsed.Jobs) != 1 {
		t.Errorf("Expected 1 job, got %d", len(parsed.Jobs))
	}
	if len(parsed.DownloadedFiles) != 1 {
		t.Errorf("Expected 1 downloaded file, got %d", len(parsed.DownloadedFiles))
	}
	if len(parsed.MissingTools) != 1 {
		t.Errorf("Expected 1 missing tool, got %d", len(parsed.MissingTools))
	}
	if len(parsed.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(parsed.Errors))
	}
	if len(parsed.Warnings) != 1 {
		t.Errorf("Expected 1 warning, got %d", len(parsed.Warnings))
	}
}

func TestAuditCachingBehavior(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for test artifacts
	tempDir := testutil.TempDir(t, "test-*")
	runOutputDir := filepath.Join(tempDir, "run-12345")
	if err := os.MkdirAll(runOutputDir, 0755); err != nil {
		t.Fatalf("Failed to create run directory: %v", err)
	}

	// Create minimal test artifacts
	awInfoPath := filepath.Join(runOutputDir, "aw_info.json")
	awInfoContent := `{"engine_id": "copilot", "workflow_name": "test-workflow"}`
	if err := os.WriteFile(awInfoPath, []byte(awInfoContent), 0644); err != nil {
		t.Fatalf("Failed to create mock aw_info.json: %v", err)
	}

	// Create a test run
	run := WorkflowRun{
		DatabaseID:   12345,
		WorkflowName: "Test Workflow",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Now(),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/org/repo/actions/runs/12345",
		TokenUsage:   1000,
		Turns:        3,
		ErrorCount:   0,
		WarningCount: 0,
		LogsPath:     runOutputDir,
	}

	metrics := LogMetrics{
		TokenUsage: 1000,
		Turns:      3,
	}

	// Create and save a run summary
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       run.DatabaseID,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run:            run,
			Metrics:        metrics,
			AccessAnalysis: nil,
			MissingTools:   []MissingToolReport{},
			MCPFailures:    []MCPFailureReport{},
			JobDetails:     []JobInfoWithDuration{},
		},
		ArtifactsList: []string{"aw_info.json"},
	}

	if err := saveRunSummary(runOutputDir, summary, false); err != nil {
		t.Fatalf("Failed to save run summary: %v", err)
	}

	summaryPath := filepath.Join(runOutputDir, runSummaryFileName)

	// Verify summary file was created
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		t.Fatalf("Run summary file should exist after saveRunSummary")
	}
	if err := markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)); err != nil {
		t.Fatalf("markArtifactDownloaded: %v", err)
	}

	// Load the summary back
	loadedSummary, ok := loadRunSummary(runOutputDir, false)
	if !ok {
		t.Fatalf("Failed to load run summary")
	}

	// Verify loaded summary matches
	if loadedSummary.RunID != summary.RunID {
		t.Errorf("Expected run ID %d, got %d", summary.RunID, loadedSummary.RunID)
	}
	if loadedSummary.CLIVersion != summary.CLIVersion {
		t.Errorf("Expected CLI version %s, got %s", summary.CLIVersion, loadedSummary.CLIVersion)
	}
	if loadedSummary.Run.WorkflowName != summary.Run.WorkflowName {
		t.Errorf("Expected workflow name %s, got %s", summary.Run.WorkflowName, loadedSummary.Run.WorkflowName)
	}

	if err := markArtifactDownloaded(runOutputDir, string(ArtifactSetAll)); err != nil {
		t.Fatalf("markArtifactDownloaded: %v", err)
	}

	// Verify that downloadRunArtifacts skips download when valid summary exists
	// This is tested by checking that the function returns without error
	// and doesn't attempt to call `gh run download`
	err := downloadRunArtifacts(context.Background(), downloadArtifactsOptions{runID: run.DatabaseID, outputDir: runOutputDir})
	if err != nil {
		t.Errorf("downloadRunArtifacts should skip download when cached artifacts are complete, but got error: %v", err)
	}
}

// TestAuditUsesRunSummaryCache verifies that when a valid run_summary.json exists on disk,
// AuditWorkflowRun returns successfully using only cached data — without calling
// fetchWorkflowRunMetadata (which would require a live GitHub API) and without
// re-processing local log files.
//
// The test is structured so that, if the early-return cache path is removed, the function
// would call fetchWorkflowRunMetadata → gh api → fail in the test environment (no credentials),
// causing the test to fail.  Only the cache path can satisfy the call without network access.
func TestAuditUsesRunSummaryCache(t *testing.T) {
	t.Parallel()
	tempDir := testutil.TempDir(t, "test-audit-cache-*")
	// AuditWorkflowRun derives runOutputDir as <outputDir>/run-<runID>, so use tempDir as
	// the outputDir and let the function build the subdirectory path.
	const runID int64 = 99999
	runOutputDir := filepath.Join(tempDir, fmt.Sprintf("run-%d", runID))
	if err := os.MkdirAll(runOutputDir, 0755); err != nil {
		t.Fatalf("Failed to create run directory: %v", err)
	}

	// Write a stub aw_info.json so the directory is non-empty
	awInfoContent := `{"engine_id": "copilot", "workflow_name": "test-workflow"}`
	if err := os.WriteFile(filepath.Join(runOutputDir, "aw_info.json"), []byte(awInfoContent), 0644); err != nil {
		t.Fatalf("Failed to write aw_info.json: %v", err)
	}

	// Write a "poison" log file with a grossly inflated token count.  If the cache path is
	// bypassed and log files are re-processed, this value would be counted and would
	// overwrite the summary — but the test verifies that never happens.
	poisonLog := `{"type":"agent_turn","usage":{"total_tokens":9999999}}` + "\n"
	if err := os.WriteFile(filepath.Join(runOutputDir, "agent-stdio.log"), []byte(poisonLog), 0644); err != nil {
		t.Fatalf("Failed to write poison log: %v", err)
	}

	// Ground-truth metrics that were captured on the first (correct) audit pass
	cachedRun := WorkflowRun{
		DatabaseID:   runID,
		WorkflowName: "GPL Dependency Cleaner",
		Status:       "completed",
		Conclusion:   "success",
		TokenUsage:   381270,
		Turns:        9,
		LogsPath:     runOutputDir,
	}
	cachedMetrics := LogMetrics{
		TokenUsage: 381270,
		Turns:      9,
	}

	cachedSummary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       runID,
		ProcessedAt: time.Now().Add(-time.Hour), // processed one hour ago
		RunAnalysis: RunAnalysis{
			Run:          cachedRun,
			Metrics:      cachedMetrics,
			MissingTools: []MissingToolReport{},
			MCPFailures:  []MCPFailureReport{},
			JobDetails:   []JobInfoWithDuration{},
		},
	}

	if err := saveRunSummary(runOutputDir, cachedSummary, false); err != nil {
		t.Fatalf("Failed to save initial run summary: %v", err)
	}

	summaryPath := filepath.Join(runOutputDir, runSummaryFileName)
	initialInfo, err := os.Stat(summaryPath)
	if err != nil {
		t.Fatalf("Could not stat run_summary.json: %v", err)
	}
	initialModTime := initialInfo.ModTime()

	// Call AuditWorkflowRun — the only way this can succeed in a test environment (no GitHub
	// credentials) is if the early-return cache path is taken, skipping fetchWorkflowRunMetadata.
	// WorkflowPath is empty in the cached summary, so renderAuditReport will not attempt any
	// GitHub API calls for baseline comparison either.
	ctx := t.Context()
	if _, err := AuditWorkflowRun(ctx, runID, AuditOptions{
		OutputDir: tempDir,
	}); err != nil {
		t.Fatalf("AuditWorkflowRun failed — cache path not taken (fetchWorkflowRunMetadata was probably called): %v", err)
	}

	// The run_summary.json must NOT have been modified — the poison log must not have been parsed
	currentInfo, err := os.Stat(summaryPath)
	if err != nil {
		t.Fatalf("Could not stat run_summary.json after AuditWorkflowRun: %v", err)
	}
	if !currentInfo.ModTime().Equal(initialModTime) {
		t.Errorf("run_summary.json was modified (mtime changed from %v to %v): "+
			"the audit must not overwrite the cache on repeated calls",
			initialModTime, currentInfo.ModTime())
	}

	// Verify cached metrics are untouched — the poison log would have inflated these if parsed
	loadedSummary, ok := loadRunSummary(runOutputDir, false)
	if !ok {
		t.Fatalf("loadRunSummary should still find a valid cached summary")
	}
	if loadedSummary.Metrics.TokenUsage != cachedMetrics.TokenUsage {
		t.Errorf("Token usage mismatch: expected cached=%d, got=%d (poison log was parsed)",
			cachedMetrics.TokenUsage, loadedSummary.Metrics.TokenUsage)
	}
	if loadedSummary.Metrics.Turns != cachedMetrics.Turns {
		t.Errorf("Turns mismatch: expected cached=%d, got=%d",
			cachedMetrics.Turns, loadedSummary.Metrics.Turns)
	}
}

// TestRenderAuditReportUsesProvidedMetrics verifies that renderAuditReport renders the report
// using the metrics supplied by the caller rather than re-extracting them from log files.
// This is the key property that ensures cache-path and fresh-path produce identical output.
func TestRenderAuditReportUsesProvidedMetrics(t *testing.T) {
	t.Parallel()
	tempDir := testutil.TempDir(t, "test-render-audit-*")
	runOutputDir := filepath.Join(tempDir, "run-11111")
	if err := os.MkdirAll(runOutputDir, 0755); err != nil {
		t.Fatalf("Failed to create run directory: %v", err)
	}

	run := WorkflowRun{
		DatabaseID:   11111,
		WorkflowName: "Test Workflow",
		Status:       "completed",
		Conclusion:   "success",
		TokenUsage:   12345,
		Turns:        7,
		LogsPath:     runOutputDir,
	}
	metrics := LogMetrics{
		TokenUsage: 12345,
		Turns:      7,
	}
	processedRun := ProcessedRun{
		Run:          run,
		MissingTools: []MissingToolReport{},
		MCPFailures:  []MCPFailureReport{},
		JobDetails:   []JobInfoWithDuration{},
	}

	// renderAuditReport should complete without error even without GitHub API access.
	// No GitHub calls are made because WorkflowPath is empty, causing findPreviousSuccessfulWorkflowRuns
	// to return early with an error before any network requests are issued.
	err := renderAuditReport(context.Background(), processedRun, metrics, nil, AuditOptions{
		OutputDir: runOutputDir,
	})
	if err != nil {
		t.Errorf("renderAuditReport returned unexpected error: %v", err)
	}
}

func TestBuildAuditDataWithFirewall(t *testing.T) {
	t.Parallel()
	// Create test data with firewall analysis
	run := WorkflowRun{
		DatabaseID:   123456,
		WorkflowName: "Test Workflow",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/org/repo/actions/runs/123456",
		TokenUsage:   1500,
		Turns:        5,
		ErrorCount:   0,
		WarningCount: 0,
		LogsPath:     testutil.TempDir(t, "test-*"),
	}

	metrics := LogMetrics{
		TokenUsage: 1500,
		Turns:      5,
	}

	firewallAnalysis := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			DomainBuckets: DomainBuckets{
				AllowedDomains: []string{"api.github.com:443", "npmjs.org:443"},
				BlockedDomains: []string{"blocked.example.com:443"},
			},
			TotalRequests:   10,
			AllowedRequests: 7,
			BlockedRequests: 3,
		},
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":      {Allowed: 5, Blocked: 0},
			"npmjs.org:443":           {Allowed: 2, Blocked: 0},
			"blocked.example.com:443": {Allowed: 0, Blocked: 3},
		},
	}

	processedRun := ProcessedRun{
		Run:              run,
		FirewallAnalysis: firewallAnalysis,
		MissingTools:     []MissingToolReport{},
		MCPFailures:      []MCPFailureReport{},
	}

	// Build audit data
	auditData := buildAuditData(context.Background(), processedRun, metrics, nil)

	// Verify firewall analysis is included
	if auditData.FirewallAnalysis == nil {
		t.Fatal("Expected firewall analysis to be included in audit data")
	}

	// Verify firewall data is correct
	if auditData.FirewallAnalysis.TotalRequests != 10 {
		t.Errorf("Expected 10 total requests, got %d", auditData.FirewallAnalysis.TotalRequests)
	}
	if auditData.FirewallAnalysis.AllowedRequests != 7 {
		t.Errorf("Expected 7 allowed requests, got %d", auditData.FirewallAnalysis.AllowedRequests)
	}
	if auditData.FirewallAnalysis.BlockedRequests != 3 {
		t.Errorf("Expected 3 denied requests, got %d", auditData.FirewallAnalysis.BlockedRequests)
	}
	if len(auditData.FirewallAnalysis.AllowedDomains) != 2 {
		t.Errorf("Expected 2 allowed domains, got %d", len(auditData.FirewallAnalysis.AllowedDomains))
	}
	if len(auditData.FirewallAnalysis.BlockedDomains) != 1 {
		t.Errorf("Expected 1 blocked domain, got %d", len(auditData.FirewallAnalysis.BlockedDomains))
	}
}

func TestRenderJSONWithFirewall(t *testing.T) {
	// Create test audit data with firewall analysis
	firewallAnalysis := &FirewallAnalysis{
		AnalysisBase: AnalysisBase{
			DomainBuckets: DomainBuckets{
				AllowedDomains: []string{"api.github.com:443"},
				BlockedDomains: []string{"blocked.example.com:443"},
			},
			TotalRequests:   10,
			AllowedRequests: 7,
			BlockedRequests: 3,
		},
		RequestsByDomain: map[string]DomainRequestStats{
			"api.github.com:443":      {Allowed: 7, Blocked: 0},
			"blocked.example.com:443": {Allowed: 0, Blocked: 3},
		},
	}

	auditData := AuditData{
		Overview: OverviewData{
			RunID:        123456,
			WorkflowName: "Test Workflow",
			Status:       "completed",
			Conclusion:   "success",
			Event:        "push",
			Branch:       "main",
			URL:          "https://github.com/org/repo/actions/runs/123456",
		},
		Metrics: MetricsData{
			TokenUsage:   1500,
			Turns:        5,
			ErrorCount:   0,
			WarningCount: 0,
		},
		FirewallAnalysis: firewallAnalysis,
		DownloadedFiles:  []FileInfo{},
		MissingTools:     []MissingToolReport{},
		MCPFailures:      []MCPFailureReport{},
		Errors:           []ValidationIssue{},
		Warnings:         []ValidationIssue{},
		ToolUsage:        []ToolUsageInfo{},
	}

	// Render to JSON
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := renderJSON(auditData)
	w.Close()

	// Read the output
	var buf strings.Builder
	io.Copy(&buf, r)
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("renderJSON failed: %v", err)
	}

	jsonOutput := buf.String()

	// Verify it's valid JSON
	var parsed AuditData
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Verify firewall analysis is included
	if parsed.FirewallAnalysis == nil {
		t.Fatal("Expected firewall analysis in JSON output")
	}

	// Verify firewall data is correct
	if parsed.FirewallAnalysis.TotalRequests != 10 {
		t.Errorf("Expected 10 total requests, got %d", parsed.FirewallAnalysis.TotalRequests)
	}
	if parsed.FirewallAnalysis.AllowedRequests != 7 {
		t.Errorf("Expected 7 allowed requests, got %d", parsed.FirewallAnalysis.AllowedRequests)
	}
	if parsed.FirewallAnalysis.BlockedRequests != 3 {
		t.Errorf("Expected 3 denied requests, got %d", parsed.FirewallAnalysis.BlockedRequests)
	}
	if len(parsed.FirewallAnalysis.AllowedDomains) != 1 {
		t.Errorf("Expected 1 allowed domain, got %d", len(parsed.FirewallAnalysis.AllowedDomains))
	}
	if len(parsed.FirewallAnalysis.BlockedDomains) != 1 {
		t.Errorf("Expected 1 blocked domain, got %d", len(parsed.FirewallAnalysis.BlockedDomains))
	}
}

func TestExtractStepOutput(t *testing.T) {
	t.Parallel()
	jobLog := `##[group]Run actions/checkout@v4
Checking out repository...
##[endgroup]
##[group]Run ./setup-environment.sh
Setting up environment...
ENVIRONMENT=test
##[endgroup]
##[group]Run npm test
Running tests...
##[error]Test failed: expected 5, got 3
Error: Process completed with exit code 1.
##[endgroup]
##[group]Run cleanup.sh
Cleaning up...
##[endgroup]`

	tests := []struct {
		name        string
		stepNumber  int
		expectError bool
		checkOutput func(t *testing.T, output string)
	}{
		{
			name:        "Extract step 3 (failing step)",
			stepNumber:  3,
			expectError: false,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "npm test") {
					t.Error("Output should contain 'npm test'")
				}
				if !strings.Contains(output, "##[error]Test failed") {
					t.Error("Output should contain error message")
				}
			},
		},
		{
			name:        "Extract step 1",
			stepNumber:  1,
			expectError: false,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "actions/checkout") {
					t.Error("Output should contain 'actions/checkout'")
				}
			},
		},
		{
			name:        "Extract non-existent step",
			stepNumber:  99,
			expectError: true,
			checkOutput: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := extractStepOutput(jobLog, tt.stepNumber)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.checkOutput != nil {
				tt.checkOutput(t, output)
			}
		})
	}
}

func TestFindFirstFailingStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		jobLog          string
		expectedStepNum int
		checkOutput     func(t *testing.T, output string)
	}{
		{
			name: "Find failing step with error marker",
			jobLog: `##[group]Step 1
Success
##[endgroup]
##[group]Step 2
Running...
##[error]Something went wrong
Error details here
##[endgroup]
##[group]Step 3
This runs after failure
##[endgroup]`,
			expectedStepNum: 2,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "##[error]Something went wrong") {
					t.Error("Output should contain error message")
				}
			},
		},
		{
			name: "Find failing step with exit code",
			jobLog: `##[group]Step 1
Success
##[endgroup]
##[group]Step 2
Running command...
exit code 1
##[endgroup]`,
			expectedStepNum: 2,
			checkOutput: func(t *testing.T, output string) {
				if !strings.Contains(output, "exit code 1") {
					t.Error("Output should contain exit code")
				}
			},
		},
		{
			name: "No failing steps",
			jobLog: `##[group]Step 1
Success
##[endgroup]
##[group]Step 2
Also success
##[endgroup]`,
			expectedStepNum: 0,
			checkOutput:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stepNum, output := findFirstFailingStep(tt.jobLog)

			if stepNum != tt.expectedStepNum {
				t.Errorf("Expected step number %d, got %d", tt.expectedStepNum, stepNum)
			}

			if tt.checkOutput != nil && stepNum > 0 {
				tt.checkOutput(t, output)
			}
		})
	}
}

func TestExtractWorkflowNameFromYAML(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "simple name field",
			content: `name: Daily CLI Tools Exploratory Tester
on:
  push:
    branches: [main]
`,
			expected: "Daily CLI Tools Exploratory Tester",
		},
		{
			name: "name with double quotes",
			content: `name: "My Workflow"
on:
  workflow_dispatch:
`,
			expected: "My Workflow",
		},
		{
			name: "name with single quotes",
			content: `name: 'Another Workflow'
on:
  push:
`,
			expected: "Another Workflow",
		},
		{
			name: "no name field",
			content: `on:
  push:
    branches: [main]
jobs:
  build:
`,
			expected: "",
		},
		{
			name: "name field after comment",
			content: `# This is a compiled workflow
name: Test Workflow
on:
  push:
`,
			expected: "Test Workflow",
		},
		{
			name: "indented name (not top-level) is ignored",
			content: `on:
  push:
jobs:
  build:
    name: build-job
`,
			// GitHub Actions requires the workflow 'name' at the top level of the document.
			// A 'name' key nested inside 'jobs' or other sections should not be returned.
			expected: "",
		},
		{
			name: "inline comment after name is stripped by YAML parser",
			content: `name: My Workflow # inline comment
on:
  push:
`,
			expected: "My Workflow",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractWorkflowNameFromYAML([]byte(tt.content))
			if result != tt.expected {
				t.Errorf("extractWorkflowNameFromYAML() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestResolveWorkflowDisplayNameFromLocalFile(t *testing.T) {
	t.Parallel()
	// Write a temporary workflow YAML file and verify the name is extracted correctly
	// via extractWorkflowNameFromYAML (the local-file path in resolveWorkflowDisplayName
	// requires a real git root, so we test the YAML extraction directly here).
	content := []byte("name: My Test Workflow\non:\n  push:\n")
	name := extractWorkflowNameFromYAML(content)
	if name != "My Test Workflow" {
		t.Errorf("extractWorkflowNameFromYAML() = %q, want %q", name, "My Test Workflow")
	}
}

// TestRunAuditMulti_Validation verifies that runAuditMulti rejects invalid
// argument combinations before attempting to download any run data.
func TestRunAuditMulti_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "self comparison rejected",
			args:    []string{"1234567890", "1234567890"},
			wantErr: "cannot diff a run against itself",
		},
		{
			name:    "duplicate comparison run ID rejected",
			args:    []string{"1234567890", "1111111111", "1111111111"},
			wantErr: "duplicate comparison run ID",
		},
		{
			name:    "invalid base run ID rejected",
			args:    []string{"not-a-run-id", "1111111111"},
			wantErr: "invalid base run",
		},
		{
			name:    "invalid comparison run ID rejected",
			args:    []string{"1234567890", "not-a-run-id"},
			wantErr: "invalid comparison run",
		},
		{
			// Job URL as base is normalized to its parent run ID (1234567890), so
			// a self-comparison against the same run ID should still be caught.
			name:    "base job URL normalized and self-comparison rejected",
			args:    []string{"https://github.com/owner/repo/actions/runs/1234567890/job/9876543210", "1234567890"},
			wantErr: "cannot diff a run against itself",
		},
		{
			// Job URL as comparison is normalized to its parent run ID (1111111111),
			// so duplicate detection should still work.
			name:    "comparison job URL normalized and duplicate detected",
			args:    []string{"1234567890", "https://github.com/owner/repo/actions/runs/1111111111/job/9876543210", "1111111111"},
			wantErr: "duplicate comparison run ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runAuditMulti(t.Context(), tt.args, "", "", false, false, "pretty", nil)
			require.Error(t, err, "runAuditMulti should return an error for invalid input")
			require.ErrorContains(t, err, tt.wantErr, "error message should be descriptive")
		})
	}
}

func TestAuditCommandStdinFlag(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	flags := cmd.Flags()

	// --stdin flag must be registered
	stdinFlag := flags.Lookup("stdin")
	require.NotNil(t, stdinFlag, "Should have 'stdin' flag")
	assert.Equal(t, "bool", stdinFlag.Value.Type(), "--stdin should be a boolean flag")
	assert.Equal(t, "false", stdinFlag.DefValue, "--stdin should default to false")
}

func TestAuditCommandStdinRejectsPositionalArgs(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	cmd.SetArgs([]string{"1234567890", "--stdin"})
	cmd.SetOut(nil)
	cmd.SetErr(nil)
	err := cmd.Execute()
	require.Error(t, err, "audit --stdin with a positional arg should return an error")
	require.ErrorContains(t, err, "positional arguments are not allowed with --stdin", "error message should explain the conflict")
}

func TestAuditCommandRequiresArgsOrStdin(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	cmd.SetArgs([]string{})
	cmd.SetOut(nil)
	cmd.SetErr(nil)
	err := cmd.Execute()
	require.Error(t, err, "audit with no args and no --stdin should return an error")
	require.ErrorContains(t, err, "at least one run ID or URL is required", "error message should prompt for required input")
}

func TestAuditCommandVariantWithoutExperiment(t *testing.T) {
	t.Parallel()
	cmd := NewAuditCommand()
	cmd.SetArgs([]string{"1234567890", "--variant", "concise"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	require.Error(t, err, "--variant without --experiment should return an error")
	require.ErrorContains(t, err, "--variant requires --experiment", "error message should explain the requirement")
}

func TestAuditCommandExperimentAndVariantFlagsAreAccepted(t *testing.T) {
	t.Parallel()
	// Verifies that --experiment and --variant are registered and parseable.
	// The command will fail before reaching GitHub API calls (no valid run ID),
	// but the parse step must succeed without an unknown-flag error.
	cmd := NewAuditCommand()
	cmd.SetArgs([]string{"1234567890", "--experiment", "style", "--variant", "concise"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	// Any error should NOT be an "unknown flag" error — flags must be registered.
	if err != nil {
		assert.NotContains(t, err.Error(), "unknown flag", "flags --experiment and --variant must be registered")
	}
}

func TestAuditCommandInvalidRuntimeIsRejected(t *testing.T) {
	t.Parallel()
	// Mirrors TestAuditCommandVariantWithoutExperiment: --runtime should be
	// eagerly validated (like the logs command) rather than silently skipping
	// every run on a typo.
	cmd := NewAuditCommand()
	cmd.SetArgs([]string{"1234567890", "--runtime", "not-a-real-runtime"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	require.Error(t, err, "invalid --runtime value should return an error")
	require.ErrorContains(t, err, "invalid runtime value", "error message should explain the invalid value")
}

func TestValidateLogsRuntimeAllowsCloudHypervisor(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateLogsRuntime(string(workflow.AgentRuntimeCloudHypervisor)))
}

// TestShouldSkipAuditRun_Runtime verifies that shouldSkipAuditRun's runtime
// filter matches the shared matchRuntimeFilter contract used by the logs
// orchestrator: matching runtime is not skipped, non-matching or missing
// runtime is skipped, with an "unknown" fallback label for the skip message.
func TestShouldSkipAuditRun_Runtime(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		awInfoContent string // empty means no aw_info.json file
		runtimeFilter string
		wantSkip      bool
	}{
		{
			name:          "matching runtime is not skipped",
			awInfoContent: `{"agent_runtime": "cloud-hypervisor"}`,
			runtimeFilter: "cloud-hypervisor",
			wantSkip:      false,
		},
		{
			name:          "non-matching runtime is skipped",
			awInfoContent: `{"agent_runtime": "docker"}`,
			runtimeFilter: "cloud-hypervisor",
			wantSkip:      true,
		},
		{
			name:          "missing aw_info.json is skipped",
			awInfoContent: "",
			runtimeFilter: "cloud-hypervisor",
			wantSkip:      true,
		},
		{
			name:          "empty agent_runtime is skipped",
			awInfoContent: `{"agent_runtime": ""}`,
			runtimeFilter: "cloud-hypervisor",
			wantSkip:      true,
		},
		{
			name:          "no runtime filter never skips",
			awInfoContent: `{"agent_runtime": "cloud-hypervisor"}`,
			runtimeFilter: "",
			wantSkip:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			if tc.awInfoContent != "" {
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "aw_info.json"), []byte(tc.awInfoContent), 0644))
			}

			gotSkip := shouldSkipAuditRun(1234567890, tmpDir, "", "", tc.runtimeFilter)
			assert.Equal(t, tc.wantSkip, gotSkip)
		})
	}
}

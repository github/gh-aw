//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupAuditFindingsForRun(t *testing.T) {
	t.Parallel()
	findings := []AuditFinding{
		{Code: AuditFindingWorkflowFailed, Title: "first failure"},
		{Code: AuditFindingWorkflowFailed, Title: "second failure"},
		{Code: AuditFindingHighTokenUsage, Title: "high tokens"},
	}

	entries := groupAuditFindingsForRun(123, findings)
	sortGroupedAuditEntries(entries)

	require.Len(t, entries, 2)
	assert.Equal(t, int64(123), entries[0].RunID)
	assert.Equal(t, AuditFindingHighTokenUsage, entries[0].Code)
	assert.Equal(t, 1, entries[0].Occurrences)
	assert.Equal(t, "high tokens", entries[0].RepresentativeEntry.Title)
	assert.Equal(t, AuditFindingWorkflowFailed, entries[1].Code)
	assert.Equal(t, 2, entries[1].Occurrences)
	assert.Equal(t, "first failure", entries[1].RepresentativeEntry.Title)
}

func TestSortGroupedAuditEntries(t *testing.T) {
	t.Parallel()
	entries := []GroupedAuditEntry{
		{RunID: 2, Code: AuditFindingWorkflowFailed},
		{RunID: 1, Code: AuditFindingWorkflowFailed},
		{RunID: 1, Code: AuditFindingHighTokenUsage},
	}

	sortGroupedAuditEntries(entries)

	assert.Equal(t, []GroupedAuditEntry{
		{RunID: 1, Code: AuditFindingHighTokenUsage},
		{RunID: 1, Code: AuditFindingWorkflowFailed},
		{RunID: 2, Code: AuditFindingWorkflowFailed},
	}, entries)
}

func TestResolveGroupedAuditRunRequests(t *testing.T) {
	t.Parallel()
	requests, err := resolveGroupedAuditRunRequests([]string{
		"https://github.com/owner/repo/actions/runs/100",
		"101",
	}, "")
	require.NoError(t, err)
	require.Len(t, requests, 2)
	assert.Equal(t, int64(100), requests[0].runID)
	assert.Equal(t, int64(101), requests[1].runID)
	assert.Equal(t, "owner", requests[1].owner)
	assert.Equal(t, "repo", requests[1].repo)
	assert.Equal(t, "github.com", requests[1].hostname)
}

func TestResolveGroupedAuditRunRequestsRejectsDuplicateRuns(t *testing.T) {
	t.Parallel()
	_, err := resolveGroupedAuditRunRequests([]string{"100", "100"}, "owner/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate run ID 100")
}

func TestRenderGroupedAuditReportPretty(t *testing.T) {
	report := GroupedAuditReport{
		RunsAnalyzed: 2,
		SkippedRuns:  []int64{99},
		Entries: []GroupedAuditEntry{
			{RunID: 100, Code: AuditFindingWorkflowFailed, Occurrences: 2, RepresentativeEntry: AuditFinding{Title: "Workflow Failed"}},
		},
	}

	output := captureAuditGroupStderr(t, func() {
		renderGroupedAuditReportPretty(report)
	})

	assert.Contains(t, output, "Grouped audit findings across 2 run(s)")
	assert.Contains(t, output, "Skipped run(s): 99")
	assert.Contains(t, output, "run 100 code workflow_failed: 2 occurrence(s); representative: Workflow Failed")
}

func TestRenderGroupedAuditReportPrettyNoFindings(t *testing.T) {
	output := captureAuditGroupStderr(t, func() {
		renderGroupedAuditReportPretty(GroupedAuditReport{RunsAnalyzed: 1})
	})
	assert.Contains(t, output, "No audit findings found.")
}

func TestRenderGroupedAuditReportMarkdown(t *testing.T) {
	report := GroupedAuditReport{
		RunsAnalyzed: 1,
		SkippedRuns:  []int64{7},
		Entries: []GroupedAuditEntry{
			{RunID: 100, Code: AuditFindingWorkflowFailed, Occurrences: 3, RepresentativeEntry: AuditFinding{Title: "Fail | Pipe"}},
		},
	}

	output := captureAuditGroupStdout(t, func() {
		renderGroupedAuditReportMarkdown(report)
	})

	assert.Contains(t, output, "### Grouped Audit Findings")
	assert.Contains(t, output, "Runs analyzed: 1")
	assert.Contains(t, output, "Skipped run(s): 7")
	assert.Contains(t, output, "| 100 | `workflow_failed` | 3 | Fail \\| Pipe |")
}

func TestRenderGroupedAuditReportJSON(t *testing.T) {
	report := GroupedAuditReport{
		RunsAnalyzed: 1,
		Entries: []GroupedAuditEntry{
			{RunID: 100, Code: AuditFindingWorkflowFailed, Occurrences: 1, RepresentativeEntry: AuditFinding{Title: "Workflow Failed"}},
		},
	}

	output := captureAuditGroupStdout(t, func() {
		require.NoError(t, renderGroupedAuditReport(report, auditCommandOptions{jsonOutput: true}))
	})

	var decoded GroupedAuditReport
	require.NoError(t, json.Unmarshal([]byte(output), &decoded))
	assert.Equal(t, report, decoded)
}

// TestRunAuditGroupedEndToEnd exercises runAuditGrouped against two locally cached runs:
// one that is fully analyzed and one that is excluded by --runtime filtering. It verifies
// that RunsAnalyzed and SkippedRuns are populated correctly and that only actionable
// findings (severity >= low) are grouped.
func TestRunAuditGroupedEndToEnd(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-audit-group-*")

	const analyzedRunID int64 = 501
	const skippedRunID int64 = 502

	writeCachedAuditRunFixture(t, tempDir, analyzedRunID, []AuditFinding{
		{Code: AuditFindingWorkflowFailed, Severity: scanfindings.SeverityHigh, Title: "Workflow Failed"},
		{Code: AuditFindingWorkflowFailed, Severity: scanfindings.SeverityHigh, Title: "Workflow Failed Again"},
		{Code: AuditFindingWorkflowSucceeded, Severity: scanfindings.SeverityInfo, Title: "Workflow Succeeded"},
	})
	// Give the analyzed run a matching aw_info.json so the --runtime filter accepts it.
	require.NoError(t, os.WriteFile(
		filepath.Join(resolveAuditOutputDir(tempDir, analyzedRunID), "aw_info.json"),
		[]byte(`{"agent_runtime": "gvisor"}`), 0644))

	writeCachedAuditRunFixture(t, tempDir, skippedRunID, []AuditFinding{
		{Code: AuditFindingWorkflowFailed, Severity: scanfindings.SeverityHigh, Title: "Should not appear"},
	})
	// skippedRunID has no aw_info.json, so it will never match the --runtime filter.

	opts := auditCommandOptions{
		outputDir:     tempDir,
		jsonOutput:    true,
		runtimeFilter: "gvisor",
	}

	output := captureAuditGroupStdout(t, func() {
		err := runAuditGrouped(context.Background(), []string{
			strconv.FormatInt(analyzedRunID, 10),
			strconv.FormatInt(skippedRunID, 10),
		}, opts)
		require.NoError(t, err)
	})

	var report GroupedAuditReport
	require.NoError(t, json.Unmarshal([]byte(output), &report))

	assert.Equal(t, 1, report.RunsAnalyzed)
	assert.Equal(t, []int64{skippedRunID}, report.SkippedRuns)
	require.Len(t, report.Entries, 1, "only the actionable finding should be grouped; info-level findings and skipped runs are excluded")
	assert.Equal(t, analyzedRunID, report.Entries[0].RunID)
	assert.Equal(t, AuditFindingWorkflowFailed, report.Entries[0].Code)
	assert.Equal(t, 2, report.Entries[0].Occurrences)
}

// TestRecordGroupedAuditRunMissingData verifies that when a run's audit.json cannot be
// loaded (missing or corrupt), recordGroupedAuditRun records it in report.SkippedRuns,
// emits a warning, and leaves report.Entries untouched instead of silently contributing
// zero entries.
func TestRecordGroupedAuditRunMissingData(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-audit-group-missing-*")

	const missingRunID int64 = 602
	const corruptRunID int64 = 603
	corruptDir := resolveAuditOutputDir(tempDir, corruptRunID)
	require.NoError(t, os.MkdirAll(corruptDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(corruptDir, auditFileName), []byte("not valid json"), 0644))

	report := GroupedAuditReport{RunsAnalyzed: 2}

	stderrOutput := captureAuditGroupStderr(t, func() {
		recordGroupedAuditRun(tempDir, missingRunID, &report)
		recordGroupedAuditRun(tempDir, corruptRunID, &report)
	})

	assert.Contains(t, stderrOutput, "No audit data found for run "+strconv.FormatInt(missingRunID, 10))
	assert.Contains(t, stderrOutput, "No audit data found for run "+strconv.FormatInt(corruptRunID, 10))
	assert.Equal(t, []int64{missingRunID, corruptRunID}, report.SkippedRuns)
	assert.Empty(t, report.Entries)
}

// TestRecordGroupedAuditRunAppendsActionableFindings verifies that recordGroupedAuditRun
// groups only actionable (severity >= low) findings from a successfully loaded audit.json.
func TestRecordGroupedAuditRunAppendsActionableFindings(t *testing.T) {
	tempDir := testutil.TempDir(t, "test-audit-group-record-*")
	const runID int64 = 604
	writeCachedAuditRunFixture(t, tempDir, runID, []AuditFinding{
		{Code: AuditFindingWorkflowFailed, Severity: scanfindings.SeverityHigh, Title: "Workflow Failed"},
		{Code: AuditFindingWorkflowSucceeded, Severity: scanfindings.SeverityInfo, Title: "Workflow Succeeded"},
	})

	var report GroupedAuditReport
	recordGroupedAuditRun(tempDir, runID, &report)

	assert.Empty(t, report.SkippedRuns)
	require.Len(t, report.Entries, 1)
	assert.Equal(t, AuditFindingWorkflowFailed, report.Entries[0].Code)
}

// writeCachedAuditRunFixture writes a cached run_summary.json and matching audit.json
// for runID under outputDir so that AuditWorkflowRun takes the offline cache path and
// runs without any network access.
func writeCachedAuditRunFixture(t *testing.T, outputDir string, runID int64, findings []AuditFinding) {
	t.Helper()
	runOutputDir := resolveAuditOutputDir(outputDir, runID)
	require.NoError(t, os.MkdirAll(runOutputDir, 0755))

	run := WorkflowRun{
		DatabaseID: runID,
		Status:     "completed",
		Conclusion: "success",
		LogsPath:   runOutputDir,
	}
	summary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       runID,
		ProcessedAt: time.Now().Add(-time.Hour),
		RunAnalysis: RunAnalysis{
			Run:          run,
			MissingTools: []MissingToolReport{},
			MCPFailures:  []MCPFailureReport{},
			JobDetails:   []JobInfoWithDuration{},
		},
	}
	require.NoError(t, saveRunSummary(runOutputDir, summary, false))

	auditData := AuditData{
		SchemaVersion: auditSchemaVersion,
		CacheSource:   auditCacheSourceFull,
		Overview: OverviewData{
			RunID:      run.DatabaseID,
			Status:     run.Status,
			Conclusion: run.Conclusion,
			UpdatedAt:  run.UpdatedAt,
		},
		DownloadedFiles: []FileInfo{},
		KeyFindings:     findings,
	}
	data, err := json.MarshalIndent(auditData, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runOutputDir, auditFileName), data, 0644))
}

func captureAuditGroupStdout(t *testing.T, fn func()) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	fn()
	os.Stdout = oldStdout
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func captureAuditGroupStderr(t *testing.T, fn func()) string {
	t.Helper()
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	fn()
	os.Stderr = oldStderr
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

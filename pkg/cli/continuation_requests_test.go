//go:build !integration

package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractContinuationRequestsFromRun(t *testing.T) {
	run := WorkflowRun{DatabaseID: 42, WorkflowName: "Example"}
	for _, flattened := range []bool{false, true} {
		t.Run(fmt.Sprint(flattened), func(t *testing.T) {
			dir := t.TempDir()
			target := dir
			if !flattened {
				target = filepath.Join(dir, "safe-output")
				require.NoError(t, os.MkdirAll(target, 0o700))
			}
			require.NoError(t, os.WriteFile(filepath.Join(target, constants.AgentOutputArtifactName.String()), []byte(`{"items":[
				{"type":"request_continuation","message":"More work remains","timestamp":"2026-11-21T00:00:00Z"},
				{"type":"noop","message":"Nothing to do"},
				{"type":"report_incomplete","message":"Failed"},
				{"type":"request_continuation","message":"Next request"}
			]}`), 0o600))
			requests, err := extractContinuationRequestsFromRun(dir, run, false, "experiment", "variant")
			require.NoError(t, err)
			require.Len(t, requests, 2)
			assert.Equal(t, "More work remains", requests[0].Message)
			assert.Equal(t, "2026-11-21T00:00:00Z", requests[0].Timestamp)
			assert.Equal(t, run.WorkflowName, requests[0].WorkflowName)
			assert.Equal(t, run.DatabaseID, requests[0].RunID)
			assert.Equal(t, "experiment", requests[0].ExperimentName)
			assert.Equal(t, "variant", requests[0].Variant)
			noops, err := extractNoopsFromRun(dir, run, false, "", "")
			require.NoError(t, err)
			require.Len(t, noops, 1)
		})
	}
}

func TestContinuationRequestsUnavailableVersusEmpty(t *testing.T) {
	for _, content := range []string{"", "{", `{}`, `{"items":null}`, `{"items":[}`, `{"items":{}}`, `{"items":[null]}`, `{"items":[{}]}`, `{"items":[12]}`, `{"items":[{"type":"request_continuation","message":12}]}`, `{"items":[{"type":"request_continuation"}]}`, `{"items":[]}`} {
		t.Run(content, func(t *testing.T) {
			dir := t.TempDir()
			if content != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, constants.AgentOutputArtifactName.String()), []byte(content), 0o600))
			}
			requests, err := extractContinuationRequestsFromRun(dir, WorkflowRun{}, false, "", "")
			require.NoError(t, err)
			if content == `{"items":[]}` {
				assert.NotNil(t, requests)
				assert.Empty(t, requests)
			} else {
				assert.Nil(t, requests)
			}
		})
	}
}

func TestContinuationRequestsMixedMalformedArtifactsAreUnavailable(t *testing.T) {
	valid := `{"type":"request_continuation","message":"More work remains"}`
	for _, malformed := range []string{
		`null`,
		`{}`,
		`12`,
		`{"type":"request_continuation","message":""}`,
		`{"type":"request_continuation"}`,
		`{"type":"request_continuation","message":"  "}`,
		`{"type":"request_continuation","message":12}`,
	} {
		for _, validFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/valid_first=%t", malformed, validFirst), func(t *testing.T) {
				items := valid + "," + malformed
				if !validFirst {
					items = malformed + "," + valid
				}
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, constants.AgentOutputArtifactName.String()),
					[]byte(`{"items":[`+items+`]}`), 0o600))
				requests, err := extractContinuationRequestsFromRun(dir, WorkflowRun{}, false, "", "")
				require.NoError(t, err)
				assert.Nil(t, requests, "partial requests must not be reported as an exact count")
				data := buildLogsData([]ProcessedRun{{ContinuationRequests: requests}}, ".", nil)
				assert.Nil(t, data.Runs[0].ContinuationRequestCount)
				assert.Equal(t, 1, data.Summary.ContinuationRequestsUnavailableRuns)
				assert.Zero(t, data.Summary.ContinuationRequestedRuns)
			})
		}
	}
}

func TestContinuationRequestsCollectionErrorsAreUnavailable(t *testing.T) {
	for _, items := range []string{
		`[]`,
		`[{"type":"request_continuation","message":"More work remains"},{"type":"noop","message":"No action"}]`,
	} {
		t.Run(items, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, constants.AgentOutputArtifactName.String()),
				[]byte(`{"items":`+items+`,"errors":["Failed to collect remaining items"]}`), 0o600))
			requests, err := extractContinuationRequestsFromRun(dir, WorkflowRun{}, false, "", "")
			require.NoError(t, err)
			assert.Nil(t, requests)
			noops, err := extractNoopsFromRun(dir, WorkflowRun{}, false, "", "")
			require.NoError(t, err)
			if items != `[]` {
				require.Len(t, noops, 1, "legacy extractors retain their tolerant parsing behavior")
			}
		})
	}
}

func TestContinuationRequestsAuditAndLogs(t *testing.T) {
	requests := []ContinuationRequestReport{{Message: "More work", ReportProvenance: ReportProvenance{RunID: 42, WorkflowName: "Example", ExperimentName: "test", Variant: "a"}}}
	run := WorkflowRun{DatabaseID: 42, WorkflowName: "Example", Conclusion: "success"}
	results := auditAnalysisResults{continuationRequests: requests}
	run = applyAuditMetrics(run, results)
	processed := buildProcessedAuditRun(run, results)
	assert.Equal(t, requests, processed.ContinuationRequests)
	assert.Equal(t, 1, processed.Run.ContinuationRequestCount)
	summary := buildAuditRunSummary(run, processed, results)
	assert.Equal(t, requests, summary.ContinuationRequests)
	session := buildSessionAnalysis(processed, LogMetrics{})
	require.NotNil(t, session.ContinuationRequestCount)
	assert.Equal(t, 1, *session.ContinuationRequestCount)
	assert.Zero(t, session.NoopCount)

	data := buildLogsData([]ProcessedRun{processed}, ".", &ContinuationData{Message: "pagination cursor"})
	assert.Equal(t, 1, data.Summary.TotalContinuationRequests)
	assert.Equal(t, 1, data.Summary.ContinuationRequestedRuns)
	assert.Equal(t, requests, data.Runs[0].ContinuationRequests)
	assert.Equal(t, requests, data.ContinuationRequests)
	assert.Zero(t, data.Summary.TotalAgentLogicFailures)
	assert.Zero(t, data.Summary.TotalErrors)
	require.NotNil(t, data.Continuation)

	for _, value := range []any{summary, data, AuditData{ContinuationRequests: requests}} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), `"continuation_requests"`)
		assert.Contains(t, string(encoded), `"experiment_name":"test"`)
	}
	var output bytes.Buffer
	require.NoError(t, renderLogsCompactToWriter(&output, data))
	assert.Contains(t, output.String(), "continuation_requests=1")
	assert.Contains(t, output.String(), "no automatic dispatch or resume")
	assert.Contains(t, output.String(), `message="More work"`)
	output.Reset()
	require.NoError(t, renderLogsCompactVerboseToWriter(&output, data))
	assert.Contains(t, output.String(), "requests=1")
	output.Reset()
	require.NoError(t, renderContinuationRequests(&output, requests))
	assert.Contains(t, output.String(), "intent only")
	output.Reset()
	renderLogsTSVToWriter(&output, data)
	assert.Contains(t, output.String(), "continuation_requests=1")

	cached := data.Runs[0]
	cachedData := buildLogsData([]ProcessedRun{{Run: run, cachedData: &cached}}, ".", nil)
	assert.Equal(t, 1, cachedData.Summary.TotalContinuationRequests)
	assert.Equal(t, requests, cachedData.ContinuationRequests)
}

func TestHealthContinuationRequestsCachedEvidence(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	cachedRun := runs[9]
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: cachedRun.DatabaseID,
		RunAnalysis: RunAnalysis{Run: cachedRun, ContinuationRequests: []ContinuationRequestReport{{Message: "More"}, {Message: "Later"}}}}
	dir := filepath.Join(config.LogsDir, fmt.Sprintf("run-%d", cachedRun.DatabaseID))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, saveRunSummary(dir, summary, false))
	health := calculateCurrentWorkflowHealth("Example", append(runs, cachedRun), config)
	assert.Equal(t, "recovery_observed", health.Status)
	assert.Equal(t, 5, health.SuccessCount)
	assert.Zero(t, health.FailureCount)
	report := health.ContinuationRequests
	require.NotNil(t, report)
	assert.Equal(t, 2, report.RequestCount)
	assert.Equal(t, 1, report.RequestedRuns)
	assert.Equal(t, 4, report.UnavailableRuns)
	require.Len(t, report.Runs, 5)
	for _, run := range report.Runs {
		if run.RunID != cachedRun.DatabaseID {
			assert.Nil(t, run.RequestCount)
		} else {
			require.NotNil(t, run.RequestCount)
			assert.Equal(t, 2, *run.RequestCount)
		}
	}
	var output bytes.Buffer
	require.NoError(t, renderHealthSessionEvidence(&output, health))
	assert.Contains(t, output.String(), "requests=unavailable")
	assert.Contains(t, output.String(), "not zero")
	serialized, err := json.Marshal(health)
	require.NoError(t, err)
	assert.Contains(t, string(serialized), `"request_count":null`)
}

func TestHealthContinuationRequestsRejectUnavailableCache(t *testing.T) {
	for _, mode := range []string{"empty", "missing field", "stale", "version mismatch"} {
		t.Run(mode, func(t *testing.T) {
			runs, config := healthSubagentFixture(t)
			run := runs[9]
			summary := &RunSummary{CLIVersion: GetVersion(), RunID: run.DatabaseID,
				RunAnalysis: RunAnalysis{Run: run, ContinuationRequests: []ContinuationRequestReport{}}}
			switch mode {
			case "missing field":
				summary.ContinuationRequests = nil
			case "stale":
				summary.Run.Attempt++
			case "version mismatch":
				summary.CLIVersion = "old"
			}
			dir := filepath.Join(config.LogsDir, fmt.Sprintf("run-%d", run.DatabaseID))
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, saveRunSummary(dir, summary, false))
			health := WorkflowHealth{Status: "success"}
			applyHealthContinuationRequests(&health, []WorkflowRun{run}, config)
			assert.Equal(t, "success", health.Status)
			report := health.ContinuationRequests
			require.Len(t, report.Runs, 1)
			if mode == "empty" {
				require.NotNil(t, report.Runs[0].RequestCount)
				assert.Zero(t, *report.Runs[0].RequestCount)
				assert.Zero(t, report.UnavailableRuns)
			} else {
				assert.Nil(t, report.Runs[0].RequestCount)
				assert.Equal(t, 1, report.UnavailableRuns)
			}
		})
	}
}

func TestContinuationRequestsCacheBackfill(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run-42")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, constants.AgentOutputArtifactName.String()),
		[]byte(`{"items":[{"type":"request_continuation","message":"Continue later"}]}`), 0o600))
	run := WorkflowRun{DatabaseID: 42, WorkflowName: "Example"}
	var requests []ContinuationRequestReport
	require.True(t, backfillContinuationRequests(&requests, run, dir, false))
	require.Len(t, requests, 1)
	assert.False(t, backfillContinuationRequests(&requests, run, dir, false))
	summary := &RunSummary{RunID: 42, RunAnalysis: RunAnalysis{Run: run}}
	processed := processedRunFromSummary(summary, dir)
	assert.Equal(t, requests, processed.ContinuationRequests)
	cached := processedRunFromCachedData(RunData{RunID: 42, WorkflowName: "Example"}, nil, root)
	assert.Equal(t, requests, cached.ContinuationRequests)
	data := buildLogsData([]ProcessedRun{cached}, root, nil)
	assert.Equal(t, 1, data.Summary.TotalContinuationRequests)
	assert.Equal(t, requests, data.ContinuationRequests)
}

func TestHealthContinuationRequestsBackfillCachedArtifacts(t *testing.T) {
	runs, config := healthSubagentFixture(t)
	run := runs[9]
	dir := filepath.Join(config.LogsDir, fmt.Sprintf("run-%d", run.DatabaseID))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	summary := &RunSummary{CLIVersion: GetVersion(), RunID: run.DatabaseID, RunAnalysis: RunAnalysis{Run: run}}
	require.NoError(t, saveRunSummary(dir, summary, false))
	require.NoError(t, os.WriteFile(filepath.Join(dir, constants.AgentOutputArtifactName.String()),
		[]byte(`{"items":[{"type":"request_continuation","message":"Continue later"}]}`), 0o600))
	health := WorkflowHealth{Status: "success"}
	applyHealthContinuationRequests(&health, []WorkflowRun{run}, config)
	require.NotNil(t, health.ContinuationRequests.Runs[0].RequestCount)
	assert.Equal(t, 1, *health.ContinuationRequests.Runs[0].RequestCount)
	assert.Equal(t, 1, health.ContinuationRequests.RequestedRuns)
	assert.Zero(t, health.ContinuationRequests.UnavailableRuns)
	assert.Equal(t, "success", health.Status)
}

func TestContinuationRequestsDrainEventsAreNotOutcomes(t *testing.T) {
	events := buildAgentEventsFromProcessedRun(ProcessedRun{
		ContinuationRequests: []ContinuationRequestReport{{Message: "More work"}},
	}, MetricsData{}, nil)
	var found bool
	for _, event := range events {
		if event.Fields["type"] == "request_continuation" {
			found = true
			assert.Equal(t, "tool_result", event.Stage)
			assert.Empty(t, event.Fields["status"])
			assert.Equal(t, "More work", event.Fields["message"])
		}
		assert.NotEqual(t, "finish", event.Stage)
		assert.NotEqual(t, "error", event.Stage)
	}
	assert.True(t, found)
}

func TestContinuationRequestsSerializationDistinguishesUnavailableAndEmpty(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			var requests []ContinuationRequestReport
			if available {
				requests = []ContinuationRequestReport{}
			}
			processed := ProcessedRun{Run: WorkflowRun{DatabaseID: 42}, ContinuationRequests: requests}
			data := buildLogsData([]ProcessedRun{processed}, ".", nil)
			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			audit, err := json.Marshal(AuditData{ContinuationRequests: requests, SessionAnalysis: buildSessionAnalysis(processed, LogMetrics{})})
			require.NoError(t, err)
			for _, serialized := range []string{string(encoded), string(audit)} {
				if available {
					assert.Contains(t, serialized, `"continuation_requests":[]`)
					assert.Contains(t, serialized, `"continuation_request_count":0`)
				} else {
					assert.Contains(t, serialized, `"continuation_requests":null`)
					assert.Contains(t, serialized, `"continuation_request_count":null`)
				}
			}
			if available {
				assert.Zero(t, data.Summary.ContinuationRequestsUnavailableRuns)
			} else {
				assert.Equal(t, 1, data.Summary.ContinuationRequestsUnavailableRuns)
			}
			assert.Zero(t, data.Summary.TotalContinuationRequests)
			assert.Zero(t, data.Summary.ContinuationRequestedRuns)
		})
	}
}

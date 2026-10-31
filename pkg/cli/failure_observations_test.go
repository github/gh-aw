package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const failureDiagnosticsFixture = `{
	"failureObservations":[
		{"schemaVersion":1,"categoryId":"ineffective-retries","severity":"warning","confidence":"high","evidenceState":"observed","recoverability":"recovered",
		 "evidence":[{"eventIndex":7,"type":"tool.execution_complete","provenance":{"component":"agent","phase":"agent","path":"session.jsonl","index":0,"attemptId":"attempt-2","startupAttemptId":"startup-1","futureSource":"kept"}}],
		 "summary":"Repeated attempts did not change the outcome.","futureDetail":{"kept":true}},
		{"schemaVersion":1,"categoryId":"premature-termination","severity":"warning","confidence":"low","evidenceState":"missing","recoverability":"unknown","evidence":[],"summary":"Termination evidence is missing."}
	],
	"totalObservations":2,"truncated":false,"futureDiagnostics":{"kept":true}
}`

func TestFailureObservationsAuditAndLogsRoundTrip(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	writeGraderFiles(t, filepath.Join(runDir, "usage", "graders"), map[string]any{
		"version": 2, "results": []map[string]any{
			{"id": "ineffective-retries", "status": "pass", "value": 0, "diagnostics": json.RawMessage(failureDiagnosticsFixture)},
			{"id": "legacy", "status": "fail", "value": 1},
		},
	}, nil)
	graders := extractGradersData(runDir)
	require.NotNil(t, graders)
	assert.Equal(t, 1, graders.Passed)
	assert.Equal(t, 1, graders.Failed)
	assert.Zero(t, graders.UnavailableCount)
	require.Len(t, graders.Results, 2)
	result := graders.Results[0]
	require.Len(t, result.FailureObservations, 2)
	observed := result.FailureObservations[0]
	assert.Equal(t, "recovered", observed.Recoverability)
	require.Len(t, observed.Evidence, 1)
	provenance := observed.Evidence[0].Provenance
	require.NotNil(t, provenance.Index)
	assert.Zero(t, *provenance.Index)
	assert.Equal(t, "attempt-2", provenance.AttemptID)
	assert.Equal(t, "startup-1", provenance.StartupAttemptID)
	assert.Equal(t, "missing", result.FailureObservations[1].EvidenceState)
	assert.Empty(t, result.FailureObservations[1].Evidence)

	for name, report := range map[string]any{
		"audit": AuditData{Graders: graders},
		"logs":  RunData{Graders: graders},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(report)
			require.NoError(t, err)
			var decoded struct {
				Graders GradersData `json:"graders"`
			}
			require.NoError(t, json.Unmarshal(data, &decoded))
			require.Len(t, decoded.Graders.Results, 2)
			assert.JSONEq(t, failureDiagnosticsFixture, string(decoded.Graders.Results[0].Diagnostics))
			assert.Equal(t, result.FailureObservations, decoded.Graders.Results[0].FailureObservations)
			legacy, err := json.Marshal(decoded.Graders.Results[1])
			require.NoError(t, err)
			assert.NotContains(t, string(legacy), "diagnostics")
			assert.NotContains(t, string(legacy), "failure_observations")
		})
	}
}

func TestFailureObservationsFutureAndMalformedDiagnostics(t *testing.T) {
	t.Parallel()
	for _, diagnostics := range []string{
		`{"failureObservations":[{"schemaVersion":2,"categoryId":"future-category","evidence":[],"newField":true}]}`,
		`{"failureObservations":[{"schemaVersion":"future","newField":true}],"future":true}`,
		`{"failureObservations":"future-format"}`,
		`{"unrelatedDiagnostics":true}`,
		`null`,
	} {
		t.Run(diagnostics, func(t *testing.T) {
			result := buildGraderResult(graderArtifactFullResult{
				ID: "future", Status: "unavailable", Diagnostics: json.RawMessage(diagnostics),
			}, graderManifestEntry{})
			assert.Equal(t, "unavailable", result.Status)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			var roundTrip GraderResult
			require.NoError(t, json.Unmarshal(encoded, &roundTrip))
			assert.JSONEq(t, diagnostics, string(roundTrip.Diagnostics))
		})
	}
}

func TestFailureObservationConsoleDetail(t *testing.T) {
	t.Parallel()
	var diagnostics struct {
		Observations []FailureObservation `json:"failureObservations"`
	}
	require.NoError(t, json.Unmarshal([]byte(failureDiagnosticsFixture), &diagnostics))
	observed := formatFailureObservation(diagnostics.Observations[0])
	for _, detail := range []string{
		"warning ineffective-retries", "evidence=observed", "confidence=high", "recoverability=recovered",
		"event=7", "type=tool.execution_complete", "component=agent", "phase=agent",
		"path=session.jsonl", "index=0", "attemptId=attempt-2", "startupAttemptId=startup-1",
	} {
		assert.Contains(t, observed, detail)
	}
	missing := formatFailureObservation(diagnostics.Observations[1])
	assert.Contains(t, missing, "evidence=missing")
	assert.Contains(t, missing, "recoverability=unknown")
	assert.NotContains(t, missing, "event=")
}

func TestGraderArtifactPreservesFailureDiagnostics(t *testing.T) {
	t.Parallel()
	var artifact graderResultsArtifact
	require.NoError(t, json.Unmarshal([]byte(`{"version":2,"results":[{"id":"ineffective-retries","status":"pass","value":0,"diagnostics":`+
		failureDiagnosticsFixture+`}]}`), &artifact))
	encoded, err := json.Marshal(artifact)
	require.NoError(t, err)
	var roundTrip graderResultsArtifact
	require.NoError(t, json.Unmarshal(encoded, &roundTrip))
	require.Len(t, roundTrip.Results, 1)
	assert.JSONEq(t, failureDiagnosticsFixture, string(roundTrip.Results[0].Diagnostics))
}

func TestFailureObservationsInvalidateOlderAuditCache(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	run := WorkflowRun{DatabaseID: 42, Status: "completed", Conclusion: "success"}
	legacy := AuditData{SchemaVersion: 10, CacheSource: auditCacheSourceFull, Overview: buildAuditOverview(run, nil)}
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(auditPath(runDir), data, 0o600))
	_, ok := loadCachedAuditData(runDir, run, auditCacheSourceFull)
	assert.False(t, ok, "pre-diagnostics audits must be rebuilt from preserved artifacts")
}

func TestFailureObservationsOutputSchemaAcceptsDiagnostics(t *testing.T) {
	t.Parallel()
	result := buildGraderResult(graderArtifactFullResult{
		ID: "ineffective-retries", Status: "pass", Diagnostics: json.RawMessage(failureDiagnosticsFixture),
	}, graderManifestEntry{})
	schema, err := GenerateOutputSchema[GraderResult]()
	require.NoError(t, err)
	resolved, err := schema.Resolve(nil)
	require.NoError(t, err)
	data, err := json.Marshal(result)
	require.NoError(t, err)
	var instance map[string]any
	require.NoError(t, json.Unmarshal(data, &instance))
	require.NoError(t, resolved.Validate(instance))

	for _, name := range []string{"audit", "logs", "logs-jsonl"} {
		data, err := GenerateNamedOutputSchema(name)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"failure_observations"`)
	}
}

func TestFailureObservationsConsoleDoesNotImplyFailedStatus(t *testing.T) {
	var diagnostics struct {
		Observations []FailureObservation `json:"failureObservations"`
	}
	require.NoError(t, json.Unmarshal([]byte(failureDiagnosticsFixture), &diagnostics))
	output := captureStderrForGuardPolicyReportTest(func() {
		renderConsoleGraders(&GradersData{
			Passed:  1,
			Results: []GraderResult{{ID: "ineffective-retries", Status: "pass", FailureObservations: diagnostics.Observations}},
		})
	})
	assert.Contains(t, output, "graders: pass=1 fail=0 error=0 unavailable=0")
	assert.Contains(t, output, "pass ineffective-retries")
	assert.Contains(t, output, "evidence=missing")
	assert.Contains(t, output, "recoverability=recovered")
	assert.Contains(t, output, "event=7")
	var logs bytes.Buffer
	captureStderrForGuardPolicyReportTest(func() {
		renderLogsConsoleToWriter(&logs, LogsData{Runs: []RunData{{
			RunID: 42,
			Graders: &GradersData{Results: []GraderResult{{
				ID: "ineffective-retries", Status: "pass", FailureObservations: diagnostics.Observations,
			}}},
		}}})
	})
	assert.Contains(t, logs.String(), "Failure Observations")
	assert.Contains(t, logs.String(), "Grader Status")
	assert.Contains(t, logs.String(), "evidence=missing")
	assert.Contains(t, logs.String(), "recoverability=recovered")
	assert.Contains(t, logs.String(), "event=7")
}

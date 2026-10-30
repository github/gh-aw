package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const regradeTestEvent = `{"type":"tool.execution_start","data":{"toolName":"view","input":{"path":"a"}},"provenance":{"component":"agent","phase":"agent","path":"session.jsonl","index":0}}`

func regradeTestFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func regradeTestRun(t *testing.T, root string, id int64, session string) ProcessedRun {
	t.Helper()
	directory := filepath.Join(root, "run-"+strconv.FormatInt(id, 10))
	require.NoError(t, os.MkdirAll(directory, 0o700))
	if session != "" {
		regradeTestFile(t, directory, "usage/aw_session.jsonl", session)
	}
	return ProcessedRun{Run: WorkflowRun{
		DatabaseID: id, Repository: "owner/repo", Attempt: 2,
		URL:      "https://github.com/owner/repo/actions/runs/" + strconv.FormatInt(id, 10),
		LogsPath: directory, WorkflowName: "recorded-workflow", HeadSha: "recorded-sha",
	}}
}

func regradeTestRequireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
}

func TestRegradeProcessedRunsPreservesEvidenceAndProvenance(t *testing.T) {
	regradeTestRequireNode(t)
	root := t.TempDir()
	run := regradeTestRun(t, root, 21, regradeTestEvent+"\n"+regradeTestEvent+"\n")
	const original = `{"version":2,"results":[{"id":"loops","status":"pass","value":-7,"implementation":{"id":"old","version":1}}]}`
	originalPath := regradeTestFile(t, run.Run.LogsPath, "usage/graders/grader_results.json", original)
	regradeTestFile(t, run.Run.LogsPath, "usage/graders/grader_manifest.json", `{"version":2,"graders":[{"id":"loops"}]}`)
	definitions := []graderRunDefinition{
		{ID: "z-count", Source: "inline", Script: "return config.offset + session.length", Config: map[string]any{"offset": 3}},
		{ID: "loops", Source: "builtin"},
	}
	result, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{run}, definitions)
	require.NoError(t, err)
	require.Len(t, result, 1)
	record := result[0]
	assert.Equal(t, harborRunIdentity(run), record.Identity)
	assert.Equal(t, "recorded", record.SessionStatus)
	assert.Equal(t, "recorded-sha", record.Run.HeadSha)
	assert.Equal(t, regradeDigest([]byte(regradeTestEvent+"\n"+regradeTestEvent+"\n")), record.SessionDigest)
	assert.Len(t, record.PayloadDigest, 64)
	assert.JSONEq(t, original, string(record.OriginalGraders))
	require.Len(t, record.OriginalEvidence, 2)
	assert.Equal(t, originalPath, record.OriginalEvidence[0].Path)
	assert.Equal(t, regradeDigest([]byte(original)), record.OriginalEvidence[0].Digest)
	require.Len(t, record.Results, 2)
	var builtin, custom map[string]any
	require.NoError(t, json.Unmarshal(record.Results[0], &builtin))
	require.NoError(t, json.Unmarshal(record.Results[1], &custom))
	assert.Equal(t, "loops", builtin["id"])
	assert.InDelta(t, 1, builtin["value"], 0)
	assert.Equal(t, "pass", builtin["status"])
	assert.NotEmpty(t, builtin["implementation"])
	assert.Equal(t, "z-count", custom["id"])
	assert.InDelta(t, 5, custom["value"], 0)
	assert.NotEmpty(t, custom["implementation"])
	require.Len(t, record.Definitions, 2)
	assert.Len(t, record.Definitions[1].ScriptDigest, 64)
	assert.Len(t, record.Definitions[1].ConfigDigest, 64)
	assert.Len(t, record.Definitions[1].DefinitionDigest, 64)
	assert.Len(t, record.Definitions[1].RuntimeDigest, 64)
	encoded, err := json.Marshal(record.Definitions)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "return config")
	after, err := os.ReadFile(originalPath)
	require.NoError(t, err)
	assert.Equal(t, original, string(after)) //nolint:testifylint // Exact evidence bytes, not semantic JSON.
	sessionAfter, err := os.ReadFile(record.SessionPath)
	require.NoError(t, err)
	assert.Equal(t, regradeTestEvent+"\n"+regradeTestEvent+"\n", string(sessionAfter)) //nolint:testifylint // Exact evidence bytes, not semantic JSON.
	require.Equal(t, "z-count", definitions[0].ID, "caller-owned definitions must not be sorted in place")
}

func TestRegradeMultipleRunsAndFailuresAreDeterministic(t *testing.T) {
	regradeTestRequireNode(t)
	root := t.TempDir()
	first := regradeTestRun(t, root, 1, regradeTestEvent)
	second := regradeTestRun(t, root, 2, regradeTestEvent)
	missing := regradeTestRun(t, root, 3, "")
	invalid := regradeTestRun(t, root, 4, "{broken")
	definitions := []graderRunDefinition{
		{ID: "working", Source: "inline", Script: "return session.length"},
		{ID: "broken", Source: "inline", Script: `throw new Error("expected grader failure")`},
	}
	runs := []ProcessedRun{invalid, second, missing, first, first}
	results, err := regradeProcessedRuns(t.Context(), root, runs, definitions)
	require.NoError(t, err)
	require.Len(t, results, 4)
	for index, result := range results {
		assert.Equal(t, int64(index+1), result.Run.DatabaseID)
		require.Len(t, result.Results, 2)
		var errorRecord, workingRecord map[string]any
		require.NoError(t, json.Unmarshal(result.Results[0], &errorRecord))
		require.NoError(t, json.Unmarshal(result.Results[1], &workingRecord))
		assert.Equal(t, "broken", errorRecord["id"])
		assert.Equal(t, "working", workingRecord["id"])
		if index < 2 {
			assert.Equal(t, "recorded", result.SessionStatus)
			assert.Equal(t, "error", errorRecord["status"])
			assert.Contains(t, errorRecord["error"], "expected grader failure")
			assert.NotEmpty(t, errorRecord["implementation"])
			assert.Equal(t, "pass", workingRecord["status"])
		} else {
			assert.NotEmpty(t, result.SessionError)
			assert.Equal(t, "unavailable", errorRecord["status"])
			assert.Equal(t, "unavailable", workingRecord["status"])
		}
	}
	assert.Equal(t, "missing", results[2].SessionStatus)
	assert.Equal(t, "invalid", results[3].SessionStatus)
	forward, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{missing, invalid}, definitions)
	require.NoError(t, err)
	reverse, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{invalid, missing}, []graderRunDefinition{definitions[1], definitions[0]})
	require.NoError(t, err)
	require.Equal(t, forward, reverse)
}

func TestRegradeSessionEvidenceVariants(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, file, content, status string
	}{
		{"unified", "aw_session.jsonl", regradeTestEvent, "recorded"},
		{"array", "aw_session.jsonl", "[" + regradeTestEvent + "]", "recorded"},
		{"prefixed usage", "abc-usage/aw_session.jsonl", regradeTestEvent, "recorded"},
		{"empty", "usage/aw_session.jsonl", "", "invalid"},
		{"empty array", "usage/aw_session.jsonl", "[]", "invalid"},
		{"schema invalid", "usage/aw_session.jsonl", `{"type":"message","data":{},"provenance":null}`, "invalid"},
		{"invalid array", "aw_session.jsonl", "[", "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := filepath.Join(root, strings.ReplaceAll(test.name, " ", "-"))
			path := regradeTestFile(t, source, test.file, test.content)
			record := RegradedRun{SessionStatus: "missing"}
			_, err := regradeSessionPayload(t.Context(), source, &record)
			assert.Equal(t, test.status, record.SessionStatus)
			assert.Equal(t, path, record.SessionPath)
			assert.Equal(t, regradeDigest([]byte(test.content)), record.SessionDigest)
			if test.status == "recorded" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestRegradeRejectsBashAndUnsafeDefinitionsBeforeEvidence(t *testing.T) {
	for _, definition := range []graderRunDefinition{
		{ID: "shell", Source: "operational-value"},
		{ID: "shell", Source: "inline", Run: "script.sh"},
		{ID: "shell", Source: "inline", ShellScript: "echo unsafe"},
		{ID: "loops", Source: "builtin", Script: "return 1"},
		{ID: "unknown", Source: "unknown"},
	} {
		_, err := regradeProcessedRuns(t.Context(), "not-present", nil, []graderRunDefinition{definition})
		require.Error(t, err)
	}
	_, err := regradeProcessedRuns(t.Context(), ".", nil, []graderRunDefinition{{ID: "loops", Source: "builtin"}, {ID: "loops", Source: "builtin"}})
	require.ErrorContains(t, err, "duplicate")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = regradeProcessedRuns(ctx, ".", []ProcessedRun{{Run: WorkflowRun{DatabaseID: 1}}}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRegradeRuntimeFailureIsExplicit(t *testing.T) {
	root := t.TempDir()
	run := regradeTestRun(t, root, 1, regradeTestEvent)
	t.Setenv("PATH", root)
	results, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{run}, []graderRunDefinition{{ID: "loops", Source: "builtin"}})
	require.NoError(t, err)
	var record map[string]any
	require.NoError(t, json.Unmarshal(results[0].Results[0], &record))
	assert.Equal(t, "error", record["status"])
	assert.Contains(t, record["error"], "node is required")
	assert.NotEmpty(t, record["implementation"])
}

func TestRegradeOriginalInvalidEvidenceExplicit(t *testing.T) {
	root := t.TempDir()
	run := regradeTestRun(t, root, 1, "")
	regradeTestFile(t, run.Run.LogsPath, "usage/graders/grader_results.json", "not-json")
	results, err := regradeProcessedRuns(t.Context(), root, []ProcessedRun{run}, nil)
	require.NoError(t, err)
	assert.Nil(t, results[0].OriginalGraders)
	require.Len(t, results[0].OriginalEvidence, 1)
	assert.Contains(t, results[0].OriginalEvidence[0].Error, "invalid JSON")
	assert.Len(t, results[0].OriginalEvidence[0].Digest, 64)
}

func TestRegradeDefinitionProvenanceTracksChanges(t *testing.T) {
	definition := graderRunDefinition{ID: "custom", Source: "inline", Script: "return 1", Config: map[string]any{"offset": 1}}
	first, err := regradeDefinitionsMetadata([]graderRunDefinition{definition})
	require.NoError(t, err)
	definition.Script = "return 2"
	second, err := regradeDefinitionsMetadata([]graderRunDefinition{definition})
	require.NoError(t, err)
	assert.NotEqual(t, first[0].DefinitionDigest, second[0].DefinitionDigest)
	assert.NotEqual(t, first[0].ScriptDigest, second[0].ScriptDigest)
	assert.Equal(t, first[0].ConfigDigest, second[0].ConfigDigest)
	definition.Config = map[string]any{"offset": 2}
	third, err := regradeDefinitionsMetadata([]graderRunDefinition{definition})
	require.NoError(t, err)
	assert.NotEqual(t, second[0].ConfigDigest, third[0].ConfigDigest)
	assert.Equal(t, second[0].RuntimeDigest, third[0].RuntimeDigest)
	var out bytes.Buffer
	require.NoError(t, json.NewEncoder(&out).Encode(third))
	assert.NotContains(t, out.String(), "return 2")
}

func TestRegradeInfrastructureErrorsAreDeterministic(t *testing.T) {
	first := fmt.Errorf("grader failed: %w: random staging path first", &exec.ExitError{})
	second := fmt.Errorf("grader failed: %w: random staging path second", &exec.ExitError{})
	assert.Equal(t, "grader runtime exited with status -1", regradeExecutionError(first))
	assert.Equal(t, regradeExecutionError(first), regradeExecutionError(second))
	assert.Equal(t, "node is required", regradeExecutionError(errors.New("node is required")))
}

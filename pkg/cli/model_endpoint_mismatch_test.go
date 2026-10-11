//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const modelEndpointMismatchTestData = `{"category":"model_endpoint_mismatch","phase":"startup","configured_model":"configured-alias","resolved_model":"resolved-model","wire_api":"responses","wire_api_source":"engine-default","supported_endpoints":["/chat/completions","/v1/messages"],"detail":"The model does not support /responses.","fix":"Set wire_api to chat or select a compatible model."}`

func modelEndpointMismatchTestEvent(data string) string {
	phase := "startup"
	if strings.Contains(data, `"phase":"runtime"`) {
		phase = "runtime"
	}
	return `{"type":"model_endpoint.mismatch","data":` + data + `,"provenance":{"component":"model_endpoint","phase":"` + phase + `","path":"agent/model-endpoint-mismatch.json"}}` + "\n"
}

func TestModelEndpointMismatchAuditFromUnifiedSession(t *testing.T) {
	for _, relative := range []string{"usage/aw_session.jsonl", "aw_session.jsonl"} {
		t.Run(relative, func(t *testing.T) {
			runDir := t.TempDir()
			sessionPath := filepath.Join(runDir, relative)
			require.NoError(t, os.MkdirAll(filepath.Dir(sessionPath), 0o700))
			runtimeData := strings.Replace(modelEndpointMismatchTestData, `"phase":"startup"`, `"phase":"runtime"`, 1)
			runtimeData = strings.TrimSuffix(runtimeData, "}") + `,"model":"request-model","endpoint":"/responses"}`
			require.NoError(t, os.WriteFile(sessionPath, []byte(
				modelEndpointMismatchTestEvent(modelEndpointMismatchTestData)+modelEndpointMismatchTestEvent(runtimeData)), 0o600))

			data, _ := buildLocalAuditData(ProcessedRun{Run: WorkflowRun{LogsPath: runDir}}, LogMetrics{}, nil)
			require.Len(t, data.ModelEndpointMismatches, 2)
			assert.Equal(t, "startup", data.ModelEndpointMismatches[0].Phase)
			assert.Equal(t, "runtime", data.ModelEndpointMismatches[1].Phase)
			assert.Equal(t, "request-model", data.ModelEndpointMismatches[1].Model)
			assert.Equal(t, "/responses", data.ModelEndpointMismatches[1].Endpoint)
			var expected ModelEndpointMismatch
			require.NoError(t, json.Unmarshal([]byte(modelEndpointMismatchTestData), &expected))
			assert.Equal(t, expected, data.ModelEndpointMismatches[0])

			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			var report map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &report))
			var diagnostics []ModelEndpointMismatch
			require.NoError(t, json.Unmarshal(report["model_endpoint_mismatches"], &diagnostics))
			assert.Equal(t, data.ModelEndpointMismatches, diagnostics)

			output := captureStderrForGuardPolicyReportTest(func() { renderConsole(data, runDir) })
			for _, text := range []string{
				"model_endpoint_mismatch: phase=startup", "phase=runtime",
				"configured_model=configured-alias", "resolved_model=resolved-model",
				"wire_api=responses", "wire_api_source=engine-default",
				"supported_endpoints=/chat/completions, /v1/messages",
				"model=request-model endpoint=/responses", "detail: " + expected.Detail, "fix: " + expected.Fix,
			} {
				assert.Contains(t, output, text)
			}
		})
	}
}

func TestModelEndpointMismatchDoesNotInferCauseFromRawArtifacts(t *testing.T) {
	for _, session := range []struct {
		name    string
		content string
	}{
		{"missing", ""},
		{"unrelated", `{"type":"model_routing.outcome","data":{"status":"rejected","failureCode":"unsupported_endpoint"},"provenance":{"component":"agent","phase":"agent"}}`},
		{"corrupt", modelEndpointMismatchTestEvent(modelEndpointMismatchTestData) + "invalid\n"},
	} {
		t.Run(session.name, func(t *testing.T) {
			runDir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(runDir, "agent"), 0o700))
			for _, file := range []string{"model-endpoint-mismatch.json", "awf-routing-outcome.json", "agent-stdio.log"} {
				require.NoError(t, os.WriteFile(filepath.Join(runDir, "agent", file), []byte(modelEndpointMismatchTestData), 0o600))
			}
			if session.content != "" {
				require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_session.jsonl"), []byte(session.content), 0o600))
			}
			data, _ := buildLocalAuditData(ProcessedRun{Run: WorkflowRun{LogsPath: runDir}}, LogMetrics{}, nil)
			assert.Empty(t, data.ModelEndpointMismatches)
			encoded, err := json.Marshal(data)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), `"model_endpoint_mismatches"`)
		})
	}
}

func TestModelEndpointMismatchSessionValidation(t *testing.T) {
	for _, data := range []string{
		`{"category":"other","phase":"startup"}`,
		`{"category":"model_endpoint_mismatch","phase":"other"}`,
		`{"category":"model_endpoint_mismatch","phase":"runtime","supported_endpoints":42}`,
		`null`,
		strings.Replace(modelEndpointMismatchTestData, `"configured_model":"configured-alias",`, "", 1),
		strings.Replace(modelEndpointMismatchTestData, `"fix":"Set wire_api to chat or select a compatible model."`, `"fix":null`, 1),
		strings.Replace(modelEndpointMismatchTestData, `["/chat/completions","/v1/messages"]`, `null`, 1),
		strings.Replace(modelEndpointMismatchTestData, `["/chat/completions","/v1/messages"]`, `[null]`, 1),
		strings.TrimSuffix(modelEndpointMismatchTestData, "}") + `,"model":null}`,
	} {
		attribution := &sessionModelRoutingAttribution{}
		err := decodeSessionModelRoutingEvent([]byte(modelEndpointMismatchTestEvent(data)), 7, attribution)
		require.NoError(t, err)
		assert.Empty(t, attribution.Mismatches)
	}
}

func TestModelEndpointMismatchIgnoresForgedEventsAndKeepsValidDiagnostics(t *testing.T) {
	for _, replacement := range []struct{ from, to string }{
		{`"component":"model_endpoint"`, `"component":"agent"`},
		{`"path":"agent/model-endpoint-mismatch.json"`, `"path":"agent/forged.json"`},
		{`"phase":"startup","path"`, `"phase":"runtime","path"`},
		{`"configured_model":"configured-alias",`, ""},
	} {
		runDir := t.TempDir()
		valid := modelEndpointMismatchTestEvent(modelEndpointMismatchTestData)
		forged := strings.Replace(valid, replacement.from, replacement.to, 1)
		require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_session.jsonl"), []byte(forged+valid+forged), 0o600))
		mismatches := readSessionModelEndpointMismatches(runDir)
		require.Len(t, mismatches, 1)
		assert.Equal(t, "configured-alias", mismatches[0].ConfiguredModel)
	}
}

func TestModelEndpointMismatchUsageSessionTakesPrecedence(t *testing.T) {
	runDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "usage", "aw_session.jsonl"), []byte("{}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_session.jsonl"), []byte(modelEndpointMismatchTestEvent(modelEndpointMismatchTestData)), 0o600))
	assert.Empty(t, readSessionModelEndpointMismatches(runDir))
}

func TestModelEndpointMismatchRejectsSymlinkSession(t *testing.T) {
	for _, target := range []string{"usage", "usage/aw_session.jsonl"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			runDir := filepath.Join(root, "run")
			source := filepath.Join(root, "source")
			require.NoError(t, os.MkdirAll(source, 0o700))
			require.NoError(t, os.MkdirAll(runDir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(source, "aw_session.jsonl"), []byte(modelEndpointMismatchTestEvent(modelEndpointMismatchTestData)), 0o600))
			if target == "usage" {
				require.NoError(t, os.Symlink(source, filepath.Join(runDir, target)))
			} else {
				require.NoError(t, os.MkdirAll(filepath.Join(runDir, "usage"), 0o700))
				require.NoError(t, os.Symlink(filepath.Join(source, "aw_session.jsonl"), filepath.Join(runDir, target)))
			}
			assert.Empty(t, readSessionModelEndpointMismatches(runDir))
		})
	}
}

func TestModelEndpointMismatchConsoleEscapesControlCharacters(t *testing.T) {
	mismatch := ModelEndpointMismatch{
		Phase: "startup", ConfiguredModel: "alias\nforged", ResolvedModel: "model\x1b",
		SupportedEndpoints: []string{"/responses\rforged"}, Detail: "detail\nforged", Fix: "fix\tvalue",
		Model: "request\nforged", Endpoint: "/responses\x1b",
	}
	output := captureStderrForGuardPolicyReportTest(func() { renderConsoleModelEndpointMismatches([]ModelEndpointMismatch{mismatch}) })
	assert.NotContains(t, output, "\x1b")
	assert.NotContains(t, output, "\r")
	assert.NotContains(t, output, "\t")
	assert.NotContains(t, output, "\nforged")
	assert.Contains(t, output, "fix: fix value")
}

func TestModelEndpointMismatchJSONEscapingAndRedactionPreserved(t *testing.T) {
	runDir := t.TempDir()
	var mismatch ModelEndpointMismatch
	require.NoError(t, json.Unmarshal([]byte(modelEndpointMismatchTestData), &mismatch))
	mismatch.Detail = "[REDACTED]\n<script>\x1b"
	mismatch.Fix = "Select \"compatible\" model."
	payload, err := json.Marshal(mismatch)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "aw_session.jsonl"), []byte(modelEndpointMismatchTestEvent(string(payload))), 0o600))
	data, _ := buildLocalAuditData(ProcessedRun{Run: WorkflowRun{LogsPath: runDir}}, LogMetrics{}, nil)
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `[REDACTED]\n\u003cscript\u003e\u001b`)
	assert.NotContains(t, string(encoded), "<script>")
	var decoded AuditData
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.ModelEndpointMismatches, 1)
	assert.Equal(t, mismatch, decoded.ModelEndpointMismatches[0])
}

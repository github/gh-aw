//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureModelsCommandStdout runs the models command with the given arguments and
// returns everything it wrote to stdout.
func captureModelsCommandStdout(t *testing.T, args ...string) string {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer

	// Drain concurrently so dense reports cannot fill the pipe buffer.
	outputChan := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		outputChan <- buf.String()
	}()

	cmd := NewModelsCommand()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	execErr := cmd.Execute()

	require.NoError(t, writer.Close())
	os.Stdout = oldStdout
	require.NoError(t, execErr)

	return <-outputChan
}

// writeModelsLogsFixture creates a logs directory containing a summary, a per-run
// token usage artifact for a run absent from the summary, and an awf-reflect file.
func writeModelsLogsFixture(t *testing.T) string {
	t.Helper()

	logsDir := t.TempDir()

	summaryPayload := map[string]any{
		"runs": []any{
			map[string]any{
				"run_id": 111,
				"token_usage_summary": map[string]any{
					"by_model": map[string]any{
						"claude-sonnet-4.6": map[string]any{
							"provider": "github-copilot",
							"requests": 3,
						},
					},
				},
			},
		},
	}
	summaryBytes, err := json.Marshal(summaryPayload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(logsDir, "summary.json"), summaryBytes, 0o644))

	// Run 111 is already represented in summary.json and must not be counted twice.
	summarizedUsageDir := filepath.Join(logsDir, "run-111", "usage", "agent")
	require.NoError(t, os.MkdirAll(summarizedUsageDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(summarizedUsageDir, "token_usage.jsonl"),
		[]byte(`{"provider":"github-copilot","model":"claude-sonnet-4.6","input_tokens":10,"output_tokens":2}`+"\n"),
		0o644,
	))

	// Run 222 is not in the summary, so its token usage is a new observation.
	newUsageDir := filepath.Join(logsDir, "run-222", "usage", "agent")
	require.NoError(t, os.MkdirAll(newUsageDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(newUsageDir, "token_usage.jsonl"),
		[]byte(`{"provider":"openai","model":"gpt-5.4","input_tokens":20,"output_tokens":5}`+"\n"),
		0o644,
	))

	reflectPath := filepath.Join(logsDir, "run-222", "sandbox", "firewall", "awf-reflect.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(reflectPath), 0o755))
	reflectPayload := map[string]any{
		"endpoints": []any{
			map[string]any{
				"provider": "copilot",
				"models":   []string{"claude-sonnet-4.6"},
			},
		},
	}
	reflectBytes, err := json.Marshal(reflectPayload)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(reflectPath, reflectBytes, 0o644))

	return logsDir
}

// TestModelsCommandJSONOutputIsSingleDocument runs `gh aw models --json` end to end
// against a fixture logs directory and verifies stdout is exactly one JSON payload
// containing alias and observed model data.
func TestModelsCommandJSONOutputIsSingleDocument(t *testing.T) {
	logsDir := writeModelsLogsFixture(t)

	output := captureModelsCommandStdout(t, "--json", "--refresh-observed=false", "--logs-dir", logsDir)

	decoder := json.NewDecoder(strings.NewReader(output))
	var report modelsReport
	require.NoError(t, decoder.Decode(&report), "stdout should contain a valid JSON report")

	// Any trailing content would mean a second payload was printed alongside the report.
	remaining, err := io.ReadAll(decoder.Buffered())
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(remaining)), "stdout should contain a single JSON document")

	require.NotEmpty(t, report.Aliases)
	assert.Empty(t, report.Warnings)

	observed := make(map[string]observedModelRow, len(report.Observed))
	for _, row := range report.Observed {
		observed[row.Provider+"/"+row.Model] = row
	}

	sonnet, ok := observed["github-copilot/claude-sonnet-4.6"]
	require.True(t, ok, "observed models should include the summary model")
	assert.Contains(t, sonnet.Sources, "summary")
	assert.Contains(t, sonnet.Sources, "awf-reflect")
	assert.NotContains(t, sonnet.Sources, "token-usage", "run-111 is already covered by summary.json")
	assert.Equal(t, 4, sonnet.Occurrences, "summary requests plus one awf-reflect sighting")
	assert.NotEmpty(t, sonnet.AliasHints)

	gpt, ok := observed["openai/gpt-5.4"]
	require.True(t, ok, "observed models should include the unsummarized run")
	assert.Contains(t, gpt.Sources, "token-usage")
}

// TestModelsCommandConsoleOutputSections runs the command without --json and verifies
// the human-readable report renders both sections.
func TestModelsCommandConsoleOutputSections(t *testing.T) {
	logsDir := writeModelsLogsFixture(t)

	output := captureModelsCommandStdout(t, "--refresh-observed=false", "--logs-dir", logsDir)

	assert.NotContains(t, output, "Catalog Models")
	assert.Contains(t, output, "Model Aliases")
	assert.Contains(t, output, "Observed Models")
	assert.Contains(t, output, "claude-sonnet-4.6")
	assert.NotContains(t, output, "No observed models found")
}

// TestModelsCommandWithEmptyLogsDir verifies alias data remains available without artifacts.
func TestModelsCommandWithEmptyLogsDir(t *testing.T) {
	output := captureModelsCommandStdout(t, "--json", "--refresh-observed=false", "--logs-dir", filepath.Join(t.TempDir(), "missing"))

	var report modelsReport
	require.NoError(t, json.Unmarshal([]byte(output), &report))
	assert.NotEmpty(t, report.Aliases)
	assert.Empty(t, report.Observed)
	assert.Empty(t, report.Warnings)
}

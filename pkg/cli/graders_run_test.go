package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeGraderRunWorkflow(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workflowDir, "test.md"), []byte(content), 0o600))
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { require.NoError(t, os.Chdir(previous)) })
	return "test"
}

func TestRunGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\ngraders: {}\n---\n")
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "loops",
		Input: bytes.NewBufferString(`{
			"toolCalls":[
				{"name":"view","arguments":{"path":"a"}},
				{"name":"view","arguments":{"path":"a"}}
			],
			"tokenUsageEntries":[],
			"retryEvents":[],
			"artifacts":[]
		}`),
		Output: &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"loops",
		"name":"Loops",
		"value":1,
		"unit":"count",
		"passed":true,
		"status":"pass",
		"source":"builtin",
		"implementation":{"id":"gh-aw/graders","version":1}
	}`, output.String())
}

func TestRunInlineScriptGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  custom-score:
    script: |
      return { value: trace.score, message: "computed" }
    unit: ratio
    direction: higher_is_better
    threshold: 0.5
---
`)
	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "custom-score",
		Input:    bytes.NewBufferString(`{"score":0.75}`),
		Output:   &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"custom-score",
		"name":"custom-score",
		"value":0.75,
		"unit":"ratio",
		"passed":true,
		"status":"pass",
		"source":"inline",
		"implementation":{
			"id":"gh-aw/graders",
			"version":1,
			"digest":"518c37ee83a83874d2added478398c3b391bdbaa7b92f6ea9567a016dd888640"
		},
		"message":"computed"
	}`, output.String())
}

func TestRunOperationalValueGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\ngraders:\n  operational-value:\n    run: .github/graders/test-operational-value.sh\n    config:\n      weight: 0.5\n---\n")
	require.NoError(t, os.Mkdir(".git", 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(".github", "graders"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".github", "graders", "test-operational-value.sh"), []byte(`#!/usr/bin/env bash
set -euo pipefail
[[ $# -eq 0 ]]
[[ "${GH_HOST:-}" == "ghe.example" ]]
payload=$(cat)
[[ "$payload" == '{"schemaVersion":1,"run":{"id":"42"},"event":{"issue":{"number":7}},"config":{"weight":0.5}}' ]]
printf '%s\n' '[{"id":"attainment","value":0.8},{"id":"coverage","value":null}]'
`), 0o700))

	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "operational-value",
		Repo:     "ghe.example/example/repo",
		Input:    bytes.NewBufferString(`{"run":{"id":"42"},"event":{"issue":{"number":7}}}`),
		Output:   &output,
	})
	require.NoError(t, err)
	assert.Equal(t, `[{"id":"attainment","value":0.8},{"id":"coverage","value":null}]`+"\n", output.String())
}

func TestGradersCommandOnlyRegistersRun(t *testing.T) {
	commands := NewGradersCommand().Commands()
	require.Len(t, commands, 1)
	assert.Equal(t, "run", commands[0].Name())
}

func TestValidateOperationalValueMetrics(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		wantErr string
	}{
		{name: "empty array", output: `[]`, wantErr: "non-empty metric array"},
		{name: "empty id", output: `[{"id":" ","value":0.5}]`, wantErr: "non-empty string"},
		{name: "duplicate id", output: `[{"id":"score","value":0.5},{"id":"score","value":null}]`, wantErr: "duplicated"},
		{name: "missing value", output: `[{"id":"score"}]`, wantErr: "must include value"},
		{name: "out of range", output: `[{"id":"score","value":1.1}]`, wantErr: "finite number in [0,1]"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateOperationalValueMetrics([]byte(test.output))
			require.ErrorContains(t, err, test.wantErr)
		})
	}
}

func TestReadGraderPayloadValidation(t *testing.T) {
	_, err := readGraderPayload(bytes.NewBufferString("not-json"), "standard input")
	require.ErrorContains(t, err, "not valid JSON")

	_, err = parseGraderRunID("0")
	require.ErrorContains(t, err, "positive integer")
}

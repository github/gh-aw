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

func TestRunScriptFileGraderFromStdin(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, `---
graders:
  operational-value:
    run: .github/graders/test-operational-value.sh
---
`)
	require.NoError(t, os.Mkdir(".git", 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(".github", "graders"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".github", "graders", "test-operational-value.sh"), []byte(`#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
--definition)
  printf '%s\n' '{"schemaVersion":4,"grader":"operational-value","repository":"example/repo","workflowName":"Test","sourcePath":".github/workflows/test.md","adoption":{"commit":"abc","adoptedAt":"2026-01-01T00:00:00Z"},"operationalValue":"Test direct script execution.","evidence":{"opportunity":"test","assignment":"payload","accepted":"stdin","repositories":["example/repo"],"collection":"test","maturation":"immediate","zeroRule":"none","missingRule":"null"},"primaryMetric":{"id":"score","formula":"payload score","direction":"higher_is_better"},"baseline":{"mode":"attainment-only","value":null,"evidenceCutoff":null,"provenance":[]},"validationExamples":{"sample":{"valid":true}}}'
  ;;
--grade-run)
  [[ "${GH_HOST:-}" == "ghe.example" ]]
  payload=$(cat)
  [[ "$payload" == '{"score":0.8}' ]]
  printf '%s\n' '{"value":0.8,"source":"script-file"}'
  ;;
*) exit 1 ;;
esac
`), 0o700))

	var output bytes.Buffer
	err := runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID,
		GraderID: "operational-value",
		Repo:     "ghe.example/example/repo",
		Input:    bytes.NewBufferString(`{"score":0.8}`),
		Output:   &output,
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":0.8,"source":"script-file"}`, output.String())
}

func TestReadGraderPayloadValidation(t *testing.T) {
	_, err := readGraderPayload(bytes.NewBufferString("not-json"), "standard input")
	require.ErrorContains(t, err, "not valid JSON")

	_, err = parseGraderRunID("0")
	require.ErrorContains(t, err, "positive integer")
}

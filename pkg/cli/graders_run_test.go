package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestStageGraderRuntimeLoadsDependencies(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	directory, err := stageGraderRuntime(t.TempDir())
	require.NoError(t, err)
	flag, err := graderNodePermissionFlag(context.Background(), node)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), graderJSTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, flag, "--allow-fs-read="+directory, "--eval", `
const path = require("node:path");
for (const name of ["trace_graders.cjs", "custom_graders.cjs", "custom_grader_worker.cjs"]) {
  require(path.join(process.argv[1], name));
}
`, directory)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{}
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestRunGraderFromUnifiedSession(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\n---\nTest.\n")
	provenance := `"provenance":{"component":"agent","phase":"agent","path":"agent-session.jsonl","index":0}`
	events := []string{
		`{"type":"tool.execution_start","data":{"toolName":"view","input":{"path":"a"}},` + provenance + `}`,
		`{"type":"tool.execution_start","data":{"toolName":"view","input":{"path":"a"}},` + provenance + `}`,
	}
	for _, input := range []string{strings.Join(events, "\n"), "[" + strings.Join(events, ",") + "]"} {
		var output bytes.Buffer
		require.NoError(t, runGrader(context.Background(), graderRunConfig{
			Workflow: workflowID, GraderID: "loops", Input: strings.NewReader(input), Output: &output,
		}))
		var result struct {
			ID     string
			Value  float64
			Status string
			Source string
		}
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		assert.Equal(t, "loops", result.ID)
		assert.InDelta(t, 1.0, result.Value, 1e-9)
		assert.Equal(t, "pass", result.Status)
		assert.Equal(t, "builtin", result.Source)
	}
}

func TestRunGraderRejectsCustomCode(t *testing.T) {
	for _, entry := range []string{
		"operational-value:\n    run: ../evaluator.sh",
		"loops:\n    script: return 1",
	} {
		t.Run(entry, func(t *testing.T) {
			workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  "+entry+"\n---\nTest.\n")
			var output bytes.Buffer
			err := runGrader(context.Background(), graderRunConfig{
				Workflow: workflowID, GraderID: "loops", Input: strings.NewReader("[]"), Output: &output,
			})
			require.Error(t, err)
			require.Contains(t, err.Error(), "graders")
			assert.Empty(t, output.String())
		})
	}
}

func TestRunOperationalValueJavaScriptFromUnifiedSession(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	workflowID := writeGraderRunWorkflow(t, `---
on: workflow_dispatch
graders:
  operational-value:
    direction: higher_is_better
    threshold: 3
    config:
      offset: 2
    script: |
      return [
        {id: "effect", value: session.length + config.offset},
        {id: "diagnostic", value: null},
      ];
---
Test.
`)
	var output bytes.Buffer
	require.NoError(t, runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID, GraderID: "operational-value", Input: strings.NewReader("[]"), Output: &output,
	}))
	var result struct {
		ID             string
		Source         string
		Value          float64
		Status         string
		Metrics        []map[string]any
		Implementation struct{ Digest string }
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	assert.Equal(t, "operational-value", result.ID)
	assert.Equal(t, "operational-value", result.Source)
	assert.InDelta(t, 2.0, result.Value, 0)
	assert.Equal(t, "fail", result.Status)
	assert.Len(t, result.Metrics, 2)
	assert.Len(t, result.Implementation.Digest, 64)
}

func TestLoadImportedOperationalValueJavaScript(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\nimports:\n  - shared/graders.md\ngraders:\n  operational-value:\n    threshold: 1\n---\nTest.\n")
	require.NoError(t, os.MkdirAll(".github/workflows/shared", 0o755))
	const source = `return [{id: "effect", value: 1}]`
	require.NoError(t, os.WriteFile(".github/workflows/shared/graders.md", []byte("---\ngraders:\n  operational-value:\n    script: |\n      "+source+"\n---\n"), 0o600))
	definition, err := loadGraderRunDefinition(workflowID, "operational-value")
	require.NoError(t, err)
	assert.Equal(t, source, strings.TrimSpace(definition.Script))
	assert.NotNil(t, definition.Threshold)
}

func TestRunCustomGraderFromUnifiedSession(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  custom-count:\n    script: return helpers.createSessionLookups(session).stats.toolCallCount + config.offset\n    config:\n      offset: 3\n---\nTest.\n")
	var output bytes.Buffer
	require.NoError(t, runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID, GraderID: "custom-count", Input: strings.NewReader("[]"), Output: &output,
	}))
	var result struct {
		ID     string
		Value  float64
		Source string
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	assert.Equal(t, "custom-count", result.ID)
	assert.Equal(t, "inline", result.Source)
	assert.InDelta(t, 3.0, result.Value, 1e-9)
}

func TestLoadGraderRunDefinitionResolvesImports(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non:\n  schedule: every 2 days\nimports:\n  - shared/graders.md\ngraders:\n  loops:\n    threshold: 9\n---\nTest.\n")
	require.NoError(t, os.MkdirAll(".github/workflows/shared", 0o755))
	require.NoError(t, os.WriteFile(".github/workflows/shared/graders.md", []byte("---\ngraders:\n  loops:\n    threshold: 2\n  recurrence-determinism: {}\n---\n"), 0o600))
	loops, err := loadGraderRunDefinition(workflowID, "loops")
	require.NoError(t, err)
	assert.InDelta(t, 9.0, *loops.Threshold, 1e-9)
	trajectory, err := loadGraderRunDefinition(workflowID, "recurrence-determinism")
	require.NoError(t, err)
	assert.Equal(t, "builtin", trajectory.Source)
}

func TestReadGraderPayloadValidation(t *testing.T) {
	for _, input := range []string{"", "null", "{}", `{"toolCalls":[]}`, "[null]", "[1]", `[{"type":"x","data":{}}]`, "{invalid", "{}\ninvalid"} {
		_, err := readGraderPayload(strings.NewReader(input), "test")
		require.Error(t, err, input)
	}
	result, err := readGraderPayload(strings.NewReader("[]"), "test")
	require.NoError(t, err)
	assert.JSONEq(t, "[]", string(result))
}

func TestGraderNodePermissionFlag(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	flag, err := graderNodePermissionFlag(context.Background(), node)
	require.NoError(t, err)
	assert.Contains(t, []string{"--permission", "--experimental-permission"}, flag)
}

func TestGraderNodePermissionFlagProbeFailure(t *testing.T) {
	_, err := graderNodePermissionFlag(context.Background(), "/nonexistent/node")
	require.ErrorContains(t, err, "failed to detect Node.js permission support")
}

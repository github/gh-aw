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
for (const name of ["trace_graders.cjs", "custom_graders.cjs", "operational_value_grader.cjs"]) {
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

func TestRunOperationalValueFromUnifiedSession(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  operational-value:\n    run: ./score.sh\n---\nTest.\n")
	require.NoError(t, exec.Command("git", "init", "-q").Run())
	require.NoError(t, os.WriteFile(".github/workflows/score.sh", []byte("#!/bin/bash\n[[ -f \"$1\" && \"$1\" == \"$GH_AW_SESSION_FILE\" ]]\nprintf '[{\"id\":\"effect\",\"value\":2},{\"id\":\"diagnostic\",\"value\":null}]'\n"), 0o600))
	var output bytes.Buffer
	require.NoError(t, runGrader(context.Background(), graderRunConfig{
		Workflow: workflowID, GraderID: "operational-value", Input: strings.NewReader("[]"), Output: &output,
	}))
	var result struct {
		ID      string
		Source  string
		Value   float64
		Metrics []map[string]any
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	assert.Equal(t, "operational-value", result.ID)
	assert.Equal(t, "operational-value", result.Source)
	assert.InDelta(t, 2.0, result.Value, 0)
	assert.Len(t, result.Metrics, 2)
}

func TestLoadImportedOperationalValueDeclaringPath(t *testing.T) {
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\nimports:\n  - shared/graders.md\ngraders:\n  operational-value:\n    threshold: 1\n---\nTest.\n")
	require.NoError(t, exec.Command("git", "init", "-q").Run())
	require.NoError(t, os.MkdirAll(".github/workflows/shared", 0o755))
	require.NoError(t, os.WriteFile(".github/workflows/shared/graders.md", []byte("---\ngraders:\n  operational-value:\n    run: ./score.sh\n---\n"), 0o600))
	const source = "#!/bin/bash\nprintf '[{\"id\":\"effect\",\"value\":1}]'\n"
	require.NoError(t, os.WriteFile(".github/workflows/shared/score.sh", []byte(source), 0o600))
	definition, err := loadGraderRunDefinition(workflowID, "operational-value")
	require.NoError(t, err)
	assert.Equal(t, source, definition.ShellScript)
	require.NoError(t, os.WriteFile(".github/workflows/score.sh", []byte("#!/bin/bash\nprintf '[{\"id\":\"override\",\"value\":2}]'\n"), 0o600))
	require.NoError(t, os.WriteFile(".github/workflows/test.md", []byte("---\non: workflow_dispatch\nimports:\n  - shared/graders.md\ngraders:\n  operational-value:\n    run: ./score.sh\n---\nTest.\n"), 0o600))
	definition, err = loadGraderRunDefinition(workflowID, "operational-value")
	require.NoError(t, err)
	assert.Contains(t, definition.ShellScript, "override")
}

func TestOperationalValueCommandSessionFileContract(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq unavailable")
	}
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\ngraders:\n  operational-value:\n    run: ./score.sh\n    config:\n      offset: 3\n---\nTest.\n")
	require.NoError(t, exec.Command("git", "init", "-q").Run())
	const source = "#!/bin/bash\njq -s --argjson config \"$GH_AW_GRADER_CONFIG\" '[{id:\"count\",value:(length + $config.offset)}]' \"$1\"\n"
	require.NoError(t, os.WriteFile(".github/workflows/score.sh", []byte(source), 0o600))
	event := `{"type":"tool.execution_complete","data":{"success":true},"provenance":{"component":"agent","phase":"agent","path":"session.jsonl","index":0}}`
	for _, input := range []string{event + "\n", "[" + event + "]"} {
		cmd := newGradersRunCommand()
		cmd.SetArgs([]string{workflowID, "operational-value"})
		cmd.SetIn(strings.NewReader(input))
		var output bytes.Buffer
		cmd.SetOut(&output)
		require.NoError(t, cmd.Execute())
		var result struct {
			Value  float64
			Source string
		}
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		assert.InDelta(t, 4.0, result.Value, 0)
		assert.Equal(t, "operational-value", result.Source)
	}
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
	workflowID := writeGraderRunWorkflow(t, "---\non: workflow_dispatch\nimports:\n  - shared/graders.md\ngraders:\n  loops:\n    threshold: 9\n---\nTest.\n")
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

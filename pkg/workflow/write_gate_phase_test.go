//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWriteGatePhasePrecedesCredentialsAndHandlers(t *testing.T) {
	source := `---
on: workflow_dispatch
engine: claude
strict: false
safe-outputs:
  github-app:
    app-id: "${{ vars.APP_ID }}"
    private-key: "${{ secrets.APP_KEY }}"
  add-comment:
jobs:
  safe_outputs:
    pre-steps:
      - name: Before downloads
        run: echo prepare
    gate-steps:
      - id: check_policy
        name: Check policy
        run: echo '{"decision":"allow","reason":"policy_allowed"}' > "$GH_AW_WRITE_DECISIONS"
---
Read the issue.
`
	compiler := NewCompiler()
	path := filepath.Join(t.TempDir(), "gate.md")
	data, err := compiler.ParseWorkflowString(source, path)
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, path)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(output), &document))
	job := document["jobs"].(map[string]any)["safe_outputs"].(map[string]any)
	steps := job["steps"].([]any)
	indices := map[string]int{}
	for index, raw := range steps {
		step := raw.(map[string]any)
		if id, ok := step["id"].(string); ok {
			indices[id] = index
		}
	}
	for _, id := range []string{"initialize_write_gate", "check_policy", "write_gate", "safe-outputs-app-token", "process_safe_outputs"} {
		require.Contains(t, indices, id)
	}
	require.Less(t, indices["initialize_write_gate"], indices["check_policy"])
	require.Less(t, indices["check_policy"], indices["write_gate"])
	require.Less(t, indices["write_gate"], indices["safe-outputs-app-token"])
	require.Less(t, indices["safe-outputs-app-token"], indices["process_safe_outputs"])
	for _, id := range []string{"safe-outputs-app-token", "process_safe_outputs"} {
		condition := steps[indices[id]].(map[string]any)["if"].(string)
		require.Contains(t, condition, "steps.write_gate.outcome == 'success'")
		require.Contains(t, condition, "steps.normalize_write_decisions.outcome == 'success'")
		require.Contains(t, condition, "steps.normalize_write_decisions.outputs.decision == 'allow'")
		require.Contains(t, condition, "steps.normalize_write_decisions.outputs.decision == 'partial'")
	}
	require.Equal(t, "always()", steps[indices["write_gate"]].(map[string]any)["if"])
	require.Contains(t, job["outputs"].(map[string]any)["write_gate"], "|| 'deny'")
}

func TestWriteGateImportsCannotBeRemoved(t *testing.T) {
	imported := `{"safe_outputs":{"gate-steps":[{"id":"imported_gate","run":"echo imported"}]}}`
	for _, name := range []string{"safe_outputs", "safe-outputs"} {
		jobs := map[string]any{name: map[string]any{"gate-steps": []any{map[string]any{"id": "main_gate", "run": "echo main"}}}}
		merged, err := NewCompiler().mergeJobsFromYAMLImports(jobs, imported)
		require.NoError(t, err)
		gates := merged["safe_outputs"].(map[string]any)["gate-steps"].([]any)
		require.Equal(t, "imported_gate", gates[0].(map[string]any)["id"])
		require.Equal(t, "main_gate", gates[1].(map[string]any)["id"])
		for _, malformed := range []any{nil, false, "disabled"} {
			_, err := NewCompiler().mergeJobsFromYAMLImports(map[string]any{name: map[string]any{"gate-steps": malformed}}, imported)
			require.ErrorContains(t, err, "cannot be removed")
		}
		merged, err = NewCompiler().mergeJobsFromYAMLImports(map[string]any{name: map[string]any{"gate-steps": []any{}}}, imported)
		require.NoError(t, err)
		require.Len(t, merged["safe_outputs"].(map[string]any)["gate-steps"].([]any), 1)
	}
}

func TestWriteGateRepositoryRequirement(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, RepoConfigFileName), []byte(`{"require_write_gate":true}`), 0600))
	compiler := NewCompiler()
	compiler.gitRoot = root
	path := filepath.Join(root, ".github", "workflows", "writer.md")
	data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nengine: claude\nsafe-outputs:\n  create-issue:\n---\nWrite an issue.", path)
	require.NoError(t, err)
	require.ErrorContains(t, compiler.validateWriteGateConfiguration(data), "requires a write gate")
	data.Jobs = map[string]any{"safe_outputs": map[string]any{"gate-steps": []any{map[string]any{"run": "echo deny"}}}}
	require.NoError(t, compiler.validateWriteGateConfiguration(data))
	data.Jobs = map[string]any{"agent": map[string]any{"gate-steps": []any{map[string]any{"run": "echo deny"}}}}
	require.ErrorContains(t, compiler.validateWriteGateConfiguration(data), "only in safe_outputs")
}

func TestWriteGateDryRunDoesNotExecuteAuthoredGate(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{DryRun: true, Jobs: map[string]any{"safe_outputs": map[string]any{"gate-steps": []any{map[string]any{"run": "untrusted-gate-script"}}}}}
	steps, err := compiler.buildWriteGatePhaseSteps(data)
	require.NoError(t, err)
	output := strings.Join(steps, "")
	require.NotContains(t, output, "untrusted-gate-script")
	require.Contains(t, output, "GH_AW_WRITE_GATE_DRY_RUN: \"true\"")
	require.Contains(t, output, "Validate write gate verdict")
}

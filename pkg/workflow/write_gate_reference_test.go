package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestWriteGateReferenceCompiles(t *testing.T) {
	dir := testutil.TempDir(t, "write-gate-reference")
	example := filepath.Join("..", "..", "docs", "examples", "write-gate")
	workflows := filepath.Join(dir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(filepath.Join(workflows, "shared"), 0700))
	for source, target := range map[string]string{
		"shared.md":   filepath.Join(workflows, "shared", "control-write-gate.md"),
		"workflow.md": filepath.Join(workflows, "reference.md"),
	} {
		content, err := os.ReadFile(filepath.Join(example, source))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(target, content, 0600))
	}
	compiler := NewCompiler(WithVersion("v0.0.0"))
	compiler.gitRoot = dir
	compiler.SetActionMode(ActionModeDev)
	require.NoError(t, compiler.CompileWorkflow(filepath.Join(workflows, "reference.md")))
	output, err := os.ReadFile(filepath.Join(workflows, "reference.lock.yml"))
	require.NoError(t, err)
	require.Contains(t, string(output), "Evaluate control repository write policy")
	require.Contains(t, string(output), "GH_AW_PENDING_WRITES")
	require.Contains(t, string(output), "GH_AW_WRITE_DECISIONS")
	require.Contains(t, string(output), "CONTROL_WORKFLOW_SHA: ${{ github.workflow_sha }}")
	var document struct {
		Jobs map[string]struct {
			Steps []struct {
				ID   string         `yaml:"id"`
				Name string         `yaml:"name"`
				Env  map[string]any `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(output, &document))
	preparation, policy, verification, verdict, handler := -1, -1, -1, -1, -1
	for index, step := range document.Jobs["safe_outputs"].Steps {
		if step.ID == "pending_writes" {
			preparation = index
			require.Equal(t, "/tmp/gh-aw/pending-writes.json", step.Env["GH_AW_PENDING_WRITES"])
		}
		if step.Name == "Evaluate control repository write policy" {
			policy = index
		}
		if step.ID == "verify_pending_writes" {
			verification = index
			require.Equal(t, "${{ steps.pending_writes.outputs.sha256 }}", step.Env["GH_AW_PENDING_WRITES_SHA256"])
			require.Equal(t, "${{ steps.pending_writes.outputs.pending_write_inputs_sha256 }}", step.Env["GH_AW_PENDING_WRITE_INPUTS_SHA256"])
		}
		if step.ID == "write_gate" {
			verdict = index
		}
		if step.ID == "process_safe_outputs" {
			handler = index
			require.Equal(t, "${{ steps.pending_writes.outputs.sha256 }}", step.Env["GH_AW_PENDING_WRITES_SHA256"])
			require.Equal(t, "${{ steps.write_gate.outputs.write_gate_decisions_sha256 }}", step.Env["GH_AW_WRITE_GATE_DECISIONS_SHA256"])
		}
	}
	require.NotEqual(t, -1, preparation, "the example needs the compiler-owned manifest producer, not only a variable name in its script")
	require.NotEqual(t, -1, policy)
	require.NotEqual(t, -1, verification)
	require.NotEqual(t, -1, verdict)
	require.NotEqual(t, -1, handler)
	require.Less(t, preparation, policy)
	require.Less(t, policy, verification)
	require.Less(t, verification, verdict)
	require.Less(t, verdict, handler)
}

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
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
}

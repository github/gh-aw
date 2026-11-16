package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalValueJavaScriptCompileBoundary(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", root, "init", "-q").Run())
	file := filepath.Join(root, "test.md")
	const source = `return [{id: "recorded-outcome", value: 1}, {id: "diagnostic", value: null}]`
	require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nengine: copilot\nstrict: false\ngraders:\n  operational-value:\n    script: |\n      "+source+"\n---\nTest.\n"), 0o600))
	compiler := NewCompiler(WithVersion("dev"))
	require.NoError(t, compiler.CompileWorkflow(file))
	lock, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
	require.NoError(t, err)
	conclusion := extractJobSection(string(lock), "conclusion")
	custom := extractJobSection(string(lock), "graders")
	assert.NotContains(t, conclusion, "recorded-outcome")
	assert.Contains(t, conclusion, `"customGraders":["operational-value"]`)
	assert.Contains(t, custom, `"source":"operational-value"`)
	assert.Contains(t, custom, "recorded-outcome")
	assert.NotContains(t, custom, "shellScript")
	assert.NotContains(t, custom, `"run"`)
	assert.NotContains(t, custom, "Checkout repository")
}

func TestOperationalValueRunRejectedAtCompileBoundary(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "authored", true: "imported"}[imported], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, exec.Command("git", "-C", root, "init", "-q").Run())
			directory := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(filepath.Join(directory, "shared"), 0o755))
			declaration := "graders:\n  operational-value:\n    run: ./score.sh\n"
			if imported {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "shared", "grader.md"), []byte("---\n"+declaration+"---\n"), 0o600))
				declaration = "imports:\n  - shared/grader.md\n"
			}
			file := filepath.Join(directory, "test.md")
			require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nengine: copilot\nstrict: false\n"+declaration+"---\nTest.\n"), 0o600))
			err := NewCompiler(WithVersion("dev")).CompileWorkflow(file)
			require.ErrorContains(t, err, "graders.operational-value.run has been removed")
			assert.Contains(t, err.Error(), "graders.operational-value.script")
		})
	}
}

func TestOperationalValueRunRejectedWhenDisabled(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		_, err := ParseGradersFromFrontmatter(map[string]any{"graders": map[string]any{
			"operational-value": map[string]any{"enabled": enabled == "true", "run": "./score.sh"},
		}})
		require.ErrorContains(t, err, "graders.operational-value.run has been removed")
	}
}

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
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
	stderr := captureStderr(func() {
		require.NoError(t, compiler.CompileWorkflow(file))
	})
	assert.NotContains(t, stderr, "graders.operational-value.run is deprecated")
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

func TestLegacyOperationalValueGraderCompileBoundary(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "authored", true: "imported"}[imported], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, exec.Command("git", "-C", root, "init", "-q").Run())
			directory := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(filepath.Join(directory, "shared"), 0o755))
			declaration := "graders:\n  operational-value:\n    run: ./score.sh\n"
			sourcePath := filepath.Join(directory, "score.sh")
			if imported {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "shared", "grader.md"), []byte("---\n"+declaration+"---\n"), 0o600))
				declaration = "imports:\n  - shared/grader.md\n"
				sourcePath = filepath.Join(directory, "shared", "score.sh")
			}
			const source = "#!/bin/bash\nprintf '[{\"id\":\"recorded-outcome\",\"value\":1}]'\n"
			require.NoError(t, os.WriteFile(sourcePath, []byte(source), 0o600))
			file := filepath.Join(directory, "test.md")
			require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nengine: copilot\nstrict: false\n"+declaration+"---\nTest.\n"), 0o600))
			compiler := NewCompiler(WithVersion("dev"))
			stderr := captureStderr(func() {
				require.NoError(t, compiler.CompileWorkflow(file))
			})
			assert.GreaterOrEqual(t, compiler.GetWarningCount(), 1)
			assert.Contains(t, stderr, "graders.operational-value.run is deprecated")
			assert.Contains(t, stderr, "automatic shell-to-JavaScript translation is not supported")
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
			require.NoError(t, err)
			conclusion := extractJobSection(string(lock), "conclusion")
			custom := extractJobSection(string(lock), "graders")
			assert.NotContains(t, conclusion, "recorded-outcome")
			assert.NotContains(t, conclusion, "score.sh")
			assert.NotContains(t, conclusion, "shellScript")
			assert.Contains(t, conclusion, `"customGraders":["operational-value"]`)
			assert.Contains(t, custom, `"source":"operational-value"`)
			assert.Contains(t, custom, "recorded-outcome")
			assert.Contains(t, custom, "contents: read")
			assert.Contains(t, custom, "needs:\n      - activation\n      - conclusion")
			assert.NotContains(t, custom, "Checkout repository")
			assert.NotContains(t, custom, "persist-credentials: true")
			assert.NotContains(t, custom, "environment:")
			assert.NotContains(t, custom, "id-token:")
			job, ok := compiler.jobManager.GetJob("conclusion")
			require.True(t, ok)
			assert.NotContains(t, job.Needs, "graders")
			var compiled struct {
				Jobs map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal(lock, &compiled))
			assert.Equal(t, map[string]string{"contents": "read"}, compiled.Jobs["graders"].Permissions)
		})
	}
}

func TestOperationalValueGraderCompilationRejectsWrites(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", root, "init", "-q").Run())
	file := filepath.Join(root, "test.md")
	for _, enabled := range []string{"true", "false"} {
		require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nstrict: false\ngraders:\n  operational-value:\n    enabled: "+enabled+"\n    run: ./score.sh\n---\nTest.\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "score.sh"), []byte("#!/bin/bash\nprintf changed > \"$1\"\n"), 0o600))
		require.ErrorContains(t, NewCompiler(WithVersion("dev")).CompileWorkflow(file), "may write a file")
	}
}

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

func TestOperationalValueJavaScriptGraderCompileBoundary(t *testing.T) {
	for _, imported := range []bool{false, true} {
		t.Run(map[bool]string{false: "authored", true: "imported"}[imported], func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, exec.Command("git", "-C", root, "init", "-q").Run())
			directory := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(filepath.Join(directory, "shared"), 0o755))
			declaration := "graders:\n  operational-value:\n    script: return session.length\n"
			if imported {
				require.NoError(t, os.WriteFile(filepath.Join(directory, "shared", "grader.md"), []byte("---\n"+declaration+"---\n"), 0o600))
				declaration = "imports:\n  - shared/grader.md\n"
			}
			file := filepath.Join(directory, "test.md")
			require.NoError(t, os.WriteFile(file, []byte("---\non: workflow_dispatch\nengine: copilot\nstrict: false\n"+declaration+"---\nTest.\n"), 0o600))
			compiler := NewCompiler(WithVersion("dev"))
			require.NoError(t, compiler.CompileWorkflow(file))
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(file))
			require.NoError(t, err)
			conclusion := extractJobSection(string(lock), "conclusion")
			custom := extractJobSection(string(lock), "graders")
			assert.NotContains(t, conclusion, "return session.length")
			assert.Contains(t, conclusion, `"customGraders":["operational-value"]`)
			assert.Contains(t, custom, `"source":"inline"`)
			assert.Contains(t, custom, `"script":"return session.length"`)
			assert.NotContains(t, custom, "shellScript")
			assert.NotContains(t, custom, `"run":`)
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

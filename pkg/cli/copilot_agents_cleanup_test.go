//go:build !integration

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotAgentCleanup(t *testing.T) {
	type logEntry struct {
		path, label string
		plain       bool
	}
	tests := []struct {
		name    string
		cleanup func(bool) error
		files   []string
		logs    []logEntry
	}{
		{
			name: "old prompt",
			cleanup: func(verbose bool) error {
				return cleanupOldPromptFile("old.prompt.md", verbose)
			},
			files: []string{".github/prompts/old.prompt.md"},
			logs:  []logEntry{{path: ".github/prompts/old.prompt.md", label: "Removed old prompt file"}},
		},
		{
			name:    "designer skill",
			cleanup: deleteAgenticWorkflowDesignerSkillDir,
			files:   []string{".github/skills/agentic-workflow-designer/nested/SKILL.md"},
			logs:    []logEntry{{path: ".github/skills/agentic-workflow-designer", label: "Removed legacy skill directory"}},
		},
		{
			name:    "setup agent and prompt",
			cleanup: deleteSetupAgenticWorkflowsAgent,
			files: []string{
				".github/agents/setup-agentic-workflows.agent.md",
				".github/prompts/setup-agentic-workflows.prompt.md",
			},
			logs: []logEntry{
				{path: ".github/agents/setup-agentic-workflows.agent.md", label: "Removed setup-agentic-workflows agent", plain: true},
				{path: ".github/prompts/setup-agentic-workflows.prompt.md", label: "Removed old prompt file"},
			},
		},
		{
			name:    "setup prompt without agent",
			cleanup: deleteSetupAgenticWorkflowsAgent,
			files:   []string{".github/prompts/setup-agentic-workflows.prompt.md"},
			logs:    []logEntry{{path: ".github/prompts/setup-agentic-workflows.prompt.md", label: "Removed old prompt file"}},
		},
		{
			name:    "templates and empty parent",
			cleanup: deleteOldTemplateFiles,
			files:   []string{"pkg/cli/templates/create-agentic-workflow.md"},
			logs: []logEntry{
				{path: "pkg/cli/templates/create-agentic-workflow.md", label: "Removed old template file"},
				{path: "pkg/cli/templates", label: "Removed empty templates directory"},
			},
		},
		{
			name:    "legacy files",
			cleanup: deleteLegacyAgentFiles,
			files: []string{
				".github/agents/create-agentic-workflow.agent.md",
				".github/aw/upgrade-agentic-workflow.md",
			},
			logs: []logEntry{
				{path: ".github/agents/create-agentic-workflow.agent.md", label: "Removed old agents file", plain: true},
				{path: ".github/aw/upgrade-agentic-workflow.md", label: "Removed old aw file", plain: true},
			},
		},
	}

	for _, tt := range tests {
		for _, verbose := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/verbose=%t", tt.name, verbose), func(t *testing.T) {
				dir := testutil.TempDir(t, "test-*")
				t.Chdir(dir)
				runCleanup := func() string {
					return testutil.CaptureStderr(t, func() {
						assert.NoError(t, tt.cleanup(verbose))
					})
				}
				assert.Empty(t, runCleanup(), "cleanup outside a git repository should be skipped")
				require.NoError(t, exec.Command("git", "init").Run())
				assert.Empty(t, runCleanup(), "missing paths should be skipped")

				for _, file := range tt.files {
					path := filepath.Join(dir, file)
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
					require.NoError(t, os.WriteFile(path, []byte("legacy content"), 0644))
				}

				output := runCleanup()
				for _, file := range tt.files {
					assert.NoFileExists(t, filepath.Join(dir, file))
				}
				if verbose {
					var expected []string
					for _, entry := range tt.logs {
						message := entry.label + ": " + filepath.Join(dir, entry.path)
						if !entry.plain {
							message = console.FormatInfoMessage(message)
						}
						expected = append(expected, message)
					}
					assert.ElementsMatch(t, expected, strings.Split(strings.TrimSuffix(output, "\n"), "\n"))
				} else {
					assert.Empty(t, output)
				}
				assert.Empty(t, runCleanup(), "repeated cleanup should be silent")
			})
		}
	}
}

func TestRemoveFileIfExists(t *testing.T) {
	for _, kind := range []string{"missing", "file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(testutil.TempDir(t, "test-*"), "old")
			switch kind {
			case "file":
				require.NoError(t, os.WriteFile(path, []byte("legacy content"), 0644))
			case "directory":
				require.NoError(t, os.Mkdir(path, 0755))
			}
			output := testutil.CaptureStderr(t, func() {
				removed, err := removeFileIfExists(path, "old file", "removed old file", true)
				require.NoError(t, err)
				assert.Equal(t, kind == "file", removed)
			})
			if kind == "file" {
				assert.NoFileExists(t, path)
				assert.Equal(t, "removed old file\n", output)
			} else {
				assert.Empty(t, output)
			}
			if kind == "directory" {
				assert.DirExists(t, path)
			}
		})
	}
}

func TestRemoveCleanupPathError(t *testing.T) {
	path := filepath.Join(testutil.TempDir(t, "test-*"), "old.md")
	removeErr := &os.PathError{Op: "remove", Path: path, Err: os.ErrPermission}
	output := testutil.CaptureStderr(t, func() {
		err := removeCleanupPath(path, "old template file old.md", "should not be logged", true, func(got string) error {
			assert.Equal(t, path, got)
			return removeErr
		})
		require.EqualError(t, err, "failed to remove old template file old.md: "+removeErr.Error())
		assert.ErrorIs(t, err, removeErr)
	})
	assert.Empty(t, output)
}

func TestDeleteOldTemplateFilesPreservesDirectory(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%t", empty), func(t *testing.T) {
			dir := testutil.TempDir(t, "test-*")
			t.Chdir(dir)
			require.NoError(t, exec.Command("git", "init").Run())
			templatesDir := filepath.Join(dir, "pkg", "cli", "templates")
			require.NoError(t, os.MkdirAll(templatesDir, 0755))
			if !empty {
				require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "create-agentic-workflow.md"), []byte("old"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(templatesDir, "custom.md"), []byte("keep"), 0644))
			}

			require.NoError(t, deleteOldTemplateFiles(false))
			assert.DirExists(t, templatesDir)
			if !empty {
				assert.NoFileExists(t, filepath.Join(templatesDir, "create-agentic-workflow.md"))
				content, err := os.ReadFile(filepath.Join(templatesDir, "custom.md"))
				require.NoError(t, err)
				assert.Equal(t, "keep", string(content))
			}
		})
	}
}

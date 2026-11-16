//go:build !integration

package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileWorkflows_ShellScriptResourcesIncludeMCPScripts(t *testing.T) {
	tmpDir := t.TempDir()
	require.NoError(t, initTestGitRepo(tmpDir))

	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0o755))

	workflowContent := `---
name: Test Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
strict: false
mcp-scripts:
  inspect:
    description: Inspect repository state
    run: |
      git status --short
graders: {}
---

# Test Workflow

This is a test workflow exercising frontmatter shell script resources.
`
	require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "test.md"), []byte(workflowContent), 0o644))

	oldWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	config := CompileConfig{
		MarkdownFiles: []string{"test"},
		NoEmit:        true,
	}

	workflowDataList, err := CompileWorkflows(context.Background(), config)
	require.NoError(t, err)
	require.Len(t, workflowDataList, 1)

	resources := workflowDataList[0].ShellScriptResources()

	var mcpScriptsFound bool
	for _, resource := range resources {
		switch resource.Name {
		case "mcp-scripts.inspect":
			mcpScriptsFound = true
			assert.Equal(t, "git status --short\n", resource.Script)
			assert.Equal(t, "test", resource.Source)
			assert.Equal(t, "bash", resource.Shell)
		}
	}

	assert.Len(t, resources, 1)
	assert.True(t, mcpScriptsFound, "expected mcp-scripts.inspect to be present in ShellScriptResources, got %+v", resources)
}

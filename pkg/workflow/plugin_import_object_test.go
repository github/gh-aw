//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginImportWithObjectFormat(t *testing.T) {
	tmpDir := t.TempDir()

	// Create shared plugins file with object format
	sharedPlugins := `---
plugins:
  - github/imported-plugin1
  - id: github/imported-mcp-plugin
    mcp:
      env:
        IMPORTED_KEY: ${{ secrets.IMPORTED_KEY }}
---
`
	sharedFile := filepath.Join(tmpDir, "shared-plugins.md")
	err := os.WriteFile(sharedFile, []byte(sharedPlugins), 0644)
	require.NoError(t, err, "Failed to write shared plugins file")

	// Create main workflow that imports the shared plugins
	mainWorkflow := `---
engine: copilot
on: workflow_dispatch
permissions:
  issues: read
  pull-requests: read
  contents: read
imports:
  - shared-plugins.md
plugins:
  - github/top-level-plugin
  - id: github/top-level-mcp-plugin
    mcp:
      env:
        TOP_KEY: ${{ secrets.TOP_KEY }}
---

Test plugin imports with object format
`
	mainFile := filepath.Join(tmpDir, "test-workflow.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main workflow file")

	// Compile workflow
	compiler := workflow.NewCompiler()
	err = compiler.CompileWorkflow(mainFile)
	require.NoError(t, err, "Compilation should succeed")

	// Read generated lock file
	lockFile := strings.Replace(mainFile, ".md", ".lock.yml", 1)
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockContent := string(content)

	// Verify all plugins are installed (imported + top-level)
	assert.Contains(t, lockContent, "copilot plugin install github/imported-plugin1",
		"Should install imported plugin1")
	assert.Contains(t, lockContent, "copilot plugin install github/imported-mcp-plugin",
		"Should install imported MCP plugin")
	assert.Contains(t, lockContent, "copilot plugin install github/top-level-plugin",
		"Should install top-level plugin")
	assert.Contains(t, lockContent, "copilot plugin install github/top-level-mcp-plugin",
		"Should install top-level MCP plugin")

	// Verify MCP environment variables are in the gateway step
	assert.Contains(t, lockContent, "Start MCP gateway",
		"Should have MCP gateway step")

	// Extract the MCP gateway section
	startIdx := strings.Index(lockContent, "Start MCP gateway")
	require.Positive(t, startIdx, "MCP gateway step should exist")

	endIdx := strings.Index(lockContent[startIdx:], "- name:")
	if endIdx == -1 {
		endIdx = len(lockContent)
	} else {
		endIdx = startIdx + endIdx
	}
	mcpGatewaySection := lockContent[startIdx:endIdx]

	// Verify top-level MCP env vars are present
	// (imported plugin MCP configs would be defined in the shared file's own execution,
	// not merged into the main workflow's MCP gateway)
	assert.Contains(t, mcpGatewaySection, "TOP_KEY: ${{ secrets.TOP_KEY }}",
		"Should contain top-level MCP environment variable")
}

//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileWorkflowWithPluginImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared plugins file
	sharedPluginsPath := filepath.Join(tempDir, "shared-plugins.md")
	sharedPluginsContent := `---
on: push
plugins:
  - github/plugin-one
  - github/plugin-two
---
`
	require.NoError(t, os.WriteFile(sharedPluginsPath, []byte(sharedPluginsContent), 0644),
		"Failed to write shared plugins file")

	// Create a workflow file that imports the shared plugins
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-plugins.md
---

# Test Workflow

This is a test workflow with imported plugins.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644),
		"Failed to write workflow file")

	// Compile the workflow
	compiler := workflow.NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath),
		"CompileWorkflow should succeed")

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err, "Failed to read lock file")

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains the imported plugins
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-one",
		"Expected workflow to install plugin-one from import")
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-two",
		"Expected workflow to install plugin-two from import")
}

func TestCompileWorkflowWithPluginImportsAndTopLevelPlugins(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared plugins file
	sharedPluginsPath := filepath.Join(tempDir, "shared-plugins.md")
	sharedPluginsContent := `---
on: push
plugins:
  - github/imported-plugin
---
`
	require.NoError(t, os.WriteFile(sharedPluginsPath, []byte(sharedPluginsContent), 0644),
		"Failed to write shared plugins file")

	// Create a workflow file that imports plugins and defines its own
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-plugins.md
plugins:
  - github/top-level-plugin
---

# Test Workflow

This workflow has both imported and top-level plugins.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644),
		"Failed to write workflow file")

	// Compile the workflow
	compiler := workflow.NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath),
		"CompileWorkflow should succeed")

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err, "Failed to read lock file")

	workflowData := string(lockFileContent)

	// Verify that both imported and top-level plugins are included
	assert.Contains(t, workflowData, "copilot plugin install github/imported-plugin",
		"Expected workflow to install imported-plugin from import")
	assert.Contains(t, workflowData, "copilot plugin install github/top-level-plugin",
		"Expected workflow to install top-level-plugin from main workflow")
}

func TestCompileWorkflowWithMultiplePluginImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create first shared plugins file
	sharedPlugins1Path := filepath.Join(tempDir, "plugins-1.md")
	sharedPlugins1Content := `---
on: push
plugins:
  - github/plugin-a
  - github/plugin-b
---
`
	require.NoError(t, os.WriteFile(sharedPlugins1Path, []byte(sharedPlugins1Content), 0644),
		"Failed to write first plugins file")

	// Create second shared plugins file
	sharedPlugins2Path := filepath.Join(tempDir, "plugins-2.md")
	sharedPlugins2Content := `---
on: push
plugins:
  - github/plugin-c
---
`
	require.NoError(t, os.WriteFile(sharedPlugins2Path, []byte(sharedPlugins2Content), 0644),
		"Failed to write second plugins file")

	// Create a workflow file that imports both plugin files
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - plugins-1.md
  - plugins-2.md
---

# Test Workflow

This workflow imports plugins from multiple files.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644),
		"Failed to write workflow file")

	// Compile the workflow
	compiler := workflow.NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath),
		"CompileWorkflow should succeed")

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err, "Failed to read lock file")

	workflowData := string(lockFileContent)

	// Verify all plugins from both imports are included
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-a",
		"Expected workflow to install plugin-a")
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-b",
		"Expected workflow to install plugin-b")
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-c",
		"Expected workflow to install plugin-c")
}

func TestCompileWorkflowWithDuplicatePluginImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create first shared plugins file with duplicate plugin
	sharedPlugins1Path := filepath.Join(tempDir, "plugins-1.md")
	sharedPlugins1Content := `---
on: push
plugins:
  - github/shared-plugin
  - github/plugin-a
---
`
	require.NoError(t, os.WriteFile(sharedPlugins1Path, []byte(sharedPlugins1Content), 0644),
		"Failed to write first plugins file")

	// Create second shared plugins file with the same shared plugin
	sharedPlugins2Path := filepath.Join(tempDir, "plugins-2.md")
	sharedPlugins2Content := `---
on: push
plugins:
  - github/shared-plugin
  - github/plugin-b
---
`
	require.NoError(t, os.WriteFile(sharedPlugins2Path, []byte(sharedPlugins2Content), 0644),
		"Failed to write second plugins file")

	// Create a workflow file that imports both files and also defines the duplicate plugin
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - plugins-1.md
  - plugins-2.md
plugins:
  - github/shared-plugin
  - github/top-level-plugin
---

# Test Workflow

This workflow has duplicate plugins across imports and top-level.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644),
		"Failed to write workflow file")

	// Compile the workflow
	compiler := workflow.NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath),
		"CompileWorkflow should succeed")

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err, "Failed to read lock file")

	workflowData := string(lockFileContent)

	// Count occurrences of the shared plugin installation
	installCmd := "copilot plugin install github/shared-plugin"
	count := strings.Count(workflowData, installCmd)

	// Verify the shared plugin is only installed once (deduplicated)
	assert.Equal(t, 1, count,
		"Expected shared-plugin to be installed only once despite appearing in multiple imports")

	// Verify all unique plugins are included
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-a",
		"Expected workflow to install plugin-a")
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-b",
		"Expected workflow to install plugin-b")
	assert.Contains(t, workflowData, "copilot plugin install github/top-level-plugin",
		"Expected workflow to install top-level-plugin")
}

func TestCompileWorkflowWithPluginImportsFromSharedWorkflow(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared plugins file
	sharedPluginsPath := filepath.Join(tempDir, "shared-plugins.md")
	sharedPluginsContent := `---
on: push
plugins:
  - github/plugin-one
---
`
	require.NoError(t, os.WriteFile(sharedPluginsPath, []byte(sharedPluginsContent), 0644),
		"Failed to write shared plugins file")

	// Create a workflow file that imports plugins from shared workflow
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
engine: copilot
imports:
  - shared-plugins.md
---

# Test Workflow

This workflow imports plugins from a shared workflow.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644),
		"Failed to write workflow file")

	// Compile the workflow
	compiler := workflow.NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath),
		"CompileWorkflow should succeed")

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	require.NoError(t, err, "Failed to read lock file")

	workflowData := string(lockFileContent)

	// Verify that Copilot engine installs the plugin from import
	assert.Contains(t, workflowData, "copilot plugin install github/plugin-one",
		"Expected Copilot engine to install plugin from import")
}

func TestCompileWorkflowWithPluginImportsClaudeEngineRejectsPlugins(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared plugins file
	sharedPluginsPath := filepath.Join(tempDir, "shared-plugins.md")
	sharedPluginsContent := `---
on: push
plugins:
  - anthropic/plugin-one
---
`
	require.NoError(t, os.WriteFile(sharedPluginsPath, []byte(sharedPluginsContent), 0644),
		"Failed to write shared plugins file")

	// Create a workflow file that imports plugins and uses Claude engine
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
engine: claude
imports:
  - shared-plugins.md
---

# Test Workflow

This workflow uses Claude engine with imported plugins.
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644),
		"Failed to write workflow file")

	// Compile the workflow - should fail because Claude doesn't support plugins
	compiler := workflow.NewCompiler()
	err := compiler.CompileWorkflow(workflowPath)
	require.Error(t, err, "CompileWorkflow should fail because Claude doesn't support plugins")
	assert.Contains(t, err.Error(), "does not support plugins",
		"Error should mention that Claude doesn't support plugins")
}

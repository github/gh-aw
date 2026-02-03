//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileWorkflow_StepOrderingWithAllThreeTypes(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "step-ordering-test*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create shared directory for other imports
	sharedDir := filepath.Join(workflowsDir, "shared")
	err = os.MkdirAll(sharedDir, 0755)
	require.NoError(t, err, "Failed to create shared directory")

	// Create copilot-setup-steps.yml (should be inserted at start)
	copilotSetupContent := `name: Copilot Setup Steps
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Copilot Step 1
        run: echo "First copilot setup step"
      - name: Copilot Step 2
        run: echo "Second copilot setup step"
`
	copilotSetupFile := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	err = os.WriteFile(copilotSetupFile, []byte(copilotSetupContent), 0600)
	require.NoError(t, err, "Failed to write copilot-setup-steps.yml")

	// Create a shared import file with steps (should come after copilot-setup but before main)
	sharedImportContent := `---
steps:
  - name: Imported Step 1
    run: echo "First imported step from shared"
  - name: Imported Step 2
    run: echo "Second imported step from shared"
---

Shared workflow content.
`
	sharedImportFile := filepath.Join(sharedDir, "shared-steps.md")
	err = os.WriteFile(sharedImportFile, []byte(sharedImportContent), 0600)
	require.NoError(t, err, "Failed to write shared import file")

	// Create a workflow that imports both copilot-setup-steps.yml and shared-steps.md, plus has custom steps
	workflowContent := `---
name: Test Step Ordering
on: issue_comment
imports:
  - copilot-setup-steps.yml
  - shared/shared-steps.md
engine: copilot
permissions:
  issues: read
  pull-requests: read
steps:
  - name: Main Step 1
    run: echo "First main frontmatter step"
  - name: Main Step 2
    run: echo "Second main frontmatter step"
---

# Test Step Ordering

This workflow tests the correct ordering of all three types of steps:
1. Copilot-setup-steps (at start)
2. Imported steps from shared files
3. Main frontmatter steps (last)
`
	workflowFile := filepath.Join(workflowsDir, "test-ordering.md")
	err = os.WriteFile(workflowFile, []byte(workflowContent), 0600)
	require.NoError(t, err, "Failed to write test workflow")

	// Change to the temp directory so the compiler can find the workflow
	originalDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	defer os.Chdir(originalDir)

	err = os.Chdir(tmpDir)
	require.NoError(t, err, "Failed to change to temp directory")

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(workflowFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the compiled lock file
	lockFile := strings.Replace(workflowFile, ".md", ".lock.yml", 1)
	yamlOutput, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")
	require.NotEmpty(t, yamlOutput, "Compiled YAML should not be empty")

	// Verify the compiled workflow structure
	yamlStr := string(yamlOutput)

	// Find all step positions
	copilotStep1Index := strings.Index(yamlStr, "Copilot Step 1")
	copilotStep2Index := strings.Index(yamlStr, "Copilot Step 2")
	importedStep1Index := strings.Index(yamlStr, "Imported Step 1")
	importedStep2Index := strings.Index(yamlStr, "Imported Step 2")
	mainStep1Index := strings.Index(yamlStr, "Main Step 1")
	mainStep2Index := strings.Index(yamlStr, "Main Step 2")

	require.NotEqual(t, -1, copilotStep1Index, "Copilot Step 1 not found")
	require.NotEqual(t, -1, copilotStep2Index, "Copilot Step 2 not found")
	require.NotEqual(t, -1, importedStep1Index, "Imported Step 1 not found")
	require.NotEqual(t, -1, importedStep2Index, "Imported Step 2 not found")
	require.NotEqual(t, -1, mainStep1Index, "Main Step 1 not found")
	require.NotEqual(t, -1, mainStep2Index, "Main Step 2 not found")

	// Verify the correct order:
	// 1. Copilot-setup-steps come first
	assert.Less(t, copilotStep1Index, copilotStep2Index, "Copilot steps should maintain their order")
	assert.Less(t, copilotStep1Index, importedStep1Index, "Copilot Step 1 should come before Imported Step 1")
	assert.Less(t, copilotStep1Index, mainStep1Index, "Copilot Step 1 should come before Main Step 1")
	assert.Less(t, copilotStep2Index, importedStep1Index, "Copilot Step 2 should come before Imported Step 1")
	assert.Less(t, copilotStep2Index, mainStep1Index, "Copilot Step 2 should come before Main Step 1")

	// 2. Imported steps come second (after copilot-setup, before main)
	assert.Less(t, importedStep1Index, importedStep2Index, "Imported steps should maintain their order")
	assert.Less(t, importedStep1Index, mainStep1Index, "Imported Step 1 should come before Main Step 1")
	assert.Less(t, importedStep2Index, mainStep1Index, "Imported Step 2 should come before Main Step 1")

	// 3. Main frontmatter steps come last
	assert.Less(t, mainStep1Index, mainStep2Index, "Main steps should maintain their order")

	// Summary verification: copilot < imported < main
	assert.Less(t, copilotStep2Index, importedStep1Index, "All copilot steps should come before all imported steps")
	assert.Less(t, importedStep2Index, mainStep1Index, "All imported steps should come before all main steps")

	// Verify no double inclusion - each step should appear exactly once
	copilotStep1Count := strings.Count(yamlStr, "Copilot Step 1")
	copilotStep2Count := strings.Count(yamlStr, "Copilot Step 2")
	importedStep1Count := strings.Count(yamlStr, "Imported Step 1")
	importedStep2Count := strings.Count(yamlStr, "Imported Step 2")
	mainStep1Count := strings.Count(yamlStr, "Main Step 1")
	mainStep2Count := strings.Count(yamlStr, "Main Step 2")

	assert.Equal(t, 1, copilotStep1Count, "Copilot Step 1 should appear exactly once")
	assert.Equal(t, 1, copilotStep2Count, "Copilot Step 2 should appear exactly once")
	assert.Equal(t, 1, importedStep1Count, "Imported Step 1 should appear exactly once")
	assert.Equal(t, 1, importedStep2Count, "Imported Step 2 should appear exactly once")
	assert.Equal(t, 1, mainStep1Count, "Main Step 1 should appear exactly once")
	assert.Equal(t, 1, mainStep2Count, "Main Step 2 should appear exactly once")
}

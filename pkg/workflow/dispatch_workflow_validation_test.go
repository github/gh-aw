//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatchWorkflowErrorMessage_EmptyList tests that empty workflow list
// error message includes example configuration
// Note: This test directly creates WorkflowData to bypass JSON schema validation
// which also rejects empty arrays. The runtime validation provides a better error message.
func TestDispatchWorkflowErrorMessage_EmptyList(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")

	dispatcherFile := filepath.Join(awDir, "dispatcher.md")

	// Create WorkflowData directly to bypass JSON schema validation
	// (which also rejects empty arrays with minItems: 1)
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			DispatchWorkflow: &DispatchWorkflowConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
				Workflows: []string{}, // Empty list
			},
		},
	}

	// Validate the workflow - should fail with enhanced error message
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should fail for empty workflows list")

	// Verify enhanced error message content
	errMsg := err.Error()
	assert.Contains(t, errMsg, "must specify at least one workflow", "Should mention the requirement")
	assert.Contains(t, errMsg, "Example configuration", "Should include example header")
	assert.Contains(t, errMsg, "safe-outputs:", "Should show YAML structure")
	assert.Contains(t, errMsg, "dispatch-workflow:", "Should show feature name")
	assert.Contains(t, errMsg, "workflows: [workflow-name-1, workflow-name-2]", "Should show example list")
	assert.Contains(t, errMsg, "without the .md extension", "Should explain naming convention")
}

// TestDispatchWorkflowErrorMessage_NotFound tests that workflow not found
// error message includes troubleshooting steps
func TestDispatchWorkflowErrorMessage_NotFound(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a dispatcher workflow that references a non-existent workflow
	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - missing-workflow
    max: 1
---

# Dispatcher Workflow

This workflow references a non-existent workflow.
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	// Change to the aw directory
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the dispatcher workflow
	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	// Validate the workflow - should fail with enhanced error message
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should fail for missing workflow")

	// Verify enhanced error message content
	errMsg := err.Error()
	assert.Contains(t, errMsg, "workflow 'missing-workflow' not found", "Should mention workflow name")
	assert.Contains(t, errMsg, "Checked for:", "Should list checked extensions")
	assert.Contains(t, errMsg, "missing-workflow.md", "Should mention .md extension")
	assert.Contains(t, errMsg, "missing-workflow.lock.yml", "Should mention .lock.yml extension")
	assert.Contains(t, errMsg, "missing-workflow.yml", "Should mention .yml extension")
	assert.Contains(t, errMsg, "To fix:", "Should include fix instructions header")
	assert.Contains(t, errMsg, "Verify the workflow file exists", "Should include verification step")
	assert.Contains(t, errMsg, "case-sensitive", "Should warn about case sensitivity")
	assert.Contains(t, errMsg, "without extension", "Should explain naming convention")
}

// TestDispatchWorkflowErrorMessage_SelfReference tests that self-reference
// error message includes explanation and alternatives
func TestDispatchWorkflowErrorMessage_SelfReference(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a dispatcher workflow that references itself
	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - dispatcher
    max: 1
---

# Dispatcher Workflow

This workflow tries to dispatch to itself.
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	// Change to the aw directory
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the dispatcher workflow
	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	// Validate the workflow - should fail with enhanced error message
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should fail for self-reference")

	// Verify enhanced error message content
	errMsg := err.Error()
	assert.Contains(t, errMsg, "self-reference not allowed", "Should state the restriction")
	assert.Contains(t, errMsg, "dispatcher", "Should mention workflow name")
	assert.Contains(t, errMsg, "cannot dispatch itself", "Should explain the issue")
	assert.Contains(t, errMsg, "infinite loops", "Should explain why it's prevented")
	assert.Contains(t, errMsg, "schedule trigger", "Should suggest schedule alternative")
	assert.Contains(t, errMsg, "workflow_dispatch", "Should suggest workflow_dispatch alternative")
}

// TestDispatchWorkflowErrorMessage_MustCompile tests that uncompiled workflow
// error message includes compilation instructions
func TestDispatchWorkflowErrorMessage_MustCompile(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create an uncompiled workflow (only .md file, no .lock.yml)
	targetWorkflow := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
---

# Target Workflow

This workflow needs to be compiled.
`
	targetFile := filepath.Join(workflowsDir, "target.md")
	err = os.WriteFile(targetFile, []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	// Create a dispatcher workflow that references the uncompiled workflow
	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - target
    max: 1
---

# Dispatcher Workflow

This workflow references an uncompiled workflow.
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	// Change to the aw directory
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the dispatcher workflow
	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	// Validate the workflow - should fail with enhanced error message
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should fail for uncompiled workflow")

	// Verify enhanced error message content
	errMsg := err.Error()
	assert.Contains(t, errMsg, "must be compiled first", "Should state the requirement")
	assert.Contains(t, errMsg, "target", "Should mention workflow name")
	assert.Contains(t, errMsg, "source file exists", "Should acknowledge file exists")
	assert.Contains(t, errMsg, "compiled .lock.yml file is missing", "Should explain what's missing")
	assert.Contains(t, errMsg, "To fix:", "Should include fix instructions header")
	assert.Contains(t, errMsg, "gh aw compile target", "Should show exact compilation command")
	assert.Contains(t, errMsg, "Commit the generated .lock.yml", "Should mention committing")
	assert.Contains(t, errMsg, ".gitignore", "Should warn about gitignore")
}

// TestDispatchWorkflowErrorMessage_MultipleErrors tests that multiple errors
// with enhanced messages are aggregated correctly
func TestDispatchWorkflowErrorMessage_MultipleErrors(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")
	compiler.failFast = false // Enable error aggregation

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create dispatcher workflow with multiple errors
	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - dispatcher  # Self-reference
      - missing     # Not found
    max: 2
---

# Dispatcher Workflow
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	// Change to the aw directory
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	// Parse the dispatcher workflow
	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	// Validate the workflow - should fail with multiple enhanced error messages
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should fail with multiple errors")

	// Verify both enhanced error messages are in the aggregated error
	errMsg := err.Error()
	assert.Contains(t, errMsg, "Found 2 dispatch-workflow errors:", "Should show error count")

	// Check self-reference error with enhancements
	assert.Contains(t, errMsg, "self-reference not allowed", "Should include self-reference error")
	assert.Contains(t, errMsg, "infinite loops", "Should include explanation for self-reference")

	// Check not found error with enhancements
	assert.Contains(t, errMsg, "not found", "Should include not found error")
	assert.Contains(t, errMsg, "To fix:", "Should include fix instructions")
	assert.Contains(t, errMsg, "Checked for:", "Should include checked extensions")
}

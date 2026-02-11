//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssignToAgentDefaultMax tests that assign-to-agent has a default max of 1
func TestAssignToAgentDefaultMax(t *testing.T) {
	tmpDir := testutil.TempDir(t, "assign-to-agent-default-max-test")

	// Create a workflow with assign-to-agent but no explicit max
	workflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  assign-to-agent:
    name: copilot
---

# Test Workflow

This workflow tests the default max for assign-to-agent.
`
	testFile := filepath.Join(tmpDir, "test-assign-to-agent.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test workflow")

	// Parse the workflow
	compiler := NewCompilerWithVersion("1.0.0")
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse workflow")

	// Verify assign-to-agent config exists and has default max of 1
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.AssignToAgent, "AssignToAgent should not be nil")
	assert.Equal(t, 1, workflowData.SafeOutputs.AssignToAgent.Max, "Default max should be 1")
}

// TestDispatchWorkflowDefaultMax tests that dispatch-workflow has a default max of 1
func TestDispatchWorkflowDefaultMax(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dispatch-workflow-default-max-test")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a target workflow with workflow_dispatch
	targetWorkflow := `name: Target
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Target workflow"
`
	targetFile := filepath.Join(workflowsDir, "target.lock.yml")
	err = os.WriteFile(targetFile, []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	// Create a dispatcher workflow with dispatch-workflow but no explicit max
	workflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    - target
---

# Test Workflow

This workflow tests the default max for dispatch-workflow.
`
	testFile := filepath.Join(tmpDir, "test-dispatch.md")
	err = os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test workflow")

	// Parse the workflow
	compiler := NewCompilerWithVersion("1.0.0")
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse workflow")

	// Verify dispatch-workflow config exists and has default max of 1
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.DispatchWorkflow, "DispatchWorkflow should not be nil")
	assert.Equal(t, 1, workflowData.SafeOutputs.DispatchWorkflow.Max, "Default max should be 1")
}

// TestAssignToAgentExplicitMax tests that explicit max overrides the default
func TestAssignToAgentExplicitMax(t *testing.T) {
	tmpDir := testutil.TempDir(t, "assign-to-agent-explicit-max-test")

	// Create a workflow with assign-to-agent with explicit max
	workflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  assign-to-agent:
    name: copilot
    max: 5
---

# Test Workflow

This workflow tests explicit max for assign-to-agent.
`
	testFile := filepath.Join(tmpDir, "test-assign-to-agent.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test workflow")

	// Parse the workflow
	compiler := NewCompilerWithVersion("1.0.0")
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse workflow")

	// Verify assign-to-agent config has explicit max of 5
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.AssignToAgent, "AssignToAgent should not be nil")
	assert.Equal(t, 5, workflowData.SafeOutputs.AssignToAgent.Max, "Explicit max should be 5")
}

// TestDispatchWorkflowExplicitMax tests that explicit max overrides the default
func TestDispatchWorkflowExplicitMax(t *testing.T) {
	tmpDir := testutil.TempDir(t, "dispatch-workflow-explicit-max-test")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a target workflow with workflow_dispatch
	targetWorkflow := `name: Target
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Target workflow"
`
	targetFile := filepath.Join(workflowsDir, "target.lock.yml")
	err = os.WriteFile(targetFile, []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	// Create a dispatcher workflow with explicit max
	workflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - target
    max: 3
---

# Test Workflow

This workflow tests explicit max for dispatch-workflow.
`
	testFile := filepath.Join(tmpDir, "test-dispatch.md")
	err = os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test workflow")

	// Parse the workflow
	compiler := NewCompilerWithVersion("1.0.0")
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse workflow")

	// Verify dispatch-workflow config has explicit max of 3
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.DispatchWorkflow, "DispatchWorkflow should not be nil")
	assert.Equal(t, 3, workflowData.SafeOutputs.DispatchWorkflow.Max, "Explicit max should be 3")
}

// TestGenerateAssignToAgentConfigDefaultMax tests the config generation with default max
func TestGenerateAssignToAgentConfigDefaultMax(t *testing.T) {
	// Test with max=0 (should use default of 1)
	config := generateAssignToAgentConfig(0, 1, "copilot", "", nil)
	assert.Equal(t, 1, config["max"], "Should use default max of 1 when max is 0")
	assert.Equal(t, "copilot", config["default_agent"], "Should have default agent")

	// Test with explicit max (should override default)
	config = generateAssignToAgentConfig(5, 1, "copilot", "", nil)
	assert.Equal(t, 5, config["max"], "Should use explicit max of 5")
	assert.Equal(t, "copilot", config["default_agent"], "Should have default agent")

	// Test with target and allowed
	config = generateAssignToAgentConfig(0, 1, "copilot", "issues", []string{"copilot", "custom"})
	assert.Equal(t, 1, config["max"], "Should use default max of 1")
	assert.Equal(t, "copilot", config["default_agent"], "Should have default agent")
	assert.Equal(t, "issues", config["target"], "Should have target")
	assert.Equal(t, []string{"copilot", "custom"}, config["allowed"], "Should have allowed list")
}

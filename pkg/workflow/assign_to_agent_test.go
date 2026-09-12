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

// TestAssignToAgentCanonicalNameKey tests that 'name' is the canonical key for assigning an agent
func TestAssignToAgentCanonicalNameKey(t *testing.T) {
	tmpDir := testutil.TempDir(t, "assign-to-agent-name-test")

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

This workflow tests canonical 'name' key.
`
	testFile := filepath.Join(tmpDir, "test-assign-to-agent.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test workflow")

	compiler := NewCompiler(WithVersion("1.0.0"))
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse workflow")

	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.AssignToAgent, "AssignToAgent should not be nil")
	assert.Equal(t, "copilot", workflowData.SafeOutputs.AssignToAgent.DefaultAgent, "Should parse 'name' key as DefaultAgent")
}

func TestAssignToAgentReasoningEffort(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "literal", value: "high"},
		{name: "model specific", value: "xhigh"},
		{name: "expression", value: "${{ inputs.reasoning_effort }}"},
		{name: "empty", value: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "assign-to-agent-reasoning-effort")
			workflow := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
safe-outputs:
  assign-to-agent:
    reasoning-effort: "` + tt.value + `"
---
# Test Workflow
`
			testFile := filepath.Join(tmpDir, "test-assign-to-agent.md")
			require.NoError(t, os.WriteFile(testFile, []byte(workflow), 0644))

			workflowData, err := NewCompiler(WithVersion("1.0.0")).ParseWorkflowFile(testFile)
			require.NoError(t, err)
			require.NotNil(t, workflowData.SafeOutputs.AssignToAgent)
			assert.Equal(t, tt.value, workflowData.SafeOutputs.AssignToAgent.ReasoningEffort)
		})
	}
}

func TestAssignToAgentReasoningEffortRejectsNonString(t *testing.T) {
	tmpDir := testutil.TempDir(t, "assign-to-agent-reasoning-effort-invalid")
	workflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  assign-to-agent:
    reasoning-effort: 42
---
# Test Workflow
`
	testFile := filepath.Join(tmpDir, "test-assign-to-agent.md")
	require.NoError(t, os.WriteFile(testFile, []byte(workflow), 0644))

	_, err := NewCompiler(WithVersion("1.0.0")).ParseWorkflowFile(testFile)
	require.Error(t, err)
}

// TestAssignToAgentInHandlerManagerStep verifies that assign_to_agent is processed within
// the handler manager step (process_safe_outputs) and that the safe_outputs job exports
// the required assign_to_agent outputs for the conclusion job.
func TestAssignToAgentInHandlerManagerStep(t *testing.T) {
	tmpDir := testutil.TempDir(t, "assign-to-agent-handler-manager")

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

This workflow tests that assign_to_agent is integrated into the handler manager step.
`
	testFile := filepath.Join(tmpDir, "test-assign-to-agent-hm.md")
	err := os.WriteFile(testFile, []byte(workflow), 0644)
	require.NoError(t, err, "Failed to write test workflow")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(testFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := filepath.Join(tmpDir, "test-assign-to-agent-hm.lock.yml")
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockContent := string(content)

	assert.Contains(t, lockContent, "id: process_safe_outputs",
		"Expected process_safe_outputs step to handle assign_to_agent")
	assert.NotContains(t, lockContent, "id: assign_to_agent",
		"Expected no dedicated assign_to_agent step — it is now handled in the handler manager")
	assert.Contains(t, lockContent, "assign_to_agent_assignment_error_count",
		"Expected safe_outputs job to export assign_to_agent_assignment_error_count output for failure propagation")
	assert.Contains(t, lockContent, "GH_AW_ASSIGN_TO_AGENT_TOKEN",
		"Expected GH_AW_ASSIGN_TO_AGENT_TOKEN env var in process_safe_outputs step")
}

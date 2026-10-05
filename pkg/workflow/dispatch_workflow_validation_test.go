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
	compiler := NewCompiler(WithVersion("1.0.0"))

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
					Max: strPtr("1"),
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
	compiler := NewCompiler(WithVersion("1.0.0"))

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
	assert.Contains(t, errMsg, "missing-workflow.yaml", "Should mention .yaml extension")
	assert.Contains(t, errMsg, "To fix:", "Should include fix instructions header")
	assert.Contains(t, errMsg, "Verify the workflow file exists", "Should include verification step")
	assert.Contains(t, errMsg, "case-sensitive", "Should warn about case sensitivity")
	assert.Contains(t, errMsg, "without extension", "Should explain naming convention")
}

func TestDispatchWorkflowValidation_SkipsLocalChecksForCrossRepoTarget(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    target-repo: my-org/target-repo
    workflows:
      - missing-workflow
    max: 1
---

# Dispatcher Workflow
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	assert.NoError(t, err, "Validation should skip local workflow checks for cross-repo target-repo")
}

func TestDispatchWorkflowValidation_DoesNotSkipForCurrentRepoExpression(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    target-repo: ${{ github.repository }}
    workflows:
      - missing-workflow
    max: 1
---

# Dispatcher Workflow
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should still run when target-repo references github.repository")
	require.ErrorContains(t, err, "workflow 'missing-workflow' not found")
}

func TestDispatchWorkflowValidation_RequiresWorkerOptInForQueueClaims(t *testing.T) {
	for _, tc := range []struct {
		name    string
		worker  string
		wantErr bool
	}{
		{name: "non-worker", wantErr: true},
		{name: "worker", worker: "    worker: true\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			awDir := filepath.Join(tmpDir, ".github", "aw")
			workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
			require.NoError(t, os.MkdirAll(awDir, 0o700))
			require.NoError(t, os.MkdirAll(workflowsDir, 0o700))

			workerWorkflow := `---
on: workflow_dispatch
tools:
  work-queue:
    storage: git
` + tc.worker + `---
Run assigned work.
`
			require.NoError(t, os.WriteFile(filepath.Join(workflowsDir, "worker.md"), []byte(workerWorkflow), 0o600))

			dispatcherPath := filepath.Join(awDir, "dispatcher.md")
			data := &WorkflowData{
				Tools: map[string]any{"work-queue": true},
				SafeOutputs: &SafeOutputsConfig{
					DispatchWorkflow: &DispatchWorkflowConfig{Workflows: []string{"worker"}},
				},
			}
			err := NewCompiler(WithVersion("1.0.0")).validateDispatchWorkflow(data, dispatcherPath)
			if tc.wantErr {
				require.ErrorContains(t, err, "tools.work-queue.worker: true")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDispatchWorkflowValidation_UsesWorkflowDirEnvOverride(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	overrideWorkflowsDir := filepath.Join(tmpDir, "custom", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(overrideWorkflowsDir, 0755)
	require.NoError(t, err, "Failed to create override workflows directory")

	t.Setenv("GH_AW_WORKFLOWS_DIR", filepath.Join("custom", "workflows"))

	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - target-worker
    max: 1
---

# Dispatcher Workflow
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	targetWorkflow := `name: Target Worker
on:
  workflow_dispatch:
jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Working"
`
	err = os.WriteFile(filepath.Join(overrideWorkflowsDir, "target-worker.lock.yml"), []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	assert.NoError(t, err, "Workflow should resolve from GH_AW_WORKFLOWS_DIR override")
}

func TestShouldSkipLocalDispatchWorkflowValidation(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	compiler.SetRepositorySlug("my-org/my-repo")

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "empty", target: "", want: false},
		{name: "github.repository expression", target: "${{ github.repository }}", want: false},
		{name: "other expression", target: "${{ vars.TARGET_REPO }}", want: false},
		{name: "invalid slug", target: "not-a-slug", want: false},
		{name: "same repo literal", target: "my-org/my-repo", want: false},
		{name: "same repo literal case-insensitive", target: "My-Org/My-Repo", want: false},
		{name: "cross-repo literal", target: "my-org/other-repo", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compiler.shouldSkipLocalDispatchWorkflowValidation(tt.target))
		})
	}

	t.Run("cross-repo literal with unknown current repo", func(t *testing.T) {
		compilerWithoutRepo := NewCompiler(WithVersion("1.0.0"))
		assert.True(t, compilerWithoutRepo.shouldSkipLocalDispatchWorkflowValidation("my-org/other-repo"))
	})
}

// TestDispatchWorkflowErrorMessage_SelfReference tests that self-reference
// error message includes explanation and alternatives
func TestDispatchWorkflowErrorMessage_SelfReference(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

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

// TestDispatchWorkflowBatchAware_MDWithDispatch tests that a workflow that only has a .md file
// (no .lock.yml) is accepted as a valid same-batch dispatch target when the .md has
// workflow_dispatch in its 'on:' section.
func TestDispatchWorkflowBatchAware_MDWithDispatch(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a target workflow that only has .md (no .lock.yml) with workflow_dispatch trigger
	targetWorkflow := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
---

# Target Workflow

This workflow is a same-batch compilation target.
`
	targetFile := filepath.Join(workflowsDir, "target.md")
	err = os.WriteFile(targetFile, []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	// Create a dispatcher workflow that references the .md-only target
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

This workflow dispatches to a same-batch target.
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

	// Validation should succeed: .md exists with workflow_dispatch (same-batch target)
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	assert.NoError(t, err, "Validation should pass for .md-only same-batch target with workflow_dispatch")
}

// TestDispatchWorkflowBatchAware_MDWithoutDispatch tests that a workflow that only has a .md file
// (no .lock.yml) and does NOT have workflow_dispatch in its 'on:' section fails validation.
func TestDispatchWorkflowBatchAware_MDWithoutDispatch(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a target workflow that only has .md (no .lock.yml) WITHOUT workflow_dispatch
	targetWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
---

# Target Workflow

This workflow does not support workflow_dispatch.
`
	targetFile := filepath.Join(workflowsDir, "target.md")
	err = os.WriteFile(targetFile, []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	// Create a dispatcher workflow that references the .md-only target
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

	// Validation should fail: .md exists but lacks workflow_dispatch
	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	require.Error(t, err, "Validation should fail when .md target lacks workflow_dispatch")
	require.ErrorContains(t, err, "does not support workflow_dispatch trigger", "Should explain missing trigger")
}

// TestDispatchWorkflowErrorMessage_MultipleErrors tests that multiple errors
// with enhanced messages are aggregated correctly
func TestDispatchWorkflowErrorMessage_MultipleErrors(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
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

// TestDispatchWorkflowValidation_YAMLExtension tests that a workflow defined as a
// plain .yaml file (instead of .yml) is correctly resolved and validated.
func TestDispatchWorkflowValidation_YAMLExtension(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))

	tmpDir := t.TempDir()
	awDir := filepath.Join(tmpDir, ".github", "aw")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")

	err := os.MkdirAll(awDir, 0755)
	require.NoError(t, err, "Failed to create aw directory")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a target workflow using the .yaml extension
	targetWorkflow := `name: Target Worker
on:
  workflow_dispatch:
jobs:
  work:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Working"
`
	err = os.WriteFile(filepath.Join(workflowsDir, "target-worker.yaml"), []byte(targetWorkflow), 0644)
	require.NoError(t, err, "Failed to write target workflow")

	dispatcherWorkflow := `---
on: issues
engine: copilot
permissions:
  contents: read
safe-outputs:
  dispatch-workflow:
    workflows:
      - target-worker
    max: 1
---

# Dispatcher Workflow
`
	dispatcherFile := filepath.Join(awDir, "dispatcher.md")
	err = os.WriteFile(dispatcherFile, []byte(dispatcherWorkflow), 0644)
	require.NoError(t, err, "Failed to write dispatcher workflow")

	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(awDir)
	require.NoError(t, err, "Failed to change directory")
	defer func() { _ = os.Chdir(oldDir) }()

	workflowData, err := compiler.ParseWorkflowFile("dispatcher.md")
	require.NoError(t, err, "Failed to parse workflow")

	err = compiler.validateDispatchWorkflow(workflowData, dispatcherFile)
	assert.NoError(t, err, "Validation should pass for a .yaml workflow target")
}

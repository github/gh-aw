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

func TestCompileWorkflow_ImportCopilotSetupSteps(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "compile-copilot-setup-test*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create copilot-setup-steps.yml
	copilotSetupContent := `name: Copilot Setup Steps
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Install gh-aw extension
        run: curl -fsSL https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh | bash
      - name: Set up Node.js
        uses: actions/setup-node@v4
        with:
          node-version: "20"
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: "1.21"
`
	copilotSetupFile := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	err = os.WriteFile(copilotSetupFile, []byte(copilotSetupContent), 0600)
	require.NoError(t, err, "Failed to write copilot-setup-steps.yml")

	// Create a workflow that imports copilot-setup-steps.yml with custom steps
	workflowContent := `---
name: Test Copilot Setup Import
on: issue_comment
imports:
  - copilot-setup-steps.yml
engine: copilot
steps:
  - name: My custom step
    run: echo "This is my custom step"
---

# Test Copilot Setup Import

This workflow imports copilot-setup-steps.yml and should have the imported steps before the custom step.
`
	workflowFile := filepath.Join(workflowsDir, "test-workflow.md")
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

	// Verify custom step is present
	assert.Contains(t, yamlStr, "My custom step", "Custom step should be in compiled workflow")

	// Verify imported steps are present
	assert.Contains(t, yamlStr, "Install gh-aw extension", "Imported install step should be in compiled workflow")
	assert.Contains(t, yamlStr, "Set up Node.js", "Imported Node.js step should be in compiled workflow")
	assert.Contains(t, yamlStr, "Set up Go", "Imported Go step should be in compiled workflow")

	// Verify the order: copilot-setup-steps → custom steps (main frontmatter steps LAST)
	customStepIndex := strings.Index(yamlStr, "My custom step")
	installStepIndex := strings.Index(yamlStr, "Install gh-aw extension")
	nodeStepIndex := strings.Index(yamlStr, "Set up Node.js")
	goStepIndex := strings.Index(yamlStr, "Set up Go")

	require.NotEqual(t, -1, customStepIndex, "Custom step not found")
	require.NotEqual(t, -1, installStepIndex, "Install step not found")
	require.NotEqual(t, -1, nodeStepIndex, "Node.js step not found")
	require.NotEqual(t, -1, goStepIndex, "Go step not found")

	// Copilot-setup-steps should come BEFORE custom steps (custom steps are LAST)
	assert.Less(t, installStepIndex, customStepIndex, "Install step should come before custom step (copilot-setup at start)")
	assert.Less(t, nodeStepIndex, customStepIndex, "Node.js step should come before custom step (copilot-setup at start)")
	assert.Less(t, goStepIndex, customStepIndex, "Go step should come before custom step (copilot-setup at start)")

	// Verify the imported steps maintain their order
	assert.Less(t, installStepIndex, nodeStepIndex, "Install step should come before Node.js step")
	assert.Less(t, nodeStepIndex, goStepIndex, "Node.js step should come before Go step")

	// Verify that job-level fields are NOT present in the compiled workflow
	// (they should have been stripped out during step extraction)
	assert.NotContains(t, yamlStr, "copilot-setup-steps:", "Should not contain the job name from imported workflow")
}

func TestCompileWorkflow_ImportCopilotSetupStepsWithoutCustomSteps(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "compile-copilot-setup-nocustom-test*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tmpDir)

	// Create .github/workflows directory
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err = os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create copilot-setup-steps.yml
	copilotSetupContent := `name: Copilot Setup Steps
on: workflow_dispatch
jobs:
  copilot-setup-steps:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - name: Install dependencies
        run: npm install
      - name: Run linter
        run: npm run lint
`
	copilotSetupFile := filepath.Join(workflowsDir, "copilot-setup-steps.yml")
	err = os.WriteFile(copilotSetupFile, []byte(copilotSetupContent), 0600)
	require.NoError(t, err, "Failed to write copilot-setup-steps.yml")

	// Create a workflow that imports copilot-setup-steps.yml WITHOUT custom steps
	workflowContent := `---
name: Test Copilot Setup Import No Custom
on: issue_comment
imports:
  - copilot-setup-steps.yml
engine: copilot
---

# Test Copilot Setup Import Without Custom Steps

This workflow imports copilot-setup-steps.yml without any custom steps.
`
	workflowFile := filepath.Join(workflowsDir, "test-workflow-no-custom.md")
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

	// Verify imported steps are present
	assert.Contains(t, yamlStr, "Install dependencies", "Imported install step should be in compiled workflow")
	assert.Contains(t, yamlStr, "Run linter", "Imported linter step should be in compiled workflow")

	// Verify they are in the correct order
	installIndex := strings.Index(yamlStr, "Install dependencies")
	lintIndex := strings.Index(yamlStr, "Run linter")
	assert.Less(t, installIndex, lintIndex, "Install step should come before linter step")
}

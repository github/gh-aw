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

// TestSafeOutputsAppImport tests that app configuration can be imported from shared workflows
func TestSafeOutputsAppImport(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with app configuration
	sharedWorkflow := `---
safe-outputs:
  app:
    app-id: ${{ vars.SHARED_APP_ID }}
    private-key: ${{ secrets.SHARED_APP_SECRET }}
    repositories:
      - "repo1"
---

# Shared App Configuration

This shared workflow provides app configuration.
`

	sharedFile := filepath.Join(workflowsDir, "shared-app.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the app configuration with relative path
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-app.md
safe-outputs:
  create-issue:
---

# Main Workflow

This workflow uses the imported app configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer os.Chdir(oldDir)

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.App, "App configuration should be imported")

	// Verify app configuration was imported correctly
	assert.Equal(t, "${{ vars.SHARED_APP_ID }}", workflowData.SafeOutputs.App.AppID)
	assert.Equal(t, "${{ secrets.SHARED_APP_SECRET }}", workflowData.SafeOutputs.App.PrivateKey)
	assert.Equal(t, []string{"repo1"}, workflowData.SafeOutputs.App.Repositories)
}

// TestSafeOutputsAppImportOverride tests that local app configuration overrides imported one
func TestSafeOutputsAppImportOverride(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with app configuration
	sharedWorkflow := `---
safe-outputs:
  app:
    app-id: ${{ vars.SHARED_APP_ID }}
    private-key: ${{ secrets.SHARED_APP_SECRET }}
---

# Shared App Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-app.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow with its own app configuration
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-app.md
safe-outputs:
  create-issue:
  app:
    app-id: ${{ vars.LOCAL_APP_ID }}
    private-key: ${{ secrets.LOCAL_APP_SECRET }}
    repositories:
      - "repo2"
---

# Main Workflow

This workflow overrides the imported app configuration.
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer os.Chdir(oldDir)

	// Parse the main workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.App, "App configuration should be present")

	// Verify local app configuration takes precedence
	assert.Equal(t, "${{ vars.LOCAL_APP_ID }}", workflowData.SafeOutputs.App.AppID)
	assert.Equal(t, "${{ secrets.LOCAL_APP_SECRET }}", workflowData.SafeOutputs.App.PrivateKey)
	assert.Equal(t, []string{"repo2"}, workflowData.SafeOutputs.App.Repositories)
}

// TestSafeOutputsAppImportStepGeneration tests that imported app config generates correct steps
func TestSafeOutputsAppImportStepGeneration(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	err := os.MkdirAll(workflowsDir, 0755)
	require.NoError(t, err, "Failed to create workflows directory")

	// Create a shared workflow with app configuration
	sharedWorkflow := `---
safe-outputs:
  app:
    app-id: ${{ vars.SHARED_APP_ID }}
    private-key: ${{ secrets.SHARED_APP_SECRET }}
---

# Shared App Configuration
`

	sharedFile := filepath.Join(workflowsDir, "shared-app.md")
	err = os.WriteFile(sharedFile, []byte(sharedWorkflow), 0644)
	require.NoError(t, err, "Failed to write shared file")

	// Create main workflow that imports the app configuration
	mainWorkflow := `---
on: issues
permissions:
  contents: read
imports:
  - ./shared-app.md
safe-outputs:
  create-issue:
---

# Main Workflow
`

	mainFile := filepath.Join(workflowsDir, "main.md")
	err = os.WriteFile(mainFile, []byte(mainWorkflow), 0644)
	require.NoError(t, err, "Failed to write main file")

	// Change to the workflows directory for relative path resolution
	oldDir, err := os.Getwd()
	require.NoError(t, err, "Failed to get current directory")
	err = os.Chdir(workflowsDir)
	require.NoError(t, err, "Failed to change directory")
	defer os.Chdir(oldDir)

	// Parse the workflow
	workflowData, err := compiler.ParseWorkflowFile("main.md")
	require.NoError(t, err, "Failed to parse workflow")

	// Build the safe_outputs job
	job, err := compiler.buildCreateOutputIssueJob(workflowData, "main")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	// Convert steps to string
	stepsStr := strings.Join(job.Steps, "")

	// Verify token minting and invalidation steps are present
	assert.Contains(t, stepsStr, "Generate GitHub App token", "Token minting step should be present")
	assert.Contains(t, stepsStr, "Invalidate GitHub App token", "Token invalidation step should be present")
	assert.Contains(t, stepsStr, "${{ vars.SHARED_APP_ID }}", "Should use imported app ID")
	assert.Contains(t, stepsStr, "${{ secrets.SHARED_APP_SECRET }}", "Should use imported secret")
}

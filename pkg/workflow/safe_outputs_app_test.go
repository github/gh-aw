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

// TestSafeOutputsAppConfiguration tests that app configuration is correctly parsed
func TestSafeOutputsAppConfiguration(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	markdown := `---
on: issues
safe-outputs:
  create-issue:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    repositories:
      - "repo1"
      - "repo2"
---

# Test Workflow

Test workflow with app configuration.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.App, "App configuration should be parsed")

	// Verify app configuration
	assert.Equal(t, "${{ vars.APP_ID }}", workflowData.SafeOutputs.App.AppID)
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", workflowData.SafeOutputs.App.PrivateKey)
	assert.Equal(t, []string{"repo1", "repo2"}, workflowData.SafeOutputs.App.Repositories)
}

// TestSafeOutputsAppConfigurationMinimal tests minimal app configuration without repositories
func TestSafeOutputsAppConfigurationMinimal(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	markdown := `---
on: issues
safe-outputs:
  create-issue:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with minimal app configuration.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.App, "App configuration should be parsed")

	// Verify app configuration
	assert.Equal(t, "${{ vars.APP_ID }}", workflowData.SafeOutputs.App.AppID)
	assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", workflowData.SafeOutputs.App.PrivateKey)
	assert.Empty(t, workflowData.SafeOutputs.App.Repositories)
}

// TestSafeOutputsAppTokenMintingStep tests that token minting step is generated
func TestSafeOutputsAppTokenMintingStep(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	markdown := `---
on: issues
permissions:
  contents: read
safe-outputs:
  create-issue:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with app token minting.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")

	// Build the safe_outputs job
	job, err := compiler.buildCreateOutputIssueJob(workflowData, "main")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	// Convert steps to string for easier assertion
	stepsStr := strings.Join(job.Steps, "")

	// Verify token minting step is present
	assert.Contains(t, stepsStr, "Generate GitHub App token", "Token minting step should be present")
	assert.Contains(t, stepsStr, "actions/create-github-app-token", "Should use create-github-app-token action")
	assert.Contains(t, stepsStr, "app-id: ${{ vars.APP_ID }}", "Should use configured app ID")
	assert.Contains(t, stepsStr, "private-key: ${{ secrets.APP_PRIVATE_KEY }}", "Should use configured private key")

	// Verify token invalidation step is present
	assert.Contains(t, stepsStr, "Invalidate GitHub App token", "Token invalidation step should be present")
	assert.Contains(t, stepsStr, "if: always()", "Invalidation step should always run")
	assert.Contains(t, stepsStr, "/installation/token", "Should call token invalidation endpoint")

	// Verify token is used in github-script step
	assert.Contains(t, stepsStr, "${{ steps.safe-outputs-app-token.outputs.token }}", "Should use app token in github-script")
}

// TestSafeOutputsAppTokenMintingStepWithRepositories tests token minting with repositories
func TestSafeOutputsAppTokenMintingStepWithRepositories(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	markdown := `---
on: issues
permissions:
  contents: read
safe-outputs:
  create-issue:
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    repositories:
      - "repo1"
      - "repo2"
---

# Test Workflow

Test workflow with app token minting and repository restrictions.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")

	// Build the safe_outputs job
	job, err := compiler.buildCreateOutputIssueJob(workflowData, "main")
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	// Convert steps to string for easier assertion
	stepsStr := strings.Join(job.Steps, "")

	// Verify repositories are included in the minting step
	assert.Contains(t, stepsStr, "repositories: repo1,repo2", "Should include repositories")
}

// TestSafeOutputsAppWithoutSafeOutputs tests that app without safe outputs doesn't break
func TestSafeOutputsAppWithoutSafeOutputs(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	markdown := `---
on: issues
permissions:
  contents: read
---

# Test Workflow

Test workflow without safe outputs.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	assert.Nil(t, workflowData.SafeOutputs, "SafeOutputs should be nil")
}

// TestSafeOutputsAppTokenDiscussionsPermission tests that discussions permission is included
func TestSafeOutputsAppTokenDiscussionsPermission(t *testing.T) {
	compiler := NewCompilerWithVersion("1.0.0")

	markdown := `---
on: issues
safe-outputs:
  create-discussion:
    category: "general"
  app:
    app-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

# Test Workflow

Test workflow with discussions permission.
`

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(markdown), 0644)
	require.NoError(t, err, "Failed to write test file")

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err, "Failed to parse markdown content")
	require.NotNil(t, workflowData.SafeOutputs, "SafeOutputs should not be nil")
	require.NotNil(t, workflowData.SafeOutputs.CreateDiscussions, "CreateDiscussions should not be nil")

	// Build the consolidated safe_outputs job
	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, "main", testFile)
	require.NoError(t, err, "Failed to build safe_outputs job")
	require.NotNil(t, job, "Job should not be nil")

	// Convert steps to string for easier assertion
	stepsStr := strings.Join(job.Steps, "")

	// Verify that permission-discussions: write is included in the GitHub App token minting step
	assert.Contains(t, stepsStr, "permission-discussions: write", "GitHub App token should include discussions write permission")
	assert.Contains(t, stepsStr, "permission-contents: read", "GitHub App token should include contents read permission")
}

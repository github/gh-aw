//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepositoryImportCheckout verifies that workflows with repository imports
// get actions/checkout steps generated before the merge step
func TestRepositoryImportCheckout(t *testing.T) {
	frontmatter := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
imports:
  - github/test-repo@main
---`
	markdown := "# Agent\n\nComplete the task."

	tmpDir := testutil.TempDir(t, "repository-import-checkout-test")

	// Create workflow file
	workflowPath := filepath.Join(tmpDir, "test.md")
	content := frontmatter + "\n\n" + markdown
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0644), "Failed to write workflow file")

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "Failed to compile workflow")

	// Calculate the lock file path
	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockContentStr := string(lockContent)

	// Verify checkout step for repository import is present
	assert.Contains(t, lockContentStr, "name: Checkout repository import github/test-repo@main",
		"Should contain checkout step for repository import")
	assert.Contains(t, lockContentStr, "repository: github/test-repo",
		"Should specify repository in checkout step")
	assert.Contains(t, lockContentStr, "ref: main",
		"Should specify ref in checkout step")
	assert.Contains(t, lockContentStr, "path: .github/aw/imports/github-test-repo-main",
		"Should specify checkout path in .github/aw/imports directory")
	assert.Contains(t, lockContentStr, "sparse-checkout:",
		"Should use sparse-checkout")
	assert.Contains(t, lockContentStr, ".github/",
		"Should checkout .github folder")

	// Verify merge step is present
	assert.Contains(t, lockContentStr, "name: Merge remote .github folder",
		"Should contain merge step")
	assert.Contains(t, lockContentStr, "GH_AW_REPOSITORY_IMPORTS",
		"Should pass repository imports to merge script")

	// Verify checkout step comes before merge step
	checkoutIndex := strings.Index(lockContentStr, "name: Checkout repository import")
	mergeIndex := strings.Index(lockContentStr, "name: Merge remote .github folder")
	assert.Greater(t, mergeIndex, checkoutIndex,
		"Merge step should come after checkout step")
}

// TestMultipleRepositoryImportCheckouts verifies that workflows with multiple repository imports
// get separate checkout steps for each import
func TestMultipleRepositoryImportCheckouts(t *testing.T) {
	frontmatter := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
imports:
  - github/repo1@main
  - github/repo2@v1.0.0
---`
	markdown := "# Agent\n\nComplete the task."

	tmpDir := testutil.TempDir(t, "multiple-repository-imports-test")

	// Create workflow file
	workflowPath := filepath.Join(tmpDir, "test.md")
	content := frontmatter + "\n\n" + markdown
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0644), "Failed to write workflow file")

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "Failed to compile workflow")

	// Calculate the lock file path
	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockContentStr := string(lockContent)

	// Verify checkout step for first repository import
	assert.Contains(t, lockContentStr, "name: Checkout repository import github/repo1@main",
		"Should contain checkout step for first repository import")
	assert.Contains(t, lockContentStr, "path: .github/aw/imports/github-repo1-main",
		"Should use correct path for first import")

	// Verify checkout step for second repository import
	assert.Contains(t, lockContentStr, "name: Checkout repository import github/repo2@v1.0.0",
		"Should contain checkout step for second repository import")
	assert.Contains(t, lockContentStr, "path: .github/aw/imports/github-repo2-v1.0.0",
		"Should use correct path for second import")

	// Verify merge step includes both imports
	assert.Contains(t, lockContentStr, `GH_AW_REPOSITORY_IMPORTS: '["github/repo1@main","github/repo2@v1.0.0"]'`,
		"Should pass all repository imports to merge script")
}

// TestRefSanitization verifies that git refs with special characters are sanitized for paths
func TestRefSanitization(t *testing.T) {
	frontmatter := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
engine: copilot
strict: false
imports:
  - github/test-repo@feature/my-branch
---`
	markdown := "# Agent\n\nComplete the task."

	tmpDir := testutil.TempDir(t, "ref-sanitization-test")

	// Create workflow file
	workflowPath := filepath.Join(tmpDir, "test.md")
	content := frontmatter + "\n\n" + markdown
	require.NoError(t, os.WriteFile(workflowPath, []byte(content), 0644), "Failed to write workflow file")

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "Failed to compile workflow")

	// Calculate the lock file path
	lockFile := strings.TrimSuffix(workflowPath, ".md") + ".lock.yml"

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read lock file")

	lockContentStr := string(lockContent)

	// Verify ref is sanitized in path (/ replaced with -)
	assert.Contains(t, lockContentStr, "path: .github/aw/imports/github-test-repo-feature-my-branch",
		"Should sanitize slashes in ref for path")
	assert.Contains(t, lockContentStr, "ref: feature/my-branch",
		"Should keep original ref in checkout step")
}

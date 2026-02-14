//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreatePullRequestCrossRepoCheckout tests that target-repo properly configures checkout and git
func TestCreatePullRequestCrossRepoCheckout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cross-repo-checkout-test")
	require.NoError(t, err, "Failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Create test workflow with cross-repo target
	workflowContent := `---
on: push
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-pull-request:
    target-repo: "microsoft/vscode-docs"
    base-branch: vnext
    draft: true
---

# Cross-Repo Test Workflow

Create a pull request in a different repository.
`

	workflowPath := filepath.Join(tmpDir, "cross-repo.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644), "Failed to write workflow file")

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "Failed to compile workflow")

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "cross-repo.lock.yml")
	compiledBytes, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledContent := string(compiledBytes)

	// Test 1: Verify repository parameter is set in actions/checkout
	assert.Contains(t, compiledContent, "repository: microsoft/vscode-docs",
		"Expected checkout to specify target repository")

	// Test 2: Verify REPO_NAME environment variable is set to target repo
	assert.Contains(t, compiledContent, `REPO_NAME: "microsoft/vscode-docs"`,
		"Expected REPO_NAME env var to be set to target repository")

	// Test 3: Verify token is included for cross-repo checkout
	assert.Contains(t, compiledContent, "token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		"Expected token to be set for cross-repo checkout")

	// Test 4: Verify it does NOT use the default github.repository
	checkoutSection := extractCheckoutSection(compiledContent)
	assert.NotContains(t, checkoutSection, "github.repository",
		"Checkout section should not reference github.repository when target-repo is set")
}

// TestCreatePullRequestSameRepoCheckout tests that without target-repo, we use default checkout
func TestCreatePullRequestSameRepoCheckout(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "same-repo-checkout-test")
	require.NoError(t, err, "Failed to create temp dir")
	defer os.RemoveAll(tmpDir)

	// Create test workflow without target-repo
	workflowContent := `---
on: push
permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read
engine: copilot
safe-outputs:
  create-pull-request:
    draft: true
---

# Same-Repo Test Workflow

Create a pull request in the same repository.
`

	workflowPath := filepath.Join(tmpDir, "same-repo.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644), "Failed to write workflow file")

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "Failed to compile workflow")

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "same-repo.lock.yml")
	compiledBytes, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledContent := string(compiledBytes)

	// Test 1: Verify no explicit repository parameter (uses default)
	checkoutSection := extractCheckoutSection(compiledContent)
	assert.NotContains(t, checkoutSection, "repository:",
		"Checkout section should not have explicit repository when using source repo")

	// Test 2: Verify REPO_NAME uses github.repository expression
	assert.Contains(t, compiledContent, "REPO_NAME: ${{ github.repository }}",
		"Expected REPO_NAME to use github.repository expression for same-repo")

	// Test 3: Verify no token in checkout (not needed for same repo)
	assert.NotContains(t, checkoutSection, "token:",
		"Checkout section should not have token for same-repo checkout")
}

// extractCheckoutSection extracts the checkout step from compiled YAML for inspection
func extractCheckoutSection(content string) string {
	lines := strings.Split(content, "\n")
	inCheckout := false
	var checkoutLines []string

	for _, line := range lines {
		if strings.Contains(line, "name: Checkout repository") {
			inCheckout = true
		}
		if inCheckout {
			checkoutLines = append(checkoutLines, line)
			// Stop at the next step (less indentation than "      -")
			if strings.HasPrefix(line, "      - name:") && !strings.Contains(line, "Checkout repository") {
				break
			}
		}
	}

	return strings.Join(checkoutLines, "\n")
}

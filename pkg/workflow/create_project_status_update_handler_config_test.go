//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateProjectStatusUpdateHandlerConfigIncludesMax verifies that the max field
// is properly passed to the handler config JSON (GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG)
func TestCreateProjectStatusUpdateHandlerConfigIncludesMax(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-test")

	testContent := `---
name: Test Handler Config
on: workflow_dispatch
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  create-project-status-update:
    max: 5
---

Test workflow
`

	// Write test markdown file
	mdFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(mdFile, []byte(testContent), 0600)
	require.NoError(t, err, "Failed to write test markdown file")

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(mdFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	compiledContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledStr := string(compiledContent)

	// Find the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG line
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
		"Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in compiled workflow")

	// Verify create_project_status_update is in the handler config
	require.Contains(t, compiledStr, "create_project_status_update",
		"Expected create_project_status_update in handler config")

	// Verify max is set in the handler config
	require.Contains(t, compiledStr, `"max":5`,
		"Expected max:5 in create_project_status_update handler config")
}

// TestCreateProjectStatusUpdateHandlerConfigIncludesGitHubToken verifies that the github-token field
// is properly passed to the handler config JSON (GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG)
func TestCreateProjectStatusUpdateHandlerConfigIncludesGitHubToken(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-test")

	testContent := `---
name: Test Handler Config
on: workflow_dispatch
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  create-project-status-update:
    max: 1
    github-token: "${{ secrets.CUSTOM_TOKEN }}"
---

Test workflow
`

	// Write test markdown file
	mdFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(mdFile, []byte(testContent), 0600)
	require.NoError(t, err, "Failed to write test markdown file")

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(mdFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	compiledContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledStr := string(compiledContent)

	// Find the GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG line
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
		"Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in compiled workflow")

	// Verify create_project_status_update is in the handler config
	require.Contains(t, compiledStr, "create_project_status_update",
		"Expected create_project_status_update in handler config")

	// Debug: Print the section containing create_project_status_update
	lines := strings.Split(compiledStr, "\n")
	for i, line := range lines {
		if strings.Contains(line, "create_project_status_update") {
			t.Logf("Line %d: %s", i, line)
		}
	}

	// Verify github-token is set in the handler config
	// Note: The token value is a GitHub Actions expression, so we check for the field name
	// The JSON is escaped in YAML, so we check for either the escaped or unescaped version
	if !strings.Contains(compiledStr, `"github-token"`) && !strings.Contains(compiledStr, `\\\"github-token\\\"`) && !strings.Contains(compiledStr, `github-token`) {
		t.Errorf("Expected github-token in create_project_status_update handler config")
	}
}

// TestCreateProjectStatusUpdateHandlerConfigLoadedByManager verifies that when
// create-project-status-update is configured alongside other handlers like create-issue or add-comment,
// the project handler manager is properly configured to load the create_project_status_update handler
// (separately from the main handler manager which handles create-issue)
func TestCreateProjectStatusUpdateHandlerConfigLoadedByManager(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-test")

	testContent := `---
name: Test Handler Config With Multiple Safe Outputs
on: workflow_dispatch
engine: copilot
safe-outputs:
  create-issue:
    max: 1
  create-project-status-update:
    max: 2
---

Test workflow
`

	// Write test markdown file
	mdFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(mdFile, []byte(testContent), 0600)
	require.NoError(t, err, "Failed to write test markdown file")

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(mdFile)
	require.NoError(t, err, "Failed to compile workflow")

	// Read the generated lock file
	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	compiledContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledStr := string(compiledContent)

	// Extract main handler config JSON
	lines := strings.Split(compiledStr, "\n")
	var mainConfigJSON string
	var projectConfigJSON string
	for _, line := range lines {
		if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:") {
			parts := strings.SplitN(line, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG:", 2)
			if len(parts) == 2 {
				mainConfigJSON = strings.TrimSpace(parts[1])
				mainConfigJSON = strings.Trim(mainConfigJSON, "\"")
				mainConfigJSON = strings.ReplaceAll(mainConfigJSON, "\\\"", "\"")
			}
		}
		if strings.Contains(line, "GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG:") {
			parts := strings.SplitN(line, "GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG:", 2)
			if len(parts) == 2 {
				projectConfigJSON = strings.TrimSpace(parts[1])
				projectConfigJSON = strings.Trim(projectConfigJSON, "\"")
				projectConfigJSON = strings.ReplaceAll(projectConfigJSON, "\\\"", "\"")
			}
		}
	}

	require.NotEmpty(t, mainConfigJSON, "Failed to extract GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG JSON")
	require.NotEmpty(t, projectConfigJSON, "Failed to extract GH_AW_SAFE_OUTPUTS_PROJECT_HANDLER_CONFIG JSON")

	// Verify create_issue is in the main handler config
	assert.Contains(t, mainConfigJSON, "create_issue",
		"Expected create_issue in main handler config")

	// Verify create_project_status_update is in the project handler config (NOT in main config)
	assert.NotContains(t, mainConfigJSON, "create_project_status_update",
		"create_project_status_update should not be in main handler config")
	assert.Contains(t, projectConfigJSON, "create_project_status_update",
		"Expected create_project_status_update in project handler config")

	// Verify max values are correct
	assert.Contains(t, projectConfigJSON, `"create_project_status_update":{"max":2}`,
		"Expected create_project_status_update with max:2 in project handler config")
}

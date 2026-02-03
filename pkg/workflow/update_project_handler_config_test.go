//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestUpdateProjectHandlerConfigIncludesFieldDefinitions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-test")

	testContent := strings.Join([]string{
		"---",
		"name: Test Update Project Handler Config",
		"on: workflow_dispatch",
		"engine: copilot",
		"safe-outputs:",
		"  update-project:",
		"    max: 1",
		"    project: \"https://github.com/orgs/test-org/projects/1\"",
		"    field-definitions:",
		"      - name: \"tracker_id\"",
		"        data-type: \"TEXT\"",
		"---",
		"",
		"Test workflow",
		"",
	}, "\n")

	mdFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(mdFile, []byte(testContent), 0600)
	require.NoError(t, err, "Failed to write test markdown file")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(mdFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	compiledContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledStr := string(compiledContent)
	// Note: update-project is now in the main handler config, not the project handler config
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "Expected main handler config env var")
	require.Contains(t, compiledStr, "update_project", "Expected update_project in handler config")

	// field_definitions uses underscore naming in the JSON config passed to JS
	require.True(
		t,
		strings.Contains(compiledStr, "field_definitions") || strings.Contains(compiledStr, "field-definitions"),
		"Expected field definitions in update_project handler config",
	)
}

func TestUpdateProjectWithProjectURLConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-test")

	testContent := `---
name: Test Update Project with Project URL
on: workflow_dispatch
engine: copilot
safe-outputs:
  update-project:
    max: 5
    project: "https://github.com/orgs/nonexistent-test-org-12345/projects/99999"
---

Test workflow
`

	mdFile := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(mdFile, []byte(testContent), 0600)
	require.NoError(t, err, "Failed to write test markdown file")

	compiler := NewCompiler()
	err = compiler.CompileWorkflow(mdFile)
	require.NoError(t, err, "Failed to compile workflow")

	lockFile := filepath.Join(tmpDir, "test-workflow.lock.yml")
	compiledContent, err := os.ReadFile(lockFile)
	require.NoError(t, err, "Failed to read compiled output")

	compiledStr := string(compiledContent)

	// Note: Since update-project is no longer in the project handler manager,
	// GH_AW_PROJECT_URL is not set when only update-project is configured.
	// update-project is now handled by the unified handler, which doesn't set GH_AW_PROJECT_URL.
	// The project URL is passed as part of the handler config instead.
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "Expected main handler config")
	require.Contains(t, compiledStr, "https://github.com/orgs/nonexistent-test-org-12345/projects/99999", "Expected project URL in handler config")
}

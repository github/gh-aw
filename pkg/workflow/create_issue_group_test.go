//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateIssueGroupFieldParsing verifies that the group field is parsed correctly
func TestCreateIssueGroupFieldParsing(t *testing.T) {
	tests := []struct {
		name          string
		frontmatter   string
		expectedGroup bool
	}{
		{
			name: "group enabled with true",
			frontmatter: `---
name: Test Workflow
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 3
    group: true
---

Test content`,
			expectedGroup: true,
		},
		{
			name: "group disabled with false",
			frontmatter: `---
name: Test Workflow
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 3
    group: false
---

Test content`,
			expectedGroup: false,
		},
		{
			name: "group not specified defaults to false",
			frontmatter: `---
name: Test Workflow
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 3
---

Test content`,
			expectedGroup: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "group-test")
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			require.NoError(t, os.WriteFile(testFile, []byte(tt.frontmatter), 0644))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(testFile))

			// Parse the workflow to check the config
			data, err := compiler.ParseWorkflowFile(testFile)
			require.NoError(t, err)

			require.NotNil(t, data.SafeOutputs)
			require.NotNil(t, data.SafeOutputs.CreateIssues)
			assert.Equal(t, tt.expectedGroup, data.SafeOutputs.CreateIssues.Group, "Group field should match expected value")
		})
	}
}

// TestCreateIssueGroupInHandlerConfig verifies that the group flag is passed to the handler config JSON
func TestCreateIssueGroupInHandlerConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "handler-config-group-test")

	testContent := `---
name: Test Handler Config Group
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 2
    group: true
    labels: [test-group]
---

Create test issues with grouping.
`

	testFile := filepath.Join(tmpDir, "test-group-handler.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-group-handler.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	compiledStr := string(compiledContent)

	// Verify GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG contains the group flag
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "Expected handler config in compiled workflow")

	// Extract and verify the JSON contains group: true
	require.Contains(t, compiledStr, `"group":true`, "Expected group flag in handler config JSON")
}

// TestCreateIssueGroupWithoutPermissions verifies compilation with group field and no issues permission
func TestCreateIssueGroupWithoutPermissions(t *testing.T) {
	tmpDir := testutil.TempDir(t, "group-no-permission-test")

	testContent := `---
name: Test Group No Permission
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 5
    group: true
---

Test grouping without explicit issues permission.
`

	testFile := filepath.Join(tmpDir, "test-group-no-perm.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	// Compile the workflow - should succeed (safe-outputs doesn't require explicit permission)
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-group-no-perm.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	compiledStr := string(compiledContent)

	// Verify the workflow compiled and contains the group flag
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG")
	require.Contains(t, compiledStr, `"group":true`)
}

// TestCreateIssueGroupWithTitlePrefix verifies group field works with title-prefix
func TestCreateIssueGroupWithTitlePrefix(t *testing.T) {
	tmpDir := testutil.TempDir(t, "group-title-prefix-test")

	testContent := `---
name: Test Group Title Prefix
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 3
    group: true
    title-prefix: "[Bot] "
    labels: [automated, grouped]
---

Test grouping with title prefix.
`

	testFile := filepath.Join(tmpDir, "test-group-prefix.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-group-prefix.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	compiledStr := string(compiledContent)

	// Verify both group and title_prefix are in the handler config
	assert.Contains(t, compiledStr, `"group":true`, "Expected group:true in compiled workflow")
	assert.Contains(t, compiledStr, `title_prefix`, "Expected title_prefix in compiled workflow")
}

// TestCreateIssueGroupInMCPConfig verifies group flag is passed to MCP config
func TestCreateIssueGroupInMCPConfig(t *testing.T) {
	tmpDir := testutil.TempDir(t, "group-mcp-config-test")

	testContent := `---
name: Test Group MCP Config
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-issue:
    max: 1
    group: true
---

Test MCP config with group.
`

	testFile := filepath.Join(tmpDir, "test-group-mcp.md")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0644))

	// Compile the workflow
	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "test-group-mcp.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	compiledStr := string(compiledContent)

	// The group flag should be in handler config
	require.Contains(t, compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "Should have handler config")
	require.Contains(t, compiledStr, `"group":true`, "Group flag should be in handler config")
}

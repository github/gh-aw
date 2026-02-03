//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestNeutralToolsIntegration(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetSkipValidation(true) // Skip schema validation for this test
	tempDir := testutil.TempDir(t, "test-*")

	workflowContent := `---
on:
  workflow_dispatch:

engine: 
  id: claude

tools:
  bash: ["echo", "ls"]
  web-fetch:
  web-search:
  edit:
  github:
    allowed: ["list_issues"]

safe-outputs:
  create-pull-request:
    title-prefix: "[test] "
---

Test workflow with neutral tools format.
`

	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow file
	lockFilePath := filepath.Join(tempDir, "test-workflow.lock.yml")
	yamlBytes, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	yamlContent := string(yamlBytes)
	// Strip the comment header to check only the actual YAML content
	yamlContentNoComments := testutil.StripYAMLCommentHeader(yamlContent)

	// Should contain Claude tools that were converted from neutral tools
	expectedClaudeTools := []string{
		"Bash(echo)",
		"Bash(ls)",
		"BashOutput",
		"KillBash",
		"WebFetch",
		"WebSearch",
		"Edit",
		"MultiEdit",
		"NotebookEdit",
		"Write",
	}

	for _, tool := range expectedClaudeTools {
		if !strings.Contains(yamlContent, tool) {
			t.Errorf("Expected Claude tool '%s' not found in compiled YAML", tool)
		}
	}

	// Should also contain MCP tools
	if !strings.Contains(yamlContent, "mcp__github__list_issues") {
		t.Error("Expected MCP tool 'mcp__github__list_issues' not found in compiled YAML")
	}

	// Should contain Git commands due to safe-outputs create-pull-request
	expectedGitTools := []string{
		"Bash(git add:*)",
		"Bash(git commit:*)",
		"Bash(git checkout:*)",
	}

	for _, tool := range expectedGitTools {
		if !strings.Contains(yamlContent, tool) {
			t.Errorf("Expected Git tool '%s' not found in compiled YAML", tool)
		}
	}

	// Verify that the old format is not present as YAML keys in the compiled output (excluding comments)
	// The check is for YAML keys specifically, not string literals in bundled JavaScript code
	// YAML keys will have format like "  bash:" or "\nbash:" at the start of a line
	if strings.Contains(yamlContentNoComments, "\n  bash:") ||
		strings.Contains(yamlContentNoComments, "\nbash:") ||
		strings.Contains(yamlContentNoComments, "\n  web-fetch:") ||
		strings.Contains(yamlContentNoComments, "\nweb-fetch:") {
		t.Error("Compiled YAML should not contain neutral tool names as YAML keys")
	}
}

func TestBackwardCompatibilityWithClaudeFormat(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetSkipValidation(true) // Skip schema validation for this test
	tempDir := testutil.TempDir(t, "test-*")

	workflowContent := `---
on:
  workflow_dispatch:

engine: 
  id: claude

tools:
  web-fetch:
  bash: ["echo", "ls"]
  github:
    allowed: ["list_issues"]
---

Test workflow with legacy Claude tools format.
`

	workflowPath := filepath.Join(tempDir, "legacy-workflow.md")
	err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow file
	lockFilePath := filepath.Join(tempDir, "legacy-workflow.lock.yml")
	yamlBytes, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read compiled workflow: %v", err)
	}
	yamlContent := string(yamlBytes)

	expectedTools := []string{
		"Bash(echo)",
		"Bash(ls)",
		"BashOutput",
		"KillBash",
		"WebFetch",
		"mcp__github__list_issues",
	}

	for _, tool := range expectedTools {
		if !strings.Contains(yamlContent, tool) {
			t.Errorf("Expected tool '%s' not found in compiled YAML", tool)
		}
	}
}

//go:build integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestToolsTimeoutIntegration(t *testing.T) {
	// Create a test workflow with timeout
	workflowContent := `---
on: workflow_dispatch
engine: claude
tools:
  timeout: 90
  github:
---

# Test Timeout

Test workflow.
`

	// Write to temporary file
	tmpFile, err := os.CreateTemp("", "test-timeout-*.md")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer os.Remove(stringutil.MarkdownToLockFile(tmpFile.Name()))

	if _, err := tmpFile.WriteString(workflowContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Compile the workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the compiled workflow
	lockFile := stringutil.MarkdownToLockFile(tmpFile.Name())
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Check for MCP_TIMEOUT: 120000 (default startup timeout) - may or may not have quotes
	if !strings.Contains(string(lockContent), "MCP_TIMEOUT: 120000") && !strings.Contains(string(lockContent), `MCP_TIMEOUT: "120000"`) {
		t.Errorf("Expected MCP_TIMEOUT: 120000 in lock file (default startup timeout), got:\n%s", string(lockContent))
	}

	// Check for MCP_TOOL_TIMEOUT: 90000 (custom tool timeout) - may or may not have quotes
	if !strings.Contains(string(lockContent), "MCP_TOOL_TIMEOUT: 90000") && !strings.Contains(string(lockContent), `MCP_TOOL_TIMEOUT: "90000"`) {
		t.Errorf("Expected MCP_TOOL_TIMEOUT: 90000 in lock file, got:\n%s", string(lockContent))
	}

	// Check for GH_AW_TOOL_TIMEOUT: 90 - may or may not have quotes
	if !strings.Contains(string(lockContent), "GH_AW_TOOL_TIMEOUT: 90") && !strings.Contains(string(lockContent), `GH_AW_TOOL_TIMEOUT: "90"`) {
		t.Errorf("Expected GH_AW_TOOL_TIMEOUT: 90 in lock file, got:\n%s", string(lockContent))
	}
}

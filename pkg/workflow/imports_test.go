//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/workflow"
)

func TestCompileWorkflowWithImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared tool file
	sharedToolPath := filepath.Join(tempDir, "shared-tool.md")
	sharedToolContent := `---
on: push
tools:
  custom-mcp:
    url: "https://example.com/mcp"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedToolPath, []byte(sharedToolContent), 0644); err != nil {
		t.Fatalf("Failed to write shared tool file: %v", err)
	}

	// Create a workflow file that imports the shared tool
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-tool.md
tools:
  cache-memory:
    retention-days: 7
---

# Test Workflow

This is a test workflow.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains the imported tool
	if !strings.Contains(workflowData, "custom-mcp") {
		t.Error("Expected compiled workflow to contain custom-mcp from imported file")
	}

	// Verify the MCP URL is present
	if !strings.Contains(workflowData, "https://example.com/mcp") {
		t.Error("Expected compiled workflow to contain MCP URL from imported file")
	}
}

func TestCompileWorkflowWithMultipleImports(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create first shared tool file
	sharedTool1Path := filepath.Join(tempDir, "shared-tool-1.md")
	sharedTool1Content := `---
on: push
tools:
  tool1:
    url: "https://example1.com/mcp"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedTool1Path, []byte(sharedTool1Content), 0644); err != nil {
		t.Fatalf("Failed to write shared tool 1 file: %v", err)
	}

	// Create second shared tool file
	sharedTool2Path := filepath.Join(tempDir, "shared-tool-2.md")
	sharedTool2Content := `---
on: push
tools:
  tool2:
    url: "https://example2.com/mcp"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedTool2Path, []byte(sharedTool2Content), 0644); err != nil {
		t.Fatalf("Failed to write shared tool 2 file: %v", err)
	}

	// Create a workflow file that imports both shared tools
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-tool-1.md
  - shared-tool-2.md
tools:
  cache-memory:
    retention-days: 7
---

# Test Workflow

This is a test workflow with multiple imports.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains both imported tools
	if !strings.Contains(workflowData, "tool1") {
		t.Error("Expected compiled workflow to contain tool1 from first import")
	}

	if !strings.Contains(workflowData, "tool2") {
		t.Error("Expected compiled workflow to contain tool2 from second import")
	}

	// Verify both URLs are present
	if !strings.Contains(workflowData, "https://example1.com/mcp") {
		t.Error("Expected compiled workflow to contain URL from first import")
	}

	if !strings.Contains(workflowData, "https://example2.com/mcp") {
		t.Error("Expected compiled workflow to contain URL from second import")
	}
}

func TestCompileWorkflowWithMCPServersImport(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	// Create a shared mcp-servers file (like tavily-mcp.md)
	sharedMCPPath := filepath.Join(tempDir, "shared-mcp.md")
	sharedMCPContent := `---
on: push
mcp-servers:
  tavily:
    url: "https://mcp.tavily.com/mcp/?tavilyApiKey=test"
    allowed: ["*"]
---
`
	if err := os.WriteFile(sharedMCPPath, []byte(sharedMCPContent), 0644); err != nil {
		t.Fatalf("Failed to write shared MCP file: %v", err)
	}

	// Create a workflow file that imports the shared MCP server
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
imports:
  - shared-mcp.md
tools:
  cache-memory:
    retention-days: 7
---

# Test Workflow

This is a test workflow with imported MCP server.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	workflowData := string(lockFileContent)

	// Verify that the compiled workflow contains the imported MCP server
	if !strings.Contains(workflowData, "tavily") {
		t.Error("Expected compiled workflow to contain tavily MCP server from imported file")
	}

	// Verify the MCP URL is present
	if !strings.Contains(workflowData, "https://mcp.tavily.com/mcp") {
		t.Error("Expected compiled workflow to contain Tavily MCP URL from imported file")
	}

	// Verify it's configured as an HTTP MCP server
	if !strings.Contains(workflowData, `"type": "http"`) {
		t.Error("Expected tavily to be configured as HTTP MCP server")
	}
}

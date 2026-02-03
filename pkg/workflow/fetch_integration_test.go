//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestWebFetchMCPServerAddition tests that when a Codex workflow uses web-fetch,
// the web-fetch MCP server is automatically added
func TestWebFetchMCPServerAddition(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that uses web-fetch with Codex engine (which doesn't support web-fetch natively)
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
tools:
  web-fetch:
---

# Test Workflow

Fetch content from the web.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Verify that the compiled workflow contains the web-fetch MCP server configuration
	lockContent := string(lockData)

	// The TOML config should contain the web-fetch server
	if !strings.Contains(lockContent, `[mcp_servers."web-fetch"]`) {
		t.Errorf("Expected compiled workflow to contain web-fetch MCP server configuration, but it didn't")
	}

	// Verify the Docker command is present
	if !strings.Contains(lockContent, `"mcp/fetch"`) {
		t.Errorf("Expected web-fetch MCP server to use the mcp/fetch Docker image, but it didn't")
	}

	// Verify that the MCP server is configured with Docker
	if !strings.Contains(lockContent, `command = "docker"`) {
		t.Errorf("Expected web-fetch MCP server to have Docker command")
	}
}

// TestWebFetchNotAddedForClaudeEngine tests that when a Claude workflow uses web-fetch,
// the web-fetch MCP server is NOT added (because Claude has native support)
func TestWebFetchNotAddedForClaudeEngine(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that uses web-fetch with Claude engine (which supports web-fetch natively)
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  web-fetch:
---

# Test Workflow

Fetch content from the web.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Verify that the compiled workflow does NOT contain the web-fetch MCP server configuration
	lockContent := string(lockData)

	// Claude uses JSON format, check that web-fetch is NOT configured as an MCP server
	// Look for the MCP server configuration pattern with "command": "docker"
	// We can't simply search for "web-fetch" because Claude will have it in the allowed tools
	if strings.Contains(lockContent, `"web-fetch": {`) && strings.Contains(lockContent, `"command": "docker"`) {
		// Check if both appear close together (indicating MCP server config)
		dockerIdx := strings.Index(lockContent, `"command": "docker"`)
		webFetchIdx := strings.Index(lockContent, `"web-fetch": {`)
		if dockerIdx > 0 && webFetchIdx > 0 && dockerIdx-webFetchIdx < 200 {
			t.Errorf("Expected Claude workflow NOT to contain web-fetch MCP server (since Claude has native web-fetch support), but it did")
		}
	}

	// Instead, Claude should have the WebFetch tool in its allowed tools list
	if !strings.Contains(lockContent, "WebFetch") {
		t.Errorf("Expected Claude workflow to have WebFetch in allowed tools, but it didn't")
	}
}

// TestWebFetchNotAddedForCopilotEngine tests that when a Copilot workflow uses web-fetch,
// the web-fetch MCP server is NOT added (because Copilot has native support)
func TestWebFetchNotAddedForCopilotEngine(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that uses web-fetch with Copilot engine (which supports web-fetch natively)
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
tools:
  web-fetch:
---

# Test Workflow

Fetch content from the web.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Verify that the compiled workflow does NOT contain the web-fetch MCP server configuration
	lockContent := string(lockData)

	// Check that web-fetch is NOT configured as an MCP server (no mcp_servers configuration)
	if strings.Contains(lockContent, `[mcp_servers."web-fetch"]`) {
		t.Errorf("Expected Copilot workflow NOT to contain web-fetch MCP server (since Copilot has native web-fetch support), but it did")
	}

	// Also check for JSON format MCP server config
	if strings.Contains(lockContent, `"web-fetch": {`) && strings.Contains(lockContent, `"command": "docker"`) {
		dockerIdx := strings.Index(lockContent, `"command": "docker"`)
		webFetchIdx := strings.Index(lockContent, `"web-fetch": {`)
		if dockerIdx > 0 && webFetchIdx > 0 && dockerIdx-webFetchIdx < 200 {
			t.Errorf("Expected Copilot workflow NOT to contain web-fetch MCP server, but it did")
		}
	}
}

// TestNoWebFetchNoMCPFetchServer tests that when a workflow doesn't use web-fetch,
// the web-fetch MCP server is not added
func TestNoWebFetchNoMCPFetchServer(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-*")

	// Create a test workflow that doesn't use web-fetch
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
tools:
  bash:
    - echo
---

# Test Workflow

Run some bash commands.
`

	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Create a compiler
	compiler := NewCompiler()

	// Compile the workflow
	err := compiler.CompileWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Read the generated lock file
	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Verify that the compiled workflow does NOT contain the web-fetch MCP server configuration
	lockContent := string(lockData)

	// Check for web-fetch MCP server configuration (Docker-based)
	if strings.Contains(lockContent, `"web-fetch"`) || strings.Contains(lockContent, `[mcp_servers."web-fetch"]`) {
		t.Errorf("Expected workflow without web-fetch NOT to contain web-fetch MCP server, but it did")
	}
}

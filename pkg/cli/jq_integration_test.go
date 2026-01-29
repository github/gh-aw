//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"time"

	"github.com/githubnext/gh-aw/pkg/testutil"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServer_StatusToolWithJq tests the status tool with jq filter parameter
func TestMCPServer_StatusToolWithJq(t *testing.T) {
	// Skip if jq is not available
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("Skipping test: jq not found in PATH")
	}

	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get absolute path to binary before changing directories
	absBinaryPath, err := filepath.Abs(binaryPath)
	if err != nil {
		t.Fatalf("Failed to get absolute path to binary: %v", err)
	}

	// Create a temporary directory with a workflow file
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a test workflow file
	workflowContent := `---
on: push
engine: copilot
---
# Test Workflow
`
	workflowFile := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Save current directory and change to temp directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Initialize git repository in the temp directory using shared helper
	if err := initTestGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess with --cmd flag to use binary directly
	serverCmd := exec.Command(absBinaryPath, "mcp-server", "--cmd", absBinaryPath)
	serverCmd.Dir = tmpDir
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Test 1: Call status tool with jq filter to get just workflow names
	params := &mcp.CallToolParams{
		Name: "status",
		Arguments: map[string]any{
			"jq": ".[].workflow",
		},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Failed to call status tool with jq filter: %v", err)
	}

	// Verify result contains the workflow name
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if textContent.Text == "" {
			t.Error("Expected non-empty text content from status tool with jq filter")
		}
		// The output should contain "test" (the workflow name)
		t.Logf("Status tool output with jq filter: %s", textContent.Text)
	} else {
		t.Error("Expected text content from status tool with jq filter")
	}

	// Test 2: Call status tool with jq filter to count workflows
	params = &mcp.CallToolParams{
		Name: "status",
		Arguments: map[string]any{
			"jq": "length",
		},
	}
	result, err = session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Failed to call status tool with jq count filter: %v", err)
	}

	// Verify result contains a number
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if textContent.Text == "" {
			t.Error("Expected non-empty text content from status tool with jq count filter")
		}
		t.Logf("Status tool count output: %s", textContent.Text)
	} else {
		t.Error("Expected text content from status tool with jq count filter")
	}
}

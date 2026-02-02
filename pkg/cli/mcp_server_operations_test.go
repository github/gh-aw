//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServer_CustomCmd(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
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

This is a test workflow.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	// Initialize git repository using shared helper
	if err := initTestGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Get absolute path to binary
	absBinaryPath, err := filepath.Abs(filepath.Join(originalDir, binaryPath))
	if err != nil {
		t.Fatalf("Failed to get absolute path: %v", err)
	}

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server with --cmd flag pointing to the binary
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

	// Call status tool
	params := &mcp.CallToolParams{
		Name:      "status",
		Arguments: map[string]any{},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Failed to call status tool: %v", err)
	}

	// Verify result is not empty
	if len(result.Content) == 0 {
		t.Error("Expected non-empty result from status tool")
	}

	// Verify result contains text content
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if textContent.Text == "" {
			t.Error("Expected non-empty text content from status tool")
		}
		t.Logf("Status tool output with custom cmd: %s", textContent.Text)
	} else {
		t.Error("Expected text content from status tool")
	}
}

func TestMCPServer_StatusTool(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
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

This is a test workflow.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	// Initialize git repository using shared helper
	if err := initTestGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(filepath.Join(originalDir, binaryPath), "mcp-server", "--cmd", filepath.Join(originalDir, binaryPath))
	serverCmd.Dir = tmpDir
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Call status tool
	params := &mcp.CallToolParams{
		Name:      "status",
		Arguments: map[string]any{},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Failed to call status tool: %v", err)
	}

	// Verify result is not empty
	if len(result.Content) == 0 {
		t.Error("Expected non-empty result from status tool")
	}

	// Verify result contains text content
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if textContent.Text == "" {
			t.Error("Expected non-empty text content from status tool")
		}
	} else {
		t.Error("Expected text content from status tool")
	}
}

func TestMCPServer_AuditTool(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get the current directory for proper path resolution
	originalDir, _ := os.Getwd()

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(filepath.Join(originalDir, binaryPath), "mcp-server", "--cmd", filepath.Join(originalDir, binaryPath))
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Call audit tool with an invalid run ID
	// The tool should return an MCP error for invalid run IDs
	params := &mcp.CallToolParams{
		Name: "audit",
		Arguments: map[string]any{
			"run_id_or_url": "1",
		},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		// Expected behavior: audit command fails with invalid run ID
		t.Logf("Audit tool correctly returned error for invalid run ID: %v", err)
		return
	}

	// Verify result is not empty
	if len(result.Content) == 0 {
		t.Error("Expected non-empty result from audit tool")
	}

	// Verify result contains text content
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from audit tool")
	}

	if textContent.Text == "" {
		t.Error("Expected non-empty text content from audit tool")
	}

	// The audit command should fail with an invalid run ID, but should return
	// a proper error message rather than crashing
	// We just verify that we got some output (either error or success)
	t.Logf("Audit tool output: %s", textContent.Text)
}

func TestMCPServer_ContextCancellation(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get the current directory for proper path resolution
	originalDir, _ := os.Getwd()

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(filepath.Join(originalDir, binaryPath), "mcp-server", "--cmd", filepath.Join(originalDir, binaryPath))
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Test context cancellation for different tools
	tools := []string{"status", "audit"}

	for _, toolName := range tools {
		t.Run(toolName, func(t *testing.T) {
			// Create a context that's already cancelled
			cancelledCtx, immediateCancel := context.WithCancel(context.Background())
			immediateCancel() // Cancel immediately

			var params *mcp.CallToolParams
			switch toolName {
			case "status":
				params = &mcp.CallToolParams{
					Name:      "status",
					Arguments: map[string]any{},
				}
			case "audit":
				params = &mcp.CallToolParams{
					Name: "audit",
					Arguments: map[string]any{
						"run_id_or_url": "1",
					},
				}
			}

			// Call the tool with a cancelled context
			result, err := session.CallTool(cancelledCtx, params)

			// The tool should handle the cancellation gracefully
			// It should either return an error OR return a result with error message
			if err != nil {
				// Check if it's a context cancellation error
				if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "cancel") {
					t.Logf("Tool returned error (acceptable): %v", err)
				} else {
					t.Logf("Tool properly detected cancellation via error: %v", err)
				}
			} else if result != nil && len(result.Content) > 0 {
				// Check if the result contains an error message about cancellation
				if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
					text := textContent.Text
					if strings.Contains(text, "context") || strings.Contains(text, "cancel") {
						t.Logf("Tool properly detected cancellation via result: %s", text)
					} else {
						t.Logf("Tool returned result (may not have detected cancellation immediately): %s", text)
					}
				}
			}

			// The important thing is that the tool doesn't hang or crash
			// Either returning an error or a result with error message is acceptable
		})
	}
}

// TestMCPServer_ToolIcons tests that all tools have icons

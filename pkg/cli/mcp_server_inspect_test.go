//go:build integration

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServer_InspectTool tests that the mcp-inspect tool is exposed and functional
func TestMCPServer_InspectTool(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess with custom command path
	serverCmd := exec.Command(binaryPath, "mcp-server", "--cmd", binaryPath)
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// List tools to verify mcp-inspect is present
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Verify mcp-inspect tool exists
	var mcpInspectTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "mcp-inspect" {
			mcpInspectTool = result.Tools[i]
			break
		}
	}

	if mcpInspectTool == nil {
		t.Fatal("mcp-inspect tool not found in MCP server tools")
	}

	// Verify the tool has proper description
	if mcpInspectTool.Description == "" {
		t.Error("mcp-inspect tool has empty description")
	}

	// Verify the description mentions key functionality
	if len(mcpInspectTool.Description) < 50 {
		t.Errorf("mcp-inspect tool description seems too short: %s", mcpInspectTool.Description)
	}
}

// TestMCPServer_InspectToolInvocation tests calling the mcp-inspect tool
func TestMCPServer_InspectToolInvocation(t *testing.T) {

	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Get absolute path to binary
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

	// Create a test workflow file with MCP configuration
	workflowContent := `---
on: push
engine: copilot
tools:
  github:
    allowed:
      - get_repository
---
# Test Workflow

This is a test workflow with MCP configuration.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Initialize git repository using shared helper
	if err := initTestGitRepo(tmpDir); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	// Create MCP client
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "test-client",
		Version: "1.0.0",
	}, nil)

	// Start the MCP server as a subprocess with custom command path (absolute path)
	serverCmd := exec.Command(absBinaryPath, "mcp-server", "--cmd", absBinaryPath)
	serverCmd.Dir = tmpDir
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Call the mcp-inspect tool without parameters (should list workflows with MCP)
	callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "mcp-inspect",
		Arguments: map[string]any{
			"workflow_file": "",
		},
	})

	if err != nil {
		t.Fatalf("Failed to call mcp-inspect tool: %v", err)
	}

	// Verify we got some output
	if len(callResult.Content) == 0 {
		t.Fatal("mcp-inspect returned no content")
	}

	// Extract text content
	var outputText string
	for _, content := range callResult.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			outputText += textContent.Text
		}
	}

	if outputText == "" {
		t.Fatal("mcp-inspect returned empty text content")
	}

	// The output should mention the test workflow or indicate MCP servers were found
	// Note: We can't be too strict here since the output format may vary
	t.Logf("mcp-inspect output:\n%s", outputText)
}

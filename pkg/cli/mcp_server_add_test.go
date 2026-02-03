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

// TestMCPServer_AddTool tests that the add tool is exposed and functional
func TestMCPServer_AddTool(t *testing.T) {
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

	// List tools to verify add is present
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Verify add tool exists
	var addTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "add" {
			addTool = result.Tools[i]
			break
		}
	}

	if addTool == nil {
		t.Fatal("add tool not found in MCP server tools")
	}

	// Verify the tool has proper description
	if addTool.Description == "" {
		t.Error("add tool has empty description")
	}

	// Verify the description mentions key functionality
	if len(addTool.Description) < 50 {
		t.Errorf("add tool description seems too short: %s", addTool.Description)
	}

	// Verify description contains key phrases
	if !strings.Contains(addTool.Description, "workflows") {
		t.Error("add tool description should mention 'workflows'")
	}
}

// TestMCPServer_AddToolInvocation tests calling the add tool
func TestMCPServer_AddToolInvocation(t *testing.T) {
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

	// Create a temporary directory
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Test 1: Call with just repository (should list workflows)
	t.Run("ListWorkflows", func(t *testing.T) {
		callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "add",
			Arguments: map[string]any{
				"workflows": []any{"githubnext/agentics"},
			},
		})

		if err != nil {
			t.Fatalf("Failed to call add tool: %v", err)
		}

		// Verify we got some output
		if len(callResult.Content) == 0 {
			t.Fatal("add tool returned no content")
		}

		// Extract text content
		var outputText string
		for _, content := range callResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				outputText += textContent.Text
			}
		}

		if outputText == "" {
			t.Fatal("add tool returned empty text content")
		}

		t.Logf("add tool output (list workflows):\n%s", outputText)

		// Output should mention available workflows or indicate repository was processed
		if !strings.Contains(outputText, "workflow") && !strings.Contains(outputText, "Workflow") {
			t.Logf("Warning: Output doesn't mention 'workflow': %s", outputText)
		}
	})

	// Test 2: Call with missing workflows parameter (should fail)
	t.Run("MissingWorkflows", func(t *testing.T) {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add",
			Arguments: map[string]any{},
		})

		// MCP SDK v1.1.0 validates parameters before calling the tool,
		// so we expect an error from CallTool itself
		if err == nil {
			t.Fatal("Expected error when calling add tool with missing workflows parameter")
		}

		// Verify the error message mentions the missing required parameter
		errMsg := err.Error()
		if !strings.Contains(errMsg, "workflows") && !strings.Contains(errMsg, "required") {
			t.Errorf("Expected error message to mention missing 'workflows' parameter, got: %s", errMsg)
		}
	})
}

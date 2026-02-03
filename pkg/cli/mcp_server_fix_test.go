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

// TestMCPServer_FixTool tests that the fix tool is exposed and functional
func TestMCPServer_FixTool(t *testing.T) {
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

	// List tools to verify fix is present
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Verify fix tool exists
	var fixTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "fix" {
			fixTool = result.Tools[i]
			break
		}
	}

	if fixTool == nil {
		t.Fatal("fix tool not found in MCP server tools")
	}

	// Verify the tool has proper description
	if fixTool.Description == "" {
		t.Error("fix tool has empty description")
	}

	// Verify the description mentions key functionality
	if len(fixTool.Description) < 50 {
		t.Errorf("fix tool description seems too short: %s", fixTool.Description)
	}

	// Verify description contains key phrases
	if !strings.Contains(fixTool.Description, "codemod") {
		t.Error("fix tool description should mention 'codemod'")
	}
	if !strings.Contains(fixTool.Description, "workflow") {
		t.Error("fix tool description should mention 'workflow'")
	}
}

// TestMCPServer_FixToolInvocation tests calling the fix tool
func TestMCPServer_FixToolInvocation(t *testing.T) {
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

	// Create a test workflow with deprecated field
	workflowContent := `---
on: push
engine: copilot
timeout_minutes: 30
---
# Test Workflow

This is a test workflow with deprecated timeout_minutes field.
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

	// Start the MCP server as a subprocess with custom command path (absolute path)
	serverCmd := exec.Command(absBinaryPath, "mcp-server", "--cmd", absBinaryPath)
	serverCmd.Dir = tmpDir
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// Test 1: Call fix with dry-run (write: false)
	t.Run("DryRun", func(t *testing.T) {
		callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "fix",
			Arguments: map[string]any{
				"workflows": []any{"test"},
				"write":     false,
			},
		})

		if err != nil {
			t.Fatalf("Failed to call fix tool: %v", err)
		}

		// Verify we got some output
		if len(callResult.Content) == 0 {
			t.Fatal("fix tool returned no content")
		}

		// Extract text content
		var outputText string
		for _, content := range callResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				outputText += textContent.Text
			}
		}

		if outputText == "" {
			t.Fatal("fix tool returned empty text content")
		}

		t.Logf("fix tool output (dry-run):\n%s", outputText)

		// Output should mention the workflow file or indicate fixes needed
		if !strings.Contains(outputText, "test.md") && !strings.Contains(outputText, "test") {
			t.Logf("Warning: Output doesn't mention test workflow: %s", outputText)
		}
	})

	// Test 2: Call fix with write: true to actually apply fixes
	t.Run("WriteMode", func(t *testing.T) {
		callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "fix",
			Arguments: map[string]any{
				"workflows": []any{"test"},
				"write":     true,
			},
		})

		if err != nil {
			t.Fatalf("Failed to call fix tool: %v", err)
		}

		// Verify we got some output
		if len(callResult.Content) == 0 {
			t.Fatal("fix tool returned no content")
		}

		// Extract text content
		var outputText string
		for _, content := range callResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				outputText += textContent.Text
			}
		}

		t.Logf("fix tool output (write mode):\n%s", outputText)

		// Read the workflow file to verify it was fixed
		updatedContent, err := os.ReadFile(workflowPath)
		if err != nil {
			t.Fatalf("Failed to read updated workflow file: %v", err)
		}

		t.Logf("Updated content:\n%s", string(updatedContent))

		// Verify the deprecated field was replaced in frontmatter
		// We need to check only the frontmatter, not the markdown body
		// The frontmatter is between the first and second "---" lines
		contentStr := string(updatedContent)

		// Extract frontmatter section (between first two ---)
		parts := strings.SplitN(contentStr, "---", 3)
		if len(parts) < 3 {
			t.Fatal("Could not parse frontmatter from updated file")
		}
		frontmatter := parts[1]

		if strings.Contains(frontmatter, "timeout_minutes") {
			t.Error("Expected timeout_minutes to be replaced in frontmatter, but it still exists")
		}
		if !strings.Contains(frontmatter, "timeout-minutes") {
			t.Error("Expected timeout-minutes to be present in frontmatter after fix")
		}
	})

	// Test 3: List codemods
	t.Run("ListCodemods", func(t *testing.T) {
		callResult, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "fix",
			Arguments: map[string]any{
				"list_codemods": true,
			},
		})

		if err != nil {
			t.Fatalf("Failed to call fix tool with list_codemods: %v", err)
		}

		// Verify we got some output
		if len(callResult.Content) == 0 {
			t.Fatal("fix tool returned no content for list_codemods")
		}

		// Extract text content
		var outputText string
		for _, content := range callResult.Content {
			if textContent, ok := content.(*mcp.TextContent); ok {
				outputText += textContent.Text
			}
		}

		t.Logf("fix tool output (list codemods):\n%s", outputText)

		// Output should mention available codemods
		if !strings.Contains(outputText, "codemod") && !strings.Contains(outputText, "Codemod") {
			t.Logf("Warning: Output doesn't mention 'codemod': %s", outputText)
		}
	})
}

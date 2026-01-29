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

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServer_ListTools(t *testing.T) {
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

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(binaryPath, "mcp-server", "--cmd", binaryPath)
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// List tools
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Verify expected tools are present
	expectedTools := []string{"status", "compile", "logs", "audit", "mcp-inspect", "add", "update", "fix"}
	toolNames := make(map[string]bool)
	for _, tool := range result.Tools {
		toolNames[tool.Name] = true
	}

	for _, expected := range expectedTools {
		if !toolNames[expected] {
			t.Errorf("Expected tool '%s' not found in MCP server tools", expected)
		}
	}

	// Verify we have exactly the expected number of tools
	if len(result.Tools) != len(expectedTools) {
		t.Errorf("Expected %d tools, got %d", len(expectedTools), len(result.Tools))
	}
}

func TestMCPServer_ServerInfo(t *testing.T) {
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

	// List tools to verify server is working properly
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Verify we can get tools, which means server initialized correctly
	if len(result.Tools) == 0 {
		t.Error("Expected server to have tools available")
	}

	t.Logf("Server initialized successfully with %d tools", len(result.Tools))
}

func TestMCPServer_UpdateToolSchema(t *testing.T) {
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

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(binaryPath, "mcp-server", "--cmd", binaryPath)
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// List tools
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Find the update tool
	var updateTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "update" {
			updateTool = result.Tools[i]
			break
		}
	}

	if updateTool == nil {
		t.Fatal("Update tool not found in MCP server tools")
	}

	// Verify the tool has a description
	if updateTool.Description == "" {
		t.Error("Update tool should have a description")
	}

	// Verify description mentions key functionality
	if !strings.Contains(updateTool.Description, "workflows") {
		t.Error("Update tool description should mention workflows")
	}

	// Verify the tool has input schema
	if updateTool.InputSchema == nil {
		t.Error("Update tool should have an input schema")
	}

	t.Logf("Update tool description: %s", updateTool.Description)
	t.Logf("Update tool schema: %+v", updateTool.InputSchema)
}

// TestMCPServer_CapabilitiesConfiguration tests that server capabilities are correctly configured

func TestMCPServer_CapabilitiesConfiguration(t *testing.T) {
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

	// Get server capabilities from the initialize result
	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("Expected non-nil InitializeResult")
	}

	serverCapabilities := initResult.Capabilities

	// Verify Tools capability is present
	if serverCapabilities.Tools == nil {
		t.Fatal("Expected server to advertise Tools capability")
	}

	// Verify ListChanged is set to false
	if serverCapabilities.Tools.ListChanged {
		t.Error("Expected Tools.ListChanged to be false (tools are static)")
	}

	t.Logf("Server capabilities configured correctly: Tools.ListChanged = %v", serverCapabilities.Tools.ListChanged)
}

// TestMCPServer_ContextCancellation tests that tool handlers properly respond to context cancellation

func TestMCPServer_ToolIcons(t *testing.T) {
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

	// Start the MCP server as a subprocess
	serverCmd := exec.Command(binaryPath, "mcp-server", "--cmd", binaryPath)
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}
	defer session.Close()

	// List tools
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Expected icons for each tool
	expectedIcons := map[string]string{
		"status":      "📊",
		"compile":     "🔨",
		"logs":        "📜",
		"audit":       "🔍",
		"mcp-inspect": "🔎",
		"add":         "➕",
		"update":      "🔄",
		"fix":         "🔧",
	}

	// Verify each tool has an icon
	for _, tool := range result.Tools {
		if len(tool.Icons) == 0 {
			t.Errorf("Tool '%s' is missing an icon", tool.Name)
			continue
		}

		// Check that the icon source matches expected emoji
		if expectedIcon, ok := expectedIcons[tool.Name]; ok {
			if tool.Icons[0].Source != expectedIcon {
				t.Errorf("Tool '%s' has unexpected icon. Expected: %s, Got: %s",
					tool.Name, expectedIcon, tool.Icons[0].Source)
			}
			t.Logf("Tool '%s' has correct icon: %s", tool.Name, tool.Icons[0].Source)
		} else {
			t.Logf("Tool '%s' has icon (not in expected list): %s", tool.Name, tool.Icons[0].Source)
		}
	}

	// Verify we checked all expected tools
	if len(result.Tools) != len(expectedIcons) {
		t.Errorf("Expected %d tools with icons, got %d tools", len(expectedIcons), len(result.Tools))
	}
}

// TestMCPServer_CompileToolWithErrors tests that compile tool returns output even when compilation fails

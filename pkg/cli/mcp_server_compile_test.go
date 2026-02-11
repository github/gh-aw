//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServer_CompileTool(t *testing.T) {
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

This is a test workflow for compilation.
`
	workflowPath := filepath.Join(workflowsDir, "test-compile.md")
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

	// Call compile tool
	params := &mcp.CallToolParams{
		Name:      "compile",
		Arguments: map[string]any{},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Failed to call compile tool: %v", err)
	}

	// Verify result is not empty
	if len(result.Content) == 0 {
		t.Error("Expected non-empty result from compile tool")
	}

	// Verify result contains text content
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if textContent.Text == "" {
			t.Error("Expected non-empty text content from compile tool")
		}
		// The compile tool is callable - it may fail in test environment
		// due to missing gh extension, but we're testing the MCP interface works
		t.Logf("Compile tool output: %s", textContent.Text)
	} else {
		t.Error("Expected text content from compile tool")
	}
}

// // TestMCPServer_LogsTool tests the logs tool
// func TestMCPServer_LogsTool(t *testing.T) {
// 	// Skip if the binary doesn't exist
// 	binaryPath := "../../gh-aw"
// 	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
// 		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
// 	}

// 	// Get the current directory for proper path resolution
// 	originalDir, _ := os.Getwd()

// 	// Create MCP client
// 	client := mcp.NewClient(&mcp.Implementation{
// 		Name:    "test-client",
// 		Version: "1.0.0",
// 	}, nil)

// 	// Start the MCP server as a subprocess
// 	serverCmd := exec.Command(filepath.Join(originalDir, binaryPath), "mcp-server", "--cmd", filepath.Join(originalDir, binaryPath))
// 	transport := &mcp.CommandTransport{Command: serverCmd}

// 	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
// 	defer cancel()

// 	session, err := client.Connect(ctx, transport, nil)
// 	if err != nil {
// 		t.Fatalf("Failed to connect to MCP server: %v", err)
// 	}
// 	defer session.Close()

// 	// Call logs tool
// 	// This will likely fail in test environment due to missing GitHub credentials
// 	// but we're testing that the tool is callable and returns a proper response
// 	params := &mcp.CallToolParams{
// 		Name: "logs",
// 		Arguments: map[string]any{
// 			"count": 1,
// 		},
// 	}
// 	result, err := session.CallTool(ctx, params)
// 	if err != nil {
// 		t.Fatalf("Failed to call logs tool: %v", err)
// 	}

// 	// Verify result is not empty
// 	if len(result.Content) == 0 {
// 		t.Error("Expected non-empty result from logs tool")
// 	}

// 	// Verify result contains text content
// 	textContent, ok := result.Content[0].(*mcp.TextContent)
// 	if !ok {
// 		t.Fatal("Expected text content from logs tool")
// 	}

// 	if textContent.Text == "" {
// 		t.Error("Expected non-empty text content from logs tool")
// 	}

// 	t.Logf("Logs tool output: %s", textContent.Text)
// }

func TestMCPServer_CompileWithSpecificWorkflow(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create a temporary directory with multiple workflow files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create two test workflow files
	workflowContent1 := `---
on: push
engine: copilot
---
# Test Workflow 1

This is the first test workflow.
`
	workflowPath1 := filepath.Join(workflowsDir, "test1.md")
	if err := os.WriteFile(workflowPath1, []byte(workflowContent1), 0644); err != nil {
		t.Fatalf("Failed to write workflow file 1: %v", err)
	}

	workflowContent2 := `---
on: pull_request
engine: claude
---
# Test Workflow 2

This is the second test workflow.
`
	workflowPath2 := filepath.Join(workflowsDir, "test2.md")
	if err := os.WriteFile(workflowPath2, []byte(workflowContent2), 0644); err != nil {
		t.Fatalf("Failed to write workflow file 2: %v", err)
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

	// Call compile tool with specific workflow
	params := &mcp.CallToolParams{
		Name: "compile",
		Arguments: map[string]any{
			"workflows": []string{"test1"},
		},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("Failed to call compile tool: %v", err)
	}

	// Verify result is not empty
	if len(result.Content) == 0 {
		t.Error("Expected non-empty result from compile tool")
	}

	// Verify result contains text content
	if textContent, ok := result.Content[0].(*mcp.TextContent); ok {
		if textContent.Text == "" {
			t.Error("Expected non-empty text content from compile tool")
		}
		// The compile tool is callable - it may fail in test environment
		// due to missing gh extension, but we're testing the MCP interface works
		t.Logf("Compile tool output: %s", textContent.Text)
	} else {
		t.Error("Expected text content from compile tool")
	}
}

// TestMCPServer_UpdateToolSchema tests that the update tool has the correct schema

func TestMCPServer_CompileToolWithErrors(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create a temporary directory with an invalid workflow file
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a workflow file with syntax errors
	workflowContent := `---
on: push
engine: copilot
toolz:
  - invalid-tool
---
# Invalid Workflow

This workflow has a syntax error in the frontmatter.
`
	workflowPath := filepath.Join(workflowsDir, "invalid.md")
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

	// Call compile tool with the invalid workflow
	params := &mcp.CallToolParams{
		Name:      "compile",
		Arguments: map[string]any{},
	}
	result, err := session.CallTool(ctx, params)

	// The key test: compile tool should NOT return an MCP error
	// even though the workflow has compilation errors
	if err != nil {
		t.Errorf("Compile tool should not return MCP error for compilation failures, got: %v", err)
	}

	// Verify we got a result with content
	if result == nil {
		t.Fatal("Expected result from compile tool, got nil")
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty result content from compile tool")
	}

	// Verify result contains text content
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from compile tool")
	}

	if textContent.Text == "" {
		t.Error("Expected non-empty text content from compile tool")
	}

	// Verify the output contains validation error information
	// The JSON output should include error details even though compilation failed
	if !strings.Contains(textContent.Text, "\"valid\"") || !strings.Contains(textContent.Text, "\"errors\"") {
		t.Errorf("Expected JSON output with validation results, got: %s", textContent.Text)
	}

	t.Logf("Compile tool correctly returned validation errors in output: %s", textContent.Text)
}

// TestMCPServer_CompileToolWithMultipleWorkflows tests compiling multiple workflows with mixed results

func TestMCPServer_CompileToolWithMultipleWorkflows(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create a temporary directory with multiple workflow files
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a valid workflow
	validWorkflow := `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
---
# Valid Workflow
This workflow should compile successfully.
`
	validPath := filepath.Join(workflowsDir, "valid.md")
	if err := os.WriteFile(validPath, []byte(validWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write valid workflow: %v", err)
	}

	// Create an invalid workflow
	invalidWorkflow := `---
on: push
engine: copilot
unknown_field: invalid
---
# Invalid Workflow
This workflow has an unknown field.
`
	invalidPath := filepath.Join(workflowsDir, "invalid.md")
	if err := os.WriteFile(invalidPath, []byte(invalidWorkflow), 0644); err != nil {
		t.Fatalf("Failed to write invalid workflow: %v", err)
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

	// Start the MCP server
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

	// Call compile tool for all workflows
	params := &mcp.CallToolParams{
		Name:      "compile",
		Arguments: map[string]any{},
	}
	result, err := session.CallTool(ctx, params)

	// Should not return MCP error even with mixed results
	if err != nil {
		t.Errorf("Compile tool should not return MCP error, got: %v", err)
	}

	// Verify we got results
	if result == nil || len(result.Content) == 0 {
		t.Fatal("Expected non-empty result content")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from compile tool")
	}

	// Parse JSON to verify structure
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Should have results for both workflows
	if len(results) < 2 {
		t.Errorf("Expected at least 2 workflow results, got %d", len(results))
	}

	t.Logf("Compile tool handled multiple workflows correctly: %d results", len(results))
}

// TestMCPServer_CompileToolWithStrictMode tests compile with strict mode flag

func TestMCPServer_CompileToolWithStrictMode(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create a temporary directory with a workflow
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a workflow that might have strict mode issues
	workflowContent := `---
on: push
engine: copilot
strict: false
---
# Test Workflow
This workflow has strict mode disabled in frontmatter.
`
	workflowPath := filepath.Join(workflowsDir, "test.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow: %v", err)
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

	// Start the MCP server
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

	// Call compile tool with strict mode enabled
	params := &mcp.CallToolParams{
		Name: "compile",
		Arguments: map[string]any{
			"strict": true,
		},
	}
	result, err := session.CallTool(ctx, params)

	// Should not return MCP error
	if err != nil {
		t.Errorf("Compile tool should not return MCP error with strict flag, got: %v", err)
	}

	// Verify we got results
	if result == nil || len(result.Content) == 0 {
		t.Fatal("Expected non-empty result content")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from compile tool")
	}

	t.Logf("Compile tool with strict mode returned: %s", textContent.Text)
}

// TestMCPServer_CompileToolWithSpecificWorkflows tests compiling specific workflows by name

func TestMCPServer_CompileToolWithSpecificWorkflows(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	// Create a temporary directory with multiple workflows
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create workflow 1
	workflow1 := `---
on: push
engine: copilot
permissions:
  contents: read
---
# Workflow 1
First test workflow.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "workflow1.md"), []byte(workflow1), 0644); err != nil {
		t.Fatalf("Failed to write workflow1: %v", err)
	}

	// Create workflow 2
	workflow2 := `---
on: pull_request
engine: copilot
permissions:
  contents: read
---
# Workflow 2
Second test workflow.
`
	if err := os.WriteFile(filepath.Join(workflowsDir, "workflow2.md"), []byte(workflow2), 0644); err != nil {
		t.Fatalf("Failed to write workflow2: %v", err)
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

	// Start the MCP server
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

	// Call compile tool for only workflow1
	params := &mcp.CallToolParams{
		Name: "compile",
		Arguments: map[string]any{
			"workflows": []string{"workflow1"},
		},
	}
	result, err := session.CallTool(ctx, params)

	// Should not return MCP error
	if err != nil {
		t.Errorf("Compile tool should not return MCP error, got: %v", err)
	}

	// Verify we got results
	if result == nil || len(result.Content) == 0 {
		t.Fatal("Expected non-empty result content")
	}

	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from compile tool")
	}

	// Parse JSON to verify only workflow1 was compiled
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(textContent.Text), &results); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	// Should have exactly 1 result
	if len(results) != 1 {
		t.Errorf("Expected 1 workflow result, got %d", len(results))
	}

	// Verify it's workflow1
	if len(results) > 0 {
		workflow, _ := results[0]["workflow"].(string)
		if !strings.Contains(workflow, "workflow1") {
			t.Errorf("Expected workflow1 in results, got: %s", workflow)
		}
	}

	t.Logf("Compile tool correctly compiled specific workflow: %s", textContent.Text)
}

// TestMCPServer_CompileToolDescriptionMentionsRecompileRequirement tests that the compile tool
// description clearly states that changes to .md files must be compiled

func TestMCPServer_CompileToolDescriptionMentionsRecompileRequirement(t *testing.T) {
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

	// Find the compile tool
	var compileTool *mcp.Tool
	for i := range result.Tools {
		if result.Tools[i].Name == "compile" {
			compileTool = result.Tools[i]
			break
		}
	}

	if compileTool == nil {
		t.Fatal("Compile tool not found in MCP server tools")
	}

	// Verify the description exists and is not empty
	if compileTool.Description == "" {
		t.Fatal("Compile tool should have a description")
	}

	// Key requirements that must be in the description
	requiredPhrases := []string{
		".github/workflows/*.md",
		"MUST be compiled",
		".lock.yml",
	}

	// Verify each required phrase is present in the description
	description := compileTool.Description
	for _, phrase := range requiredPhrases {
		if !strings.Contains(description, phrase) {
			t.Errorf("Compile tool description should mention '%s' but it doesn't.\nDescription: %s", phrase, description)
		}
	}

	// Verify description emphasizes the importance (should contain warning indicator)
	if !strings.Contains(description, "⚠️") && !strings.Contains(description, "IMPORTANT") {
		t.Error("Compile tool description should emphasize importance with warning or 'IMPORTANT' marker")
	}

	// Verify description explains why compilation is needed
	if !strings.Contains(description, "GitHub Actions") {
		t.Error("Compile tool description should explain that GitHub Actions executes the .lock.yml files")
	}

	t.Logf("Compile tool description successfully emphasizes recompilation requirement:\n%s", description)
}

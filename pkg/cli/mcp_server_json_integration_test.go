//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// isValidJSON checks if a string is valid JSON
func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

// extractJSONFromOutput extracts JSON from output that may contain console formatting
// It looks for the first '[' or '{' character and takes everything from there
func extractJSONFromOutput(output string) string {
	// Find first occurrence of [ or {
	for i, ch := range output {
		if ch == '[' || ch == '{' {
			return output[i:]
		}
	}
	return output
}

// setupMCPServerTest creates a test environment and returns the MCP session
func setupMCPServerTest(t *testing.T, binaryPath string) (*mcp.ClientSession, string, context.Context, context.CancelFunc) {
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
	t.Cleanup(func() { os.Chdir(originalDir) })
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

	// Start the MCP server with --cmd flag to use the binary directly
	serverCmd := exec.Command(absBinaryPath, "mcp-server", "--cmd", absBinaryPath)
	serverCmd.Dir = tmpDir
	transport := &mcp.CommandTransport{Command: serverCmd}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		cancel()
		t.Fatalf("Failed to connect to MCP server: %v", err)
	}

	return session, originalDir, ctx, cancel
}

// TestMCPServer_StatusToolReturnsValidJSON tests that the status tool returns valid JSON
func TestMCPServer_StatusToolReturnsValidJSON(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	session, _, ctx, cancel := setupMCPServerTest(t, binaryPath)
	defer cancel()
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
		t.Fatal("Expected non-empty result from status tool")
	}

	// Get text content
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from status tool")
	}

	if textContent.Text == "" {
		t.Fatal("Expected non-empty text content from status tool")
	}

	// Extract JSON from output (may contain console formatting)
	jsonOutput := extractJSONFromOutput(textContent.Text)

	// Verify JSON is valid
	if !isValidJSON(jsonOutput) {
		t.Errorf("Status tool did not return valid JSON. Output: %s", textContent.Text)
	}

	// Verify JSON can be unmarshaled to expected structure
	var statusData []map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &statusData); err != nil {
		t.Errorf("Failed to unmarshal status JSON: %v", err)
	}

	// Verify expected fields are present
	if len(statusData) > 0 {
		expectedFields := []string{"workflow", "engine_id", "compiled", "status"}
		for _, field := range expectedFields {
			if _, ok := statusData[0][field]; !ok {
				t.Errorf("Expected field '%s' not found in status output", field)
			}
		}
	}

	t.Logf("Status tool returned valid JSON with %d workflows", len(statusData))
}

// TestMCPServer_CompileToolReturnsValidJSON tests that the compile tool returns valid JSON
func TestMCPServer_CompileToolReturnsValidJSON(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	session, _, ctx, cancel := setupMCPServerTest(t, binaryPath)
	defer cancel()
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
		t.Fatal("Expected non-empty result from compile tool")
	}

	// Get text content
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from compile tool")
	}

	if textContent.Text == "" {
		t.Fatal("Expected non-empty text content from compile tool")
	}

	// Extract JSON from output (may contain console formatting)
	jsonOutput := extractJSONFromOutput(textContent.Text)

	// Verify JSON is valid
	if !isValidJSON(jsonOutput) {
		t.Errorf("Compile tool did not return valid JSON. Output: %s", textContent.Text)
	}

	// Verify JSON can be unmarshaled to expected structure
	var compileData []map[string]any
	if err := json.Unmarshal([]byte(jsonOutput), &compileData); err != nil {
		t.Errorf("Failed to unmarshal compile JSON: %v", err)
	}

	// Verify expected fields are present
	if len(compileData) > 0 {
		expectedFields := []string{"workflow", "valid"}
		for _, field := range expectedFields {
			if _, ok := compileData[0][field]; !ok {
				t.Errorf("Expected field '%s' not found in compile output", field)
			}
		}
	}

	t.Logf("Compile tool returned valid JSON with %d workflows", len(compileData))
}

// TestMCPServer_AuditToolReturnsValidJSON tests that the audit tool returns valid JSON
func TestMCPServer_AuditToolReturnsValidJSON(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	session, _, ctx, cancel := setupMCPServerTest(t, binaryPath)
	defer cancel()
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
		t.Fatal("Expected non-empty result from audit tool")
	}

	// Get text content
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from audit tool")
	}

	if textContent.Text == "" {
		t.Fatal("Expected non-empty text content from audit tool")
	}

	// The audit tool may return an error message for invalid run IDs
	// We check if the output contains "Error:" which indicates an error message
	if len(textContent.Text) >= 6 && textContent.Text[0:6] == "Error:" {
		t.Logf("Audit tool returned error message as expected for invalid run ID")
		return
	}

	// If not an error, verify JSON is valid
	if !isValidJSON(textContent.Text) {
		// Try extracting JSON from output
		jsonOutput := extractJSONFromOutput(textContent.Text)
		if !isValidJSON(jsonOutput) {
			t.Errorf("Audit tool did not return valid JSON. Output: %s", textContent.Text)
		} else {
			// Verify JSON can be unmarshaled to expected structure
			var auditData map[string]any
			if err := json.Unmarshal([]byte(jsonOutput), &auditData); err != nil {
				t.Errorf("Failed to unmarshal audit JSON: %v", err)
			}
			t.Logf("Audit tool returned valid JSON structure")
		}
	} else {
		// Verify JSON can be unmarshaled to expected structure
		var auditData map[string]any
		if err := json.Unmarshal([]byte(textContent.Text), &auditData); err != nil {
			t.Errorf("Failed to unmarshal audit JSON: %v", err)
		}
		t.Logf("Audit tool returned valid JSON structure")
	}
}

// TestMCPServer_LogsToolReturnsValidJSON tests that the logs tool returns valid JSON
func TestMCPServer_LogsToolReturnsValidJSON(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	session, _, ctx, cancel := setupMCPServerTest(t, binaryPath)
	defer cancel()
	defer session.Close()

	// Call logs tool with minimal parameters
	// The tool should return an MCP error without proper GitHub credentials/workflow runs
	params := &mcp.CallToolParams{
		Name: "logs",
		Arguments: map[string]any{
			"count": 1,
		},
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		// Expected behavior: logs command fails without valid workflow runs
		t.Logf("Logs tool correctly returned error (expected without GitHub credentials/workflow runs): %v", err)
		return
	}

	// Verify result is not empty
	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty result from logs tool")
	}

	// Get text content
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("Expected text content from logs tool")
	}

	if textContent.Text == "" {
		t.Fatal("Expected non-empty text content from logs tool")
	}

	// The logs tool may return an error message in test environment
	if len(textContent.Text) >= 6 && textContent.Text[0:6] == "Error:" {
		t.Logf("Logs tool returned error message (expected in test environment without GitHub credentials)")
		return
	}

	// If not an error, verify JSON is valid
	if !isValidJSON(textContent.Text) {
		// Try extracting JSON from output
		jsonOutput := extractJSONFromOutput(textContent.Text)
		if !isValidJSON(jsonOutput) {
			t.Errorf("Logs tool did not return valid JSON. Output: %s", textContent.Text)
		} else {
			// Verify JSON can be unmarshaled to expected structure
			var logsData map[string]any
			if err := json.Unmarshal([]byte(jsonOutput), &logsData); err != nil {
				t.Errorf("Failed to unmarshal logs JSON: %v", err)
			}
			t.Logf("Logs tool returned valid JSON structure")
		}
	} else {
		// Verify JSON can be unmarshaled to expected structure
		var logsData map[string]any
		if err := json.Unmarshal([]byte(textContent.Text), &logsData); err != nil {
			t.Errorf("Failed to unmarshal logs JSON: %v", err)
		}
		t.Logf("Logs tool returned valid JSON structure")
	}
}

// TestMCPServer_AllToolsReturnContent tests that all tools return non-empty content
func TestMCPServer_AllToolsReturnContent(t *testing.T) {
	// Skip if the binary doesn't exist
	binaryPath := "../../gh-aw"
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		t.Skip("Skipping test: gh-aw binary not found. Run 'make build' first.")
	}

	session, _, ctx, cancel := setupMCPServerTest(t, binaryPath)
	defer cancel()
	defer session.Close()

	// Define test cases for all tools
	testCases := []struct {
		name          string
		toolName      string
		args          map[string]any
		expectJSON    bool
		mayFailInTest bool // Tool may return MCP error in test environment
	}{
		{
			name:       "status",
			toolName:   "status",
			args:       map[string]any{},
			expectJSON: true,
		},
		{
			name:       "compile",
			toolName:   "compile",
			args:       map[string]any{},
			expectJSON: true,
		},
		{
			name:     "audit",
			toolName: "audit",
			args: map[string]any{
				"run_id": int64(1),
			},
			expectJSON:    false, // May return error message
			mayFailInTest: true,  // Expected to fail with invalid run ID
		},
		{
			name:     "logs",
			toolName: "logs",
			args: map[string]any{
				"count": 1,
			},
			expectJSON:    false, // May return error message in test environment
			mayFailInTest: true,  // Expected to fail without workflow runs
		},
		{
			name:       "mcp-inspect",
			toolName:   "mcp-inspect",
			args:       map[string]any{},
			expectJSON: false, // Returns text output
		},
		{
			name:          "update",
			toolName:      "update",
			args:          map[string]any{},
			expectJSON:    false, // Returns text output
			mayFailInTest: true,  // Expected to fail without changes
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := &mcp.CallToolParams{
				Name:      tc.toolName,
				Arguments: tc.args,
			}

			result, err := session.CallTool(ctx, params)
			if err != nil {
				if tc.mayFailInTest {
					t.Logf("%s tool correctly returned error in test environment: %v", tc.toolName, err)
					return
				}
				t.Fatalf("Failed to call %s tool: %v", tc.toolName, err)
			}

			// Verify result is not empty
			if len(result.Content) == 0 {
				t.Errorf("Expected non-empty result from %s tool", tc.toolName)
				return
			}

			// Get text content
			textContent, ok := result.Content[0].(*mcp.TextContent)
			if !ok {
				t.Errorf("Expected text content from %s tool", tc.toolName)
				return
			}

			if textContent.Text == "" {
				t.Errorf("Expected non-empty text content from %s tool", tc.toolName)
				return
			}

			// If tool is expected to return JSON, validate it
			if tc.expectJSON {
				// Skip JSON validation if output starts with "Error:"
				if len(textContent.Text) >= 6 && textContent.Text[0:6] == "Error:" {
					t.Logf("%s tool returned error (may be expected in test environment)", tc.toolName)
					return
				}

				// Extract JSON from output (may contain console formatting)
				jsonOutput := extractJSONFromOutput(textContent.Text)
				if !isValidJSON(jsonOutput) {
					t.Errorf("%s tool did not return valid JSON. Output: %s", tc.toolName, textContent.Text)
				} else {
					t.Logf("%s tool returned valid JSON", tc.toolName)
				}
			} else {
				// For non-JSON tools, just log that we got content
				outputPreview := textContent.Text
				if len(outputPreview) > 100 {
					outputPreview = outputPreview[:100] + "..."
				}
				t.Logf("%s tool returned content: %s", tc.toolName, outputPreview)
			}
		})
	}
}

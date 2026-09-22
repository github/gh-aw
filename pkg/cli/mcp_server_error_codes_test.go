//go:build integration

package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServer_ErrorCodes_InvalidParams tests that InvalidParams error code is returned for parameter validation errors
func TestMCPServer_ErrorCodes_InvalidParams(t *testing.T) {
	binaryPath := testutil.RequireGhAwBinary(t)

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

	// Test 1: add tool with missing workflows parameter
	t.Run("add_missing_workflows", func(t *testing.T) {
		params := &mcp.CallToolParams{
			Name:      "add",
			Arguments: map[string]any{}, // Missing required workflows
		}

		result, err := session.CallTool(ctx, params)
		if err != nil {
			// Protocol error (older SDK behavior)
			errMsg := err.Error()
			if !strings.Contains(errMsg, "missing required parameter") && !strings.Contains(errMsg, "missing properties") {
				t.Errorf("Expected error message about missing parameter, got: %s", errMsg)
			} else {
				t.Logf("✓ Correct protocol error for missing workflows: %s", errMsg)
			}
			return
		}

		// Tool error (SDK v1.5.0+ behavior - schema validation returns IsError=true)
		if result == nil || !result.IsError {
			t.Error("Expected error for missing workflows parameter, got nil")
			return
		}

		if len(result.Content) > 0 {
			if tc, ok := result.Content[0].(*mcp.TextContent); ok {
				if !strings.Contains(tc.Text, "missing required parameter") && !strings.Contains(tc.Text, "missing properties") {
					t.Errorf("Expected error message about missing parameter, got: %s", tc.Text)
				} else {
					t.Logf("✓ Correct tool error for missing workflows: %s", tc.Text)
				}
			} else {
				t.Errorf("Expected text content in tool error, got: %T", result.Content[0])
			}
		} else {
			t.Error("Expected non-empty content in tool error for missing workflows parameter")
		}
	})

	// Test 2: logs tool with conflicting firewall parameters
	t.Run("logs_conflicting_params", func(t *testing.T) {
		params := &mcp.CallToolParams{
			Name: "logs",
			Arguments: map[string]any{
				"firewall":    true,
				"no_firewall": true, // Conflicting with firewall
			},
		}

		_, err := session.CallTool(ctx, params)
		if err == nil {
			t.Error("Expected error for conflicting parameters, got nil")
			return
		}

		// The error message should contain the conflicting parameters error
		errMsg := err.Error()
		if !strings.Contains(errMsg, "conflicting parameters") {
			t.Errorf("Expected error message about conflicting parameters, got: %s", errMsg)
		} else {
			t.Logf("✓ Correct error for conflicting parameters: %s", errMsg)
		}
	})

}

// isTestEnvPermissionError returns true when the error string indicates a permission or
// authentication issue expected in CI/test environments without full GitHub credentials.
func isTestEnvPermissionError(errMsg string) bool {
	return strings.Contains(errMsg, "could not determine repository") ||
		strings.Contains(errMsg, "permission denied") ||
		strings.Contains(errMsg, "failed to get repository")
}

// TestMCPServer_ErrorCodes_InternalError tests that audit failures return structured JSON
// content rather than a protocol-level MCP error (-32603).
func TestMCPServer_ErrorCodes_InternalError(t *testing.T) {
	binaryPath := testutil.RequireGhAwBinary(t)

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

	// Test: audit tool with invalid run_id_or_url returns a JSON error envelope, not an MCP error.
	t.Run("audit_invalid_run_id_returns_json", func(t *testing.T) {
		params := &mcp.CallToolParams{
			Name: "audit",
			Arguments: map[string]any{
				"run_id_or_url": "1", // Invalid / non-existent run ID
			},
		}

		result, err := session.CallTool(ctx, params)
		if err != nil {
			// A permission-check failure (e.g. no gh auth) is acceptable in CI.
			if isTestEnvPermissionError(err.Error()) {
				t.Logf("Skipping assertion: permission check failed as expected in test environment: %s", err.Error())
				return
			}
			t.Errorf("Expected audit to return JSON content, not an MCP error, got: %s", err.Error())
			return
		}

		// The tool must not set IsError=true – that would cause the bridge to exit with code 1.
		if result.IsError {
			t.Errorf("Expected IsError=false from audit tool (graceful JSON error), got IsError=true; content: %v", result.Content)
			return
		}

		// The tool must return text content that is valid JSON containing an "error" field.
		if len(result.Content) == 0 {
			t.Fatal("Expected non-empty content from audit tool")
		}
		textContent, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatal("Expected text content from audit tool")
		}

		var envelope map[string]any
		if jsonErr := json.Unmarshal([]byte(textContent.Text), &envelope); jsonErr != nil {
			t.Errorf("Audit response is not valid JSON: %v\nContent: %s", jsonErr, textContent.Text)
			return
		}
		if _, hasError := envelope["error"]; !hasError {
			t.Errorf("Expected 'error' field in audit JSON response, got: %s", textContent.Text)
		}
		if _, hasRunID := envelope["run_ids_or_urls"]; !hasRunID {
			t.Errorf("Expected 'run_ids_or_urls' field in audit JSON response, got: %s", textContent.Text)
		}
		if _, hasSuggestions := envelope["suggestions"]; !hasSuggestions {
			t.Errorf("Expected 'suggestions' field in audit JSON response, got: %s", textContent.Text)
		}
		t.Logf("✓ audit returned graceful JSON error envelope (IsError=false): %s", textContent.Text)
	})
}

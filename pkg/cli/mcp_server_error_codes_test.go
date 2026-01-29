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

	"github.com/githubnext/gh-aw/pkg/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPServer_ErrorCodes_InvalidParams tests that InvalidParams error code is returned for parameter validation errors
func TestMCPServer_ErrorCodes_InvalidParams(t *testing.T) {
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

	// Test 1: add tool with missing workflows parameter
	t.Run("add_missing_workflows", func(t *testing.T) {
		params := &mcp.CallToolParams{
			Name:      "add",
			Arguments: map[string]any{}, // Missing required workflows
		}

		_, err := session.CallTool(ctx, params)
		if err == nil {
			t.Error("Expected error for missing workflows parameter, got nil")
			return
		}

		// The error message should contain the InvalidParams error message
		errMsg := err.Error()
		if !strings.Contains(errMsg, "missing required parameter") && !strings.Contains(errMsg, "missing properties") {
			t.Errorf("Expected error message about missing parameter, got: %s", errMsg)
		} else {
			t.Logf("✓ Correct error for missing workflows: %s", errMsg)
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

	// Test 3: invalid jq filter
	t.Run("status_invalid_jq_filter", func(t *testing.T) {
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

		// Initialize git repository using shared helper
		if err := initTestGitRepo(tmpDir); err != nil {
			t.Fatalf("Failed to initialize git repository: %v", err)
		}

		// Start new MCP server in the temp directory
		serverCmd := exec.Command(filepath.Join(originalDir, binaryPath), "mcp-server", "--cmd", filepath.Join(originalDir, binaryPath))
		serverCmd.Dir = tmpDir
		transport := &mcp.CommandTransport{Command: serverCmd}

		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()

		session2, err := client.Connect(ctx2, transport, nil)
		if err != nil {
			t.Fatalf("Failed to connect to MCP server: %v", err)
		}
		defer session2.Close()

		params := &mcp.CallToolParams{
			Name: "status",
			Arguments: map[string]any{
				"jq": ".invalid[syntax", // Invalid jq filter
			},
		}

		_, err = session2.CallTool(ctx2, params)
		if err == nil {
			t.Error("Expected error for invalid jq filter, got nil")
			return
		}

		// The error message should contain the invalid jq filter error
		errMsg := err.Error()
		if !strings.Contains(errMsg, "invalid jq filter") {
			t.Errorf("Expected error message about invalid jq filter, got: %s", errMsg)
		} else {
			t.Logf("✓ Correct error for invalid jq filter: %s", errMsg)
		}
	})
}

// TestMCPServer_ErrorCodes_InternalError tests that InternalError code is returned for execution failures
func TestMCPServer_ErrorCodes_InternalError(t *testing.T) {
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

	// Test: audit tool with invalid run_id_or_url (should cause internal error)
	t.Run("audit_invalid_run_id", func(t *testing.T) {
		params := &mcp.CallToolParams{
			Name: "audit",
			Arguments: map[string]any{
				"run_id_or_url": "1", // Invalid run ID
			},
		}

		_, err := session.CallTool(ctx, params)
		if err == nil {
			t.Error("Expected error for invalid run_id_or_url, got nil")
			return
		}

		// The error message should contain the failed audit error or validation error
		errMsg := err.Error()
		if !strings.Contains(errMsg, "failed to audit") && !strings.Contains(errMsg, "could not determine repository") {
			t.Errorf("Expected error message about failed audit or invalid parameters, got: %s", errMsg)
		} else {
			t.Logf("✓ Correct error for failed audit: %s", errMsg)
		}
	})
}

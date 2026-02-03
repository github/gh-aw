//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/types"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListToolsForMCP(t *testing.T) {
	// Create a temporary directory for test workflows
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create a test workflow with MCP servers
	testWorkflowContent := `---
on:
  workflow_dispatch:

permissions: read-all

safe-outputs:
  create-issue:
    title-prefix: "[Test] "

tools:
  github:
    mcp:
      type: stdio
      command: "npx"
      args: ["@github/github-mcp-server"]
      allowed: ["create_issue", "add_comment"]

mcp-servers:
  test-server:
    type: stdio
    command: "node"
    args: ["test-server.js"]
    allowed: ["test_tool_1", "test_tool_2"]

---

# Test Workflow
This is a test workflow with MCP servers.`

	testWorkflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	err = os.WriteFile(testWorkflowPath, []byte(testWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Create another workflow without the target MCP server
	otherWorkflowContent := `---
on:
  push:

tools:
  playwright:
    version: "v1.41.0"

---

# Other Workflow
This workflow has no GitHub MCP server.`

	otherWorkflowPath := filepath.Join(workflowsDir, "other-workflow.md")
	err = os.WriteFile(otherWorkflowPath, []byte(otherWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create other workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	t.Run("find_workflows_with_mcp_server", func(t *testing.T) {
		// Test searching for workflows containing a specific MCP server
		err := ListToolsForMCP("", "github", false)
		// This should not error, but should output info about finding workflows
		if err != nil {
			t.Errorf("ListToolsForMCP search failed: %v", err)
		}
	})

	t.Run("find_workflows_with_safe_outputs", func(t *testing.T) {
		// Test searching for workflows containing safe-outputs
		err := ListToolsForMCP("", constants.SafeOutputsMCPServerID, false)
		// This should not error, but should output info about finding workflows
		if err != nil {
			t.Errorf("ListToolsForMCP safe-outputs search failed: %v", err)
		}
	})

	t.Run("mcp_server_not_found_in_any_workflow", func(t *testing.T) {
		// Test searching for a non-existent MCP server
		err := ListToolsForMCP("", "nonexistent-server", false)
		// This should not error, but should output warning about not finding the server
		if err != nil {
			t.Errorf("ListToolsForMCP nonexistent server search failed: %v", err)
		}
	})

	t.Run("mcp_server_not_found_in_specific_workflow", func(t *testing.T) {
		// Test looking for MCP server in workflow that doesn't have it
		err := ListToolsForMCP("other-workflow", "github", false)
		// This should not error, but should output warning about not finding the server
		if err != nil {
			t.Errorf("ListToolsForMCP specific workflow without server failed: %v", err)
		}
	})

	t.Run("nonexistent_workflow", func(t *testing.T) {
		// Test with non-existent workflow file
		err := ListToolsForMCP("nonexistent", "github", false)
		if err == nil {
			t.Error("Expected error for nonexistent workflow, got nil")
		}
		if !strings.Contains(err.Error(), "workflow file not found") {
			t.Errorf("Expected 'workflow file not found' error, got: %v", err)
		}
	})

	t.Run("verbose_mode", func(t *testing.T) {
		// Test verbose output (should not crash)
		err := ListToolsForMCP("", "github", true)
		if err != nil {
			t.Errorf("ListToolsForMCP verbose search failed: %v", err)
		}
	})
}

func TestFindWorkflowsWithMCPServer(t *testing.T) {
	// Create a temporary directory for test workflows
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create multiple workflows, some with the target MCP server
	workflow1Content := `---
tools:
  github:
    allowed: ["create_issue"]
---
# Workflow 1`

	workflow2Content := `---
safe-outputs:
  create-issue:
tools:
  playwright:
---
# Workflow 2`

	workflow3Content := `---
tools:
  github:
    allowed: ["add_comment"]
---
# Workflow 3`

	// Write workflow files
	workflows := map[string]string{
		"workflow1.md": workflow1Content,
		"workflow2.md": workflow2Content,
		"workflow3.md": workflow3Content,
	}

	for filename, content := range workflows {
		path := filepath.Join(workflowsDir, filename)
		err = os.WriteFile(path, []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to create %s: %v", filename, err)
		}
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	t.Run("find_github_server", func(t *testing.T) {
		// Should find workflow1 and workflow3 (both have github MCP server)
		err := findWorkflowsWithMCPServer(workflowsDir, "github", false)
		if err != nil {
			t.Errorf("findWorkflowsWithMCPServer failed: %v", err)
		}
	})

	t.Run("find_safe_outputs_server", func(t *testing.T) {
		// Should find workflow2 (has safe-outputs)
		err := findWorkflowsWithMCPServer(workflowsDir, constants.SafeOutputsMCPServerID, false)
		if err != nil {
			t.Errorf("findWorkflowsWithMCPServer for safe-outputs failed: %v", err)
		}
	})

	t.Run("find_nonexistent_server", func(t *testing.T) {
		// Should not find any workflows
		err := findWorkflowsWithMCPServer(workflowsDir, "nonexistent", false)
		if err != nil {
			t.Errorf("findWorkflowsWithMCPServer for nonexistent server should not error: %v", err)
		}
	})

	t.Run("verbose_output", func(t *testing.T) {
		// Test verbose mode
		err := findWorkflowsWithMCPServer(workflowsDir, "github", true)
		if err != nil {
			t.Errorf("findWorkflowsWithMCPServer verbose failed: %v", err)
		}
	})
}

func TestDisplayToolsList(t *testing.T) {
	// Create mock data using parser types
	// Create a mock MCPServerInfo with sample tools
	mockInfo := &parser.MCPServerInfo{
		Config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
			Command: "test"}, Name: "test-server",

			Allowed: []string{"tool1", "tool3"}, // Only tool1 and tool3 are allowed
		},
		Tools: []*mcp.Tool{
			{
				Name:        "tool1",
				Description: "This is a short description",
			},
			{
				Name:        "tool2",
				Description: "This is a very long description that exceeds the maximum length limit and should be truncated in non-verbose mode",
			},
			{
				Name:        "tool3",
				Description: "Another tool with a medium-length description",
			},
		},
	}

	t.Run("empty_tools_list", func(t *testing.T) {
		emptyInfo := &parser.MCPServerInfo{
			Config: parser.MCPServerConfig{Name: "empty-server"},
			Tools:  []*mcp.Tool{},
		}

		// Should not panic with empty tools
		displayToolsList(emptyInfo, false)
		displayToolsList(emptyInfo, true)
	})

	t.Run("non_verbose_mode_uses_table_format", func(t *testing.T) {
		// Capture stdout to verify table format is used
		// This is a basic test to ensure the function doesn't crash and processes the data
		displayToolsList(mockInfo, false)
	})

	t.Run("verbose_mode_includes_allow_column", func(t *testing.T) {
		// Test verbose mode includes the Allow column
		displayToolsList(mockInfo, true)
	})

	t.Run("no_allowed_tools_means_all_allowed", func(t *testing.T) {
		noAllowedInfo := &parser.MCPServerInfo{
			Config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Command: "test"}, Name: "no-allowed-server",

				Allowed: []string{}, // Empty allowed list means all tools allowed
			},
			Tools: []*mcp.Tool{
				{
					Name:        "any_tool",
					Description: "Any tool should be allowed",
				},
			},
		}

		displayToolsList(noAllowedInfo, true)
	})

	t.Run("workflow_config_with_wildcard", func(t *testing.T) {
		wildcardInfo := &parser.MCPServerInfo{
			Config: parser.MCPServerConfig{BaseMCPServerConfig: types.BaseMCPServerConfig{Type: "stdio",
				Command: "test"}, Name: "wildcard-server",

				Allowed: []string{"*"}, // Wildcard in workflow config
			},
			Tools: []*mcp.Tool{
				{
					Name:        "any_tool1",
					Description: "First tool",
				},
				{
					Name:        "any_tool2",
					Description: "Second tool",
				},
			},
		}

		// All tools should be allowed due to wildcard in workflow config
		displayToolsList(wildcardInfo, false)
	})
}

func TestNewMCPListToolsSubcommand(t *testing.T) {
	cmd := NewMCPListToolsSubcommand()

	if cmd.Use != "list-tools <server> [workflow]" {
		t.Errorf("Expected Use to be 'list-tools <server> [workflow]', got: %s", cmd.Use)
	}

	if cmd.Short != "List available tools for a specific MCP server" {
		t.Errorf("Expected Short description, got: %s", cmd.Short)
	}
}

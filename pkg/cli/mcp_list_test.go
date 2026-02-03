//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/types"
)

func TestListWorkflowMCP(t *testing.T) {
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
      allowed: ["create_issue"]

---

# Test Workflow
This is a test workflow.`

	testWorkflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	err = os.WriteFile(testWorkflowPath, []byte(testWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	t.Run("list_specific_workflow", func(t *testing.T) {
		err := ListWorkflowMCP("test-workflow", false)
		if err != nil {
			t.Errorf("ListWorkflowMCP failed: %v", err)
		}
	})

	t.Run("list_specific_workflow_verbose", func(t *testing.T) {
		err := ListWorkflowMCP("test-workflow", true)
		if err != nil {
			t.Errorf("ListWorkflowMCP verbose failed: %v", err)
		}
	})

	t.Run("list_all_workflows", func(t *testing.T) {
		err := ListWorkflowMCP("", false)
		if err != nil {
			t.Errorf("ListWorkflowMCP all workflows failed: %v", err)
		}
	})

	t.Run("nonexistent_workflow", func(t *testing.T) {
		err := ListWorkflowMCP("nonexistent", false)
		if err == nil {
			t.Error("Expected error for nonexistent workflow, got nil")
		}
		if !strings.Contains(err.Error(), "workflow file not found") {
			t.Errorf("Expected 'workflow file not found' error, got: %v", err)
		}
	})
}

func TestListWorkflowsWithMCPServers(t *testing.T) {
	// Create a temporary directory for test workflows
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, constants.GetWorkflowDir())
	err := os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create workflow with MCP servers
	mcpWorkflowContent := `---
safe-outputs:
  create-issue:
tools:
  github:
    mcp:
      type: stdio
---
# MCP Workflow`

	// Create workflow without MCP servers
	noMcpWorkflowContent := `---
on:
  push:
---
# No MCP Workflow`

	// Write test files
	err = os.WriteFile(filepath.Join(workflowsDir, "with-mcp.md"), []byte(mcpWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create MCP workflow file: %v", err)
	}

	err = os.WriteFile(filepath.Join(workflowsDir, "without-mcp.md"), []byte(noMcpWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create non-MCP workflow file: %v", err)
	}

	// Change to the temporary directory
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)
	os.Chdir(tmpDir)

	t.Run("list_workflows_with_mcp", func(t *testing.T) {
		err := listWorkflowsWithMCPServers(".github/workflows", false)
		if err != nil {
			t.Errorf("listWorkflowsWithMCPServers failed: %v", err)
		}
	})

	t.Run("list_workflows_with_mcp_verbose", func(t *testing.T) {
		err := listWorkflowsWithMCPServers(".github/workflows", true)
		if err != nil {
			t.Errorf("listWorkflowsWithMCPServers verbose failed: %v", err)
		}
	})

	t.Run("nonexistent_directory", func(t *testing.T) {
		err := listWorkflowsWithMCPServers("nonexistent", false)
		if err == nil {
			t.Error("Expected error for nonexistent directory, got nil")
		}
		if !strings.Contains(err.Error(), "workflows directory not found") {
			t.Errorf("Expected 'workflows directory not found' error, got: %v", err)
		}
	})
}

func TestNewMCPListSubcommand(t *testing.T) {
	cmd := NewMCPListSubcommand()

	if cmd.Use != "list [workflow]" {
		t.Errorf("Expected Use to be 'list [workflow]', got %s", cmd.Use)
	}

	if cmd.Short != "List MCP servers defined in agentic workflows" {
		t.Errorf("Expected Short description, got %s", cmd.Short)
	}

	// Check that the command accepts 0 or 1 arguments
	if cmd.Args == nil {
		t.Error("Expected Args validation to be set")
	}
}

// TestDetermineConfigStatus tests the configuration status determination
func TestDetermineConfigStatus(t *testing.T) {
	tests := []struct {
		name     string
		config   parser.MCPServerConfig
		expected string
	}{
		{
			name: "valid_stdio_config",
			config: parser.MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Command: "npx",
				},
			},
			expected: "✓ Ready",
		},
		{
			name: "valid_http_config",
			config: parser.MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					URL: "http://localhost:3000",
				},
			},
			expected: "✓ Ready",
		},
		{
			name: "valid_container_config",
			config: parser.MCPServerConfig{
				BaseMCPServerConfig: types.BaseMCPServerConfig{
					Container: "docker",
				},
			},
			expected: "✓ Ready",
		},
		{
			name:     "incomplete_config",
			config:   parser.MCPServerConfig{},
			expected: "⚠ Incomplete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineConfigStatus(tt.config)
			if result != tt.expected {
				t.Errorf("Expected status %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestFormatToolsCount tests the tools count formatting
func TestFormatToolsCount(t *testing.T) {
	tests := []struct {
		name     string
		allowed  []string
		expected string
	}{
		{
			name:     "no_restrictions",
			allowed:  []string{},
			expected: "All tools",
		},
		{
			name:     "wildcard",
			allowed:  []string{"*"},
			expected: "All tools",
		},
		{
			name:     "single_tool",
			allowed:  []string{"create_issue"},
			expected: "1 tool",
		},
		{
			name:     "multiple_tools",
			allowed:  []string{"create_issue", "create_pr", "list_issues"},
			expected: "3 tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatToolsCount(tt.allowed)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestFormatNetworkAccess tests network access formatting
func TestFormatNetworkAccess(t *testing.T) {
	tests := []struct {
		name      string
		hasAccess bool
		expected  string
	}{
		{
			name:      "enabled",
			hasAccess: true,
			expected:  "✓ Enabled",
		},
		{
			name:      "disabled",
			hasAccess: false,
			expected:  "✗ Disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatNetworkAccess(tt.hasAccess)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestCheckNetworkAccess tests network access detection
func TestCheckNetworkAccess(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    bool
	}{
		{
			name:        "nil_frontmatter",
			frontmatter: nil,
			expected:    false,
		},
		{
			name:        "no_network_field",
			frontmatter: map[string]any{},
			expected:    false,
		},
		{
			name: "network_with_allowed_domains",
			frontmatter: map[string]any{
				"network": map[string]any{
					"allowed": []any{"github.com", "api.github.com"},
				},
			},
			expected: true,
		},
		{
			name: "network_with_empty_allowed",
			frontmatter: map[string]any{
				"network": map[string]any{
					"allowed": []any{},
				},
			},
			expected: false,
		},
		{
			name: "network_with_other_config",
			frontmatter: map[string]any{
				"network": map[string]any{
					"proxy": "http://proxy:8080",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkNetworkAccess(tt.frontmatter)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

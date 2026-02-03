//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
)

func TestLoadWorkflowMCPConfigs(t *testing.T) {
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

mcp-servers:
  test-server:
    type: stdio
    command: "node"
    args: ["test-server.js"]
    allowed: ["test_tool_1", "test_tool_2"]

---

# Test Workflow
This is a test workflow.`

	testWorkflowPath := filepath.Join(workflowsDir, "test-workflow.md")
	err = os.WriteFile(testWorkflowPath, []byte(testWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	t.Run("load_workflow_with_no_filter", func(t *testing.T) {
		workflowData, mcpConfigs, err := loadWorkflowMCPConfigs(testWorkflowPath, "")
		if err != nil {
			t.Errorf("loadWorkflowMCPConfigs() error = %v", err)
			return
		}

		if workflowData == nil {
			t.Error("Expected workflowData to be non-nil")
			return
		}

		if workflowData.Frontmatter == nil {
			t.Error("Expected workflowData.Frontmatter to be non-nil")
			return
		}

		// Should find multiple MCP servers (github, test-server, safe-outputs)
		if len(mcpConfigs) < 2 {
			t.Errorf("Expected at least 2 MCP servers, got %d", len(mcpConfigs))
		}
	})

	t.Run("load_workflow_with_server_filter", func(t *testing.T) {
		workflowData, mcpConfigs, err := loadWorkflowMCPConfigs(testWorkflowPath, "github")
		if err != nil {
			t.Errorf("loadWorkflowMCPConfigs() error = %v", err)
			return
		}

		if workflowData == nil {
			t.Error("Expected workflowData to be non-nil")
			return
		}

		// Should find only the github MCP server
		found := false
		for _, config := range mcpConfigs {
			if strings.EqualFold(config.Name, "github") {
				found = true
				break
			}
		}

		if !found {
			t.Error("Expected to find 'github' MCP server with filter")
		}
	})

	t.Run("load_nonexistent_workflow", func(t *testing.T) {
		nonexistentPath := filepath.Join(workflowsDir, "nonexistent.md")
		_, _, err := loadWorkflowMCPConfigs(nonexistentPath, "")
		if err == nil {
			t.Error("Expected error for nonexistent workflow, got nil")
		}
		if !strings.Contains(err.Error(), "failed to read workflow file") {
			t.Errorf("Expected 'failed to read workflow file' error, got: %v", err)
		}
	})

	t.Run("load_workflow_with_invalid_frontmatter", func(t *testing.T) {
		invalidWorkflowContent := `---
invalid: yaml: content: [
---
# Invalid Workflow`

		invalidWorkflowPath := filepath.Join(workflowsDir, "invalid-workflow.md")
		err = os.WriteFile(invalidWorkflowPath, []byte(invalidWorkflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create invalid workflow file: %v", err)
		}

		_, _, err = loadWorkflowMCPConfigs(invalidWorkflowPath, "")
		if err == nil {
			t.Error("Expected error for invalid frontmatter, got nil")
		}
		if !strings.Contains(err.Error(), "failed to parse workflow file") {
			t.Errorf("Expected 'failed to parse workflow file' error, got: %v", err)
		}
	})

	t.Run("load_workflow_without_mcp_servers", func(t *testing.T) {
		noMCPWorkflowContent := `---
on:
  push:
permissions: read-all
---
# No MCP Workflow`

		noMCPWorkflowPath := filepath.Join(workflowsDir, "no-mcp-workflow.md")
		err = os.WriteFile(noMCPWorkflowPath, []byte(noMCPWorkflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create no-MCP workflow file: %v", err)
		}

		workflowData, mcpConfigs, err := loadWorkflowMCPConfigs(noMCPWorkflowPath, "")
		if err != nil {
			t.Errorf("loadWorkflowMCPConfigs() error = %v", err)
			return
		}

		if workflowData == nil {
			t.Error("Expected workflowData to be non-nil")
			return
		}

		// Should return empty list, not error
		if len(mcpConfigs) != 0 {
			t.Errorf("Expected 0 MCP servers, got %d", len(mcpConfigs))
		}
	})
}

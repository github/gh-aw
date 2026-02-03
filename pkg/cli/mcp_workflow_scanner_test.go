//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestScanWorkflowsForMCP(t *testing.T) {
	// Create a temporary directory for test workflows
	tmpDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create test workflow files
	testCases := []struct {
		name     string
		content  string
		hasMCP   bool
		mcpCount int
	}{
		{
			name: "workflow-with-github-mcp.md",
			content: `---
on: push
tools:
  github:
    allowed: [create_issue]
---
# Test workflow
`,
			hasMCP:   true,
			mcpCount: 1,
		},
		{
			name: "workflow-with-safe-outputs.md",
			content: `---
on: push
safe-outputs:
  create-issue:
---
# Test workflow
`,
			hasMCP:   true,
			mcpCount: 1,
		},
		{
			name: "workflow-without-mcp.md",
			content: `---
on: push
tools:
  edit:
---
# Test workflow
`,
			hasMCP:   false,
			mcpCount: 0,
		},
		{
			name: "workflow-with-multiple-mcp.md",
			content: `---
on: push
tools:
  github:
  playwright:
safe-outputs:
  create-issue:
---
# Test workflow
`,
			hasMCP:   true,
			mcpCount: 3,
		},
	}

	for _, tc := range testCases {
		filePath := filepath.Join(workflowsDir, tc.name)
		if err := os.WriteFile(filePath, []byte(tc.content), 0644); err != nil {
			t.Fatalf("Failed to write test file %s: %v", tc.name, err)
		}
	}

	t.Run("scan all workflows", func(t *testing.T) {
		results, err := ScanWorkflowsForMCP(workflowsDir, "", false)
		if err != nil {
			t.Fatalf("ScanWorkflowsForMCP failed: %v", err)
		}

		expectedWithMCP := 3
		if len(results) != expectedWithMCP {
			t.Errorf("Expected %d workflows with MCP, got %d", expectedWithMCP, len(results))
		}

		// Verify each result
		for _, result := range results {
			if result.BaseName == "" {
				t.Error("BaseName should not be empty")
			}
			if result.FileName == "" {
				t.Error("FileName should not be empty")
			}
			if result.FilePath == "" {
				t.Error("FilePath should not be empty")
			}
			if len(result.MCPConfigs) == 0 {
				t.Errorf("Expected MCP configs for %s, got none", result.BaseName)
			}
		}
	})

	t.Run("scan with server filter", func(t *testing.T) {
		results, err := ScanWorkflowsForMCP(workflowsDir, "github", false)
		if err != nil {
			t.Fatalf("ScanWorkflowsForMCP failed: %v", err)
		}

		// Should find workflows that have github MCP server
		expectedWithGitHub := 2
		if len(results) != expectedWithGitHub {
			t.Errorf("Expected %d workflows with GitHub MCP, got %d", expectedWithGitHub, len(results))
		}

		for _, result := range results {
			hasGitHub := false
			for _, config := range result.MCPConfigs {
				if config.Name == "github" {
					hasGitHub = true
					break
				}
			}
			if !hasGitHub {
				t.Errorf("Expected GitHub MCP in %s, but not found", result.BaseName)
			}
		}
	})

	t.Run("scan non-existent directory", func(t *testing.T) {
		_, err := ScanWorkflowsForMCP("/nonexistent/directory", "", false)
		if err == nil {
			t.Error("Expected error for non-existent directory, got nil")
		}
	})

	t.Run("verbose mode with invalid file", func(t *testing.T) {
		// Create an invalid workflow file
		invalidPath := filepath.Join(workflowsDir, "invalid.md")
		if err := os.WriteFile(invalidPath, []byte("invalid yaml ---"), 0644); err != nil {
			t.Fatalf("Failed to write invalid file: %v", err)
		}

		// Should not error, just skip the invalid file
		results, err := ScanWorkflowsForMCP(workflowsDir, "", true)
		if err != nil {
			t.Fatalf("ScanWorkflowsForMCP should not fail on invalid files: %v", err)
		}

		// Should still find the valid workflows
		if len(results) < 3 {
			t.Errorf("Expected at least 3 valid workflows, got %d", len(results))
		}
	})
}

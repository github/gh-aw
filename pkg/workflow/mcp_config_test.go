//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestGitHubMCPConfiguration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "mcp-config-test")

	compiler := NewCompiler()

	tests := []struct {
		name                string
		frontmatter         string
		expectedType        string // "http" or "docker"
		expectedURL         string
		expectedCommand     string
		expectedDockerImage string
	}{
		{
			name: "default Docker server",
			frontmatter: `---
on: push
engine: claude
tools:
  github:
    allowed: [list_issues, create_issue]
---`,
			// With Docker MCP always enabled, default is docker (not services)
			expectedType:        "docker",
			expectedCommand:     "docker",
			expectedDockerImage: "ghcr.io/github/github-mcp-server:v0.30.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test MCP Configuration

This is a test workflow for MCP configuration.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Replace the file extension to .lock.yml
			lockFile := stringutil.MarkdownToLockFile(testFile)
			// Read the generated lock file
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContent := string(content)

			// Check the MCP configuration based on expected type
			switch tt.expectedType {
			case "http":
				// Should contain HTTP configuration
				if !strings.Contains(lockContent, `"type": "http"`) {
					t.Errorf("Expected HTTP configuration but didn't find 'type: http' in:\n%s", lockContent)
				}
				if !strings.Contains(lockContent, tt.expectedURL) {
					t.Errorf("Expected URL '%s' but didn't find it in:\n%s", tt.expectedURL, lockContent)
				}
				if !strings.Contains(lockContent, `"Authorization": "Bearer ${{ secrets.GITHUB_TOKEN }}"`) {
					t.Errorf("Expected Authorization header but didn't find it in:\n%s", lockContent)
				}
				// Should NOT contain Docker configuration
				if strings.Contains(lockContent, `"command": "docker"`) {
					t.Errorf("Expected no Docker configuration but found it in:\n%s", lockContent)
				}
			case "docker":
				// Should contain container configuration (new MCP gateway format)
				if !strings.Contains(lockContent, `"container": "`+tt.expectedDockerImage+`"`) {
					t.Errorf("Expected container with image '%s' but didn't find it in:\n%s", tt.expectedDockerImage, lockContent)
				}
				// Security fix: Verify env block contains GitHub expression and JSON contains shell variable
				if !strings.Contains(lockContent, `GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}`) {
					t.Errorf("Expected GITHUB_MCP_SERVER_TOKEN in env block but didn't find it in:\n%s", lockContent)
				}
				if !strings.Contains(lockContent, `"GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_MCP_SERVER_TOKEN"`) {
					t.Errorf("Expected GITHUB_PERSONAL_ACCESS_TOKEN to use shell variable but didn't find it in:\n%s", lockContent)
				}
				// Should NOT contain HTTP configuration
				if strings.Contains(lockContent, `"type": "http"`) {
					t.Errorf("Expected no HTTP configuration but found it in:\n%s", lockContent)
				}
				// Should NOT contain services configuration
				if strings.Contains(lockContent, `services:`) {
					t.Errorf("Expected no services configuration but found it in:\n%s", lockContent)
				}
			}

			// All configurations should contain the github server
			if !strings.Contains(lockContent, `"github": {`) {
				t.Errorf("Expected github server configuration but didn't find it in:\n%s", lockContent)
			}
		})
	}
}

func TestGenerateGitHubMCPConfig(t *testing.T) {
	tests := []struct {
		name         string
		githubTool   any
		expectedType string
	}{
		{
			name:       "nil github tool",
			githubTool: nil,
			// With new defaults, nil tool defaults to docker (not services)
			expectedType: "docker",
		},
		{
			name: "empty github tool config",
			githubTool: map[string]any{
				"allowed": []any{"list_issues"},
			},
			// With Docker always enabled, empty config defaults to docker (not services)
			expectedType: "docker",
		},
		{
			name: "explicit docker config (redundant)",
			githubTool: map[string]any{
				"allowed": []any{"list_issues"},
			},
			// Docker is always enabled now
			expectedType: "docker",
		},
		{
			name:       "non-map github tool",
			githubTool: "invalid",
			// With Docker always enabled, invalid tool config defaults to docker (not services)
			expectedType: "docker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yamlBuilder strings.Builder

			// Use unified renderer with Claude-specific options
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               true,
			})
			workflowData := &WorkflowData{}
			renderer.RenderGitHubMCP(&yamlBuilder, tt.githubTool, workflowData)

			result := yamlBuilder.String()

			switch tt.expectedType {
			case "docker":
				if !strings.Contains(result, `"container": "ghcr.io/github/github-mcp-server:v0.30.3"`) {
					t.Errorf("Expected container field with GitHub MCP image but got:\n%s", result)
				}
				if strings.Contains(result, `"type": "http"`) {
					t.Errorf("Expected no HTTP type but found it in:\n%s", result)
				}
			}
		})
	}
}

func TestMCPConfigurationEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		githubTool any
		isLast     bool
		expected   string
	}{
		{
			name: "last server with docker config",
			githubTool: map[string]any{
				"allowed": []any{"list_issues"},
			},
			isLast:   true,
			expected: `              }`,
		},
		{
			name: "not last server with docker config",
			githubTool: map[string]any{
				"allowed": []any{"list_issues"},
			},
			isLast:   false,
			expected: `              },`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yamlBuilder strings.Builder

			// Use unified renderer with Claude-specific options
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               tt.isLast,
			})
			workflowData := &WorkflowData{}
			renderer.RenderGitHubMCP(&yamlBuilder, tt.githubTool, workflowData)

			result := yamlBuilder.String()

			if !strings.Contains(result, tt.expected) {
				t.Errorf("Expected result to end with '%s' but got:\n%s", tt.expected, result)
			}
		})
	}
}

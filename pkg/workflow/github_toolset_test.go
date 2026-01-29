//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGetGitHubToolsets(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "No toolsets configured",
			input:    map[string]any{},
			expected: "context,repos,issues,pull_requests", // defaults to action-friendly toolsets
		},
		{
			name: "Toolsets as array of strings",
			input: map[string]any{
				"toolsets": []string{"repos", "issues", "pull_requests"},
			},
			expected: "repos,issues,pull_requests",
		},
		{
			name: "Toolsets as array of any",
			input: map[string]any{
				"toolsets": []any{"repos", "issues", "actions"},
			},
			expected: "repos,issues,actions",
		},
		{
			name: "Special 'all' toolset as array",
			input: map[string]any{
				"toolsets": []string{"all"},
			},
			expected: "all",
		},
		{
			name: "Special 'default' toolset as array - expands to action-friendly",
			input: map[string]any{
				"toolsets": []string{"default"},
			},
			expected: "context,repos,issues,pull_requests",
		},
		{
			name: "Default with additional toolsets - expands default to action-friendly",
			input: map[string]any{
				"toolsets": []string{"default", "discussions"},
			},
			expected: "context,repos,issues,pull_requests,discussions",
		},
		{
			name:     "Non-map input returns action-friendly",
			input:    "not a map",
			expected: "context,repos,issues,pull_requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getGitHubToolsets(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestClaudeEngineGitHubToolsetsRendering(t *testing.T) {
	tests := []struct {
		name           string
		githubTool     any
		expectedInYAML []string
		notInYAML      []string
	}{
		{
			name: "Toolsets configured with array",
			githubTool: map[string]any{
				"toolsets": []string{"repos", "issues", "pull_requests"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "repos,issues,pull_requests"`,
			},
			notInYAML: []string{},
		},
		{
			name:       "No toolsets configured",
			githubTool: map[string]any{},
			expectedInYAML: []string{
				`"GITHUB_PERSONAL_ACCESS_TOKEN"`,
				"GITHUB_TOOLSETS",
				"context,repos,issues,pull_requests", // defaults to action-friendly toolsets
			},
			notInYAML: []string{},
		},
		{
			name: "All toolset as array",
			githubTool: map[string]any{
				"toolsets": []string{"all"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "all"`,
			},
			notInYAML: []string{},
		},
		{
			name: "Default toolset as array - expands to action-friendly",
			githubTool: map[string]any{
				"toolsets": []string{"default"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "context,repos,issues,pull_requests"`,
			},
			notInYAML: []string{},
		},
		{
			name: "Default with additional toolsets - expands default",
			githubTool: map[string]any{
				"toolsets": []string{"default", "discussions"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "context,repos,issues,pull_requests,discussions"`,
			},
			notInYAML: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use unified renderer with Claude-specific options
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               true,
			})
			var yaml strings.Builder
			workflowData := &WorkflowData{}
			renderer.RenderGitHubMCP(&yaml, tt.githubTool, workflowData)

			result := yaml.String()

			for _, expected := range tt.expectedInYAML {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected YAML to contain %q, but it didn't.\nYAML:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.notInYAML {
				if strings.Contains(result, notExpected) {
					t.Errorf("Expected YAML to NOT contain %q, but it did.\nYAML:\n%s", notExpected, result)
				}
			}
		})
	}
}

func TestCopilotEngineGitHubToolsetsRendering(t *testing.T) {
	tests := []struct {
		name           string
		githubTool     any
		expectedInYAML []string
		notInYAML      []string
	}{
		{
			name: "Toolsets configured with array",
			githubTool: map[string]any{
				"toolsets": []string{"repos", "issues", "pull_requests"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "repos,issues,pull_requests"`,
			},
			notInYAML: []string{},
		},
		{
			name:       "No toolsets configured",
			githubTool: map[string]any{},
			expectedInYAML: []string{
				`GITHUB_PERSONAL_ACCESS_TOKEN`,
				"GITHUB_TOOLSETS",
				"context,repos,issues,pull_requests", // defaults to action-friendly toolsets
			},
			notInYAML: []string{},
		},
		{
			name: "Default toolset as array - expands to action-friendly",
			githubTool: map[string]any{
				"toolsets": []string{"default"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "context,repos,issues,pull_requests"`,
			},
			notInYAML: []string{},
		},
		{
			name: "Default with additional toolsets - expands default",
			githubTool: map[string]any{
				"toolsets": []string{"default", "actions"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS": "context,repos,issues,pull_requests,actions"`,
			},
			notInYAML: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			workflowData := &WorkflowData{}
			// Use unified renderer instead of direct method call
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               true,
			})
			renderer.RenderGitHubMCP(&yaml, tt.githubTool, workflowData)

			result := yaml.String()

			for _, expected := range tt.expectedInYAML {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected YAML to contain %q, but it didn't.\nYAML:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.notInYAML {
				if strings.Contains(result, notExpected) {
					t.Errorf("Expected YAML to NOT contain %q, but it did.\nYAML:\n%s", notExpected, result)
				}
			}
		})
	}
}

func TestCodexEngineGitHubToolsetsRendering(t *testing.T) {
	tests := []struct {
		name           string
		githubTool     any
		expectedInYAML []string
		notInYAML      []string
	}{
		{
			name: "Toolsets configured with array",
			githubTool: map[string]any{
				"toolsets": []string{"repos", "issues"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS" = "repos,issues"`,
			},
			notInYAML: []string{},
		},
		{
			name:       "No toolsets configured",
			githubTool: map[string]any{},
			expectedInYAML: []string{
				`GITHUB_PERSONAL_ACCESS_TOKEN`,
				"GITHUB_TOOLSETS",
				"context,repos,issues,pull_requests", // defaults to action-friendly toolsets
			},
			notInYAML: []string{},
		},
		{
			name: "Default toolset as array - expands to action-friendly",
			githubTool: map[string]any{
				"toolsets": []string{"default"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS" = "context,repos,issues,pull_requests"`,
			},
			notInYAML: []string{},
		},
		{
			name: "Default with additional toolsets - expands default",
			githubTool: map[string]any{
				"toolsets": []string{"default", "discussions"},
			},
			expectedInYAML: []string{
				`"GITHUB_TOOLSETS" = "context,repos,issues,pull_requests,discussions"`,
			},
			notInYAML: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use unified renderer with Codex engine options
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "toml",
				IsLast:               false,
			})
			var yaml strings.Builder
			workflowData := &WorkflowData{Name: "test-workflow"}
			renderer.RenderGitHubMCP(&yaml, tt.githubTool, workflowData)

			result := yaml.String()

			for _, expected := range tt.expectedInYAML {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected YAML to contain %q, but it didn't.\nYAML:\n%s", expected, result)
				}
			}

			for _, notExpected := range tt.notInYAML {
				if strings.Contains(result, notExpected) {
					t.Errorf("Expected YAML to NOT contain %q, but it did.\nYAML:\n%s", notExpected, result)
				}
			}
		})
	}
}

func TestGitHubToolsetsWithOtherConfiguration(t *testing.T) {
	tests := []struct {
		name           string
		githubTool     any
		expectedInYAML []string
	}{
		{
			name: "Toolsets with read-only mode",
			githubTool: map[string]any{
				"toolsets":  []string{"repos", "issues"},
				"read-only": true,
			},
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`repos,issues`,
				`GITHUB_READ_ONLY`,
			},
		},
		{
			name: "Toolsets with custom token",
			githubTool: map[string]any{
				"toolsets":     []string{"all"},
				"github-token": "${{ secrets.CUSTOM_PAT }}",
			},
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`all`,
				// Security fix: Custom token is now passed via env block, not embedded in JSON
				`$GITHUB_MCP_SERVER_TOKEN`,
			},
		},
		{
			name: "Toolsets with custom Docker version",
			githubTool: map[string]any{
				"toolsets": []string{"repos", "issues", "pull_requests"},
				"version":  "latest",
			},
			expectedInYAML: []string{
				`GITHUB_TOOLSETS`,
				`repos,issues,pull_requests`,
				`github-mcp-server:latest`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use unified renderer with Claude-specific options
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: false,
				InlineArgs:           false,
				Format:               "json",
				IsLast:               true,
			})
			var yaml strings.Builder
			workflowData := &WorkflowData{}
			renderer.RenderGitHubMCP(&yaml, tt.githubTool, workflowData)

			result := yaml.String()

			for _, expected := range tt.expectedInYAML {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected YAML to contain %q, but it didn't.\nYAML:\n%s", expected, result)
				}
			}
		})
	}
}

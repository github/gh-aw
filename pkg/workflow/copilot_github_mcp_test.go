//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestRenderGitHubCopilotMCPConfig_AllowedTools(t *testing.T) {
	tests := []struct {
		name              string
		githubTool        any
		isLast            bool
		expectedContent   []string
		unexpectedContent []string
	}{
		{
			name: "GitHub with specific allowed tools",
			githubTool: map[string]any{
				"allowed": []string{"list_workflows", "list_workflow_runs", "list_workflow_run_artifacts"},
			},
			isLast: true,
			expectedContent: []string{
				`"github": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/github-mcp-server:v0.30.3"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
			},
			unexpectedContent: []string{},
		},
		{
			name:       "GitHub with no allowed tools (defaults to all)",
			githubTool: map[string]any{},
			isLast:     true,
			expectedContent: []string{
				`"github": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/github-mcp-server:v0.30.3"`,
				`"env": {`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "GitHub with empty allowed array (defaults to all)",
			githubTool: map[string]any{
				"allowed": []string{},
			},
			isLast: true,
			expectedContent: []string{
				`"github": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/github-mcp-server:v0.30.3"`,
				`"env": {`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "GitHub remote mode with specific allowed tools",
			githubTool: map[string]any{
				"mode":    "remote",
				"allowed": []string{"get_repository", "list_commits"},
			},
			isLast: true,
			expectedContent: []string{
				`"github": {`,
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
			},
			unexpectedContent: []string{},
		},
		{
			name: "GitHub remote mode with no allowed tools",
			githubTool: map[string]any{
				"mode": "remote",
			},
			isLast: true,
			expectedContent: []string{
				`"github": {`,
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
			},
			unexpectedContent: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			workflowData := &WorkflowData{}
			// Use unified renderer instead of direct method call
			renderer := NewMCPConfigRenderer(MCPRendererOptions{
				IncludeCopilotFields: true,
				InlineArgs:           true,
				Format:               "json",
				IsLast:               tt.isLast,
			})
			renderer.RenderGitHubMCP(&output, tt.githubTool, workflowData)
			result := output.String()

			// Check expected content
			for _, expected := range tt.expectedContent {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected content not found: %q\nActual output:\n%s", expected, result)
				}
			}

			// Check unexpected content
			for _, unexpected := range tt.unexpectedContent {
				if strings.Contains(result, unexpected) {
					t.Errorf("Unexpected content found: %q\nActual output:\n%s", unexpected, result)
				}
			}
		})
	}
}

func TestGetGitHubAllowedTools(t *testing.T) {
	tests := []struct {
		name       string
		githubTool any
		expected   []string
	}{
		{
			name: "Specific allowed tools",
			githubTool: map[string]any{
				"allowed": []string{"get_repository", "list_commits"},
			},
			expected: []string{"get_repository", "list_commits"},
		},
		{
			name: "Empty allowed array",
			githubTool: map[string]any{
				"allowed": []string{},
			},
			expected: []string{},
		},
		{
			name:       "No allowed field",
			githubTool: map[string]any{},
			expected:   nil,
		},
		{
			name: "Allowed with []any type",
			githubTool: map[string]any{
				"allowed": []any{"tool1", "tool2", "tool3"},
			},
			expected: []string{"tool1", "tool2", "tool3"},
		},
		{
			name:       "Not a map",
			githubTool: "invalid",
			expected:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getGitHubAllowedTools(tt.githubTool)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected %v, got nil", tt.expected)
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d tools, got %d", len(tt.expected), len(result))
				return
			}

			for i, tool := range tt.expected {
				if result[i] != tool {
					t.Errorf("Expected tool[%d] = %s, got %s", i, tool, result[i])
				}
			}
		})
	}
}

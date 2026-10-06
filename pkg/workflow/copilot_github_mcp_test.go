//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestRenderGitHubCopilotMCPConfig_AllowedTools(t *testing.T) {
	tests := []struct {
		name              string
		githubTool        map[string]any
		isLast            bool
		expectedContent   []string
		unexpectedContent []string
	}{
		{
			name: "GitHub with specific allowed tools",
			githubTool: map[string]any{
				"allowed": []string{"actions_list", "actions_get", "get_job_logs"},
			},
			isLast: true,
			expectedContent: []string{
				`"github": {`,
				`"type": "stdio"`,
				`"container": "ghcr.io/github/github-mcp-server:` + string(constants.DefaultGitHubMCPServerVersion) + `"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "${GITHUB_MCP_SERVER_TOKEN}"`,
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
				`"container": "ghcr.io/github/github-mcp-server:` + string(constants.DefaultGitHubMCPServerVersion) + `"`,
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
				`"container": "ghcr.io/github/github-mcp-server:` + string(constants.DefaultGitHubMCPServerVersion) + `"`,
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
		githubTool map[string]any
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
			name: "Allowed supports object entries with max-calls",
			githubTool: map[string]any{
				"allowed": []any{
					map[string]any{"name": "issue_read", "max-calls": 1},
					"list_labels",
				},
			},
			expected: []string{"issue_read", "list_labels"},
		},
		{
			name: "Allowed string entries with colons are preserved as tool names",
			githubTool: map[string]any{
				"allowed": []any{"issue_read:1", "list_labels"},
			},
			expected: []string{"issue_read:1", "list_labels"},
		},
		{
			name:       "Nil tool",
			githubTool: nil,
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

func TestGetGitHubGuardPoliciesToolCallLimits(t *testing.T) {
	tests := []struct {
		name       string
		githubTool map[string]any
		expected   map[string]any
	}{
		{
			name: "tool-call-limits only creates allow-only policy with default repos",
			githubTool: map[string]any{
				"allowed": []any{
					map[string]any{"name": "issue_read", "max-calls": 1},
					"list_labels",
				},
			},
			expected: map[string]any{
				"allow-only": map[string]any{
					"repos": "all",
					"tool-call-limits": map[string]int{
						"issue_read": 1,
					},
					"blocked-users":   guardExprSentinel + "${GH_AW_GUARD_BLOCKED_USERS}",
					"trusted-users":   guardExprSentinel + "${GH_AW_GUARD_TRUSTED_USERS}",
					"approval-labels": guardExprSentinel + "${GH_AW_GUARD_APPROVAL_LABELS}",
				},
			},
		},
		{
			name: "tool-call-limits merge with existing guard policy",
			githubTool: map[string]any{
				"allowed-repos": "github/gh-aw",
				"min-integrity": "approved",
				"allowed": []any{
					"list_labels",
					map[string]any{"name": "issue_read", "max-calls": 2},
				},
			},
			expected: map[string]any{
				"allow-only": map[string]any{
					"repos":         "github/gh-aw",
					"min-integrity": "approved",
					"tool-call-limits": map[string]int{
						"issue_read": 2,
					},
					"blocked-users":   guardExprSentinel + "${GH_AW_GUARD_BLOCKED_USERS}",
					"trusted-users":   guardExprSentinel + "${GH_AW_GUARD_TRUSTED_USERS}",
					"approval-labels": guardExprSentinel + "${GH_AW_GUARD_APPROVAL_LABELS}",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, getGitHubGuardPolicies(tt.githubTool))
		})
	}
}

func TestRenderGitHubGuardPoliciesDefersRuntimeExpressions(t *testing.T) {
	var output strings.Builder
	renderGuardPoliciesJSON(&output, getGitHubGuardPolicies(map[string]any{
		"allowed": []any{
			map[string]any{"name": "issue_read", "max-calls": 1},
		},
	}), "")

	result := output.String()
	for _, field := range []string{"blocked-users", "trusted-users", "approval-labels"} {
		assert.Contains(t, result, `"`+field+`": ${GH_AW_GUARD_`+strings.ToUpper(strings.ReplaceAll(field, "-", "_"))+`}`)
	}
	assert.NotContains(t, result, guardExprSentinel)
}

func TestParseGitHubToolAllowedObjectEntries(t *testing.T) {
	parsed := parseGitHubTool(map[string]any{
		"allowed": []any{
			map[string]any{"name": "issue_read", "max-calls": 1},
			"list_labels",
		},
	})

	assert.NotNil(t, parsed)
	assert.Equal(t, GitHubAllowedTools{"issue_read", "list_labels"}, parsed.Allowed)
}

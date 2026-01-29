//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestRenderGitHubMCPRemoteConfig(t *testing.T) {
	tests := []struct {
		name           string
		options        GitHubMCPRemoteOptions
		expectedOutput []string // Expected strings to be present in output
		notExpected    []string // Strings that should NOT be present
	}{
		{
			name: "Claude-style config without tools or env",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				AuthorizationValue: "Bearer ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
				IncludeToolsField:  false,
				AllowedTools:       nil,
				IncludeEnvSection:  false,
			},
			expectedOutput: []string{
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
				`"headers": {`,
				`"Authorization": "Bearer ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"`,
				`"X-MCP-Toolsets": "default"`,
			},
			notExpected: []string{
				`"tools"`,
				`"env"`,
				`"X-MCP-Readonly"`,
			},
		},
		{
			name: "Claude-style config with read-only",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           true,
				Toolsets:           "repos,issues",
				AuthorizationValue: "Bearer ${{ secrets.CUSTOM_PAT }}",
				IncludeToolsField:  false,
				AllowedTools:       nil,
				IncludeEnvSection:  false,
			},
			expectedOutput: []string{
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
				`"headers": {`,
				`"Authorization": "Bearer ${{ secrets.CUSTOM_PAT }}"`,
				`"X-MCP-Readonly": "true"`,
				`"X-MCP-Toolsets": "repos,issues"`,
			},
			notExpected: []string{
				`"tools"`,
				`"env"`,
			},
		},
		{
			name: "Copilot-style config with tools and env",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				AuthorizationValue: "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}",
				IncludeToolsField:  true,
				AllowedTools:       []string{"list_issues", "create_issue"},
				IncludeEnvSection:  true,
			},
			expectedOutput: []string{
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
				`"headers": {`,
				`"Authorization": "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"`,
				`"X-MCP-Toolsets": "default"`,
				`"tools": [`,
				`"list_issues"`,
				`"create_issue"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
			},
			notExpected: []string{
				`"X-MCP-Readonly"`,
			},
		},
		{
			name: "Copilot-style config with wildcard tools",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           false,
				Toolsets:           "all",
				AuthorizationValue: "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}",
				IncludeToolsField:  true,
				AllowedTools:       nil, // Empty array should result in wildcard
				IncludeEnvSection:  true,
			},
			expectedOutput: []string{
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
				`"headers": {`,
				`"Authorization": "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"`,
				`"X-MCP-Toolsets": "all"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
			},
			notExpected: []string{
				`"X-MCP-Readonly"`,
			},
		},
		{
			name: "Copilot-style config with read-only and specific tools",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           true,
				Toolsets:           "repos",
				AuthorizationValue: "Bearer \\${GITHUB_PERSONAL_ACCESS_TOKEN}",
				IncludeToolsField:  true,
				AllowedTools:       []string{"list_repositories", "get_repository"},
				IncludeEnvSection:  true,
			},
			expectedOutput: []string{
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
				`"headers": {`,
				`"Authorization": "Bearer \${GITHUB_PERSONAL_ACCESS_TOKEN}"`,
				`"X-MCP-Readonly": "true"`,
				`"X-MCP-Toolsets": "repos"`,
				`"tools": [`,
				`"list_repositories"`,
				`"get_repository"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
			},
			notExpected: []string{},
		},
		{
			name: "No toolsets configured",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           false,
				Toolsets:           "",
				AuthorizationValue: "Bearer ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
				IncludeToolsField:  false,
				AllowedTools:       nil,
				IncludeEnvSection:  false,
			},
			expectedOutput: []string{
				`"type": "http"`,
				`"url": "https://api.githubcopilot.com/mcp/"`,
				`"headers": {`,
				`"Authorization": "Bearer ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"`,
			},
			notExpected: []string{
				`"X-MCP-Toolsets"`,
				`"X-MCP-Readonly"`,
				`"tools"`,
				`"env"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			RenderGitHubMCPRemoteConfig(&yaml, tt.options)
			output := yaml.String()

			// Check for expected strings
			for _, expected := range tt.expectedOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nGot:\n%s", expected, output)
				}
			}

			// Check that unexpected strings are not present
			for _, notExpected := range tt.notExpected {
				if strings.Contains(output, notExpected) {
					t.Errorf("Expected output NOT to contain %q, but it did.\nGot:\n%s", notExpected, output)
				}
			}
		})
	}
}

func TestRenderGitHubMCPRemoteConfigHeaderOrder(t *testing.T) {
	// Test that headers are sorted alphabetically for deterministic output
	var yaml strings.Builder
	RenderGitHubMCPRemoteConfig(&yaml, GitHubMCPRemoteOptions{
		ReadOnly:           true,
		Toolsets:           "repos,issues",
		AuthorizationValue: "Bearer token",
		IncludeToolsField:  false,
		AllowedTools:       nil,
		IncludeEnvSection:  false,
	})
	output := yaml.String()

	// Authorization should come before X-MCP-Readonly, which should come before X-MCP-Toolsets
	authIndex := strings.Index(output, `"Authorization"`)
	readonlyIndex := strings.Index(output, `"X-MCP-Readonly"`)
	toolsetsIndex := strings.Index(output, `"X-MCP-Toolsets"`)

	if authIndex == -1 || readonlyIndex == -1 || toolsetsIndex == -1 {
		t.Fatal("Expected all three headers to be present")
	}

	if authIndex >= readonlyIndex {
		t.Errorf("Expected Authorization to come before X-MCP-Readonly, but got:\n%s", output)
	}

	if readonlyIndex >= toolsetsIndex {
		t.Errorf("Expected X-MCP-Readonly to come before X-MCP-Toolsets, but got:\n%s", output)
	}
}

func TestRenderGitHubMCPRemoteConfigToolsCommas(t *testing.T) {
	// Test that tools array is properly formatted with commas
	var yaml strings.Builder
	RenderGitHubMCPRemoteConfig(&yaml, GitHubMCPRemoteOptions{
		ReadOnly:           false,
		Toolsets:           "default",
		AuthorizationValue: "Bearer token",
		IncludeToolsField:  true,
		AllowedTools:       []string{"tool1", "tool2", "tool3"},
		IncludeEnvSection:  true,
	})
	output := yaml.String()

	// First and second tools should have commas
	if !strings.Contains(output, `"tool1",`) {
		t.Errorf("Expected first tool to have comma, got:\n%s", output)
	}
	if !strings.Contains(output, `"tool2",`) {
		t.Errorf("Expected second tool to have comma, got:\n%s", output)
	}

	// Last tool should NOT have a comma
	if !strings.Contains(output, `"tool3"`) || strings.Contains(output, `"tool3",`) {
		t.Errorf("Expected last tool to NOT have comma, got:\n%s", output)
	}
}

//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestGitHubLockdownField(t *testing.T) {
	tests := []struct {
		name     string
		toolsMap map[string]any
		expected bool
	}{
		{
			name: "lockdown explicitly enabled",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown": true,
				},
			},
			expected: true,
		},
		{
			name: "lockdown explicitly disabled",
			toolsMap: map[string]any{
				"github": map[string]any{
					"lockdown": false,
				},
			},
			expected: false,
		},
		{
			name: "lockdown not specified (default false)",
			toolsMap: map[string]any{
				"github": map[string]any{
					"mode": "local",
				},
			},
			expected: false,
		},
		{
			name:     "empty github config (default false)",
			toolsMap: map[string]any{"github": map[string]any{}},
			expected: false,
		},
		{
			name:     "nil github config (default false)",
			toolsMap: map[string]any{"github": nil},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			githubTool := tt.toolsMap["github"]
			result := getGitHubLockdown(githubTool)
			if result != tt.expected {
				t.Errorf("getGitHubLockdown() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGitHubToolConfigLockdownParsing(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		expected bool
	}{
		{
			name: "lockdown true in config",
			config: map[string]any{
				"lockdown": true,
				"mode":     "local",
			},
			expected: true,
		},
		{
			name: "lockdown false in config",
			config: map[string]any{
				"lockdown": false,
				"mode":     "remote",
			},
			expected: false,
		},
		{
			name: "lockdown not present",
			config: map[string]any{
				"mode": "local",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := parseGitHubTool(tt.config)
			if parsed.Lockdown != tt.expected {
				t.Errorf("parseGitHubTool().Lockdown = %v, want %v", parsed.Lockdown, tt.expected)
			}
		})
	}
}

func TestRenderGitHubMCPDockerConfigWithLockdown(t *testing.T) {
	tests := []struct {
		name     string
		options  GitHubMCPDockerOptions
		expected []string
		notFound []string
	}{
		{
			name: "Docker mode with lockdown enabled",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Lockdown:           true,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				IncludeTypeField:   true,
				AllowedTools:       nil,
			},
			expected: []string{
				`"type": "stdio"`,
				`"GITHUB_LOCKDOWN_MODE": "1"`,
				`"GITHUB_TOOLSETS": "default"`,
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
			},
			notFound: []string{},
		},
		{
			name: "Docker mode with lockdown disabled",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Lockdown:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				IncludeTypeField:   true,
				AllowedTools:       nil,
			},
			expected: []string{
				`"type": "stdio"`,
				`"GITHUB_TOOLSETS": "default"`,
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
			},
			notFound: []string{
				`"GITHUB_LOCKDOWN_MODE"`,
			},
		},
		{
			name: "Docker mode with lockdown and read-only both enabled",
			options: GitHubMCPDockerOptions{
				ReadOnly:           true,
				Lockdown:           true,
				Toolsets:           "default",
				DockerImageVersion: "v1.0.0",
				IncludeTypeField:   false,
				AllowedTools:       nil,
			},
			expected: []string{
				`"GITHUB_READ_ONLY": "1"`,
				`"GITHUB_LOCKDOWN_MODE": "1"`,
				`"container": "ghcr.io/github/github-mcp-server:v1.0.0"`,
			},
			notFound: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			RenderGitHubMCPDockerConfig(&yaml, tt.options)
			output := yaml.String()

			// Check expected strings
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it doesn't.\nOutput: %s", expected, output)
				}
			}

			// Check strings that should NOT be present
			for _, notFound := range tt.notFound {
				if strings.Contains(output, notFound) {
					t.Errorf("Expected output NOT to contain %q, but it does.\nOutput: %s", notFound, output)
				}
			}
		})
	}
}

func TestRenderGitHubMCPRemoteConfigWithLockdown(t *testing.T) {
	tests := []struct {
		name     string
		options  GitHubMCPRemoteOptions
		expected []string
		notFound []string
	}{
		{
			name: "Remote mode with lockdown enabled",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           false,
				Lockdown:           true,
				Toolsets:           "default",
				AuthorizationValue: "Bearer test-token",
				IncludeToolsField:  true,
				AllowedTools:       []string{"*"},
				IncludeEnvSection:  false,
			},
			expected: []string{
				`"type": "http"`,
				`"X-MCP-Lockdown": "true"`,
				`"X-MCP-Toolsets": "default"`,
				`"Authorization": "Bearer test-token"`,
			},
			notFound: []string{
				`"X-MCP-Readonly"`,
			},
		},
		{
			name: "Remote mode with lockdown disabled",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           false,
				Lockdown:           false,
				Toolsets:           "default",
				AuthorizationValue: "Bearer test-token",
				IncludeToolsField:  true,
				AllowedTools:       []string{"*"},
				IncludeEnvSection:  false,
			},
			expected: []string{
				`"type": "http"`,
				`"X-MCP-Toolsets": "default"`,
				`"Authorization": "Bearer test-token"`,
			},
			notFound: []string{
				`"X-MCP-Lockdown"`,
				`"X-MCP-Readonly"`,
			},
		},
		{
			name: "Remote mode with lockdown and read-only both enabled",
			options: GitHubMCPRemoteOptions{
				ReadOnly:           true,
				Lockdown:           true,
				Toolsets:           "repos,issues",
				AuthorizationValue: "Bearer test-token",
				IncludeToolsField:  false,
				AllowedTools:       nil,
				IncludeEnvSection:  false,
			},
			expected: []string{
				`"type": "http"`,
				`"X-MCP-Readonly": "true"`,
				`"X-MCP-Lockdown": "true"`,
				`"X-MCP-Toolsets": "repos,issues"`,
			},
			notFound: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			RenderGitHubMCPRemoteConfig(&yaml, tt.options)
			output := yaml.String()

			// Check expected strings
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it doesn't.\nOutput: %s", expected, output)
				}
			}

			// Check strings that should NOT be present
			for _, notFound := range tt.notFound {
				if strings.Contains(output, notFound) {
					t.Errorf("Expected output NOT to contain %q, but it does.\nOutput: %s", notFound, output)
				}
			}
		})
	}
}

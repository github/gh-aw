//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestRenderGitHubMCPDockerConfigWithMounts tests that mounts are correctly rendered in JSON format
func TestRenderGitHubMCPDockerConfigWithMounts(t *testing.T) {
	tests := []struct {
		name     string
		options  GitHubMCPDockerOptions
		expected []string
		notFound []string
	}{
		{
			name: "GitHub MCP with mounts",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				IncludeTypeField:   true,
				Mounts:             []string{"/data:/data:ro", "/tmp:/tmp:rw"},
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				`"mounts": [`,
				`"/data:/data:ro"`,
				`"/tmp:/tmp:rw"`,
			},
			notFound: []string{},
		},
		{
			name: "GitHub MCP without mounts",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				IncludeTypeField:   false,
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
			},
			notFound: []string{
				`"mounts"`,
			},
		},
		{
			name: "GitHub MCP with single mount",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				IncludeTypeField:   true,
				Mounts:             []string{"/opt:/opt:ro"},
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				`"mounts": [`,
				`"/opt:/opt:ro"`,
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

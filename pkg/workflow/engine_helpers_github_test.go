//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestRenderGitHubMCPDockerConfig(t *testing.T) {
	tests := []struct {
		name     string
		options  GitHubMCPDockerOptions
		expected []string // Expected substrings in the output
		notFound []string // Substrings that should NOT be in the output
	}{
		{
			name: "Claude engine configuration (no type field, with effective token)",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				CustomArgs:         nil,
				IncludeTypeField:   false,
				AllowedTools:       nil,
				EffectiveToken:     "${{ secrets.GITHUB_TOKEN }}",
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_MCP_SERVER_TOKEN"`,
				`"GITHUB_TOOLSETS": "default"`,
			},
			notFound: []string{
				`"type": "stdio"`,
				`"tools":`,
				`"command": "docker"`,
				`"run"`,
			},
		},
		{
			name: "Copilot engine configuration (with type field, no effective token)",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				CustomArgs:         nil,
				IncludeTypeField:   true,
				AllowedTools:       []string{"create_issue", "issue_read"},
				EffectiveToken:     "",
			},
			expected: []string{
				`"type": "stdio"`,
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				`"env": {`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "\${GITHUB_MCP_SERVER_TOKEN}"`,
				`"GITHUB_TOOLSETS": "default"`,
			},
			notFound: []string{
				`"command": "docker"`,
				`"run"`,
				// Note: tools field is added by converter script, not rendered here
			},
		},
		{
			name: "Read-only mode enabled",
			options: GitHubMCPDockerOptions{
				ReadOnly:           true,
				Toolsets:           "default",
				DockerImageVersion: "v1.0.0",
				CustomArgs:         nil,
				IncludeTypeField:   false,
				AllowedTools:       nil,
				EffectiveToken:     "",
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:v1.0.0"`,
				`"env": {`,
				`"GITHUB_READ_ONLY": "1"`,
				`"GITHUB_PERSONAL_ACCESS_TOKEN": "$GITHUB_MCP_SERVER_TOKEN"`,
				`"GITHUB_TOOLSETS": "default"`,
			},
			notFound: []string{
				`"command": "docker"`,
			},
		},
		{
			name: "Custom args provided",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				CustomArgs:         []string{"--verbose", "--debug"},
				IncludeTypeField:   false,
				AllowedTools:       nil,
				EffectiveToken:     "",
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				`"args": [`,
				`"--verbose"`,
				`"--debug"`,
			},
			notFound: []string{
				`"command": "docker"`,
			},
		},
		{
			name: "Copilot with wildcard tools (no allowed tools specified)",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "default",
				DockerImageVersion: "latest",
				CustomArgs:         nil,
				IncludeTypeField:   true,
				AllowedTools:       nil, // When nil, should default to wildcard
				EffectiveToken:     "",
			},
			expected: []string{
				`"type": "stdio"`,
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				// Note: tools field is added by converter script, not rendered here
			},
			notFound: []string{
				`"command": "docker"`,
			},
		},
		{
			name: "Custom toolsets",
			options: GitHubMCPDockerOptions{
				ReadOnly:           false,
				Toolsets:           "repos,issues,pull_requests",
				DockerImageVersion: "latest",
				CustomArgs:         nil,
				IncludeTypeField:   false,
				AllowedTools:       nil,
				EffectiveToken:     "",
			},
			expected: []string{
				`"container": "ghcr.io/github/github-mcp-server:latest"`,
				`"GITHUB_TOOLSETS": "repos,issues,pull_requests"`,
			},
			notFound: []string{
				`"command": "docker"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var yaml strings.Builder
			RenderGitHubMCPDockerConfig(&yaml, tt.options)
			output := yaml.String()

			// Check for expected substrings
			for _, expected := range tt.expected {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nOutput:\n%s", expected, output)
				}
			}

			// Check that unwanted substrings are not present
			for _, notFound := range tt.notFound {
				if strings.Contains(output, notFound) {
					t.Errorf("Expected output NOT to contain %q, but it did.\nOutput:\n%s", notFound, output)
				}
			}
		})
	}
}

func TestRenderGitHubMCPDockerConfig_OutputStructure(t *testing.T) {
	// Test that the output has the expected JSON structure
	var yaml strings.Builder
	RenderGitHubMCPDockerConfig(&yaml, GitHubMCPDockerOptions{
		ReadOnly:           true,
		Toolsets:           "default",
		DockerImageVersion: "latest",
		CustomArgs:         []string{"--test"},
		IncludeTypeField:   true,
		AllowedTools:       []string{"tool1", "tool2"},
		EffectiveToken:     "",
	})

	output := yaml.String()

	// Verify the order of key elements (format: type -> container -> args -> env)
	// Note: tools field is NOT included here - converter scripts add it back for Copilot
	// Note: mounts field is optional and only appears if Mounts is specified
	typeIndex := strings.Index(output, `"type": "stdio"`)
	containerIndex := strings.Index(output, `"container": "ghcr.io/github/github-mcp-server:latest"`)
	argsIndex := strings.Index(output, `"args": [`)
	envIndex := strings.Index(output, `"env": {`)

	if typeIndex == -1 || containerIndex == -1 || argsIndex == -1 || envIndex == -1 {
		t.Fatalf("Missing required JSON structure elements in output:\n%s", output)
	}

	// Verify order: type -> container -> args -> env
	if typeIndex >= containerIndex || containerIndex >= argsIndex || argsIndex >= envIndex {
		t.Errorf("JSON elements are not in expected order. Indices: type=%d, container=%d, args=%d, env=%d\nOutput:\n%s",
			typeIndex, containerIndex, argsIndex, envIndex, output)
	}
}

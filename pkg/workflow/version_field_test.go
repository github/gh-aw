//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
)

func TestVersionField(t *testing.T) {
	// Test GitHub tool version extraction
	t.Run("GitHub version field extraction", func(t *testing.T) {
		// Test "version" field with string
		githubTool := map[string]any{
			"allowed": []any{"create_issue"},
			"version": "v2.0.0",
		}
		result := getGitHubDockerImageVersion(githubTool)
		if result != "v2.0.0" {
			t.Errorf("Expected v2.0.0, got %s", result)
		}

		// Test "version" field with integer
		githubToolInt := map[string]any{
			"version": 20,
		}
		result = getGitHubDockerImageVersion(githubToolInt)
		if result != "20" {
			t.Errorf("Expected 20, got %s", result)
		}

		// Test "version" field with uint64 (as YAML parser returns)
		githubToolUint64 := map[string]any{
			"version": uint64(42),
		}
		result = getGitHubDockerImageVersion(githubToolUint64)
		if result != "42" {
			t.Errorf("Expected 42, got %s", result)
		}

		// Test "version" field with float
		githubToolFloat := map[string]any{
			"version": 3.11,
		}
		result = getGitHubDockerImageVersion(githubToolFloat)
		if result != "3.11" {
			t.Errorf("Expected 3.11, got %s", result)
		}

		// Test default value when version field is not present
		githubToolDefault := map[string]any{
			"allowed": []any{"create_issue"},
		}
		result = getGitHubDockerImageVersion(githubToolDefault)
		if result != string(constants.DefaultGitHubMCPServerVersion) {
			t.Errorf("Expected default %s, got %s", string(constants.DefaultGitHubMCPServerVersion), result)
		}
	})

	// Test MCP parser integration
	t.Run("MCP parser version field integration", func(t *testing.T) {
		// Test GitHub tool with "version" field
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"allowed": []any{"create_issue"},
					"version": "v2.0.0",
				},
			},
		}

		configs, err := parser.ExtractMCPConfigurations(frontmatter, "")
		if err != nil {
			t.Fatalf("Error parsing with version field: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		found := false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "ghcr.io/github/github-mcp-server:v2.0.0") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find v2.0.0 in args, got: %v", configs[0].Args)
		}

	})

	t.Run("Playwright CLI version field extraction", func(t *testing.T) {
		config := parsePlaywrightTool(map[string]any{"version": "0.1.18"})
		if config.Version != "0.1.18" {
			t.Errorf("Expected 0.1.18, got %s", config.Version)
		}
	})

	t.Run("Custom mcp-servers.playwright is not classified as CLI tool", func(t *testing.T) {
		config := parsePlaywrightTool(map[string]any{
			"command": "npx",
			"args":    []any{"--yes", "@playwright/mcp@0.0.79"},
		})
		if config != nil {
			t.Errorf("Expected nil PlaywrightToolConfig for custom MCP server entry, got: %+v", config)
		}
	})
}

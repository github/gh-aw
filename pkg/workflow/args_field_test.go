//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
)

func TestArgsField(t *testing.T) {
	// Test GitHub tool args extraction
	t.Run("GitHub args field extraction", func(t *testing.T) {
		// Test "args" field with []any
		githubTool := map[string]any{
			"allowed": []any{"create_issue"},
			"args":    []any{"--verbose", "--debug"},
		}
		result := getGitHubCustomArgs(githubTool)
		if len(result) != 2 {
			t.Errorf("Expected 2 args, got %d", len(result))
		}
		if result[0] != "--verbose" || result[1] != "--debug" {
			t.Errorf("Expected [--verbose --debug], got %v", result)
		}

		// Test "args" field with []string
		githubToolString := map[string]any{
			"allowed": []any{"create_issue"},
			"args":    []string{"--custom-flag"},
		}
		result = getGitHubCustomArgs(githubToolString)
		if len(result) != 1 {
			t.Errorf("Expected 1 arg, got %d", len(result))
		}
		if result[0] != "--custom-flag" {
			t.Errorf("Expected [--custom-flag], got %v", result)
		}

		// Test no args field (default behavior)
		githubToolDefault := map[string]any{
			"allowed": []any{"create_issue"},
		}
		result = getGitHubCustomArgs(githubToolDefault)
		if result != nil {
			t.Errorf("Expected nil args, got %v", result)
		}
	})

	// Test MCP parser integration for GitHub
	t.Run("MCP parser GitHub args field integration", func(t *testing.T) {
		// Test GitHub tool with "args" field
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"allowed": []any{"create_issue"},
					"args":    []any{"--verbose", "--debug"},
				},
			},
		}

		configs, err := parser.ExtractMCPConfigurations(frontmatter, "")
		if err != nil {
			t.Fatalf("Error parsing with args field: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		// Check that custom args are appended
		foundVerbose := false
		foundDebug := false
		for _, arg := range configs[0].Args {
			if arg == "--verbose" {
				foundVerbose = true
			}
			if arg == "--debug" {
				foundDebug = true
			}
		}
		if !foundVerbose || !foundDebug {
			t.Errorf("Expected to find --verbose and --debug in args, got: %v", configs[0].Args)
		}

		// Verify that the Docker image is still present (should come before custom args)
		foundDockerImage := false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "ghcr.io/github/github-mcp-server:") {
				foundDockerImage = true
				break
			}
		}
		if !foundDockerImage {
			t.Errorf("Expected to find Docker image in args, got: %v", configs[0].Args)
		}
	})

	// Test combined version and args fields
	t.Run("Combined version and args fields", func(t *testing.T) {
		frontmatter := map[string]any{
			"tools": map[string]any{
				"github": map[string]any{
					"allowed": []any{"create_issue"},
					"version": "v2.0.0",
					"args":    []any{"--verbose"},
				},
			},
		}

		configs, err := parser.ExtractMCPConfigurations(frontmatter, "")
		if err != nil {
			t.Fatalf("Error parsing with version and args: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		// Check that both version and args are applied
		foundVersion := false
		foundVerbose := false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "ghcr.io/github/github-mcp-server:v2.0.0") {
				foundVersion = true
			}
			if arg == "--verbose" {
				foundVerbose = true
			}
		}
		if !foundVersion {
			t.Errorf("Expected to find v2.0.0 in args, got: %v", configs[0].Args)
		}
		if !foundVerbose {
			t.Errorf("Expected to find --verbose in args, got: %v", configs[0].Args)
		}
	})
}

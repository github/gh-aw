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

	// Test Playwright tool version extraction
	t.Run("Playwright version field extraction", func(t *testing.T) {
		// Test "version" field
		playwrightTool := map[string]any{
			"allowed_domains": []any{"example.com"},
			"version":         "v1.41.0",
		}
		playwrightConfig := parsePlaywrightTool(playwrightTool)
		result := getPlaywrightDockerImageVersion(playwrightConfig)
		if result != "v1.41.0" {
			t.Errorf("Expected v1.41.0, got %s", result)
		}

		// Test default value when version field is not present
		playwrightToolDefault := map[string]any{
			"allowed_domains": []any{"example.com"},
		}
		playwrightConfigDefault := parsePlaywrightTool(playwrightToolDefault)
		result = getPlaywrightDockerImageVersion(playwrightConfigDefault)
		if result != string(constants.DefaultPlaywrightBrowserVersion) {
			t.Errorf("Expected default %s, got %s", string(constants.DefaultPlaywrightBrowserVersion), result)
		}

		// Test integer version
		playwrightToolInt := map[string]any{
			"allowed_domains": []any{"example.com"},
			"version":         20,
		}
		playwrightConfigInt := parsePlaywrightTool(playwrightToolInt)
		result = getPlaywrightDockerImageVersion(playwrightConfigInt)
		if result != "20" {
			t.Errorf("Expected 20, got %s", result)
		}

		// Test float version
		playwrightToolFloat := map[string]any{
			"allowed_domains": []any{"example.com"},
			"version":         1.41,
		}
		playwrightConfigFloat := parsePlaywrightTool(playwrightToolFloat)
		result = getPlaywrightDockerImageVersion(playwrightConfigFloat)
		if result != "1.41" {
			t.Errorf("Expected 1.41, got %s", result)
		}

		// Test int64 version
		playwrightToolInt64 := map[string]any{
			"allowed_domains": []any{"example.com"},
			"version":         int64(142),
		}
		playwrightConfigInt64 := parsePlaywrightTool(playwrightToolInt64)
		result = getPlaywrightDockerImageVersion(playwrightConfigInt64)
		if result != "142" {
			t.Errorf("Expected 142, got %s", result)
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

		// Test Playwright tool with "version" field
		frontmatterPlaywright := map[string]any{
			"tools": map[string]any{
				"playwright": map[string]any{
					"allowed_domains": []any{"example.com"},
					"version":         "v1.41.0",
				},
			},
		}

		configs, err = parser.ExtractMCPConfigurations(frontmatterPlaywright, "")
		if err != nil {
			t.Fatalf("Error parsing Playwright with version field: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		found = false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "mcr.microsoft.com/playwright:v1.41.0") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find v1.41.0 in args, got: %v", configs[0].Args)
		}

		// Test Playwright tool with integer version
		frontmatterPlaywrightInt := map[string]any{
			"tools": map[string]any{
				"playwright": map[string]any{
					"allowed_domains": []any{"example.com"},
					"version":         20,
				},
			},
		}

		configs, err = parser.ExtractMCPConfigurations(frontmatterPlaywrightInt, "")
		if err != nil {
			t.Fatalf("Error parsing Playwright with integer version: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		found = false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "mcr.microsoft.com/playwright:20") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find :20 in args, got: %v", configs[0].Args)
		}

		// Test Playwright tool with float version
		frontmatterPlaywrightFloat := map[string]any{
			"tools": map[string]any{
				"playwright": map[string]any{
					"allowed_domains": []any{"example.com"},
					"version":         1.41,
				},
			},
		}

		configs, err = parser.ExtractMCPConfigurations(frontmatterPlaywrightFloat, "")
		if err != nil {
			t.Fatalf("Error parsing Playwright with float version: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		found = false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "mcr.microsoft.com/playwright:1.41") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find :1.41 in args, got: %v", configs[0].Args)
		}
	})
}

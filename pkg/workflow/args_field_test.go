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

	// Test Playwright tool args extraction
	t.Run("Playwright args field extraction", func(t *testing.T) {
		// Test "args" field with []any
		playwrightTool := map[string]any{
			"allowed_domains": []any{"example.com"},
			"args":            []any{"--browser", "firefox"},
		}
		playwrightConfig := parsePlaywrightTool(playwrightTool)
		result := getPlaywrightCustomArgs(playwrightConfig)
		if len(result) != 2 {
			t.Errorf("Expected 2 args, got %d", len(result))
		}
		if result[0] != "--browser" || result[1] != "firefox" {
			t.Errorf("Expected [--browser firefox], got %v", result)
		}

		// Test "args" field with []string
		playwrightToolString := map[string]any{
			"allowed_domains": []any{"example.com"},
			"args":            []string{"--headless"},
		}
		playwrightConfigString := parsePlaywrightTool(playwrightToolString)
		result = getPlaywrightCustomArgs(playwrightConfigString)
		if len(result) != 1 {
			t.Errorf("Expected 1 arg, got %d", len(result))
		}
		if result[0] != "--headless" {
			t.Errorf("Expected [--headless], got %v", result)
		}

		// Test no args field (default behavior)
		playwrightToolDefault := map[string]any{
			"allowed_domains": []any{"example.com"},
		}
		playwrightConfigDefault := parsePlaywrightTool(playwrightToolDefault)
		result = getPlaywrightCustomArgs(playwrightConfigDefault)
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

	// Test MCP parser integration for Playwright
	t.Run("MCP parser Playwright args field integration", func(t *testing.T) {
		// Test Playwright tool with "args" field
		frontmatterPlaywright := map[string]any{
			"tools": map[string]any{
				"playwright": map[string]any{
					"allowed_domains": []any{"example.com"},
					"args":            []any{"--browser", "firefox"},
				},
			},
		}

		configs, err := parser.ExtractMCPConfigurations(frontmatterPlaywright, "")
		if err != nil {
			t.Fatalf("Error parsing Playwright with args field: %v", err)
		}

		if len(configs) == 0 {
			t.Fatal("No configs returned")
		}

		// Check that custom args are appended
		foundBrowser := false
		foundFirefox := false
		for _, arg := range configs[0].Args {
			if arg == "--browser" {
				foundBrowser = true
			}
			if arg == "firefox" {
				foundFirefox = true
			}
		}
		if !foundBrowser || !foundFirefox {
			t.Errorf("Expected to find --browser and firefox in args, got: %v", configs[0].Args)
		}

		// Verify that the Docker image is still present (should come before custom args)
		foundDockerImage := false
		for _, arg := range configs[0].Args {
			if strings.Contains(arg, "mcr.microsoft.com/playwright:") {
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

// TestMountsFieldExtraction tests the extraction of mounts field from tool configurations
func TestMountsFieldExtraction(t *testing.T) {
	// Test GitHub tool mounts extraction
	t.Run("GitHub mounts field extraction with []any", func(t *testing.T) {
		githubTool := map[string]any{
			"mounts": []any{"/data:/data:ro", "/tmp:/tmp:rw"},
		}
		result := getGitHubMounts(githubTool)
		if len(result) != 2 {
			t.Errorf("Expected 2 mounts, got %d", len(result))
		}
		if result[0] != "/data:/data:ro" || result[1] != "/tmp:/tmp:rw" {
			t.Errorf("Expected [/data:/data:ro /tmp:/tmp:rw], got %v", result)
		}
	})

	t.Run("GitHub mounts field extraction with []string", func(t *testing.T) {
		githubToolString := map[string]any{
			"mounts": []string{"/opt:/opt:ro"},
		}
		result := getGitHubMounts(githubToolString)
		if len(result) != 1 {
			t.Errorf("Expected 1 mount, got %d", len(result))
		}
		if result[0] != "/opt:/opt:ro" {
			t.Errorf("Expected [/opt:/opt:ro], got %v", result)
		}
	})

	t.Run("GitHub no mounts field - default behavior", func(t *testing.T) {
		githubToolDefault := map[string]any{
			"allowed": []any{"create_issue"},
		}
		result := getGitHubMounts(githubToolDefault)
		if result != nil {
			t.Errorf("Expected nil mounts, got %v", result)
		}
	})

	t.Run("GitHub empty mounts array", func(t *testing.T) {
		githubToolEmpty := map[string]any{
			"mounts": []string{},
		}
		result := getGitHubMounts(githubToolEmpty)
		if len(result) != 0 {
			t.Errorf("Expected empty mounts array, got %v", result)
		}
	})
}

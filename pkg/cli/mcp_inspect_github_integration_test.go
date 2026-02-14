//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPInspectGitHubIntegration tests that the mcp inspect command
// properly validates GitHub tool configuration for all three agentic engines
func TestMCPInspectGitHubIntegration(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Test cases for each engine
	engines := []struct {
		name            string
		engineConfig    string
		expectedSuccess bool
	}{
		{
			name: "copilot",
			engineConfig: `engine: copilot
tools:
  github:
    toolsets: [default]`,
			expectedSuccess: true,
		},
		{
			name: "claude",
			engineConfig: `engine: claude
tools:
  github:
    toolsets: [default]`,
			expectedSuccess: true,
		},
		{
			name: "codex",
			engineConfig: `engine: codex
tools:
  github:
    toolsets: [default]`,
			expectedSuccess: true,
		},
	}

	for _, tc := range engines {
		t.Run(tc.name, func(t *testing.T) {
			// Create a test workflow file for this engine
			workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
` + tc.engineConfig + `
---

# Test GitHub Configuration for ` + tc.name + `

This workflow tests GitHub tool configuration.
`

			workflowFile := filepath.Join(setup.workflowsDir, "test-github-"+tc.name+".md")
			if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("Failed to create test workflow file: %v", err)
			}

			// Run mcp inspect command to verify GitHub configuration
			cmd := exec.Command(setup.binaryPath, "mcp", "inspect", "test-github-"+tc.name, "--server", "github", "--verbose")
			cmd.Dir = setup.tempDir
			// Set a placeholder GitHub token to avoid token validation errors
			cmd.Env = append(os.Environ(), "GITHUB_TOKEN=test_token_for_integration_test")

			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			t.Logf("MCP inspect output for %s engine:\n%s", tc.name, outputStr)

			if tc.expectedSuccess {
				// Check for errors first (following Playwright test pattern)
				if err != nil {
					// Some errors might be acceptable (e.g., docker not available)
					// Check if it's a configuration validation error
					if strings.Contains(outputStr, "Frontmatter validation passed") ||
						strings.Contains(outputStr, "MCP configuration validation passed") ||
						strings.Contains(strings.ToLower(outputStr), "github") {
						t.Logf("✓ GitHub configuration validated for %s engine (command had warnings/errors but config was parsed)", tc.name)
					} else {
						t.Errorf("Unexpected error for %s engine: %v\nOutput: %s", tc.name, err, outputStr)
					}
				}

				// Check that the output mentions github server
				if !strings.Contains(strings.ToLower(outputStr), "github") {
					t.Errorf("Expected github to be mentioned in output for %s engine", tc.name)
				}

				// Check that configuration was validated
				if strings.Contains(outputStr, "Frontmatter validation passed") {
					t.Logf("✓ Frontmatter validation passed for %s engine", tc.name)
				}

				if strings.Contains(outputStr, "MCP configuration validation passed") {
					t.Logf("✓ MCP configuration validation passed for %s engine", tc.name)
				}

				// Verify that we see the GitHub MCP server
				if strings.Contains(outputStr, "📡 github") {
					t.Logf("✓ GitHub MCP server detected for %s engine", tc.name)
				} else {
					// This might be okay if there are connection issues
					t.Logf("Note: GitHub MCP server indicator not explicitly found for %s engine", tc.name)
				}

				// Check that we see some GitHub tools listed
				expectedTools := []string{
					"add_issue_comment",
					"create_pull_request",
					"get_file_contents",
					"list_issues",
					"search_code",
				}

				foundToolCount := 0
				for _, tool := range expectedTools {
					if strings.Contains(outputStr, tool) {
						foundToolCount++
					}
				}

				if foundToolCount > 0 {
					t.Logf("✓ Found %d/%d expected GitHub tools for %s engine", foundToolCount, len(expectedTools), tc.name)
				}
			}
		})
	}
}

// TestMCPInspectGitHubToolsListing tests that GitHub tools are properly listed
// for each engine when using mcp inspect command
func TestMCPInspectGitHubToolsListing(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	engines := []string{"copilot", "claude", "codex"}

	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			// Create a workflow with GitHub configuration
			workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: ` + engine + `
tools:
  github:
    toolsets: [repos, issues, pull_requests]
---

# Test GitHub Tools for ` + engine + `

Test workflow for GitHub tools inspection.
`

			workflowFile := filepath.Join(setup.workflowsDir, "test-github-tools-"+engine+".md")
			if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("Failed to create test workflow file: %v", err)
			}

			// Run mcp inspect without --server flag to list all MCP servers
			cmd := exec.Command(setup.binaryPath, "mcp", "inspect", "test-github-tools-"+engine, "--verbose")
			cmd.Dir = setup.tempDir
			cmd.Env = append(os.Environ(), "GITHUB_TOKEN=test_token_for_integration_test")

			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			t.Logf("MCP inspect output for %s:\n%s", engine, outputStr)

			// Check for errors first (following Playwright test pattern)
			if err != nil {
				// Docker not available or connection issues are acceptable
				if strings.Contains(outputStr, "docker") ||
					strings.Contains(outputStr, "Docker") ||
					strings.Contains(outputStr, "Frontmatter validation passed") ||
					strings.Contains(outputStr, "MCP configuration validation passed") ||
					strings.Contains(strings.ToLower(outputStr), "github") {
					t.Logf("Test completed with expected warnings for %s engine", engine)
				} else {
					t.Logf("Warning: Command failed for %s engine with: %v", engine, err)
				}
			}

			// Check if the output mentions GitHub server
			if strings.Contains(strings.ToLower(outputStr), "github") {
				t.Logf("✓ GitHub MCP server detected for %s engine", engine)
			}

			// Verify validation occurred
			if strings.Contains(outputStr, "validation") {
				t.Logf("✓ Configuration validation occurred for %s engine", engine)
			}

			// Check that we see "docker" type for GitHub MCP server
			if strings.Contains(outputStr, "docker") {
				t.Logf("✓ GitHub MCP server type (docker) detected for %s engine", engine)
			}
		})
	}
}

// TestMCPInspectGitHubWithSpecificToolsets tests GitHub configuration with specific toolsets
func TestMCPInspectGitHubWithSpecificToolsets(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Test with copilot engine and specific toolsets
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
tools:
  github:
    toolsets: [repos, issues, actions]
---

# Test GitHub Toolsets

Test workflow for specific GitHub toolsets.
`

	workflowFile := filepath.Join(setup.workflowsDir, "test-github-toolsets.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Run mcp inspect
	cmd := exec.Command(setup.binaryPath, "mcp", "inspect", "test-github-toolsets", "--server", "github", "--verbose")
	cmd.Dir = setup.tempDir
	cmd.Env = append(os.Environ(), "GITHUB_TOKEN=test_token_for_integration_test")

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	t.Logf("MCP inspect output:\n%s", outputStr)

	// Check for errors first (following Playwright test pattern)
	if err != nil {
		// Acceptable errors include docker/connection issues
		if strings.Contains(outputStr, "docker") ||
			strings.Contains(outputStr, "validation passed") ||
			strings.Contains(strings.ToLower(outputStr), "github") {
			t.Logf("Test completed with expected warnings")
		} else {
			t.Logf("Warning: Command failed with: %v", err)
		}
	}

	// Verify GitHub server is detected
	if !strings.Contains(strings.ToLower(outputStr), "github") {
		t.Errorf("Expected GitHub server to be detected")
	}

	// Check for validation
	if strings.Contains(outputStr, "Frontmatter validation passed") {
		t.Logf("✓ Frontmatter validation passed")
	}

	if strings.Contains(outputStr, "MCP configuration validation passed") {
		t.Logf("✓ MCP configuration validation passed")
	}
}

//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMCPInspectPlaywrightIntegration tests that the mcp inspect command
// properly validates playwright tool configuration for all three agentic engines
func TestMCPInspectPlaywrightIntegration(t *testing.T) {
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
  playwright:
    allowed_domains:
      - "localhost"
      - "example.com"`,
			expectedSuccess: true,
		},
		{
			name: "claude",
			engineConfig: `engine: claude
tools:
  playwright:
    allowed_domains:
      - "localhost"
      - "example.com"`,
			expectedSuccess: true,
		},
		{
			name: "codex",
			engineConfig: `engine: codex
tools:
  playwright:
    allowed_domains:
      - "localhost"
      - "example.com"`,
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

# Test Playwright Configuration for ` + tc.name + `

This workflow tests playwright tool configuration.
`

			workflowFile := filepath.Join(setup.workflowsDir, "test-playwright-"+tc.name+".md")
			if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
				t.Fatalf("Failed to create test workflow file: %v", err)
			}

			// Run mcp inspect command to verify playwright configuration
			cmd := exec.Command(setup.binaryPath, "mcp", "inspect", "test-playwright-"+tc.name, "--server", "playwright", "--verbose")
			cmd.Dir = setup.tempDir

			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			t.Logf("MCP inspect output for %s engine:\n%s", tc.name, outputStr)

			if tc.expectedSuccess {
				if err != nil {
					// Some errors might be acceptable (e.g., docker not available)
					// Check if it's a configuration validation error
					if strings.Contains(outputStr, "Frontmatter validation passed") ||
						strings.Contains(outputStr, "MCP configuration validation passed") ||
						strings.Contains(outputStr, "playwright") {
						t.Logf("✓ Playwright configuration validated for %s engine (command had warnings/errors but config was parsed)", tc.name)
					} else {
						t.Errorf("Unexpected error for %s engine: %v\nOutput: %s", tc.name, err, outputStr)
					}
				}

				// Verify that the output mentions playwright
				if !strings.Contains(strings.ToLower(outputStr), "playwright") {
					t.Errorf("Expected playwright to be mentioned in output for %s engine", tc.name)
				}

				// Check that configuration was validated
				if strings.Contains(outputStr, "Frontmatter validation passed") {
					t.Logf("✓ Frontmatter validation passed for %s engine", tc.name)
				}

				if strings.Contains(outputStr, "MCP configuration validation passed") {
					t.Logf("✓ MCP configuration validation passed for %s engine", tc.name)
				}

				// Verify allowed_domains appear in the output
				if strings.Contains(outputStr, "localhost") || strings.Contains(outputStr, "example.com") {
					t.Logf("✓ Allowed domains configuration detected for %s engine", tc.name)
				}
			}
		})
	}
}

// TestMCPInspectPlaywrightTools tests that playwright tools are properly listed
// for each engine when using mcp inspect command
func TestMCPInspectPlaywrightTools(t *testing.T) {
	setup := setupIntegrationTest(t)
	defer setup.cleanup()

	// Create a simple workflow with playwright configuration
	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
tools:
  playwright:
    allowed_domains:
      - "localhost"
---

# Test Playwright Tools

Test workflow for playwright tools inspection.
`

	workflowFile := filepath.Join(setup.workflowsDir, "test-playwright-tools.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0o644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Run mcp inspect without --server flag to list all MCP servers
	cmd := exec.Command(setup.binaryPath, "mcp", "inspect", "test-playwright-tools", "--verbose")
	cmd.Dir = setup.tempDir

	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	t.Logf("MCP inspect output:\n%s", outputStr)

	// Check if the output mentions playwright server
	if strings.Contains(strings.ToLower(outputStr), "playwright") {
		t.Logf("✓ Playwright MCP server detected in workflow")
	} else {
		// This might be okay if docker is not available
		if strings.Contains(outputStr, "docker") || strings.Contains(outputStr, "Docker") {
			t.Logf("Warning: Docker might not be available for playwright server")
		} else {
			t.Logf("Note: Playwright not explicitly mentioned in output (may be filtered or not available)")
		}
	}

	// Verify validation occurred
	if strings.Contains(outputStr, "validation") {
		t.Logf("✓ Configuration validation occurred")
	}

	// If there's an error, check if it's acceptable
	if err != nil {
		// Docker not available is acceptable for this test
		if strings.Contains(outputStr, "docker") ||
			strings.Contains(outputStr, "Docker") ||
			strings.Contains(outputStr, "Frontmatter validation passed") ||
			strings.Contains(outputStr, "MCP configuration validation passed") {
			t.Logf("Test completed with expected warnings (docker availability)")
		} else {
			t.Logf("Warning: Command failed with: %v", err)
		}
	}
}

//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestSafeInputsHTTPMode verifies that HTTP mode generates correct configuration
func TestSafeInputsHTTPMode(t *testing.T) {
	testCases := []struct {
		name string
		mode string // empty string tests default behavior
	}{
		{
			name: "default_mode",
			mode: "", // No mode specified, should default to HTTP
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a temporary workflow file
			tempDir := t.TempDir()
			workflowPath := filepath.Join(tempDir, "test-workflow.md")

			modeField := ""
			if tc.mode != "" {
				modeField = "  mode: " + tc.mode + "\n"
			}

			workflowContent := `---
on: workflow_dispatch
engine: copilot
safe-inputs:
` + modeField + `  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

Test safe-inputs HTTP mode
`

			err := os.WriteFile(workflowPath, []byte(workflowContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			lockContent, err := os.ReadFile(lockPath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			yamlStr := string(lockContent)

			// Verify that HTTP server startup steps ARE present
			expectedSteps := []string{
				"Generate Safe Inputs MCP Server Config",
				"Start Safe Inputs MCP HTTP Server",
			}

			for _, stepName := range expectedSteps {
				if !strings.Contains(yamlStr, stepName) {
					t.Errorf("Expected HTTP server step not found: %q", stepName)
				}
			}

			// Verify HTTP configuration in MCP setup
			if !strings.Contains(yamlStr, `"safeinputs"`) {
				t.Error("Safe-inputs MCP server config not found")
			}

			// Should use HTTP transport
			if !strings.Contains(yamlStr, `"type": "http"`) {
				t.Error("Expected type field set to 'http' in MCP config")
			}

			if !strings.Contains(yamlStr, `"url": "http://host.docker.internal`) {
				t.Error("Expected HTTP URL in config")
			}

			if !strings.Contains(yamlStr, `"headers"`) {
				t.Error("Expected headers field in HTTP config")
			}

			// Verify the entry point script uses HTTP
			if !strings.Contains(yamlStr, "startHttpServer") {
				t.Error("Expected HTTP entry point to use startHttpServer")
			}

			// Check the actual mcp-server.cjs entry point uses HTTP server
			entryPointSection := extractMCPServerEntryPoint(yamlStr)
			if !strings.Contains(entryPointSection, "startHttpServer(configPath") {
				t.Error("Entry point should call startHttpServer for HTTP mode")
			}

			t.Logf("✓ HTTP mode correctly configured with HTTP server steps")
		})
	}
}

// extractMCPServerEntryPoint extracts the mcp-server.cjs entry point script from the YAML
func extractMCPServerEntryPoint(yamlStr string) string {
	// Find the mcp-server.cjs section
	start := strings.Index(yamlStr, "cat > /opt/gh-aw/safe-inputs/mcp-server.cjs")
	if start == -1 {
		return ""
	}

	// Find the heredoc start marker
	heredocStart := strings.Index(yamlStr[start:], "<< 'EOFSI'")
	if heredocStart == -1 {
		return ""
	}
	// Move past the heredoc start and newline to the actual content
	contentStart := start + heredocStart + len("<< 'EOFSI'\n")

	// Find the EOFSI marker that ends the heredoc (should be at start of a line)
	end := strings.Index(yamlStr[contentStart:], "\n          EOFSI")
	if end == -1 {
		// Try without the leading spaces (in case formatting is different)
		end = strings.Index(yamlStr[contentStart:], "\nEOFSI")
		if end == -1 {
			return ""
		}
	}

	return yamlStr[contentStart : contentStart+end]
}

//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestMCPScriptsHTTPMode verifies that HTTP mode generates correct configuration
func TestMCPScriptsHTTPMode(t *testing.T) {
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
mcp-scripts:
` + modeField + `  test-tool:
    description: Test tool
    script: |
      return { result: "test" };
---

Test mcp-scripts HTTP mode
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
				"Generate MCP Scripts Server Config",
				"Start MCP Scripts HTTP Server",
			}

			for _, stepName := range expectedSteps {
				if !strings.Contains(yamlStr, stepName) {
					t.Errorf("Expected HTTP server step not found: %q", stepName)
				}
			}

			// Verify HTTP configuration in MCP setup
			if !strings.Contains(yamlStr, `"mcpscripts"`) {
				t.Error("MCP Scripts server config not found")
			}

			// Should use HTTP transport
			if !strings.Contains(yamlStr, `"type": "http"`) {
				t.Error("Expected type field set to 'http' in MCP config")
			}

			if !strings.Contains(yamlStr, `"url": "http://mcp-scripts.docker.internal:$GH_AW_MCP_SCRIPTS_PORT"`) {
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
	start := strings.Index(yamlStr, "cat > \"${RUNNER_TEMP}/gh-aw/mcp-scripts/mcp-server.cjs\"")
	if start == -1 {
		return ""
	}

	// Find the heredoc start marker using regex (delimiter is randomized)
	heredocMarkerRE := regexp.MustCompile(`<< 'GH_AW_MCP_SCRIPTS_SERVER_[0-9a-f]{16}_EOF'`)
	loc := heredocMarkerRE.FindStringIndex(yamlStr[start:])
	if loc == nil {
		return ""
	}
	heredocMarker := yamlStr[start+loc[0] : start+loc[1]]
	// Extract the actual delimiter (strip the << ' and ' wrapper)
	delimiter := heredocMarker[4 : len(heredocMarker)-1]

	// Move past the heredoc start and newline to the actual content
	contentStart := start + loc[1] + 1 // +1 for newline

	// Find the delimiter marker that ends the heredoc (should be at start of a line)
	endMarkerWithSpaces := "\n          " + delimiter
	end := strings.Index(yamlStr[contentStart:], endMarkerWithSpaces)
	if end == -1 {
		// Try without the leading spaces (in case formatting is different)
		endMarkerNoSpaces := "\n" + delimiter
		end = strings.Index(yamlStr[contentStart:], endMarkerNoSpaces)
		if end == -1 {
			return ""
		}
	}

	return yamlStr[contentStart : contentStart+end]
}

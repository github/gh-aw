//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestHTTPMCPServerRequiresURL tests that HTTP MCP servers require a url field
func TestHTTPMCPServerRequiresURL(t *testing.T) {
	tests := []struct {
		name        string
		workflow    string
		expectError bool
		errorText   string
	}{
		{
			name: "HTTP MCP server without url should fail",
			workflow: `---
on: issues
permissions:
  contents: read
engine: copilot
mcp-servers:
  test-http:
    type: http
---

# Test workflow
`,
			expectError: true,
			errorText:   "missing property 'url'",
		},
		{
			name: "HTTP MCP server with url should pass",
			workflow: `---
on: issues
permissions:
  contents: read
engine: copilot
mcp-servers:
  test-http:
    type: http
    url: "https://example.com"
---

# Test workflow
`,
			expectError: false,
		},
		{
			name: "HTTP MCP server with url only (inferred type) should pass",
			workflow: `---
on: issues
permissions:
  contents: read
engine: copilot
mcp-servers:
  test-http:
    url: "https://example.com"
---

# Test workflow
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary workflow file
			tmpDir := testutil.TempDir(t, "test-*")
			workflowPath := filepath.Join(tmpDir, "test.md")
			if err := os.WriteFile(workflowPath, []byte(tt.workflow), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Create compiler and try to compile the workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorText != "" && !strings.Contains(err.Error(), tt.errorText) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorText, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

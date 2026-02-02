//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestAgenticWorkflowsPermissionValidation(t *testing.T) {
	tests := []struct {
		name          string
		workflowMD    string
		expectError   bool
		errorContains string
	}{
		{
			name: "agentic-workflows tool requires actions:read permission",
			workflowMD: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    toolsets: [default]
  agentic-workflows: true
---

# Test workflow
`,
			expectError:   true,
			errorContains: "Missing required permission for agentic-workflows tool",
		},
		{
			name: "agentic-workflows tool with actions:read succeeds",
			workflowMD: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
tools:
  github:
    toolsets: [default]
  agentic-workflows: true
---

# Test workflow
`,
			expectError: false,
		},
		{
			name: "workflow without agentic-workflows doesn't require actions:read",
			workflowMD: `---
on: workflow_dispatch
engine: copilot
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    toolsets: [default]
  bash: true
---

# Test workflow
`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tempDir := testutil.TempDir(t, "test-*")
			workflowsDir := filepath.Join(tempDir, ".github", "workflows")
			if err := os.MkdirAll(workflowsDir, 0755); err != nil {
				t.Fatalf("Failed to create workflows directory: %v", err)
			}

			// Write workflow file
			workflowPath := filepath.Join(workflowsDir, "test.md")
			if err := os.WriteFile(workflowPath, []byte(tt.workflowMD), 0644); err != nil {
				t.Fatalf("Failed to write workflow file: %v", err)
			}

			// Compile workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowPath)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
		})
	}
}

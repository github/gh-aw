//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

// TestAwInfoBeforeValidateSecret verifies that the generate_aw_info step
// appears before the validate-secret step in the generated workflow.
func TestAwInfoBeforeValidateSecret(t *testing.T) {
	tests := []struct {
		name            string
		workflowContent string
		engine          string
	}{
		{
			name: "copilot engine",
			workflowContent: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
---

# Test Copilot Workflow

This workflow tests that generate_aw_info appears before validate-secret.
`,
			engine: "copilot",
		},
		{
			name: "claude engine",
			workflowContent: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
---

# Test Claude Workflow

This workflow tests that generate_aw_info appears before validate-secret.
`,
			engine: "claude",
		},
		{
			name: "codex engine",
			workflowContent: `---
on: push
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: codex
---

# Test Codex Workflow

This workflow tests that generate_aw_info appears before validate-secret.
`,
			engine: "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary directory for test files
			tmpDir := testutil.TempDir(t, "aw-info-order-test")

			// Create test file
			testFile := filepath.Join(tmpDir, "test-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflowContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(testFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}

			lockStr := string(lockContent)

			// Find the positions of both steps
			awInfoPos := strings.Index(lockStr, "id: generate_aw_info")
			validateSecretPos := strings.Index(lockStr, "id: validate-secret")

			// Both steps should exist
			if awInfoPos == -1 {
				t.Error("Expected 'id: generate_aw_info' to be present in generated workflow")
			}
			if validateSecretPos == -1 {
				t.Error("Expected 'id: validate-secret' to be present in generated workflow")
			}

			// generate_aw_info must come before validate-secret
			if awInfoPos != -1 && validateSecretPos != -1 {
				if awInfoPos > validateSecretPos {
					t.Errorf("Step ordering error: generate_aw_info (pos %d) should come before validate-secret (pos %d)",
						awInfoPos, validateSecretPos)
				} else {
					t.Logf("✓ Step ordering correct: generate_aw_info (pos %d) comes before validate-secret (pos %d)",
						awInfoPos, validateSecretPos)
				}
			}
		})
	}
}

//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestPermissionsShortcutInIncludedFiles tests that permissions shortcuts (read-all, write-all, none)
// work correctly in included files, matching the UX of main workflows.
func TestPermissionsShortcutInIncludedFiles(t *testing.T) {
	tests := []struct {
		name                   string
		includedPermissions    string
		mainPermissions        string
		expectCompilationError bool
		expectLockFileContains string
	}{
		{
			name:                   "read-all shortcut in included file",
			includedPermissions:    "permissions: read-all",
			mainPermissions:        "permissions: read-all",
			expectCompilationError: false,
			expectLockFileContains: "permissions: read-all",
		},
		{
			name:                   "write-all shortcut in included file",
			includedPermissions:    "permissions: write-all",
			mainPermissions:        "permissions: write-all\nfeatures:\n  dangerous-permissions-write: true",
			expectCompilationError: false,
			expectLockFileContains: "permissions: write-all",
		},
		{
			name: "object form still works in included file",
			includedPermissions: `permissions:
  contents: read
  issues: write`,
			mainPermissions: `permissions:
  contents: read
  issues: write
  pull-requests: read
features:
  dangerous-permissions-write: true`,
			expectCompilationError: false,
			expectLockFileContains: "issues: write",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			tempDir := testutil.TempDir(t, "test-*")
			sharedDir := filepath.Join(tempDir, ".github", "workflows", "shared")
			if err := os.MkdirAll(sharedDir, 0755); err != nil {
				t.Fatalf("Failed to create shared directory: %v", err)
			}

			// Create a shared workflow file with permissions shortcut
			sharedWorkflowContent := "---\n" + tt.includedPermissions + "\n---\n\n# Shared workflow\n"
			sharedWorkflowPath := filepath.Join(sharedDir, "shared-permissions.md")
			if err := os.WriteFile(sharedWorkflowPath, []byte(sharedWorkflowContent), 0644); err != nil {
				t.Fatalf("Failed to create shared workflow file: %v", err)
			}

			// Create main workflow that imports the shared file
			mainWorkflowContent := `---
on: issues
engine: copilot
strict: false
` + tt.mainPermissions + `
imports:
  - shared/shared-permissions.md
tools:
  github:
    toolsets: [default]
---

# Main workflow
`
			mainWorkflowPath := filepath.Join(tempDir, ".github", "workflows", "test-workflow.md")
			if err := os.WriteFile(mainWorkflowPath, []byte(mainWorkflowContent), 0644); err != nil {
				t.Fatalf("Failed to create main workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(mainWorkflowPath)

			if tt.expectCompilationError {
				if err == nil {
					t.Fatalf("Expected compilation to fail but it succeeded")
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected compilation to succeed but got error: %v", err)
			}

			// Read the generated lock file
			lockFilePath := filepath.Join(tempDir, ".github", "workflows", "test-workflow.lock.yml")
			lockContent, err := os.ReadFile(lockFilePath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)
			if !strings.Contains(lockStr, tt.expectLockFileContains) {
				t.Errorf("Expected lock file to contain '%s', but it doesn't. Lock file:\n%s", tt.expectLockFileContains, lockStr)
			}
		})
	}
}

// TestPermissionsShortcutMixedUsage tests that shortcuts and object form can be mixed across files
func TestPermissionsShortcutMixedUsage(t *testing.T) {
	tests := []struct {
		name                   string
		includedPermissions    string
		mainPermissions        string
		expectCompilationError bool
		expectLockFileContains []string
	}{
		{
			name:                   "shortcut in included file, object in main",
			includedPermissions:    "permissions: read-all",
			mainPermissions:        "permissions:\n  contents: read\n  issues: read",
			expectCompilationError: false,
			expectLockFileContains: []string{"contents: read", "issues: read"},
		},
		{
			name:                   "object in included file, shortcut in main",
			includedPermissions:    "permissions:\n  contents: read",
			mainPermissions:        "permissions: read-all",
			expectCompilationError: false,
			expectLockFileContains: []string{"permissions: read-all"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for test files
			tempDir := testutil.TempDir(t, "test-*")
			sharedDir := filepath.Join(tempDir, ".github", "workflows", "shared")
			if err := os.MkdirAll(sharedDir, 0755); err != nil {
				t.Fatalf("Failed to create shared directory: %v", err)
			}

			// Create a shared workflow file with permissions
			sharedWorkflowContent := "---\n" + tt.includedPermissions + "\n---\n\n# Shared workflow\n"
			sharedWorkflowPath := filepath.Join(sharedDir, "shared-permissions.md")
			if err := os.WriteFile(sharedWorkflowPath, []byte(sharedWorkflowContent), 0644); err != nil {
				t.Fatalf("Failed to create shared workflow file: %v", err)
			}

			// Create main workflow
			mainWorkflowContent := `---
on: issues
engine: copilot
strict: false
` + tt.mainPermissions + `
imports:
  - shared/shared-permissions.md
tools:
  github:
    toolsets: [default]
---

# Main workflow
`
			mainWorkflowPath := filepath.Join(tempDir, ".github", "workflows", "test-workflow.md")
			if err := os.WriteFile(mainWorkflowPath, []byte(mainWorkflowContent), 0644); err != nil {
				t.Fatalf("Failed to create main workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(mainWorkflowPath)

			if tt.expectCompilationError {
				if err == nil {
					t.Fatalf("Expected compilation to fail but it succeeded")
				}
				return
			}

			if err != nil {
				t.Fatalf("Expected compilation to succeed but got error: %v", err)
			}

			// Read the generated lock file
			lockFilePath := filepath.Join(tempDir, ".github", "workflows", "test-workflow.lock.yml")
			lockContent, err := os.ReadFile(lockFilePath)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockStr := string(lockContent)
			for _, expected := range tt.expectLockFileContains {
				if !strings.Contains(lockStr, expected) {
					t.Errorf("Expected lock file to contain '%s', but it doesn't", expected)
				}
			}
		})
	}
}

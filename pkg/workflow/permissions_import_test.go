//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/parser"
)

func TestValidateIncludedPermissions(t *testing.T) {
	tests := []struct {
		name                string
		topPermissionsYAML  string
		importedPermissions string
		expectError         bool
		errorContains       string
	}{
		{
			name:                "No imported permissions passes validation",
			topPermissionsYAML:  "permissions:\n  contents: read",
			importedPermissions: "",
			expectError:         false,
		},
		{
			name:                "Empty imported permissions passes validation",
			topPermissionsYAML:  "permissions:\n  contents: read",
			importedPermissions: "{}",
			expectError:         false,
		},
		{
			name:                "Missing permission fails validation",
			topPermissionsYAML:  "permissions:\n  contents: read",
			importedPermissions: `{"actions":"read"}`,
			expectError:         true,
			errorContains:       "Missing permissions",
		},
		{
			name:                "Insufficient permission level fails validation",
			topPermissionsYAML:  "permissions:\n  contents: read",
			importedPermissions: `{"contents":"write"}`,
			expectError:         true,
			errorContains:       "Insufficient permissions",
		},
		{
			name:                "Sufficient permissions pass validation",
			topPermissionsYAML:  "permissions:\n  contents: write",
			importedPermissions: `{"contents":"read"}`,
			expectError:         false,
		},
		{
			name:                "Multiple missing permissions fails validation",
			topPermissionsYAML:  "",
			importedPermissions: strings.Join([]string{`{"actions":"read"}`, `{"issues":"write"}`}, "\n"),
			expectError:         true,
			errorContains:       "Missing permissions",
		},
		{
			name:                "All required permissions present passes validation",
			topPermissionsYAML:  "permissions:\n  contents: write\n  issues: read\n  actions: read\n  pull-requests: write",
			importedPermissions: strings.Join([]string{`{"actions":"read"}`, `{"contents":"write"}`, `{"pull-requests":"write"}`}, "\n"),
			expectError:         false,
		},
		{
			name:                "Write satisfies read requirement",
			topPermissionsYAML:  "permissions:\n  actions: write",
			importedPermissions: `{"actions":"read"}`,
			expectError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			err := compiler.ValidateIncludedPermissions(tt.topPermissionsYAML, tt.importedPermissions)

			if tt.expectError {
				if err == nil {
					t.Errorf("ValidateIncludedPermissions() expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("ValidateIncludedPermissions() error should contain '%s', got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateIncludedPermissions() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestPermissionsImportIntegration(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")
	sharedDir := filepath.Join(tempDir, ".github", "workflows", "shared")
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	// Create a shared workflow file with permissions
	sharedWorkflowContent := `---
permissions:
  actions: read
---

# Shared workflow with permissions
`
	sharedWorkflowPath := filepath.Join(sharedDir, "shared-permissions.md")
	if err := os.WriteFile(sharedWorkflowPath, []byte(sharedWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to create shared workflow file: %v", err)
	}

	// Test 1: Workflow with all required permissions validates successfully
	t.Run("Workflow with all required permissions validates", func(t *testing.T) {
		mainWorkflowContent := `---
on: issues
engine: copilot
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
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
		if err != nil {
			t.Fatalf("Expected compilation to succeed but got error: %v", err)
		}

		// Read the generated lock file to verify permissions are from main workflow
		lockFilePath := filepath.Join(tempDir, ".github", "workflows", "test-workflow.lock.yml")
		lockContent, err := os.ReadFile(lockFilePath)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockStr := string(lockContent)
		// Check that permissions are included in the lock file
		if !strings.Contains(lockStr, "actions: read") {
			t.Errorf("Expected lock file to contain 'actions: read', got: %s", lockStr)
		}
		if !strings.Contains(lockStr, "issues: read") {
			t.Errorf("Expected lock file to contain 'issues: read', got: %s", lockStr)
		}
		if !strings.Contains(lockStr, "contents: read") {
			t.Errorf("Expected lock file to contain 'contents: read', got: %s", lockStr)
		}
	})

	// Test 2: Missing permission from import fails validation
	t.Run("Missing permission from import fails validation", func(t *testing.T) {
		mainWorkflowContent := `---
on: issues
engine: copilot
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
imports:
  - shared/shared-permissions.md
tools:
  github:
    toolsets: [default]
---

# Main workflow missing actions: read
`
		mainWorkflowPath := filepath.Join(tempDir, ".github", "workflows", "test-missing.md")
		if err := os.WriteFile(mainWorkflowPath, []byte(mainWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create main workflow file: %v", err)
		}

		// Compile the workflow - should fail
		compiler := NewCompiler()
		err := compiler.CompileWorkflow(mainWorkflowPath)
		if err == nil {
			t.Fatalf("Expected compilation to fail due to missing permissions")
		}

		// Check error message contains relevant info
		if !strings.Contains(err.Error(), "Missing permissions") {
			t.Errorf("Expected error to mention 'Missing permissions', got: %v", err)
		}
		if !strings.Contains(err.Error(), "actions") {
			t.Errorf("Expected error to mention 'actions' permission, got: %v", err)
		}
	})

	// Test 3: Insufficient permission level fails validation
	t.Run("Insufficient permission level fails validation", func(t *testing.T) {
		sharedWorkflowUpgradeContent := `---
permissions:
  contents: write
  issues: read
  pull-requests: read
---

# Shared workflow with write permission
`
		sharedWorkflowUpgradePath := filepath.Join(sharedDir, "shared-upgrade.md")
		if err := os.WriteFile(sharedWorkflowUpgradePath, []byte(sharedWorkflowUpgradeContent), 0644); err != nil {
			t.Fatalf("Failed to create shared upgrade workflow file: %v", err)
		}

		mainWorkflowContent := `---
on: issues
engine: copilot
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
imports:
  - shared/shared-upgrade.md
tools:
  github:
    toolsets: [default]
---

# Main workflow with insufficient permission
`
		mainWorkflowPath := filepath.Join(tempDir, ".github", "workflows", "test-insufficient.md")
		if err := os.WriteFile(mainWorkflowPath, []byte(mainWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create main workflow file: %v", err)
		}

		// Compile the workflow - should fail
		compiler := NewCompiler()
		err := compiler.CompileWorkflow(mainWorkflowPath)
		if err == nil {
			t.Fatalf("Expected compilation to fail due to insufficient permissions")
		}

		// Check error message
		if !strings.Contains(err.Error(), "Insufficient permissions") {
			t.Errorf("Expected error to mention 'Insufficient permissions', got: %v", err)
		}
	})

	// Test 4: Write satisfies read requirement
	t.Run("Write satisfies read requirement", func(t *testing.T) {
		mainWorkflowContent := `---
on: issues
engine: copilot
strict: false
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: write
features:
  dangerous-permissions-write: true
imports:
  - shared/shared-permissions.md
tools:
  github:
    toolsets: [default]
---

# Main workflow with write satisfying read requirement
`
		mainWorkflowPath := filepath.Join(tempDir, ".github", "workflows", "test-write-satisfies-read.md")
		if err := os.WriteFile(mainWorkflowPath, []byte(mainWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create main workflow file: %v", err)
		}

		// Compile the workflow - should succeed
		compiler := NewCompiler()
		err := compiler.CompileWorkflow(mainWorkflowPath)
		if err != nil {
			t.Fatalf("Expected compilation to succeed but got error: %v", err)
		}
	})
}

func TestExtractPermissionsFromContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
		wantErr  bool
	}{
		{
			name: "Simple permissions",
			content: `---
on: push
permissions:
  contents: read
  issues: write
  pull-requests: read
---
# Content`,
			expected: `{"contents":"read","issues":"write","pull-requests":"read"}`,
			wantErr:  false,
		},
		{
			name: "No permissions",
			content: `---
on: issues
---
# Content`,
			expected: "{}",
			wantErr:  false,
		},
		{
			name: "Empty frontmatter",
			content: `---
---
# Content`,
			expected: "{}",
			wantErr:  false,
		},
		{
			name:     "No frontmatter",
			content:  "# Just markdown content",
			expected: "{}",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.ExtractPermissionsFromContent(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractPermissionsFromContent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("extractPermissionsFromContent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

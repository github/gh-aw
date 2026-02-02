//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestWorkflowRunBranchValidation tests the validation of workflow_run triggers with and without branch restrictions
func TestWorkflowRunBranchValidation(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-run-validation-test")

	tests := []struct {
		name          string
		frontmatter   string
		filename      string
		strictMode    bool
		expectError   bool
		expectWarning bool
		errorContains string
		warningCount  int
	}{
		{
			name: "workflow_run without branches - normal mode - should warn",
			frontmatter: `---
strict: false
on:
  workflow_run:
    workflows: ["build"]
    types: [completed]
tools:
  github: false
sandbox: false
---

# Workflow Run Without Branches
Test workflow content.`,
			filename:      "workflow-run-no-branches.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: true,
			warningCount:  2, // 1 for workflow_run without branches + 1 for sandbox: false
		},
		{
			name: "workflow_run without branches - strict mode - should error",
			frontmatter: `---
on:
  workflow_run:
    workflows: ["build"]
    types: [completed]
tools:
  github:
    toolsets: [repos]
---

# Workflow Run Without Branches Strict
Test workflow content.`,
			filename:      "workflow-run-no-branches-strict.md",
			strictMode:    true,
			expectError:   true,
			expectWarning: false,
			errorContains: "workflow_run trigger should include branch restrictions",
		},
		{
			name: "workflow_run with branches - should pass",
			frontmatter: `---
strict: false
on:
  workflow_run:
    workflows: ["build"]
    types: [completed]
    branches:
      - main
      - develop
tools:
  github: false
sandbox: false
---

# Workflow Run With Branches
Test workflow content.`,
			filename:      "workflow-run-with-branches.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  1, // 1 for sandbox: false
		},
		{
			name: "workflow_run with branches - strict mode - should pass",
			frontmatter: `---
on:
  workflow_run:
    workflows: ["build"]
    types: [completed]
    branches:
      - main
tools:
  github:
    toolsets: [repos]
---

# Workflow Run With Branches Strict
Test workflow content.`,
			filename:      "workflow-run-with-branches-strict.md",
			strictMode:    true,
			expectError:   false,
			expectWarning: false,
			warningCount:  0,
		},
		{
			name: "no workflow_run - should pass",
			frontmatter: `---
strict: false
on:
  push:
    branches: [main]
tools:
  github: false
sandbox: false
---

# Push Workflow
Test workflow content.`,
			filename:      "push-workflow.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  1, // 1 for sandbox: false
		},
		{
			name: "mixed triggers with workflow_run without branches - should warn/error",
			frontmatter: `---
strict: false
on:
  push:
    branches: [main]
  workflow_run:
    workflows: ["build"]
    types: [completed]
tools:
  github: false
sandbox: false
---

# Mixed Triggers
Test workflow content.`,
			filename:      "mixed-triggers.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: true,
			warningCount:  2, // 1 for workflow_run without branches + 1 for sandbox: false
		},
		{
			name: "workflow_run with empty branches array - should warn/error",
			frontmatter: `---
strict: false
on:
  workflow_run:
    workflows: ["build"]
    types: [completed]
    branches: []
tools:
  github: false
sandbox: false
---

# Workflow Run With Empty Branches
Test workflow content.`,
			filename:      "workflow-run-empty-branches.md",
			strictMode:    false,
			expectError:   false,
			expectWarning: false,
			warningCount:  1, // 1 for sandbox: false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the markdown file
			mdFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(mdFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatal(err)
			}

			// Create compiler with appropriate mode
			compiler := NewCompiler()
			compiler.SetStrictMode(tt.strictMode)
			compiler.SetNoEmit(true) // Don't write lock files for these tests

			// Compile the workflow
			err := compiler.CompileWorkflow(mdFile)

			// Check error expectations
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q but got: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			// Check warning count
			if compiler.GetWarningCount() != tt.warningCount {
				t.Errorf("Expected %d warnings but got %d", tt.warningCount, compiler.GetWarningCount())
			}
		})
	}
}

// TestWorkflowRunBranchValidationEdgeCases tests edge cases for workflow_run validation
func TestWorkflowRunBranchValidationEdgeCases(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-run-validation-edge-test")

	tests := []struct {
		name         string
		frontmatter  string
		filename     string
		expectError  bool
		warningCount int
	}{
		{
			name: "on field empty - should not error",
			frontmatter: `---
strict: false
on: push
tools:
  github: false
sandbox: false
---

# No On Field
Test workflow content.`,
			filename:     "no-on-field.md",
			expectError:  false,
			warningCount: 1, // 1 for sandbox: false
		},
		{
			name: "multiple workflow_run configs - first without branches - should warn",
			frontmatter: `---
strict: false
on:
  workflow_run:
    workflows: ["build", "test"]
    types: [completed]
tools:
  github: false
sandbox: false
---

# Multiple Workflows
Test workflow content.`,
			filename:     "multiple-workflows.md",
			expectError:  false,
			warningCount: 2, // 1 for workflow_run without branches + 1 for sandbox: false
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the markdown file
			mdFile := filepath.Join(tmpDir, tt.filename)
			if err := os.WriteFile(mdFile, []byte(tt.frontmatter), 0644); err != nil {
				t.Fatal(err)
			}

			// Create compiler in normal mode
			compiler := NewCompiler()
			compiler.SetNoEmit(true)

			// Compile the workflow
			err := compiler.CompileWorkflow(mdFile)

			// Check error expectations
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}

			// Check warning count
			if compiler.GetWarningCount() != tt.warningCount {
				t.Errorf("Expected %d warnings but got %d", tt.warningCount, compiler.GetWarningCount())
			}
		})
	}
}

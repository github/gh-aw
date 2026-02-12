//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrencyGroupValidationIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "concurrency-validation-integration")
	compiler := NewCompiler()

	tests := []struct {
		name        string
		frontmatter string
		content     string
		expectError bool
		errorSubstr string // Expected substring in error message
		description string
	}{
		// Valid workflow-level concurrency
		{
			name: "valid workflow-level concurrency string",
			frontmatter: `---
on: push
concurrency: "my-workflow-${{ github.ref }}"
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with valid workflow-level concurrency.",
			expectError: false,
			description: "Valid workflow-level concurrency string should compile",
		},
		{
			name: "valid workflow-level concurrency object",
			frontmatter: `---
on: pull_request
concurrency:
  group: "pr-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with valid workflow-level concurrency object.",
			expectError: false,
			description: "Valid workflow-level concurrency object should compile",
		},

		// Valid engine-level concurrency
		{
			name: "valid engine-level concurrency string",
			frontmatter: `---
on: push
engine:
  id: copilot
  concurrency: "copilot-${{ github.workflow }}"
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with valid engine-level concurrency string.",
			expectError: false,
			description: "Valid engine-level concurrency string should compile",
		},
		{
			name: "valid engine-level concurrency object",
			frontmatter: `---
on: push
engine:
  id: copilot
  concurrency:
    group: "copilot-${{ github.workflow }}-${{ github.ref }}"
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with valid engine-level concurrency object.",
			expectError: false,
			description: "Valid engine-level concurrency object should compile",
		},

		// Invalid workflow-level concurrency
		{
			name: "invalid workflow-level concurrency - unclosed braces",
			frontmatter: `---
on: push
concurrency: workflow-${{ github.ref
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with invalid workflow-level concurrency.",
			expectError: true,
			errorSubstr: "unclosed expression braces",
			description: "Unclosed braces in workflow-level concurrency should fail",
		},
		{
			name: "invalid workflow-level concurrency - empty expression",
			frontmatter: `---
on: push
concurrency: workflow-${{}}
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with empty expression in workflow-level concurrency.",
			expectError: true,
			errorSubstr: "empty expression content",
			description: "Empty expression in workflow-level concurrency should fail",
		},
		{
			name: "invalid workflow-level concurrency - unbalanced parentheses",
			frontmatter: `---
on: push
concurrency:
  group: workflow-${{ (github.workflow }}
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with unbalanced parentheses.",
			expectError: true,
			errorSubstr: "unclosed parentheses",
			description: "Unbalanced parentheses in workflow-level concurrency should fail",
		},

		// Invalid engine-level concurrency
		{
			name: "invalid engine-level concurrency - unclosed braces",
			frontmatter: `---
on: push
engine:
  id: copilot
  concurrency: copilot-${{ github.workflow
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with invalid engine-level concurrency.",
			expectError: true,
			errorSubstr: "unclosed expression braces",
			description: "Unclosed braces in engine-level concurrency should fail",
		},
		{
			name: "invalid engine-level concurrency - malformed operators",
			frontmatter: `---
on: push
engine:
  id: copilot
  concurrency:
    group: copilot-${{ github.workflow && && github.ref }}
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with malformed operators in engine-level concurrency.",
			expectError: true,
			errorSubstr: "invalid expression syntax",
			description: "Malformed operators in engine-level concurrency should fail",
		},

		// Both levels with mixed validity
		{
			name: "valid workflow-level, invalid engine-level",
			frontmatter: `---
on: push
concurrency: workflow-${{ github.ref }}
engine:
  id: copilot
  concurrency: copilot-${{ github.workflow
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with valid workflow-level but invalid engine-level concurrency.",
			expectError: true,
			errorSubstr: "engine.concurrency validation failed",
			description: "Should fail on invalid engine-level concurrency even if workflow-level is valid",
		},
		{
			name: "invalid workflow-level, valid engine-level",
			frontmatter: `---
on: push
concurrency: workflow-${{ github.ref
engine:
  id: copilot
  concurrency: copilot-${{ github.workflow }}
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with invalid workflow-level but valid engine-level concurrency.",
			expectError: true,
			errorSubstr: "workflow-level concurrency validation failed",
			description: "Should fail on invalid workflow-level concurrency even if engine-level is valid",
		},

		// Edge cases
		{
			name: "complex valid expression",
			frontmatter: `---
on: push
concurrency:
  group: workflow-${{ (github.workflow || github.ref) && github.repository }}
tools:
  github:
    allowed: [list_issues]
---`,
			content:     "Test workflow with complex valid expression.",
			expectError: false,
			description: "Complex valid expression should compile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create markdown file
			markdown := tt.frontmatter + "\n" + tt.content
			mdPath := filepath.Join(tmpDir, tt.name+".md")
			err := os.WriteFile(mdPath, []byte(markdown), 0644)
			require.NoError(t, err, "Failed to write test markdown file")

			// Try to compile
			err = compiler.CompileWorkflow(mdPath)

			if tt.expectError {
				assert.Error(t, err, "Expected error for: %s", tt.description)
				if tt.errorSubstr != "" {
					assert.Contains(t, err.Error(), tt.errorSubstr,
						"Error should contain expected substring for: %s", tt.description)
				}
			} else {
				assert.NoError(t, err, "Expected no error for: %s. Got: %v", tt.description, err)
			}

			// Clean up lock file if created
			lockPath := mdPath[:len(mdPath)-3] + ".lock.yml"
			_ = os.Remove(lockPath)
		})
	}
}

// TestConcurrencyGroupValidationWithRealWorkflows tests validation with real workflow patterns
func TestConcurrencyGroupValidationWithRealWorkflows(t *testing.T) {
	tmpDir := testutil.TempDir(t, "concurrency-validation-real-workflows")
	compiler := NewCompiler()

	// Test with real concurrency patterns used in the codebase
	realPatterns := []struct {
		name        string
		frontmatter string
		description string
	}{
		{
			name: "pr-workflow-pattern",
			frontmatter: `---
on:
  pull_request:
    types: [opened, synchronize]
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true
tools:
  github:
    allowed: [list_issues]
---`,
			description: "Real PR workflow concurrency pattern",
		},
		{
			name: "issue-workflow-pattern",
			frontmatter: `---
on:
  issues:
    types: [opened]
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}"
tools:
  github:
    allowed: [list_issues]
---`,
			description: "Real issue workflow concurrency pattern",
		},
		{
			name: "command-workflow-pattern",
			frontmatter: `---
on:
  command:
    name: test-bot
concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number }}"
tools:
  github:
    allowed: [list_issues]
---`,
			description: "Real command workflow concurrency pattern",
		},
		{
			name: "engine-level-pattern",
			frontmatter: `---
on:
  workflow_dispatch:
engine:
  id: copilot
  concurrency:
    group: "gh-aw-copilot-${{ github.workflow }}"
tools:
  github:
    allowed: [list_issues]
---`,
			description: "Real engine-level concurrency pattern",
		},
	}

	for _, pattern := range realPatterns {
		t.Run(pattern.name, func(t *testing.T) {
			// Create markdown file
			markdown := pattern.frontmatter + "\nTest workflow content."
			mdPath := filepath.Join(tmpDir, pattern.name+".md")
			err := os.WriteFile(mdPath, []byte(markdown), 0644)
			require.NoError(t, err, "Failed to write test markdown file")

			// Should compile successfully
			err = compiler.CompileWorkflow(mdPath)
			assert.NoError(t, err, "Real workflow pattern should compile: %s. Got: %v", pattern.description, err)

			// Clean up
			lockPath := mdPath[:len(mdPath)-3] + ".lock.yml"
			_ = os.Remove(lockPath)
		})
	}
}

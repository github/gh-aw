//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompilerRejectsIncludesInTemplateRegions(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-*")

	tests := []struct {
		name        string
		content     string
		shouldError bool
		errContains string
	}{
		{
			name: "valid workflow with include outside template",
			content: `---
on: issues
permissions:
  issues: write
strict: false
features:
  dangerous-permissions-write: true
---

# Valid Workflow

@include? shared/tools.md

{{#if github.event.issue.number}}
This is valid.
{{/if}}`,
			shouldError: false,
		},
		{
			name: "invalid workflow with include inside template",
			content: `---
on: issues
permissions:
  issues: write
strict: false
features:
  dangerous-permissions-write: true
---

# Invalid Workflow

{{#if github.event.issue.number}}
@include shared/tools.md
This should fail.
{{/if}}`,
			shouldError: true,
			errContains: "template region validation failed",
		},
		{
			name: "invalid workflow with import inside template",
			content: `---
on: pull_request
permissions:
  pull-requests: write
strict: false
---

# Invalid Workflow with Import

{{#if github.event.pull_request.number}}
@import shared/config.md
{{/if}}`,
			shouldError: true,
			errContains: "import directives cannot be used inside template regions",
		},
		{
			name: "valid workflow with multiple templates and includes between them",
			content: `---
on: issues
permissions:
  issues: write
strict: false
---

# Valid Complex Workflow

@include? shared/header.md

{{#if github.event.issue.number}}
First template
{{/if}}

@include? shared/middle.md

{{#if github.actor}}
Second template
{{/if}}

@include? shared/footer.md`,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test workflow file
			workflowPath := filepath.Join(tempDir, "test-workflow.md")
			err := os.WriteFile(workflowPath, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test workflow file: %v", err)
			}

			// Try to compile the workflow
			compiler := NewCompiler()
			_, err = compiler.ParseWorkflowFile(workflowPath)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error for %s, but got nil", tt.name)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, but got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for %s: %v", tt.name, err)
				}
			}
		})
	}
}

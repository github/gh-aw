//go:build integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestCompileWorkflow(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-test")

	// Create a test markdown file with basic frontmatter
	testContent := `---
on: push
timeout-minutes: 10
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: copilot
strict: false
features:
  dangerous-permissions-write: true
tools:
  github:
    allowed: [list_issues, create_issue]
  bash: ["echo", "ls"]
---

# Test Workflow

This is a test workflow for compilation.
`

	testFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	tests := []struct {
		name        string
		inputFile   string
		expectError bool
	}{
		{
			name:        "empty input file",
			inputFile:   "",
			expectError: true, // Should error with empty file
		},
		{
			name:        "nonexistent file",
			inputFile:   "/nonexistent/file.md",
			expectError: true, // Should error with nonexistent file
		},
		{
			name:        "valid workflow file",
			inputFile:   testFile,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compiler.CompileWorkflow(tt.inputFile)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for test '%s', got nil", tt.name)
			} else if !tt.expectError && err != nil {
				t.Errorf("Unexpected error for test '%s': %v", tt.name, err)
			}

			// If compilation succeeded, check that lock file was created
			if !tt.expectError && err == nil {
				lockFile := stringutil.MarkdownToLockFile(tt.inputFile)
				if _, statErr := os.Stat(lockFile); os.IsNotExist(statErr) {
					t.Errorf("Expected lock file %s to be created", lockFile)
				}
			}
		})
	}
}

func TestEmptyMarkdownContentError(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "empty-markdown-test")

	compiler := NewCompiler()

	tests := []struct {
		name             string
		content          string
		expectError      bool
		expectedErrorMsg string
		description      string
	}{
		{
			name: "frontmatter_only_no_content",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
tools:
  github:
    allowed: [create_issue_comment]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---`,
			expectError:      true,
			expectedErrorMsg: "no markdown content found",
			description:      "Should error when workflow has only frontmatter with no markdown content",
		},
		{
			name: "frontmatter_with_empty_lines",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
tools:
  github:
    allowed: [create_issue_comment]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---


`,
			expectError:      true,
			expectedErrorMsg: "no markdown content found",
			description:      "Should error when workflow has only frontmatter followed by empty lines",
		},
		{
			name: "frontmatter_with_whitespace_only",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  issues: write
tools:
  github:
    allowed: [create_issue_comment]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---
   	   
`,
			expectError:      true,
			expectedErrorMsg: "no markdown content found",
			description:      "Should error when workflow has only frontmatter followed by whitespace (spaces and tabs)",
		},
		{
			name:             "frontmatter_with_just_newlines",
			content:          "---\non:\n  issues:\n    types: [opened]\npermissions:\n  issues: write\ntools:\n  github:\n    allowed: [add_issue_comment]\nengine: claude\n---\n\n\n\n",
			expectError:      true,
			expectedErrorMsg: "no markdown content found",
			description:      "Should error when workflow has only frontmatter followed by just newlines",
		},
		{
			name: "valid_workflow_with_content",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [create_issue_comment]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

# Test Workflow

This is a valid workflow with actual markdown content.
`,
			expectError:      false,
			expectedErrorMsg: "",
			description:      "Should succeed when workflow has frontmatter and valid markdown content",
		},
		{
			name: "workflow_with_minimal_content",
			content: `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
tools:
  github:
    allowed: [create_issue_comment]
engine: claude
features:
  dangerous-permissions-write: true
strict: false
---

Brief content`,
			expectError:      false,
			expectedErrorMsg: "",
			description:      "Should succeed when workflow has frontmatter and minimal but valid markdown content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			err := compiler.CompileWorkflow(testFile)

			if tt.expectError {
				if err == nil {
					t.Errorf("%s: Expected error but compilation succeeded", tt.description)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedErrorMsg) {
					t.Errorf("%s: Expected error containing '%s', got: %s", tt.description, tt.expectedErrorMsg, err.Error())
				}
				// Verify error contains file:line:column format for better IDE integration
				// The error should contain the filename (relative or absolute) with :line:column:
				expectedPattern := fmt.Sprintf("%s:1:1:", tt.name+".md")
				if !strings.Contains(err.Error(), expectedPattern) {
					t.Errorf("%s: Error should contain '%s' for IDE integration, got: %s", tt.description, expectedPattern, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("%s: Unexpected error: %v", tt.description, err)
					return
				}
				// Verify lock file was created
				lockFile := stringutil.MarkdownToLockFile(testFile)
				if _, statErr := os.Stat(lockFile); os.IsNotExist(statErr) {
					t.Errorf("%s: Expected lock file %s to be created", tt.description, lockFile)
				}
			}
		})
	}
}

func TestWorkflowDataStructure(t *testing.T) {
	// Test the WorkflowData structure
	data := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test Content",
	}

	if data.Name != "Test Workflow" {
		t.Errorf("Expected Name 'Test Workflow', got '%s'", data.Name)
	}

	if data.MarkdownContent != "# Test Content" {
		t.Errorf("Expected MarkdownContent '# Test Content', got '%s'", data.MarkdownContent)
	}

}

func TestWorkflowNameWithColon(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "workflow-test")

	// Create a test markdown file with a header containing a colon
	testContent := `---
on: push
timeout-minutes: 10
permissions:
  contents: read
  issues: read
  pull-requests: read
tools:
  github:
    allowed: [list_issues]
---

# Playground: Everything Echo Test

This is a test workflow with a colon in the header.
`

	testFile := filepath.Join(tmpDir, "test-colon-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Test compilation
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	// Verify the workflow name is properly quoted
	lockContentStr := string(lockContent)
	if !strings.Contains(lockContentStr, `name: "Playground: Everything Echo Test"`) {
		t.Errorf("Expected quoted workflow name 'name: \"Playground: Everything Echo Test\"' not found in lock file. Content:\n%s", lockContentStr)
	}

	// Verify it doesn't contain the unquoted version which would be invalid YAML
	if strings.Contains(lockContentStr, "name: Playground: Everything Echo Test\n") {
		t.Errorf("Found unquoted workflow name which would be invalid YAML. Content:\n%s", lockContentStr)
	}
}

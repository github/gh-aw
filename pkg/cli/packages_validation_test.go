//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsValidWorkflowFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "workflow-validation-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name: "valid workflow with frontmatter",
			content: `---
on: push
permissions:
  contents: read
---

# My Workflow

This is a valid workflow.
`,
			expected: true,
		},
		{
			name: "valid workflow with minimal frontmatter",
			content: `---
on: issues
---

# Issue Handler
`,
			expected: true,
		},
		{
			name: "markdown without frontmatter",
			content: `# README

This is just a regular markdown file without frontmatter.
`,
			expected: false, // No frontmatter = not a workflow
		},
		{
			name: "markdown with unclosed frontmatter",
			content: `---
on: push
permissions:
  contents: read

# My Workflow

Missing closing ---
`,
			expected: false,
		},
		{
			name: "markdown with invalid YAML",
			content: `---
on: push
permissions:
  contents: read
  invalid yaml: {{{
---

# My Workflow
`,
			expected: false,
		},
		{
			name:     "empty file",
			content:  ``,
			expected: false, // Empty content has no "on" field
		},
		{
			name: "file with only frontmatter",
			content: `---
on: push
---
`,
			expected: true, // Has "on" field = valid workflow
		},
		{
			name: "frontmatter without on field",
			content: `---
title: My Document
author: Someone
---

# My Document
`,
			expected: false, // No "on" field = not a workflow
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tmpDir, tt.name+".md")
			err := os.WriteFile(testFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Test the function
			result := isValidWorkflowFile(testFile)
			if result != tt.expected {
				t.Errorf("isValidWorkflowFile() = %v, want %v", result, tt.expected)
			}
		})
	}

	// Test with non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		result := isValidWorkflowFile("/nonexistent/path/file.md")
		if result != false {
			t.Errorf("isValidWorkflowFile() for non-existent file = %v, want false", result)
		}
	})
}

func TestIsValidWorkflowFileFilteringBehavior(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "workflow-filtering-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files that should be filtered out
	testCases := []struct {
		filename      string
		content       string
		shouldBeValid bool
	}{
		{
			filename: "README.md",
			content: `# README

This project contains agentic workflows.
`,
			shouldBeValid: false, // No frontmatter = not a workflow
		},
		{
			filename: "CODE_OF_CONDUCT.md",
			content: `# Code of Conduct

Be nice to each other.
`,
			shouldBeValid: false, // No frontmatter = not a workflow
		},
		{
			filename: "SUPPORT.md",
			content: `# Support

Contact us for support.
`,
			shouldBeValid: false, // No frontmatter = not a workflow
		},
		{
			filename: "valid-workflow.md",
			content: `---
on: issues
permissions:
  issues: write
---

# Issue Triage

This is a valid workflow.
`,
			shouldBeValid: true,
		},
		{
			filename: "malformed.md",
			content: `---
on: push
{ invalid yaml
---

# Bad Workflow
`,
			shouldBeValid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tc.filename)
			err := os.WriteFile(testFile, []byte(tc.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			result := isValidWorkflowFile(testFile)
			if result != tc.shouldBeValid {
				t.Errorf("isValidWorkflowFile(%s) = %v, want %v", tc.filename, result, tc.shouldBeValid)
			}
		})
	}
}

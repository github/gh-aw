//go:build !integration

package cli

import (
	"os"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestExtractWorkflowDescription tests the ExtractWorkflowDescription function
func TestExtractWorkflowDescription(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "workflow with description",
			content: `---
name: Test Workflow
description: This is a test workflow description
on: push
---

# Test Workflow

This is the workflow content.`,
			expected: "This is a test workflow description",
		},
		{
			name: "workflow without description",
			content: `---
name: Test Workflow
on: push
---

# Test Workflow

This is the workflow content.`,
			expected: "",
		},
		{
			name: "workflow with empty description",
			content: `---
name: Test Workflow
description: ""
on: push
---

# Test Workflow

This is the workflow content.`,
			expected: "",
		},
		{
			name: "workflow with multi-line description",
			content: `---
name: Test Workflow
description: |
  This is a multi-line
  test workflow description
on: push
---

# Test Workflow

This is the workflow content.`,
			expected: "This is a multi-line\ntest workflow description\n",
		},
		{
			name:     "workflow without frontmatter",
			content:  "# Test Workflow\n\nThis is the workflow content.",
			expected: "",
		},
		{
			name: "workflow with non-string description",
			content: `---
name: Test Workflow
description: 123
on: push
---

# Test Workflow`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractWorkflowDescription(tt.content)
			if result != tt.expected {
				t.Errorf("ExtractWorkflowDescription() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractWorkflowDescriptionFromFile tests the ExtractWorkflowDescriptionFromFile function
func TestExtractWorkflowDescriptionFromFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name: "file with description",
			content: `---
name: Test Workflow
description: This is a test workflow description from file
on: push
---

# Test Workflow

This is the workflow content.`,
			expected: "This is a test workflow description from file",
		},
		{
			name: "file without description",
			content: `---
name: Test Workflow
on: push
---

# Test Workflow`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpFile := testutil.TempDir(t, "test-*") + "/test-workflow.md"
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}

			result := ExtractWorkflowDescriptionFromFile(tmpFile)
			if result != tt.expected {
				t.Errorf("ExtractWorkflowDescriptionFromFile() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestExtractWorkflowDescriptionFromFile_NonExistentFile tests handling of non-existent files
func TestExtractWorkflowDescriptionFromFile_NonExistentFile(t *testing.T) {
	result := ExtractWorkflowDescriptionFromFile("/path/that/does/not/exist.md")
	if result != "" {
		t.Errorf("ExtractWorkflowDescriptionFromFile() with non-existent file = %q, want empty string", result)
	}
}

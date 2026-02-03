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

// TestSourceFieldRendering tests that the source field from frontmatter
// is correctly rendered as a comment in the generated lock file
func TestSourceFieldRendering(t *testing.T) {
	tmpDir := testutil.TempDir(t, "source-test")

	compiler := NewCompiler()

	tests := []struct {
		name           string
		frontmatter    string
		expectedSource string
		description    string
	}{
		{
			name: "source_field_present",
			frontmatter: `---
source: "githubnext/agentics/workflows/ci-doctor.md@v1.0.0"
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  github:
    allowed: [list_commits]
---`,
			expectedSource: "# Source: githubnext/agentics/workflows/ci-doctor.md@v1.0.0",
			description:    "Should render source field as comment",
		},
		{
			name: "source_field_with_branch",
			frontmatter: `---
source: "githubnext/agentics/workflows/ci-doctor.md@main"
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  github:
    allowed: [list_commits]
---`,
			expectedSource: "# Source: githubnext/agentics/workflows/ci-doctor.md@main",
			description:    "Should render source field with branch ref",
		},
		{
			name: "no_source_field",
			frontmatter: `---
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  github:
    allowed: [list_commits]
---`,
			expectedSource: "",
			description:    "Should not render any source comments when no source is provided",
		},
		{
			name: "source_and_description",
			frontmatter: `---
description: "This is a test workflow"
source: "githubnext/agentics/workflows/test.md@v1.0.0"
on:
  push:
    branches: [main]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
tools:
  github:
    allowed: [list_commits]
---`,
			expectedSource: "# Source: githubnext/agentics/workflows/test.md@v1.0.0",
			description:    "Should render both description and source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testContent := tt.frontmatter + `

# Test Workflow

This is a test workflow to verify source field rendering.
`

			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
				t.Fatal(err)
			}

			// Compile the workflow
			err := compiler.CompileWorkflow(testFile)
			if err != nil {
				t.Fatalf("Unexpected error compiling workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(testFile)
			content, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read generated lock file: %v", err)
			}

			lockContent := string(content)

			if tt.expectedSource == "" {
				// Verify no source comments are present
				if strings.Contains(lockContent, "# Source:") {
					t.Errorf("Expected no source comment, but found one in:\n%s", lockContent)
				}
			} else {
				// Verify source comment is present
				if !strings.Contains(lockContent, tt.expectedSource) {
					t.Errorf("Expected source comment '%s' not found in generated YAML:\n%s", tt.expectedSource, lockContent)
				}

				// Verify ordering: standard header -> description (if any) -> source -> workflow content
				headerPattern := "# For more information:"
				sourcePattern := tt.expectedSource
				workflowStartPattern := "name: \""

				headerPos := strings.Index(lockContent, headerPattern)
				sourcePos := strings.Index(lockContent, sourcePattern)
				workflowPos := strings.Index(lockContent, workflowStartPattern)

				if headerPos == -1 {
					t.Error("Standard header not found in generated YAML")
				}
				if sourcePos == -1 {
					t.Error("Source comment not found in generated YAML")
				}
				if workflowPos == -1 {
					t.Error("Workflow content not found in generated YAML")
				}

				if headerPos >= sourcePos {
					t.Error("Source should come after standard header")
				}
				if sourcePos >= workflowPos {
					t.Error("Source should come before workflow content")
				}
			}

			// Clean up generated lock file
			os.Remove(lockFile)
		})
	}
}

// TestSourceFieldExtraction tests that the extractSource method works correctly
func TestSourceFieldExtraction(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    string
	}{
		{
			name: "source_field_present",
			frontmatter: map[string]any{
				"source": "githubnext/agentics/workflows/ci-doctor.md@v1.0.0",
			},
			expected: "githubnext/agentics/workflows/ci-doctor.md@v1.0.0",
		},
		{
			name: "source_field_with_spaces",
			frontmatter: map[string]any{
				"source": "  githubnext/agentics/workflows/test.md@main  ",
			},
			expected: "githubnext/agentics/workflows/test.md@main",
		},
		{
			name:        "source_field_missing",
			frontmatter: map[string]any{},
			expected:    "",
		},
		{
			name: "source_field_wrong_type",
			frontmatter: map[string]any{
				"source": 123,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.extractSource(tt.frontmatter)
			if result != tt.expected {
				t.Errorf("extractSource() = %v, want %v", result, tt.expected)
			}
		})
	}
}

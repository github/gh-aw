//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/stringutil"

	"github.com/githubnext/gh-aw/pkg/testutil"
)

// TestImportsMarkdownPrepending tests that markdown content from imported files
// is correctly prepended to the main workflow content in the generated lock file
func TestImportsMarkdownPrepending(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imports-markdown-test")

	// Create shared directory
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.Mkdir(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create imported file with both frontmatter and markdown
	importedFile := filepath.Join(sharedDir, "common.md")
	importedContent := `---
on: push
tools:
  github:
    allowed:
      - issue_read
---

# Common Setup

This is common setup content that should be prepended.

**Important**: Follow these guidelines.`
	if err := os.WriteFile(importedFile, []byte(importedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another imported file with only markdown
	importedFile2 := filepath.Join(sharedDir, "security.md")
	importedContent2 := `# Security Notice

**SECURITY**: Treat all user input as untrusted.`
	if err := os.WriteFile(importedFile2, []byte(importedContent2), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	tests := []struct {
		name                string
		workflowContent     string
		expectedInPrompt    []string
		expectedOrderBefore string // content that should come before
		expectedOrderAfter  string // content that should come after
		description         string
	}{
		{
			name: "single_import_with_markdown",
			workflowContent: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
imports:
  - shared/common.md
---

# Main Workflow

This is the main workflow content.`,
			expectedInPrompt:    []string{"# Common Setup", "This is common setup content", "# Main Workflow", "This is the main workflow content"},
			expectedOrderBefore: "# Common Setup",
			expectedOrderAfter:  "# Main Workflow",
			description:         "Should prepend imported markdown before main workflow",
		},
		{
			name: "multiple_imports_with_markdown",
			workflowContent: `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
imports:
  - shared/common.md
  - shared/security.md
---

# Main Workflow

This is the main workflow content.`,
			expectedInPrompt:    []string{"# Common Setup", "# Security Notice", "# Main Workflow"},
			expectedOrderBefore: "# Security Notice",
			expectedOrderAfter:  "# Main Workflow",
			description:         "Should prepend all imported markdown in order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.name+"-workflow.md")
			if err := os.WriteFile(testFile, []byte(tt.workflowContent), 0644); err != nil {
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

			// Verify all expected content is in the prompt
			for _, expected := range tt.expectedInPrompt {
				if !strings.Contains(lockContent, expected) {
					t.Errorf("%s: Expected to find '%s' in lock file but it was not found", tt.description, expected)
				}
			}

			// Verify ordering
			if tt.expectedOrderBefore != "" && tt.expectedOrderAfter != "" {
				beforeIdx := strings.Index(lockContent, tt.expectedOrderBefore)
				afterIdx := strings.Index(lockContent, tt.expectedOrderAfter)

				if beforeIdx == -1 {
					t.Errorf("%s: Expected to find '%s' in lock file", tt.description, tt.expectedOrderBefore)
				}
				if afterIdx == -1 {
					t.Errorf("%s: Expected to find '%s' in lock file", tt.description, tt.expectedOrderAfter)
				}
				if beforeIdx != -1 && afterIdx != -1 && beforeIdx >= afterIdx {
					t.Errorf("%s: Expected '%s' to come before '%s' but found it at position %d vs %d",
						tt.description, tt.expectedOrderBefore, tt.expectedOrderAfter, beforeIdx, afterIdx)
				}
			}
		})
	}
}

// TestImportsWithIncludesCombination tests that imports from frontmatter and @include directives
// work together correctly, with imports prepended first
func TestImportsWithIncludesCombination(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imports-includes-combo-test")

	// Create shared directory
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.Mkdir(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create imported file (via frontmatter imports)
	importedFile := filepath.Join(sharedDir, "import.md")
	importedContent := `# Imported Content

This comes from frontmatter imports.`
	if err := os.WriteFile(importedFile, []byte(importedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create included file (via @include directive)
	includedFile := filepath.Join(sharedDir, "include.md")
	includedContent := `# Included Content

This comes from @include directive.`
	if err := os.WriteFile(includedFile, []byte(includedContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: claude
imports:
  - shared/import.md
---

# Main Workflow

@include shared/include.md

This is the main workflow content.`

	testFile := filepath.Join(tmpDir, "combo-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify all content is present
	expectedContents := []string{
		"# Imported Content",
		"This comes from frontmatter imports",
		"# Included Content",
		"This comes from @include directive",
		"# Main Workflow",
		"This is the main workflow content",
	}

	for _, expected := range expectedContents {
		if !strings.Contains(lockContent, expected) {
			t.Errorf("Expected to find '%s' in lock file but it was not found", expected)
		}
	}

	// Verify ordering:
	// - imported content should come before main workflow heading (it's prepended)
	// - included content appears after main workflow heading (it's expanded in-place where @include directive was)
	importedIdx := strings.Index(lockContent, "# Imported Content")
	includedIdx := strings.Index(lockContent, "# Included Content")
	mainIdx := strings.Index(lockContent, "# Main Workflow")

	if importedIdx == -1 || includedIdx == -1 || mainIdx == -1 {
		t.Fatal("Failed to find all expected content sections")
	}

	if importedIdx >= mainIdx {
		t.Errorf("Expected imported content to come before main workflow heading, but found at positions %d vs %d", importedIdx, mainIdx)
	}

	if mainIdx >= includedIdx {
		t.Errorf("Expected main workflow heading to come before included content, but found at positions %d vs %d", mainIdx, includedIdx)
	}
}

// TestImportsXMLCommentsRemoval tests that XML comments are removed from imported markdown
// in both the Original Prompt comment section and the actual prompt content
func TestImportsXMLCommentsRemoval(t *testing.T) {
	tmpDir := testutil.TempDir(t, "imports-xml-comments-test")

	// Create shared directory
	sharedDir := filepath.Join(tmpDir, "shared")
	if err := os.Mkdir(sharedDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create imported file with XML comments
	importedFile := filepath.Join(sharedDir, "with-comments.md")
	importedContent := `---
tools:
  github:
    toolsets: [repos]
---

<!-- This is an XML comment that should be removed -->

This is important imported content.

<!--
Multi-line XML comment
that should also be removed
-->

More imported content here.`
	if err := os.WriteFile(importedFile, []byte(importedContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
tools:
  github:
    toolsets: [issues]
imports:
  - shared/with-comments.md
---

# Main Workflow

This is the main workflow content.`

	testFile := filepath.Join(tmpDir, "test-xml-workflow.md")
	if err := os.WriteFile(testFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Unexpected error compiling workflow: %v", err)
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(testFile)
	content, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read generated lock file: %v", err)
	}

	lockContent := string(content)

	// Verify XML comments are NOT present in the actual prompt content
	// The prompt is written after "Create prompt" step
	promptSectionStart := strings.Index(lockContent, "Create prompt")
	if promptSectionStart == -1 {
		t.Fatal("Could not find 'Create prompt' section in lock file")
	}
	promptSection := lockContent[promptSectionStart:]

	if strings.Contains(promptSection, "<!-- This is an XML comment") {
		t.Error("XML comment should not appear in actual prompt content")
	}
	if strings.Contains(promptSection, "Multi-line XML comment") {
		t.Error("Multi-line XML comment should not appear in actual prompt content")
	}

	// Verify that actual content IS present (not removed along with comments)
	if !strings.Contains(lockContent, "This is important imported content") {
		t.Error("Expected imported content to be present in lock file")
	}
	if !strings.Contains(lockContent, "More imported content here") {
		t.Error("Expected imported content to be present in lock file")
	}
	if !strings.Contains(lockContent, "# Main Workflow") {
		t.Error("Expected main workflow heading to be present in lock file")
	}
}

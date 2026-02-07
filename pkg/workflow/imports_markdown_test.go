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

			// With the new runtime-import approach:
			// - Both imported content AND main workflow use runtime-import macros
			// - NO content is inlined in the lock file (all loaded at runtime)
			// So we check lock file for runtime-import macros, not inlined content

			// Verify runtime-import macros are present for imported files
			// Check for the first import in the test (all tests have shared/common.md)
			if !strings.Contains(lockContent, "{{#runtime-import shared/common.md}}") {
				t.Errorf("%s: Expected to find runtime-import macro for shared/common.md in lock file", tt.description)
			}

			// For multiple imports test, also check for security.md
			if strings.Contains(tt.name, "multiple_imports") {
				if !strings.Contains(lockContent, "{{#runtime-import shared/security.md}}") {
					t.Errorf("%s: Expected to find runtime-import macro for shared/security.md in lock file", tt.description)
				}
			}

			// Verify runtime-import macro is present for main workflow
			workflowFilename := tt.name + "-workflow.md"
			expectedMainWorkflowMacro := "{{#runtime-import " + workflowFilename + "}}"
			if !strings.Contains(lockContent, expectedMainWorkflowMacro) {
				t.Errorf("%s: Expected to find runtime-import macro '%s' for main workflow in lock file", tt.description, expectedMainWorkflowMacro)
			}

			// Verify ordering: import macros should come before main workflow macro
			if tt.expectedOrderBefore != "" {
				// For runtime imports, we check the order of the runtime-import macros
				// Import macro should come before main workflow macro
				firstImportIdx := strings.Index(lockContent, "{{#runtime-import shared/")
				mainWorkflowMacroIdx := strings.Index(lockContent, expectedMainWorkflowMacro)

				if firstImportIdx == -1 {
					t.Errorf("%s: Expected to find import runtime-import macro in lock file", tt.description)
				}
				if mainWorkflowMacroIdx == -1 {
					t.Errorf("%s: Expected to find main workflow runtime-import macro '%s' in lock file", tt.description, expectedMainWorkflowMacro)
				}
				// Import macros should come before the main workflow macro
				if firstImportIdx != -1 && mainWorkflowMacroIdx != -1 && firstImportIdx >= mainWorkflowMacroIdx {
					t.Errorf("%s: Expected import runtime-import macro to come before main workflow runtime-import macro", tt.description)
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

	// Verify runtime-import macro is present
	if !strings.Contains(lockContent, "{{#runtime-import") {
		t.Error("Lock file should contain runtime-import macro for main workflow")
	}

	// With the new approach:
	// - Imported content (from frontmatter imports) → inlined in lock file
	// - Main workflow content (including @include expansion) → runtime-imported

	// Verify imported content is in lock file (inlined)
	if !strings.Contains(lockContent, "# Imported Content") {
		t.Error("Imported content from frontmatter imports should be inlined in lock file")
	}
	if !strings.Contains(lockContent, "This comes from frontmatter imports") {
		t.Error("Imported markdown content should be inlined in lock file")
	}

	// Note: Main workflow content and @include content are runtime-imported
	// They are NOT in the lock file - only the runtime-import macro is present
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

	// With new approach, main workflow content is runtime-imported (not inlined)
	if !strings.Contains(lockContent, "{{#runtime-import") {
		t.Error("Expected runtime-import macro in lock file")
	}
}

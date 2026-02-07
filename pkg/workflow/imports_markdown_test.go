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

	// With the new runtime-import approach:
	// - Imported content (from frontmatter imports) → uses runtime-import macro
	// - Main workflow content (including @include expansion) → runtime-imported

	// Verify runtime-import macro for imports is in lock file
	if !strings.Contains(lockContent, "{{#runtime-import shared/import.md}}") {
		t.Error("Lock file should contain runtime-import macro for imported file")
	}

	// Verify runtime-import macro for main workflow is in lock file
	if !strings.Contains(lockContent, "{{#runtime-import combo-workflow.md}}") {
		t.Error("Lock file should contain runtime-import macro for main workflow")
	}

	// Note: Neither imported content nor main workflow content are inlined -
	// they are all loaded at runtime via runtime-import macros
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

	// Verify XML comments are NOT present in the lock file
	// (They would only be present if we were inlining content, which we're not doing anymore)
	if strings.Contains(lockContent, "<!-- This is an XML comment") {
		t.Error("XML comment should not appear in lock file")
	}
	if strings.Contains(lockContent, "Multi-line XML comment") {
		t.Error("Multi-line XML comment should not appear in lock file")
	}

	// With runtime-import approach:
	// - Imported content is NOT inlined, so we check for runtime-import macros instead
	// - XML comments will be removed at runtime when the import is processed
	if !strings.Contains(lockContent, "{{#runtime-import shared/with-comments.md}}") {
		t.Error("Expected runtime-import macro for imported file in lock file")
	}

	// Verify runtime-import macro for main workflow is present
	if !strings.Contains(lockContent, "{{#runtime-import test-xml-workflow.md}}") {
		t.Error("Expected runtime-import macro for main workflow in lock file")
	}
}

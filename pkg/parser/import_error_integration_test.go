//go:build integration

package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestImportFileNotFoundError(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-error-*")

	// Create a workflow with a missing import
	workflowPath := filepath.Join(tempDir, "workflow.md")
	workflowContent := `---
on: push
imports:
  - nonexistent.md
---

# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Extract frontmatter
	result, err := parser.ExtractFrontmatterFromContent(workflowContent)
	if err != nil {
		t.Fatalf("Failed to extract frontmatter: %v", err)
	}

	// Try to process imports with source information
	_, err = parser.ProcessImportsFromFrontmatterWithSource(
		result.Frontmatter,
		tempDir,
		nil,
		workflowPath,
		workflowContent,
	)

	// Should get a formatted error
	if err == nil {
		t.Fatal("Expected error for missing import file, got nil")
	}

	errStr := err.Error()

	// Check that error contains source location
	wantContains := []string{
		"workflow.md:4:",        // Line number
		"error:",                // Error type
		"import file not found", // Error message
		"nonexistent.md",        // Import path
	}

	for _, want := range wantContains {
		if !strings.Contains(errStr, want) {
			t.Errorf("Error missing expected string %q\nGot:\n%s", want, errStr)
		}
	}
}

func TestMultipleImportsWithError(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-error-*")

	// Create one valid import file
	validImportPath := filepath.Join(tempDir, "valid.md")
	validImportContent := `---
on: push
tools:
  bash: {}
---
`
	if err := os.WriteFile(validImportPath, []byte(validImportContent), 0644); err != nil {
		t.Fatalf("Failed to write valid import file: %v", err)
	}

	// Create a workflow with one valid and one invalid import
	workflowPath := filepath.Join(tempDir, "workflow.md")
	workflowContent := `---
on: push
imports:
  - valid.md
  - missing.md
---

# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Extract frontmatter
	result, err := parser.ExtractFrontmatterFromContent(workflowContent)
	if err != nil {
		t.Fatalf("Failed to extract frontmatter: %v", err)
	}

	// Try to process imports with source information
	_, err = parser.ProcessImportsFromFrontmatterWithSource(
		result.Frontmatter,
		tempDir,
		nil,
		workflowPath,
		workflowContent,
	)

	// Should get a formatted error for the missing file
	if err == nil {
		t.Fatal("Expected error for missing import file, got nil")
	}

	errStr := err.Error()

	// Check that error points to the correct import
	if !strings.Contains(errStr, "missing.md") {
		t.Errorf("Error should mention 'missing.md', got:\n%s", errStr)
	}

	// Check line number (should be line 5)
	if !strings.Contains(errStr, ":5:") {
		t.Errorf("Error should point to line 5, got:\n%s", errStr)
	}
}

func TestObjectStyleImportWithError(t *testing.T) {
	tempDir := testutil.TempDir(t, "import-error-*")

	// Create a workflow with object-style import
	workflowPath := filepath.Join(tempDir, "workflow.md")
	workflowContent := `---
on: push
imports:
  - path: shared/missing.md
    inputs:
      foo: bar
---

# Test Workflow
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Extract frontmatter
	result, err := parser.ExtractFrontmatterFromContent(workflowContent)
	if err != nil {
		t.Fatalf("Failed to extract frontmatter: %v", err)
	}

	// Try to process imports with source information
	_, err = parser.ProcessImportsFromFrontmatterWithSource(
		result.Frontmatter,
		tempDir,
		nil,
		workflowPath,
		workflowContent,
	)

	// Should get a formatted error
	if err == nil {
		t.Fatal("Expected error for missing import file, got nil")
	}

	errStr := err.Error()

	// Check that error contains the import path
	if !strings.Contains(errStr, "shared/missing.md") {
		t.Errorf("Error should mention 'shared/missing.md', got:\n%s", errStr)
	}

	// Should show error type
	if !strings.Contains(errStr, "error:") {
		t.Errorf("Error should contain 'error:', got:\n%s", errStr)
	}
}

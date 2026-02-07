//go:build !integration

package workflow_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

// TestImportWithInputs tests that imports with inputs correctly substitute values
func TestImportWithInputs(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-import-inputs-*")

	// Create shared workflow file with inputs
	sharedPath := filepath.Join(tempDir, "shared", "data-fetch.md")
	sharedDir := filepath.Dir(sharedPath)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	sharedContent := `---
inputs:
  count:
    description: Number of items to fetch
    type: number
    default: 100
  category:
    description: Category to filter
    type: string
    default: "general"
---

# Data Fetch Instructions

Fetch ${{ github.aw.inputs.count }} items from the ${{ github.aw.inputs.category }} category.
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports shared with inputs
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - path: shared/data-fetch.md
    inputs:
      count: 50
      category: "technology"
---

# Test Workflow

This workflow tests import with inputs.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(lockFileContent)

	// The prompt is assembled from multiple heredocs:
	// - the first heredoc is often just the <system> wrapper and built-in context
	// - user/imported content is appended in later heredocs
	// So validate substitutions against the lock content as a whole.
	if !strings.Contains(lockContent, "Fetch 50 items") {
		t.Errorf("Expected generated workflow to include substituted count value '50' in prompt content")
	}
	if !strings.Contains(lockContent, "technology") {
		t.Errorf("Expected generated workflow to include substituted category value 'technology' in prompt content")
	}

	// Ensure github.aw.inputs.* expressions were substituted during compilation.
	if strings.Contains(lockContent, "github.aw.inputs.count") {
		t.Error("Generated workflow should not contain unsubstituted github.aw.inputs.count expression")
	}
	if strings.Contains(lockContent, "github.aw.inputs.category") {
		t.Error("Generated workflow should not contain unsubstituted github.aw.inputs.category expression")
	}
}

// TestImportWithInputsStringFormat tests that string import format still works
func TestImportWithInputsStringFormat(t *testing.T) {
	// Create a temporary directory for test files
	tempDir := testutil.TempDir(t, "test-import-string-*")

	// Create shared workflow file (no inputs needed for this test)
	sharedPath := filepath.Join(tempDir, "shared", "simple.md")
	sharedDir := filepath.Dir(sharedPath)
	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("Failed to create shared directory: %v", err)
	}

	sharedContent := `---
tools:
  bash:
    - "echo *"
---

# Simple Shared Instructions

Do something simple.
`
	if err := os.WriteFile(sharedPath, []byte(sharedContent), 0644); err != nil {
		t.Fatalf("Failed to write shared file: %v", err)
	}

	// Create main workflow that imports using string format
	workflowPath := filepath.Join(tempDir, "test-workflow.md")
	workflowContent := `---
on: issues
permissions:
  contents: read
  issues: read
engine: copilot
imports:
  - shared/simple.md
---

# Test Workflow

This workflow tests that string imports still work.
`
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write workflow file: %v", err)
	}

	// Compile the workflow
	compiler := workflow.NewCompiler()
	if err := compiler.CompileWorkflow(workflowPath); err != nil {
		t.Fatalf("CompileWorkflow failed: %v", err)
	}

	// Read the generated lock file
	lockFilePath := stringutil.MarkdownToLockFile(workflowPath)
	lockFileContent, err := os.ReadFile(lockFilePath)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContent := string(lockFileContent)

	// With runtime-import approach, imports without inputs use runtime-import macros
	// (not inlined content)
	if !strings.Contains(lockContent, "{{#runtime-import shared/simple.md}}") {
		t.Error("Expected lock file to contain runtime-import macro for shared workflow")
	}

	// The content should NOT be inlined (it's loaded at runtime)
	if strings.Contains(lockContent, "Simple Shared Instructions") {
		t.Error("Expected shared content to NOT be inlined (should use runtime-import)")
	}
}

// TestImportInputsExpressionValidation tests that github.aw.inputs expressions are allowed
func TestImportInputsExpressionValidation(t *testing.T) {
	// This test just verifies the expression is allowed in the markdown content
	content := "Process ${{ github.aw.inputs.limit }} items."
	err := workflow.ValidateExpressionSafetyPublic(content)
	if err != nil {
		t.Errorf("Expression validation should allow github.aw.inputs.* expressions: %v", err)
	}
}

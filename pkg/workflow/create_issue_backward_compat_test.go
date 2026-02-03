//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestCreateIssueBackwardCompatibility ensures existing workflows without assignees still compile correctly
func TestCreateIssueBackwardCompatibility(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "backward-compat-test")

	// Test with an existing workflow format (no assignees)
	testContent := `---
name: Legacy Workflow Format
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
    title-prefix: "[legacy] "
    labels: [automation]
    max: 2
---

# Legacy Workflow

This workflow uses the old format without assignees and should continue to work.
`

	testFile := filepath.Join(tmpDir, "legacy-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Legacy workflow should compile without errors: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "legacy-workflow.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledStr := string(compiledContent)

	// Verify that safe_outputs job exists
	if !strings.Contains(compiledStr, "safe_outputs:") {
		t.Error("Expected safe_outputs job in compiled workflow")
	}

	// Verify that Create Issue step is present via handler manager (consolidated mode uses handler manager)
	if !strings.Contains(compiledStr, "name: Process Safe Outputs") && !strings.Contains(compiledStr, "id: process_safe_outputs") {
		t.Error("Expected Process Safe Outputs step in compiled workflow (create-issue is now handled by handler manager)")
	}

	// Verify handler config contains create_issue
	if !strings.Contains(compiledStr, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
		t.Error("Expected GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG in compiled workflow")
	}
	if !strings.Contains(compiledStr, "create_issue") {
		t.Error("Expected create_issue in handler config")
	}

	// Verify that no assignee steps are present
	if strings.Contains(compiledStr, "Assign issue to") {
		t.Error("Did not expect assignee steps in legacy workflow")
	}

	// Verify that outputs are still set correctly - handler manager uses process_safe_outputs step
	if !strings.Contains(compiledStr, "process_safe_outputs") {
		t.Error("Expected process_safe_outputs step outputs in compiled workflow")
	}
}

// TestCreateIssueMinimalConfiguration ensures minimal configuration still works
func TestCreateIssueMinimalConfiguration(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "minimal-config-test")

	// Test with minimal configuration (just enabling create-issue)
	testContent := `---
name: Minimal Workflow
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
engine: copilot
strict: false
safe-outputs:
  create-issue:
---

# Minimal Workflow

Create an issue with minimal configuration.
`

	testFile := filepath.Join(tmpDir, "minimal-workflow.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Compile the workflow
	compiler := NewCompiler()
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Minimal workflow should compile without errors: %v", err)
	}

	// Read the compiled output
	outputFile := filepath.Join(tmpDir, "minimal-workflow.lock.yml")
	compiledContent, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read compiled output: %v", err)
	}

	compiledStr := string(compiledContent)

	// Verify that safe_outputs job exists
	if !strings.Contains(compiledStr, "safe_outputs:") {
		t.Error("Expected safe_outputs job in compiled workflow")
	}

	// Verify that no assignee steps are present
	if strings.Contains(compiledStr, "Assign issue to") {
		t.Error("Did not expect assignee steps in minimal workflow")
	}

	// Verify basic job structure
	if !strings.Contains(compiledStr, "permissions:") {
		t.Error("Expected permissions section in safe_outputs job")
	}
	if !strings.Contains(compiledStr, "issues: write") {
		t.Error("Expected issues: write permission in safe_outputs job")
	}
}

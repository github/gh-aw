//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestUpdateIssueConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-issue-test")

	// Test case with basic update-issue configuration
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
---

# Test Update Issue Configuration

This workflow tests the update-issue configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-update-issue.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with update-issue config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	// Check defaults
	if workflowData.SafeOutputs.UpdateIssues.Max != 1 {
		t.Fatalf("Expected max to be 1, got %d", workflowData.SafeOutputs.UpdateIssues.Max)
	}

	if workflowData.SafeOutputs.UpdateIssues.Target != "" {
		t.Fatalf("Expected target to be empty (default), got '%s'", workflowData.SafeOutputs.UpdateIssues.Target)
	}

	if workflowData.SafeOutputs.UpdateIssues.Status != nil {
		t.Fatal("Expected status to be nil by default (not updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Title != nil {
		t.Fatal("Expected title to be nil by default (not updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Body != nil {
		t.Fatal("Expected body to be nil by default (not updatable)")
	}
}

func TestUpdateIssueConfigWithAllOptions(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-issue-all-test")

	// Test case with all options configured
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
    max: 3
    target: "*"
    status:
    title:
    body:
---

# Test Update Issue Full Configuration

This workflow tests the update-issue configuration with all options.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-full.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with full update-issue config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	// Check all options
	if workflowData.SafeOutputs.UpdateIssues.Max != 3 {
		t.Fatalf("Expected max to be 3, got %d", workflowData.SafeOutputs.UpdateIssues.Max)
	}

	if workflowData.SafeOutputs.UpdateIssues.Target != "*" {
		t.Fatalf("Expected target to be '*', got '%s'", workflowData.SafeOutputs.UpdateIssues.Target)
	}

	if workflowData.SafeOutputs.UpdateIssues.Status == nil {
		t.Fatal("Expected status to be non-nil (updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Title == nil {
		t.Fatal("Expected title to be non-nil (updatable)")
	}

	if workflowData.SafeOutputs.UpdateIssues.Body == nil {
		t.Fatal("Expected body to be non-nil (updatable)")
	}
}

func TestUpdateIssueConfigTargetParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-issue-target-test")

	// Test case with specific target number
	testContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: read
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
    target: "123"
    title:
---

# Test Update Issue Target Configuration

This workflow tests the update-issue target configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-update-issue-target.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with target update-issue config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues == nil {
		t.Fatal("Expected update-issue configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateIssues.Target != "123" {
		t.Fatalf("Expected target to be '123', got '%s'", workflowData.SafeOutputs.UpdateIssues.Target)
	}

	if workflowData.SafeOutputs.UpdateIssues.Title == nil {
		t.Fatal("Expected title to be non-nil (updatable)")
	}
}

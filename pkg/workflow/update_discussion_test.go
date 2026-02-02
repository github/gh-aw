//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestUpdateDiscussionConfigParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-discussion-test")

	// Test case with basic update-discussion configuration
	testContent := `---
on:
  discussion:
    types: [created]
permissions:
  contents: read
  discussions: write
engine: claude
strict: false
safe-outputs:
  update-discussion:
---

# Test Update Discussion Configuration

This workflow tests the update-discussion configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-update-discussion.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with update-discussion config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions == nil {
		t.Fatal("Expected update-discussion configuration to be parsed")
	}

	// Check defaults
	if workflowData.SafeOutputs.UpdateDiscussions.Max != 1 {
		t.Fatalf("Expected max to be 1, got %d", workflowData.SafeOutputs.UpdateDiscussions.Max)
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Target != "" {
		t.Fatalf("Expected target to be empty (default), got '%s'", workflowData.SafeOutputs.UpdateDiscussions.Target)
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Title != nil {
		t.Fatal("Expected title to be nil by default (not updatable)")
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Body != nil {
		t.Fatal("Expected body to be nil by default (not updatable)")
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Labels != nil {
		t.Fatal("Expected labels to be nil by default (not updatable)")
	}

	if len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels) != 0 {
		t.Fatal("Expected allowed-labels to be empty by default")
	}
}

func TestUpdateDiscussionConfigWithAllOptions(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-discussion-all-test")

	// Test case with all options configured
	testContent := `---
on:
  discussion:
    types: [created]
permissions:
  contents: read
  discussions: write
engine: claude
strict: false
safe-outputs:
  update-discussion:
    max: 3
    target: "*"
    title:
    body:
    labels:
    allowed-labels: [bug, enhancement, documentation]
---

# Test Update Discussion Full Configuration

This workflow tests the update-discussion configuration with all options.
`

	testFile := filepath.Join(tmpDir, "test-update-discussion-full.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with full update-discussion config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions == nil {
		t.Fatal("Expected update-discussion configuration to be parsed")
	}

	// Check all options
	if workflowData.SafeOutputs.UpdateDiscussions.Max != 3 {
		t.Fatalf("Expected max to be 3, got %d", workflowData.SafeOutputs.UpdateDiscussions.Max)
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Target != "*" {
		t.Fatalf("Expected target to be '*', got '%s'", workflowData.SafeOutputs.UpdateDiscussions.Target)
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Title == nil {
		t.Fatal("Expected title to be non-nil (updatable)")
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Body == nil {
		t.Fatal("Expected body to be non-nil (updatable)")
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Labels == nil {
		t.Fatal("Expected labels to be non-nil (updatable)")
	}

	// Check allowed-labels
	expectedAllowedLabels := []string{"bug", "enhancement", "documentation"}
	if len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels) != len(expectedAllowedLabels) {
		t.Fatalf("Expected %d allowed-labels, got %d", len(expectedAllowedLabels), len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels))
	}

	for i, expected := range expectedAllowedLabels {
		if workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels[i] != expected {
			t.Fatalf("Expected allowed-label[%d] to be '%s', got '%s'", i, expected, workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels[i])
		}
	}
}

func TestUpdateDiscussionConfigTargetParsing(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-discussion-target-test")

	// Test case with specific target number
	testContent := `---
on:
  discussion:
    types: [created]
permissions:
  contents: read
  discussions: write
engine: claude
strict: false
safe-outputs:
  update-discussion:
    target: "123"
    title:
---

# Test Update Discussion Target Configuration

This workflow tests the update-discussion target configuration parsing.
`

	testFile := filepath.Join(tmpDir, "test-update-discussion-target.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with target update-discussion config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions == nil {
		t.Fatal("Expected update-discussion configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Target != "123" {
		t.Fatalf("Expected target to be '123', got '%s'", workflowData.SafeOutputs.UpdateDiscussions.Target)
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Title == nil {
		t.Fatal("Expected title to be non-nil (updatable)")
	}
}

func TestUpdateDiscussionConfigLabelsOnly(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-discussion-labels-test")

	// Test case with only labels configuration
	testContent := `---
on:
  discussion:
    types: [created]
permissions:
  contents: read
  discussions: write
engine: claude
strict: false
safe-outputs:
  update-discussion:
    labels:
    allowed-labels: [question, idea]
---

# Test Update Discussion Labels Configuration

This workflow tests the update-discussion labels configuration.
`

	testFile := filepath.Join(tmpDir, "test-update-discussion-labels.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with labels update-discussion config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions == nil {
		t.Fatal("Expected update-discussion configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions.Labels == nil {
		t.Fatal("Expected labels to be non-nil (updatable)")
	}

	// Check allowed-labels
	expectedAllowedLabels := []string{"question", "idea"}
	if len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels) != len(expectedAllowedLabels) {
		t.Fatalf("Expected %d allowed-labels, got %d", len(expectedAllowedLabels), len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels))
	}

	for i, expected := range expectedAllowedLabels {
		if workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels[i] != expected {
			t.Fatalf("Expected allowed-label[%d] to be '%s', got '%s'", i, expected, workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels[i])
		}
	}
}

func TestUpdateDiscussionConfigAllowedLabelsImplicitlyEnablesLabels(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := testutil.TempDir(t, "output-update-discussion-implicit-labels-test")

	// Test case with only allowed-labels (no explicit labels:)
	testContent := `---
on:
  discussion:
    types: [created]
permissions:
  contents: read
  discussions: write
engine: claude
strict: false
safe-outputs:
  update-discussion:
    allowed-labels: [bug, enhancement]
---

# Test Update Discussion Implicit Labels Configuration

This workflow tests that allowed-labels implicitly enables labels.
`

	testFile := filepath.Join(tmpDir, "test-update-discussion-implicit-labels.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Parse the workflow data
	workflowData, err := compiler.ParseWorkflowFile(testFile)
	if err != nil {
		t.Fatalf("Unexpected error parsing workflow with implicit labels config: %v", err)
	}

	// Verify output configuration is parsed correctly
	if workflowData.SafeOutputs == nil {
		t.Fatal("Expected output configuration to be parsed")
	}

	if workflowData.SafeOutputs.UpdateDiscussions == nil {
		t.Fatal("Expected update-discussion configuration to be parsed")
	}

	// The key test: labels should be implicitly enabled when allowed-labels is present
	if workflowData.SafeOutputs.UpdateDiscussions.Labels == nil {
		t.Fatal("Expected labels to be implicitly enabled when allowed-labels is present")
	}

	// Check allowed-labels
	expectedAllowedLabels := []string{"bug", "enhancement"}
	if len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels) != len(expectedAllowedLabels) {
		t.Fatalf("Expected %d allowed-labels, got %d", len(expectedAllowedLabels), len(workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels))
	}

	for i, expected := range expectedAllowedLabels {
		if workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels[i] != expected {
			t.Fatalf("Expected allowed-label[%d] to be '%s', got '%s'", i, expected, workflowData.SafeOutputs.UpdateDiscussions.AllowedLabels[i])
		}
	}
}

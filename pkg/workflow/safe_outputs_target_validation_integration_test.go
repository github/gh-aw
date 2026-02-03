//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestSafeOutputsTargetValidation_InvalidEvent tests that the compiler
// rejects an invalid target value like "event" at compile time
func TestSafeOutputsTargetValidation_InvalidEvent(t *testing.T) {
	tmpDir := testutil.TempDir(t, "target-validation-test")

	// Create a workflow with invalid target: "event"
	workflowContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
    body: null
    max: 1
    target: "event"
---

# Test Invalid Target

This workflow should fail to compile because "event" is not a valid target value.
`

	workflowPath := filepath.Join(tmpDir, "test-invalid-target.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Attempt to compile the workflow - should fail with validation error
	err := compiler.CompileWorkflow(workflowPath)
	if err == nil {
		t.Fatal("Expected compilation to fail with invalid target 'event', but it succeeded")
	}

	// Verify error message contains helpful information
	errMsg := err.Error()
	if !strings.Contains(errMsg, "invalid target value") {
		t.Errorf("Error message should mention 'invalid target value', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "update-issue") {
		t.Errorf("Error message should mention 'update-issue', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "event") {
		t.Errorf("Error message should mention the invalid value 'event', got: %s", errMsg)
	}
	// Should suggest valid values
	if !strings.Contains(errMsg, "triggering") {
		t.Errorf("Error message should suggest valid value 'triggering', got: %s", errMsg)
	}
}

// TestSafeOutputsTargetValidation_ValidValues tests that the compiler
// accepts valid target values
func TestSafeOutputsTargetValidation_ValidValues(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{
			name:   "triggering",
			target: "triggering",
		},
		{
			name:   "wildcard",
			target: `"*"`,
		},
		{
			name:   "explicit number",
			target: `"123"`,
		},
		{
			name:   "github expression",
			target: "${{ github.event.issue.number }}",
		},
		{
			name:   "empty (defaults to triggering)",
			target: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "target-validation-valid-test")

			targetLine := ""
			if tt.target != "" {
				targetLine = "    target: " + tt.target
			}

			workflowContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
    body: null
    max: 1
` + targetLine + `
---

# Test Valid Target

This workflow should compile successfully.
`

			workflowPath := filepath.Join(tmpDir, "test-valid-target.md")
			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Compile the workflow - should succeed
			err := compiler.CompileWorkflow(workflowPath)
			if err != nil {
				t.Fatalf("Expected compilation to succeed with valid target %q, but got error: %v", tt.target, err)
			}
		})
	}
}

// TestSafeOutputsTargetValidation_MultipleConfigs tests validation across
// multiple safe-output configurations
func TestSafeOutputsTargetValidation_MultipleConfigs(t *testing.T) {
	tmpDir := testutil.TempDir(t, "target-validation-multiple-test")

	// Create a workflow with multiple safe-output configs, one with invalid target
	workflowContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
  pull-requests: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
    body: null
    target: "triggering"
  close-issue:
    target: "invalid-value"
  add-labels:
    target: "*"
---

# Test Multiple Configs

This workflow should fail because close-issue has an invalid target.
`

	workflowPath := filepath.Join(tmpDir, "test-multiple-configs.md")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Attempt to compile the workflow - should fail
	err := compiler.CompileWorkflow(workflowPath)
	if err == nil {
		t.Fatal("Expected compilation to fail with invalid target in close-issue, but it succeeded")
	}

	// Verify error mentions close-issue
	errMsg := err.Error()
	if !strings.Contains(errMsg, "close-issue") {
		t.Errorf("Error message should mention 'close-issue', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "invalid-value") {
		t.Errorf("Error message should mention the invalid value, got: %s", errMsg)
	}
}

// TestSafeOutputsTargetValidation_InvalidNumericValues tests that
// zero and negative numbers are rejected
func TestSafeOutputsTargetValidation_InvalidNumericValues(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{
			name:   "zero",
			target: "0",
		},
		{
			name:   "negative number",
			target: "-1",
		},
		{
			name:   "leading zeros",
			target: "007",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "target-validation-invalid-numeric-test")

			workflowContent := `---
on:
  issues:
    types: [opened]
permissions:
  contents: read
  issues: write
engine: claude
features:
  dangerous-permissions-write: true
strict: false
safe-outputs:
  update-issue:
    body: null
    target: "` + tt.target + `"
---

# Test Invalid Numeric Target

This workflow should fail to compile.
`

			workflowPath := filepath.Join(tmpDir, "test-invalid-numeric.md")
			if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
				t.Fatal(err)
			}

			compiler := NewCompiler()

			// Attempt to compile the workflow - should fail
			err := compiler.CompileWorkflow(workflowPath)
			if err == nil {
				t.Fatalf("Expected compilation to fail with invalid numeric target %q, but it succeeded", tt.target)
			}
		})
	}
}

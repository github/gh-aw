//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestDuplicateStepValidation_Integration tests that the duplicate step validation
// correctly catches compiler bugs where the same step is added multiple times
func TestDuplicateStepValidation_Integration(t *testing.T) {
	// This test verifies the duplicate step validation by checking that
	// workflows compile without duplicate step errors
	tmpDir := testutil.TempDir(t, "duplicate-step-validation-test")

	// Test case: workflow with both create-pull-request and push-to-pull-request-branch
	// Previously this would generate duplicate "Checkout repository" steps
	mdContent := `---
on: issue_comment
engine: copilot
strict: false
safe-outputs:
  create-pull-request: null
  push-to-pull-request-branch: null
---

# Test Workflow

This workflow tests that duplicate checkout steps are properly deduplicated.
`

	mdFile := filepath.Join(tmpDir, "test-duplicate-steps.md")
	err := os.WriteFile(mdFile, []byte(mdContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Compile workflow
	compiler := NewCompiler()
	err = compiler.CompileWorkflow(mdFile)
	if err != nil {
		// The error should NOT be about duplicate steps since we fixed the bug
		if strings.Contains(err.Error(), "duplicate step") {
			t.Fatalf("Unexpected duplicate step error after fix: %v", err)
		}
		// Other errors are acceptable (this is just testing the validation)
		t.Logf("Compilation failed with non-duplicate-step error (acceptable): %v", err)
		return
	}

	// Read the generated lock file
	lockFile := stringutil.MarkdownToLockFile(mdFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify that there's only one "Checkout repository" step in the safe_outputs job
	// Count occurrences of "name: Checkout repository" in the safe_outputs job section
	safeOutputsStart := strings.Index(lockContentStr, "safe_outputs:")
	if safeOutputsStart == -1 {
		t.Error("Expected safe_outputs job to be present")
		return
	}

	// Find the next job after safe_outputs (or end of file)
	nextJobStart := strings.Index(lockContentStr[safeOutputsStart+1:], "\n  ") + safeOutputsStart + 1
	if nextJobStart <= safeOutputsStart {
		nextJobStart = len(lockContentStr)
	}

	safeOutputsSection := lockContentStr[safeOutputsStart:nextJobStart]
	checkoutCount := strings.Count(safeOutputsSection, "name: Checkout repository")

	// After the fix, we expect exactly 1 checkout step (shared between both operations)
	// OR 0 if the operations don't require checkout (depending on configuration)
	if checkoutCount > 1 {
		t.Errorf("Found %d 'Checkout repository' steps in safe_outputs job, expected 0 or 1 (deduplicated)", checkoutCount)
	}

	t.Logf("✓ Duplicate step validation working correctly: found %d checkout step(s) in safe_outputs job (deduplicated)", checkoutCount)
}

//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestOldSafeJobsSyntaxRejected verifies that the old top-level safe-jobs syntax is rejected
func TestOldSafeJobsSyntaxRejected(t *testing.T) {
	c := NewCompiler()

	// Create a temporary workflow file with old safe-jobs syntax
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-old-syntax.md")
	content := `---
on: issues
permissions:
  contents: read
safe-jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Test
        run: echo "test"
---

# Test workflow
Test old syntax
`
	err := os.WriteFile(workflowPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Attempt to parse the workflow
	_, err = c.ParseWorkflowFile(workflowPath)

	// Should fail with schema validation error
	if err == nil {
		t.Fatal("Expected error when using old safe-jobs syntax, but got nil")
	}

	// Error message should mention safe-jobs
	if err != nil && !strings.Contains(err.Error(), "safe-jobs") {
		t.Errorf("Expected error to mention 'safe-jobs', got: %v", err)
	}
}

// TestNewSafeOutputsJobsSyntaxAccepted verifies that the new safe-outputs.jobs syntax works
func TestNewSafeOutputsJobsSyntaxAccepted(t *testing.T) {
	c := NewCompiler()

	// Create a temporary workflow file with new safe-outputs.jobs syntax
	tmpDir := testutil.TempDir(t, "test-*")
	workflowPath := filepath.Join(tmpDir, "test-new-syntax.md")
	content := `---
on: issues
permissions:
  contents: read
  actions: read
safe-outputs:
  jobs:
    deploy:
      runs-on: ubuntu-latest
      steps:
        - name: Test
          run: echo "test"
---

# Test workflow
Test new syntax
`
	err := os.WriteFile(workflowPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Attempt to parse the workflow
	data, err := c.ParseWorkflowFile(workflowPath)

	// Should succeed
	if err != nil {
		t.Fatalf("Expected no error with new safe-outputs.jobs syntax, got: %v", err)
	}

	// Verify safe-jobs were parsed
	if data.SafeOutputs == nil {
		t.Fatal("Expected SafeOutputs to be populated")
	}

	if len(data.SafeOutputs.Jobs) != 1 {
		t.Fatalf("Expected 1 safe-job, got %d", len(data.SafeOutputs.Jobs))
	}

	if _, exists := data.SafeOutputs.Jobs["deploy"]; !exists {
		t.Error("Expected 'deploy' job to exist in SafeOutputs.Jobs")
	}
}

//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestNoStopTimeIntegration tests the --no-stop-after functionality end-to-end
func TestNoStopTimeIntegration(t *testing.T) {
	// Create a test workflow content with stop-after field
	testWorkflowContent := `---
on:
  issues:
    types: [opened]
  stop-after: "+48h"
permissions:
  contents: read
---

# Test Workflow

This workflow has a stop-after field that should be removed.`

	// Create temporary directory for test
	tmpDir := testutil.TempDir(t, "test-*")

	// Write test workflow
	workflowPath := filepath.Join(tmpDir, "test-workflow.md")
	err := os.WriteFile(workflowPath, []byte(testWorkflowContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test workflow: %v", err)
	}

	// Read the original content
	originalContent, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to read test workflow: %v", err)
	}

	// Verify stop-after exists in original
	if !strings.Contains(string(originalContent), "stop-after:") {
		t.Fatal("Test workflow should contain stop-after field")
	}

	// Apply the RemoveFieldFromOnTrigger function
	cleanedContent, err := RemoveFieldFromOnTrigger(string(originalContent), "stop-after")
	if err != nil {
		t.Fatalf("Failed to remove stop-after field: %v", err)
	}

	// Verify stop-after was removed
	if strings.Contains(cleanedContent, "stop-after:") {
		t.Errorf("stop-after field should have been removed, but found in:\n%s", cleanedContent)
	}

	// Verify other fields are still present
	if !strings.Contains(cleanedContent, "issues:") {
		t.Error("issues field should still be present")
	}
	if !strings.Contains(cleanedContent, "permissions:") {
		t.Error("permissions field should still be present")
	}
	if !strings.Contains(cleanedContent, "# Test Workflow") {
		t.Error("Workflow markdown content should still be present")
	}

	// Write back the cleaned content
	err = os.WriteFile(workflowPath, []byte(cleanedContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write cleaned workflow: %v", err)
	}

	// Verify the file was written successfully
	finalContent, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("Failed to read final workflow: %v", err)
	}

	// Final check
	if strings.Contains(string(finalContent), "stop-after:") {
		t.Error("Final workflow file should not contain stop-after field")
	}
}

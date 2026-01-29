//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/githubnext/gh-aw/pkg/stringutil"

	"github.com/githubnext/gh-aw/pkg/testutil"
)

// TestWorkflowTimestampCheckUsesJavaScript verifies that the workflow timestamp check uses JavaScript instead of inline bash
func TestWorkflowTimestampCheckUsesJavaScript(t *testing.T) {
	tmpDir := testutil.TempDir(t, "workflow-timestamp-js-test")

	compiler := NewCompiler()

	workflowContent := `---
on:
  workflow_dispatch:
engine: claude
---

# Test Workflow

This is a test workflow to verify timestamp checking uses JavaScript.
`
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := compiler.CompileWorkflow(workflowFile); err != nil {
		t.Fatalf("Compilation failed: %v", err)
	}

	lockFile := stringutil.MarkdownToLockFile(workflowFile)
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Verify the "Check workflow file timestamps" step exists
	if !strings.Contains(lockContentStr, "Check workflow file timestamps") {
		t.Error("Expected 'Check workflow file timestamps' step to be present")
	}

	// Verify it uses actions/github-script instead of inline bash
	timestampCheckIdx := strings.Index(lockContentStr, "Check workflow file timestamps")
	if timestampCheckIdx == -1 {
		t.Fatal("Could not find timestamp check step")
	}

	// Extract a section after the timestamp check (next ~500 chars should contain the uses directive)
	sectionAfterTimestampCheck := lockContentStr[timestampCheckIdx:]
	if len(sectionAfterTimestampCheck) > 500 {
		sectionAfterTimestampCheck = sectionAfterTimestampCheck[:500]
	}

	// Verify it uses actions/github-script@
	if !strings.Contains(sectionAfterTimestampCheck, "uses: actions/github-script@") {
		t.Error("Expected timestamp check to use actions/github-script")
	}

	// Verify it does NOT use inline bash (run: |)
	// Find the next step boundary or job boundary to limit the search
	nextStepOrJobIdx := strings.Index(sectionAfterTimestampCheck, "- name:")
	if nextStepOrJobIdx == -1 {
		nextStepOrJobIdx = strings.Index(sectionAfterTimestampCheck, "  agent:")
	}
	if nextStepOrJobIdx == -1 {
		nextStepOrJobIdx = len(sectionAfterTimestampCheck)
	}
	timestampCheckSection := sectionAfterTimestampCheck[:nextStepOrJobIdx]

	// The step should NOT contain "run: |" (which would indicate inline bash)
	if strings.Contains(timestampCheckSection, "run: |") {
		t.Error("Expected timestamp check to NOT use inline bash (run: |)")
	}

	// Verify the JavaScript content is present (check for key functions)
	if !strings.Contains(lockContentStr, "GITHUB_WORKSPACE") {
		t.Error("Expected JavaScript to reference GITHUB_WORKSPACE")
	}
	if !strings.Contains(lockContentStr, "GITHUB_WORKFLOW") {
		t.Error("Expected JavaScript to reference GITHUB_WORKFLOW")
	}

	// Verify the old bash-specific variables are NOT present in the inline format
	// (They might still be referenced within the JavaScript, but not as bash variable assignments)
	if strings.Contains(timestampCheckSection, "WORKFLOW_FILE=\"${GITHUB_WORKSPACE}") {
		t.Error("Expected timestamp check to NOT contain inline bash variable assignment")
	}
}

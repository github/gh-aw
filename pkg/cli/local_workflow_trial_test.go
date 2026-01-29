//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalWorkflowTrialMode tests the local workflow installation for trial mode
func TestLocalWorkflowTrialMode(t *testing.T) {
	// Clear the repository slug cache to ensure clean test state
	ClearCurrentRepoSlugCache()

	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "gh-aw-local-trial-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test workflow file
	testWorkflowContent := `---
description: "Test local workflow"
on:
  workflow_dispatch:
---

# Test Workflow

This is a test workflow.

## Steps

- name: Test step
  run: echo "Hello World"
`

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	testFile := filepath.Join(originalDir, "test-workflow.md")
	if err := os.WriteFile(testFile, []byte(testWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}
	defer os.Remove(testFile)

	// Parse the local workflow spec
	spec, err := parseWorkflowSpec("./test-workflow.md")
	if err != nil {
		t.Fatalf("Failed to parse local workflow spec: %v", err)
	}

	// Verify the spec
	if !strings.HasPrefix(spec.WorkflowPath, "./") {
		t.Errorf("Expected WorkflowPath to start with './', got: %s", spec.WorkflowPath)
	}

	if spec.WorkflowName != "test-workflow" {
		t.Errorf("Expected WorkflowName to be 'test-workflow', got: %s", spec.WorkflowName)
	}

	// Test the local installation function
	err = installLocalWorkflowInTrialMode(originalDir, tempDir, spec, "", false)
	if err != nil {
		t.Fatalf("Failed to install local workflow: %v", err)
	}

	// Verify the file was copied correctly
	expectedDest := filepath.Join(tempDir, ".github/workflows", "test-workflow.md")
	if _, err := os.Stat(expectedDest); os.IsNotExist(err) {
		t.Errorf("Expected workflow file to be copied to %s, but it doesn't exist", expectedDest)
	}

	// Verify the content matches
	copiedContent, err := os.ReadFile(expectedDest)
	if err != nil {
		t.Fatalf("Failed to read copied workflow file: %v", err)
	}

	if string(copiedContent) != testWorkflowContent {
		t.Errorf("Copied workflow content doesn't match original")
	}
}

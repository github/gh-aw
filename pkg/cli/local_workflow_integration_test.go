//go:build integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestLocalWorkflowIntegration(t *testing.T) {
	// Create a temporary directory
	tempDir := testutil.TempDir(t, "test-*")
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalWd)

	// Change to temp directory
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test workflow file
	workflowsDir := "workflows"
	err = os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	testWorkflowPath := filepath.Join(workflowsDir, "test-local.md")
	testContent := `---
description: "Test local workflow"
on:
  push:
    branches: [main]
permissions:
  contents: read
engine: claude
tools:
  github:
    allowed: [list_commits]
---

# Test Local Workflow

This is a test local workflow.
`

	err = os.WriteFile(testWorkflowPath, []byte(testContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Test parsing local workflow spec
	// Note: This will fail if we're not in a git repository, which is expected
	spec, err := parseWorkflowSpec("./workflows/test-local.md")
	if err != nil {
		// If we're not in a git repository, skip the rest of the test
		if strings.Contains(err.Error(), "failed to get current repository info") {
			t.Skip("Skipping test because we're not in a git repository (this is expected behavior)")
		}
		t.Fatalf("Failed to parse local workflow spec: %v", err)
	}

	// Verify parsed spec
	if spec.WorkflowPath != "./workflows/test-local.md" {
		t.Errorf("Expected WorkflowPath './workflows/test-local.md', got %q", spec.WorkflowPath)
	}
	if spec.WorkflowName != "test-local" {
		t.Errorf("Expected WorkflowName 'test-local', got %q", spec.WorkflowName)
	}
	if spec.Version != "" {
		t.Errorf("Expected empty Version for local workflow, got %q", spec.Version)
	}

	// Test String() method
	stringResult := spec.String()
	if stringResult != "./workflows/test-local.md" {
		t.Errorf("Expected String() './workflows/test-local.md', got %q", stringResult)
	}

	// Test buildSourceString (should remove ./ prefix)
	sourceString := buildSourceString(spec)
	expectedSourceString := spec.RepoSlug + "/workflows/test-local.md"
	if sourceString != expectedSourceString {
		t.Errorf("Expected buildSourceString() %q, got %q", expectedSourceString, sourceString)
	}

	// Test findWorkflowInPackageForRepo
	content, sourceInfo, err := findWorkflowInPackageForRepo(spec, false)
	if err != nil {
		t.Fatalf("Failed to find local workflow: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Content mismatch")
	}

	if sourceInfo.PackagePath != "." {
		t.Errorf("Expected PackagePath '.', got %q", sourceInfo.PackagePath)
	}

	if sourceInfo.SourcePath != "./workflows/test-local.md" {
		t.Errorf("Expected SourcePath './workflows/test-local.md', got %q", sourceInfo.SourcePath)
	}

	if sourceInfo.CommitSHA != "" {
		t.Errorf("Expected empty CommitSHA for local workflow, got %q", sourceInfo.CommitSHA)
	}
}

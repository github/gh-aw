//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindWorkflowWithGitHubWorkflowsFallback tests that the workflow resolution
// falls back to .github/workflows/ when the initial workflows/ path doesn't exist
func TestFindWorkflowWithGitHubWorkflowsFallback(t *testing.T) {
	// Get the packages directory
	packagesDir, err := getPackagesDir()
	if err != nil {
		t.Fatal(err)
	}

	// Create a unique test directory to avoid conflicts
	testRepoPath := filepath.Join(packagesDir, "test-fallback-org", "test-fallback-repo")

	// Clean up before and after test
	defer os.RemoveAll(filepath.Join(packagesDir, "test-fallback-org"))
	os.RemoveAll(filepath.Join(packagesDir, "test-fallback-org")) // Clean up any existing test data

	// Create .github/workflows/ directory structure
	githubWorkflowsDir := filepath.Join(testRepoPath, ".github", "workflows")
	err = os.MkdirAll(githubWorkflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test workflow file in .github/workflows/
	testWorkflowContent := `---
description: "Test fallback workflow"
on:
  push:
    branches: [main]
permissions:
  contents: read
engine: claude
---

# Test Fallback Workflow

This workflow tests the fallback path resolution.
`

	workflowFilePath := filepath.Join(githubWorkflowsDir, "test-fallback-workflow.md")
	err = os.WriteFile(workflowFilePath, []byte(testWorkflowContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create .commit-sha metadata file
	commitSHA := "abc123def456"
	commitSHAPath := filepath.Join(testRepoPath, ".commit-sha")
	err = os.WriteFile(commitSHAPath, []byte(commitSHA), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a workflow spec that would normally look for workflows/test-fallback-workflow.md
	// (which doesn't exist), but should fall back to .github/workflows/test-fallback-workflow.md
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "test-fallback-org/test-fallback-repo",
			Version:  "main",
		},
		WorkflowPath: "workflows/test-fallback-workflow.md",
		WorkflowName: "test-fallback-workflow",
	}

	// Test findWorkflowInPackageForRepo - should find the file via fallback
	content, sourceInfo, err := findWorkflowInPackageForRepo(spec, false)
	if err != nil {
		t.Fatalf("Failed to find workflow with fallback: %v", err)
	}

	// Verify content
	if string(content) != testWorkflowContent {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", testWorkflowContent, string(content))
	}

	// Verify source info
	expectedSourcePath := filepath.Join(testRepoPath, ".github", "workflows", "test-fallback-workflow.md")
	if sourceInfo.SourcePath != expectedSourcePath {
		t.Errorf("Expected SourcePath %q, got %q", expectedSourcePath, sourceInfo.SourcePath)
	}

	if sourceInfo.PackagePath != testRepoPath {
		t.Errorf("Expected PackagePath %q, got %q", testRepoPath, sourceInfo.PackagePath)
	}

	if sourceInfo.CommitSHA != commitSHA {
		t.Errorf("Expected CommitSHA %q, got %q", commitSHA, sourceInfo.CommitSHA)
	}
}

// TestFindWorkflowWithoutFallback tests that the original path is used when it exists
func TestFindWorkflowWithoutFallback(t *testing.T) {
	// Get the packages directory
	packagesDir, err := getPackagesDir()
	if err != nil {
		t.Fatal(err)
	}

	// Create a unique test directory to avoid conflicts
	testRepoPath := filepath.Join(packagesDir, "test-original-org", "test-original-repo")

	// Clean up before and after test
	defer os.RemoveAll(filepath.Join(packagesDir, "test-original-org"))
	os.RemoveAll(filepath.Join(packagesDir, "test-original-org")) // Clean up any existing test data

	// Create workflows/ directory structure
	workflowsDir := filepath.Join(testRepoPath, "workflows")
	err = os.MkdirAll(workflowsDir, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create a test workflow file in workflows/
	testWorkflowContent := `---
description: "Test original path workflow"
on:
  push:
    branches: [main]
permissions:
  contents: read
engine: claude
---

# Test Original Path Workflow

This workflow tests that the original path is used when it exists.
`

	workflowFilePath := filepath.Join(workflowsDir, "test-original-workflow.md")
	err = os.WriteFile(workflowFilePath, []byte(testWorkflowContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create a workflow spec
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "test-original-org/test-original-repo",
			Version:  "main",
		},
		WorkflowPath: "workflows/test-original-workflow.md",
		WorkflowName: "test-original-workflow",
	}

	// Test findWorkflowInPackageForRepo - should find the file at the original path
	content, sourceInfo, err := findWorkflowInPackageForRepo(spec, false)
	if err != nil {
		t.Fatalf("Failed to find workflow at original path: %v", err)
	}

	// Verify content
	if string(content) != testWorkflowContent {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", testWorkflowContent, string(content))
	}

	// Verify source info - should be the original workflows/ path
	expectedSourcePath := filepath.Join(testRepoPath, "workflows", "test-original-workflow.md")
	if sourceInfo.SourcePath != expectedSourcePath {
		t.Errorf("Expected SourcePath %q, got %q", expectedSourcePath, sourceInfo.SourcePath)
	}
}

// TestFindWorkflowFallbackFailure tests that an error is returned when neither path exists
func TestFindWorkflowFallbackFailure(t *testing.T) {
	// Get the packages directory
	packagesDir, err := getPackagesDir()
	if err != nil {
		t.Fatal(err)
	}

	// Create a unique test directory to avoid conflicts
	testRepoPath := filepath.Join(packagesDir, "test-failure-org", "test-failure-repo")

	// Clean up before and after test
	defer os.RemoveAll(filepath.Join(packagesDir, "test-failure-org"))
	os.RemoveAll(filepath.Join(packagesDir, "test-failure-org")) // Clean up any existing test data

	// Create just the repo directory but no workflow files
	err = os.MkdirAll(testRepoPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create a workflow spec for a non-existent file
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "test-failure-org/test-failure-repo",
			Version:  "main",
		},
		WorkflowPath: "workflows/nonexistent.md",
		WorkflowName: "nonexistent",
	}

	// Test findWorkflowInPackageForRepo - should fail since file doesn't exist
	_, _, err = findWorkflowInPackageForRepo(spec, false)
	if err == nil {
		t.Fatal("Expected error when workflow file doesn't exist, got nil")
	}

	// Verify error message
	expectedErrSubstring := "not found in repo"
	if !strings.Contains(err.Error(), expectedErrSubstring) {
		t.Errorf("Expected error to contain %q, got %q", expectedErrSubstring, err.Error())
	}
}

// TestFindWorkflowNonWorkflowsPath tests that non-workflows/ paths don't trigger fallback
func TestFindWorkflowNonWorkflowsPath(t *testing.T) {
	// Get the packages directory
	packagesDir, err := getPackagesDir()
	if err != nil {
		t.Fatal(err)
	}

	// Create a unique test directory to avoid conflicts
	testRepoPath := filepath.Join(packagesDir, "test-nonworkflows-org", "test-nonworkflows-repo")

	// Clean up before and after test
	defer os.RemoveAll(filepath.Join(packagesDir, "test-nonworkflows-org"))
	os.RemoveAll(filepath.Join(packagesDir, "test-nonworkflows-org")) // Clean up any existing test data

	// Create just the repo directory but no workflow files
	err = os.MkdirAll(testRepoPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	// Create a workflow spec with a custom path (not starting with "workflows/")
	spec := &WorkflowSpec{
		RepoSpec: RepoSpec{
			RepoSlug: "test-nonworkflows-org/test-nonworkflows-repo",
			Version:  "main",
		},
		WorkflowPath: "custom/path/workflow.md",
		WorkflowName: "workflow",
	}

	// Test findWorkflowInPackageForRepo - should fail without attempting fallback
	_, _, err = findWorkflowInPackageForRepo(spec, false)
	if err == nil {
		t.Fatal("Expected error when workflow file doesn't exist and path is not workflows/, got nil")
	}

	// Verify error message
	expectedErrSubstring := "not found in repo"
	if !strings.Contains(err.Error(), expectedErrSubstring) {
		t.Errorf("Expected error to contain %q, got %q", expectedErrSubstring, err.Error())
	}
}

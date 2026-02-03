//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestReadWorkflowFileWithRelativePath tests that readWorkflowFile correctly handles relative paths on all platforms
func TestReadWorkflowFileWithRelativePath(t *testing.T) {
	// Create a temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a test workflow file
	testFile := filepath.Join(workflowsDir, "test-workflow.md")
	testContent := []byte("---\non: push\n---\n# Test Workflow")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test reading the workflow file with a relative path
	// This simulates what happens when getWorkflowsDir() returns a string and it's used with filepath.Join
	content, path, err := readWorkflowFile("test-workflow.md", workflowsDir)
	if err != nil {
		t.Errorf("readWorkflowFile failed: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, content)
	}

	// The returned path should be the full path to the file
	expectedPath := testFile
	if path != expectedPath {
		t.Errorf("Path mismatch. Expected: %s, Got: %s", expectedPath, path)
	}
}

// TestReadWorkflowFileWithAbsolutePath tests that readWorkflowFile correctly handles absolute paths
func TestReadWorkflowFileWithAbsolutePath(t *testing.T) {
	// Create a temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a test workflow file
	testFile := filepath.Join(workflowsDir, "test-workflow.md")
	testContent := []byte("---\non: push\n---\n# Test Workflow")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test reading the workflow file with an absolute path
	// The workflowsDir parameter should be ignored when filePath is absolute
	content, path, err := readWorkflowFile(testFile, workflowsDir)
	if err != nil {
		t.Errorf("readWorkflowFile failed with absolute path: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, content)
	}

	// The returned path should be the absolute path
	if path != testFile {
		t.Errorf("Path mismatch. Expected: %s, Got: %s", testFile, path)
	}
}

// TestReadWorkflowFilePathSeparators tests that the function works correctly regardless of path separator
func TestReadWorkflowFilePathSeparators(t *testing.T) {
	// Create a temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create a test workflow file in a subdirectory
	subDir := filepath.Join(workflowsDir, "subfolder")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subfolder: %v", err)
	}

	testFile := filepath.Join(subDir, "test-workflow.md")
	testContent := []byte("---\non: push\n---\n# Test Workflow")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test reading with OS-appropriate path separators
	// filepath.Join will use the correct separator for the current OS
	relativePath := filepath.Join("subfolder", "test-workflow.md")
	content, path, err := readWorkflowFile(relativePath, workflowsDir)
	if err != nil {
		t.Errorf("readWorkflowFile failed with subdirectory path: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("Content mismatch. Expected: %s, Got: %s", testContent, content)
	}

	// The returned path should be the full path to the file
	if path != testFile {
		t.Errorf("Path mismatch. Expected: %s, Got: %s", testFile, path)
	}
}

// TestReadWorkflowFileNonExistent tests error handling for non-existent files
func TestReadWorkflowFileNonExistent(t *testing.T) {
	// Create a temporary directory structure
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Try to read a non-existent file
	_, _, err := readWorkflowFile("non-existent.md", workflowsDir)
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

// TestWorkflowResolutionWindowsCompatibility tests the complete workflow resolution flow
// This test specifically addresses the issue reported in GitHub where Windows users
// experienced "workflow not found" errors due to path separator mismatches
func TestWorkflowResolutionWindowsCompatibility(t *testing.T) {
	// Create a temporary directory structure that mimics the user's setup
	tempDir := testutil.TempDir(t, "test-*")
	workflowsDir := filepath.Join(tempDir, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}

	// Create workflow files similar to the user's scenario
	mdFile := filepath.Join(workflowsDir, "update-docs.md")
	lockFile := filepath.Join(workflowsDir, "update-docs.lock.yml")

	mdContent := []byte(`---
on:
  workflow_dispatch:
permissions:
  contents: read
---
# Update Documentation
Test workflow for Windows path handling
`)
	if err := os.WriteFile(mdFile, mdContent, 0644); err != nil {
		t.Fatalf("Failed to write markdown file: %v", err)
	}

	lockContent := []byte("name: update-docs\non:\n  workflow_dispatch:\n")
	if err := os.WriteFile(lockFile, lockContent, 0644); err != nil {
		t.Fatalf("Failed to write lock file: %v", err)
	}

	// Change to the temp directory to simulate the user's working directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	// Test 1: Read workflow file using relative path with getWorkflowsDir()
	// This simulates the exact path used in resolveWorkflowFile
	content, path, err := readWorkflowFile("update-docs.md", getWorkflowsDir())
	if err != nil {
		t.Errorf("Failed to read workflow file with getWorkflowsDir(): %v", err)
	}

	if string(content) != string(mdContent) {
		t.Errorf("Content mismatch when reading with getWorkflowsDir()")
	}

	// Verify the returned path exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("Returned path does not exist: %s (error: %v)", path, err)
	}

	// Test 2: Verify lock file can be found using the same approach
	lockPath := filepath.Join(getWorkflowsDir(), "update-docs.lock.yml")
	if _, err := os.Stat(lockPath); err != nil {
		t.Errorf("Lock file not found at expected path: %s (error: %v)", lockPath, err)
	}

	// Test 3: Test with subdirectories (edge case)
	subDir := filepath.Join(workflowsDir, "subfolder")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	subFile := filepath.Join(subDir, "nested-workflow.md")
	if err := os.WriteFile(subFile, mdContent, 0644); err != nil {
		t.Fatalf("Failed to write nested workflow file: %v", err)
	}

	nestedPath := filepath.Join("subfolder", "nested-workflow.md")
	content2, path2, err := readWorkflowFile(nestedPath, getWorkflowsDir())
	if err != nil {
		t.Errorf("Failed to read nested workflow file: %v", err)
	}

	if string(content2) != string(mdContent) {
		t.Errorf("Content mismatch when reading nested workflow")
	}

	if _, err := os.Stat(path2); err != nil {
		t.Errorf("Returned nested path does not exist: %s (error: %v)", path2, err)
	}
}

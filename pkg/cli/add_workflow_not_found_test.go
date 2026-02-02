//go:build !integration

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestDisplayAvailableWorkflows tests that displayAvailableWorkflows shows the list of available workflows
func TestDisplayAvailableWorkflows(t *testing.T) {
	// Create a temporary packages directory structure
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create a mock package structure
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-owner", "test-repo")
	workflowsDir := filepath.Join(packagePath, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create some mock workflow files with valid frontmatter
	validWorkflowContent := `---
on: push
---

# Test Workflow
`

	workflows := []string{
		"ci-doctor.md",
		"daily-plan.md",
		"weekly-summary.md",
	}

	for _, wf := range workflows {
		wfPath := filepath.Join(workflowsDir, wf)
		if err := os.WriteFile(wfPath, []byte(validWorkflowContent), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", wf, err)
		}
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call displayAvailableWorkflows
	err := displayAvailableWorkflows("test-owner/test-repo", "", false)

	// Restore stderr and capture output
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("displayAvailableWorkflows() unexpected error: %v", err)
	}

	// Check that the output contains the expected workflow names
	expectedWorkflows := []string{"ci-doctor", "daily-plan", "weekly-summary"}
	for _, wf := range expectedWorkflows {
		if !strings.Contains(output, wf) {
			t.Errorf("displayAvailableWorkflows() output should contain workflow '%s', got:\n%s", wf, output)
		}
	}

	// Check that the output contains helpful information
	if !strings.Contains(output, "Available workflows") {
		t.Errorf("displayAvailableWorkflows() output should contain 'Available workflows', got:\n%s", output)
	}

	if !strings.Contains(output, "Example:") {
		t.Errorf("displayAvailableWorkflows() output should contain 'Example:', got:\n%s", output)
	}
}

// TestDisplayAvailableWorkflowsWithVersion tests displayAvailableWorkflows with a version
func TestDisplayAvailableWorkflowsWithVersion(t *testing.T) {
	// Create a temporary packages directory structure
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create a mock package structure
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-owner", "test-repo")
	workflowsDir := filepath.Join(packagePath, "workflows")
	if err := os.MkdirAll(workflowsDir, 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Create a mock workflow file
	validWorkflowContent := `---
on: push
---

# Test Workflow
`
	wfPath := filepath.Join(workflowsDir, "test-workflow.md")
	if err := os.WriteFile(wfPath, []byte(validWorkflowContent), 0644); err != nil {
		t.Fatalf("Failed to create workflow file: %v", err)
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call displayAvailableWorkflows with a version
	err := displayAvailableWorkflows("test-owner/test-repo", "v1.0.0", false)

	// Restore stderr and capture output
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("displayAvailableWorkflows() unexpected error: %v", err)
	}

	// Check that the version is included in the example command
	if !strings.Contains(output, "@v1.0.0") {
		t.Errorf("displayAvailableWorkflows() output should include version '@v1.0.0', got:\n%s", output)
	}
}

// TestDisplayAvailableWorkflowsNoWorkflows tests when no workflows are found
func TestDisplayAvailableWorkflowsNoWorkflows(t *testing.T) {
	// Create a temporary packages directory structure
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create an empty package structure
	packagePath := filepath.Join(tempDir, ".aw", "packages", "test-owner", "test-repo")
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		t.Fatalf("Failed to create test directories: %v", err)
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call displayAvailableWorkflows
	err := displayAvailableWorkflows("test-owner/test-repo", "", false)

	// Restore stderr and capture output
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Errorf("displayAvailableWorkflows() unexpected error: %v", err)
	}

	// Check that the output contains a warning about no workflows
	if !strings.Contains(output, "No workflows found") {
		t.Errorf("displayAvailableWorkflows() output should contain 'No workflows found', got:\n%s", output)
	}
}

// TestDisplayAvailableWorkflowsPackageNotFound tests when package is not found
func TestDisplayAvailableWorkflowsPackageNotFound(t *testing.T) {
	// Create a temporary packages directory
	tempDir := testutil.TempDir(t, "test-*")

	// Override packages directory for testing
	t.Setenv("HOME", tempDir)

	// Create packages directory but don't create the specific package
	packagesDir := filepath.Join(tempDir, ".aw", "packages")
	if err := os.MkdirAll(packagesDir, 0755); err != nil {
		t.Fatalf("Failed to create packages directory: %v", err)
	}

	// Call displayAvailableWorkflows with non-existent package
	err := displayAvailableWorkflows("nonexistent/repo", "", false)

	if err == nil {
		t.Error("displayAvailableWorkflows() expected error for non-existent package, got nil")
	}

	if !strings.Contains(err.Error(), "package not found") {
		t.Errorf("displayAvailableWorkflows() error should contain 'package not found', got: %v", err)
	}
}

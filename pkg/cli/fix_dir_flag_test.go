//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFixWithDirFlag tests the --dir flag functionality
func TestFixWithDirFlag(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "custom-workflows")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflow directory: %v", err)
	}

	// Create a test workflow with deprecated field
	workflowContent := `---
on:
  workflow_dispatch:

timeout_minutes: 30

permissions:
  contents: read
---

# Test Workflow

This is a test workflow with deprecated timeout_minutes field.
`
	workflowFile := filepath.Join(customDir, "test.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Run fix command with --dir flag
	config := FixConfig{
		WorkflowIDs: []string{},
		Write:       true,
		Verbose:     false,
		WorkflowDir: customDir,
	}

	err := RunFix(config)
	if err != nil {
		t.Fatalf("RunFix with --dir should succeed, got error: %v", err)
	}

	// Verify the file was fixed
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	updatedStr := string(updatedContent)
	if strings.Contains(updatedStr, "timeout_minutes:") {
		t.Error("Expected timeout_minutes to be replaced")
	}
	if !strings.Contains(updatedStr, "timeout-minutes: 30") {
		t.Errorf("Expected timeout-minutes: 30 in updated content, got:\n%s", updatedStr)
	}
}

// TestFixWithDirFlagAndSpecificWorkflow tests --dir with specific workflow
func TestFixWithDirFlagAndSpecificWorkflow(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "custom-workflows")
	if err := os.MkdirAll(customDir, 0755); err != nil {
		t.Fatalf("Failed to create custom workflow directory: %v", err)
	}

	// Create two test workflows
	workflow1Content := `---
on: workflow_dispatch
timeout_minutes: 30
---
# Workflow 1
`
	workflow1File := filepath.Join(customDir, "workflow1.md")
	if err := os.WriteFile(workflow1File, []byte(workflow1Content), 0644); err != nil {
		t.Fatalf("Failed to create workflow1 file: %v", err)
	}

	workflow2Content := `---
on: workflow_dispatch
timeout_minutes: 60
---
# Workflow 2
`
	workflow2File := filepath.Join(customDir, "workflow2.md")
	if err := os.WriteFile(workflow2File, []byte(workflow2Content), 0644); err != nil {
		t.Fatalf("Failed to create workflow2 file: %v", err)
	}

	// Run fix command on specific workflow with --dir flag
	config := FixConfig{
		WorkflowIDs: []string{workflow1File},
		Write:       true,
		Verbose:     false,
		WorkflowDir: customDir,
	}

	err := RunFix(config)
	if err != nil {
		t.Fatalf("RunFix with --dir and specific workflow should succeed, got error: %v", err)
	}

	// Verify workflow1 was fixed
	updated1Content, err := os.ReadFile(workflow1File)
	if err != nil {
		t.Fatalf("Failed to read updated workflow1 file: %v", err)
	}

	if !strings.Contains(string(updated1Content), "timeout-minutes: 30") {
		t.Error("Expected workflow1 to be fixed")
	}

	// Verify workflow2 was NOT fixed (we didn't specify it)
	updated2Content, err := os.ReadFile(workflow2File)
	if err != nil {
		t.Fatalf("Failed to read workflow2 file: %v", err)
	}

	if strings.Contains(string(updated2Content), "timeout-minutes:") {
		t.Error("Expected workflow2 to NOT be fixed")
	}
}

// TestFixDirFlagDefaultBehavior tests that empty dir defaults to .github/workflows
func TestFixDirFlagDefaultBehavior(t *testing.T) {
	// Create a temporary directory for test
	tmpDir := t.TempDir()
	defaultDir := filepath.Join(tmpDir, ".github", "workflows")
	if err := os.MkdirAll(defaultDir, 0755); err != nil {
		t.Fatalf("Failed to create default workflow directory: %v", err)
	}

	// Create a test workflow
	workflowContent := `---
on: workflow_dispatch
timeout_minutes: 30
---
# Test Workflow
`
	workflowFile := filepath.Join(defaultDir, "test.md")
	if err := os.WriteFile(workflowFile, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create test workflow file: %v", err)
	}

	// Save original directory and change to tmpDir
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Run fix command without --dir flag (should default to .github/workflows)
	config := FixConfig{
		WorkflowIDs: []string{},
		Write:       true,
		Verbose:     false,
		WorkflowDir: "", // Empty should default to .github/workflows
	}

	err = RunFix(config)
	if err != nil {
		t.Fatalf("RunFix with default dir should succeed, got error: %v", err)
	}

	// Verify the file was fixed
	updatedContent, err := os.ReadFile(workflowFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	if !strings.Contains(string(updatedContent), "timeout-minutes: 30") {
		t.Error("Expected file in default directory to be fixed")
	}
}

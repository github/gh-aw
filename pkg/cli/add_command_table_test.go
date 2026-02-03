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

// TestListWorkflowsWithMetadata tests that listWorkflowsWithMetadata extracts workflow metadata correctly
func TestListWorkflowsWithMetadata(t *testing.T) {
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

	// Create workflow files with different metadata configurations
	testWorkflows := []struct {
		filename     string
		content      string
		expectedID   string
		expectedName string
		expectedDesc string
	}{
		{
			filename: "ci-doctor.md",
			content: `---
on: push
name: CI Doctor
description: Diagnoses and fixes CI/CD pipeline issues
---

# CI Doctor Workflow

This workflow analyzes your CI/CD pipeline.
`,
			expectedID:   "ci-doctor",
			expectedName: "CI Doctor",
			expectedDesc: "Diagnoses and fixes CI/CD pipeline issues",
		},
		{
			filename: "daily-plan.md",
			content: `---
on:
  schedule:
    - cron: "0 9 * * *"
description: Creates a daily plan based on repository activity
---

# Daily Planning Assistant

This workflow creates a daily plan.
`,
			expectedID:   "daily-plan",
			expectedName: "Daily Planning Assistant",
			expectedDesc: "Creates a daily plan based on repository activity",
		},
		{
			filename: "weekly-summary.md",
			content: `---
on: workflow_dispatch
---

# Weekly Summary Report

This workflow generates a weekly summary.
`,
			expectedID:   "weekly-summary",
			expectedName: "Weekly Summary Report",
			expectedDesc: "",
		},
	}

	for _, wf := range testWorkflows {
		wfPath := filepath.Join(workflowsDir, wf.filename)
		if err := os.WriteFile(wfPath, []byte(wf.content), 0644); err != nil {
			t.Fatalf("Failed to create workflow file %s: %v", wf.filename, err)
		}
	}

	// Call listWorkflowsWithMetadata
	workflows, err := listWorkflowsWithMetadata("test-owner/test-repo", false)
	if err != nil {
		t.Fatalf("listWorkflowsWithMetadata failed: %v", err)
	}

	// Verify the results
	if len(workflows) != len(testWorkflows) {
		t.Fatalf("Expected %d workflows, got %d", len(testWorkflows), len(workflows))
	}

	// Create a map for easier verification
	workflowMap := make(map[string]WorkflowInfo)
	for _, wf := range workflows {
		workflowMap[wf.ID] = wf
	}

	// Verify each workflow
	for _, expected := range testWorkflows {
		wf, exists := workflowMap[expected.expectedID]
		if !exists {
			t.Errorf("Workflow with ID %s not found", expected.expectedID)
			continue
		}

		if wf.Name != expected.expectedName {
			t.Errorf("Workflow %s: expected name %q, got %q", expected.expectedID, expected.expectedName, wf.Name)
		}

		if wf.Description != expected.expectedDesc {
			t.Errorf("Workflow %s: expected description %q, got %q", expected.expectedID, expected.expectedDesc, wf.Description)
		}
	}
}

// TestHandleRepoOnlySpecTableDisplay tests that handleRepoOnlySpec displays workflows as a table
func TestHandleRepoOnlySpecTableDisplay(t *testing.T) {
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

	// Create a workflow file with metadata
	workflowContent := `---
on: push
name: CI Doctor
description: Diagnoses and fixes CI/CD pipeline issues
---

# CI Doctor Workflow
`

	wfPath := filepath.Join(workflowsDir, "ci-doctor.md")
	if err := os.WriteFile(wfPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to create workflow file: %v", err)
	}

	// Capture stderr output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call displayAvailableWorkflows (used by handleRepoOnlySpec)
	err := displayAvailableWorkflows("test-owner/test-repo", "", false)

	// Restore stderr and capture output
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	if err != nil {
		t.Fatalf("displayAvailableWorkflows failed: %v", err)
	}

	// Verify table-like output is present
	// Should contain header row with ID, Name, Description
	if !strings.Contains(output, "ID") {
		t.Errorf("Output should contain 'ID' header, got:\n%s", output)
	}

	if !strings.Contains(output, "Name") {
		t.Errorf("Output should contain 'Name' header, got:\n%s", output)
	}

	if !strings.Contains(output, "Description") {
		t.Errorf("Output should contain 'Description' header, got:\n%s", output)
	}

	// Should contain workflow data
	if !strings.Contains(output, "ci-doctor") {
		t.Errorf("Output should contain workflow ID 'ci-doctor', got:\n%s", output)
	}

	if !strings.Contains(output, "CI Doctor") {
		t.Errorf("Output should contain workflow name 'CI Doctor', got:\n%s", output)
	}

	if !strings.Contains(output, "Diagnoses and fixes CI/CD pipeline issues") {
		t.Errorf("Output should contain workflow description, got:\n%s", output)
	}

	// Should contain example command
	if !strings.Contains(output, "Example:") {
		t.Errorf("Output should contain 'Example:' section, got:\n%s", output)
	}

	if !strings.Contains(output, "gh aw add test-owner/test-repo/ci-doctor") {
		t.Errorf("Output should contain example command with workflow ID, got:\n%s", output)
	}
}

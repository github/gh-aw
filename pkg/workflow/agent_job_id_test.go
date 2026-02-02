//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestMainJobAlwaysHasAgentID verifies that the main job always gets the ID "agent"
// regardless of the workflow name
func TestMainJobAlwaysHasAgentID(t *testing.T) {
	// Create temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "agent-job-id-test*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name            string
		workflowContent string
		expectedJobName string
	}{
		{
			name: "simple_workflow_name",
			workflowContent: `---
on: workflow_dispatch
---

# Simple Workflow

This is a simple test workflow.`,
			expectedJobName: "agent",
		},
		{
			name: "complex_workflow_name_with_special_chars",
			workflowContent: `---
on: workflow_dispatch
---

# CI/CD: Pipeline Runner (v2.0) @main

This workflow has complex naming with special characters.`,
			expectedJobName: "agent",
		},
		{
			name: "workflow_with_safe_outputs",
			workflowContent: `---
on: workflow_dispatch
safe-outputs:
  create-issue:
---

# Test Workflow with Safe Outputs

Test workflow that creates issues.`,
			expectedJobName: "agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create workflow file
			workflowFile := filepath.Join(tmpDir, tt.name+".md")
			if err := os.WriteFile(workflowFile, []byte(tt.workflowContent), 0644); err != nil {
				t.Fatalf("Failed to create workflow file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			err := compiler.CompileWorkflow(workflowFile)
			if err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Read the generated lock file
			lockFile := stringutil.MarkdownToLockFile(workflowFile)
			lockContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read lock file: %v", err)
			}

			lockContentStr := string(lockContent)

			// Verify the main job has the expected name
			expectedJobLine := "  " + tt.expectedJobName + ":"
			if !strings.Contains(lockContentStr, expectedJobLine) {
				t.Errorf("Expected main job to have ID '%s', but job section not found", tt.expectedJobName)
				t.Logf("Lock file content:\n%s", lockContentStr)
			}

			// For workflows with safe-outputs, verify they reference the correct job name
			if strings.Contains(tt.workflowContent, "safe-outputs:") {
				expectedNeedsLine := "needs: " + tt.expectedJobName
				if !strings.Contains(lockContentStr, expectedNeedsLine) {
					t.Errorf("Safe output jobs should depend on '%s' job", tt.expectedJobName)
					t.Logf("Lock file content:\n%s", lockContentStr)
				}
			}

			// Clean up lock file
			os.Remove(lockFile)
		})
	}
}

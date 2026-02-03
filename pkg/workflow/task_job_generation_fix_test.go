//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestTaskJobGenerationFix tests that task job is only generated when required
func TestTaskJobGenerationFix(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "task-job-generation-test*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("no_task_job_for_safe_events_and_roles_all", func(t *testing.T) {
		// This workflow should now ALWAYS generate an activation job because:
		// 1. Activation jobs are now always emitted to perform timestamp checks
		// 2. Even with safe events and roles: all, we still want the timestamp check
		workflowContent := `---
on:
  workflow_dispatch:
roles: all
---

# Simple Workflow

This is a simple workflow that should not need a task job.
Do some simple work.`

		workflowFile := filepath.Join(tmpDir, "safe-workflow.md")
		err = os.WriteFile(workflowFile, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()
		if err := compiler.CompileWorkflow(workflowFile); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the generated lock file
		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify that activation job IS generated (new behavior - always emit activation job)
		if !strings.Contains(lockContentStr, "activation:") {
			t.Error("Expected activation job to be generated for timestamp check")
		}

		// Verify that the main job exists (should be named after the workflow)
		if !strings.Contains(lockContentStr, "jobs:") {
			t.Error("Expected jobs section to be present")
		}

		// Verify main job has "needs: activation" since activation job is now always generated
		if !strings.Contains(lockContentStr, "needs: activation") {
			t.Error("Main job should depend on activation job since activation job is now always generated")
		}

		// Verify activation job contains timestamp check
		if !strings.Contains(lockContentStr, "Check workflow file timestamps") {
			t.Error("Activation job should contain timestamp check step")
		}
	})

	t.Run("task_job_for_unsafe_events", func(t *testing.T) {
		// This workflow SHOULD generate a task job because:
		// 1. Uses unsafe events (push) which require permission checks
		workflowContent := `---
on:
  push:
    branches: [main]
---

# Unsafe Event Workflow

This workflow uses push events and should generate a task job for permission checks.
Do some work.`

		workflowFile := filepath.Join(tmpDir, "unsafe-workflow.md")
		err = os.WriteFile(workflowFile, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()
		if err := compiler.CompileWorkflow(workflowFile); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the generated lock file
		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify that activation job is generated
		if !strings.Contains(lockContentStr, "activation:") {
			t.Error("Expected activation job for unsafe events (push)")
		}

		// Verify main job depends on activation
		if !strings.Contains(lockContentStr, "needs: activation") {
			t.Error("Main job should depend on activation job when activation job is generated")
		}
	})

	t.Run("task_job_for_if_condition", func(t *testing.T) {
		// This workflow SHOULD generate a task job because:
		// 1. Has an if condition (even though events are safe and roles: all)
		workflowContent := `---
on:
  workflow_dispatch:
roles: all
if: ${{ github.ref == 'refs/heads/main' }}
---

# Conditional Workflow

This workflow has an if condition and should generate a task job.
Do conditional work.`

		workflowFile := filepath.Join(tmpDir, "conditional-workflow.md")
		err = os.WriteFile(workflowFile, []byte(workflowContent), 0644)
		if err != nil {
			t.Fatalf("Failed to write workflow file: %v", err)
		}

		// Compile the workflow
		compiler := NewCompiler()
		if err := compiler.CompileWorkflow(workflowFile); err != nil {
			t.Fatalf("Failed to compile workflow: %v", err)
		}

		// Read the generated lock file
		lockFile := stringutil.MarkdownToLockFile(workflowFile)
		lockContent, err := os.ReadFile(lockFile)
		if err != nil {
			t.Fatalf("Failed to read lock file: %v", err)
		}

		lockContentStr := string(lockContent)

		// Verify that activation job IS generated due to if condition
		if !strings.Contains(lockContentStr, "activation:") {
			t.Error("Expected activation job for workflow with if condition")
		}

		// Verify activation job has the if condition
		if !strings.Contains(lockContentStr, "if: ${{ github.ref == 'refs/heads/main' }}") {
			t.Error("Expected activation job to have the if condition")
		}

		// Verify main job depends on activation
		if !strings.Contains(lockContentStr, "needs: activation") {
			t.Error("Main job should depend on activation job when activation job is generated")
		}
	})
}

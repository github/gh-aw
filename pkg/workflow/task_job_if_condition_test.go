//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

func TestActivationJobWithIfConditionHasPlaceholderStep(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "task-job-if-test*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	workflowContent := `---
on:
  workflow_run:
    workflows: ["Daily Perf Improver", "Daily Test Coverage Improver"]
    types:
      - completed
  stop-after: +48h

if: ${{ github.event.workflow_run.conclusion == 'failure' }}
---

# CI Doctor

This workflow runs when CI workflows fail to help diagnose issues.

Check the failed workflow and provide analysis.`

	// Write the test workflow file
	workflowFile := filepath.Join(tmpDir, "test-workflow.md")
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

	// Test 1: Verify activation job exists
	if !strings.Contains(lockContentStr, "activation:") {
		t.Error("Expected activation job to be present in generated workflow")
	}

	// Test 2: Verify activation job has the user's if condition (may be combined with other conditions)
	if !strings.Contains(lockContentStr, "github.event.workflow_run.conclusion == 'failure'") {
		t.Error("Expected activation job to include the user's if condition")
	}

	// Test 2b: Verify activation job has the workflow_run event_name check (using !=)
	if !strings.Contains(lockContentStr, "github.event_name != 'workflow_run'") {
		t.Error("Expected activation job to include the event_name check with != operator")
	}

	// Test 2c: Verify activation job has the workflow_run repository safety check
	if !strings.Contains(lockContentStr, "github.event.workflow_run.repository.id == github.repository_id") {
		t.Error("Expected activation job to include the workflow_run repository safety check")
	}

	// Test 3: Verify activation job has steps (specifically the placeholder step)
	if !strings.Contains(lockContentStr, "steps:") {
		t.Error("Activation job should contain steps section")
	}

	// Test 4: Verify the timestamp check step is present (replaces placeholder step)
	if !strings.Contains(lockContentStr, "Check workflow file timestamps") {
		t.Error("Activation job should contain the timestamp check step")
	}

	// Test 5: Verify no name field is present for the step (we removed it)
	if strings.Contains(lockContentStr, "name: Task job condition barrier") {
		t.Error("Activation job should not contain the old step name")
	}

	// Test 6: Verify main job depends on activation job
	if !strings.Contains(lockContentStr, "needs: activation") {
		t.Error("Main job should depend on activation job")
	}

	// Test 7: Verify the generated YAML is valid (no empty activation job)
	// Check that there's no activation job section that has only "if:" and "runs-on:" without steps
	lines := strings.Split(lockContentStr, "\n")
	inTaskJob := false
	hasSteps := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "task:" {
			inTaskJob = true
			hasSteps = false
			continue
		}

		if inTaskJob {
			// If we hit another job at the same level, stop checking task job
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, " ") && strings.Contains(line, ":") {
				break
			}

			// Check if we found steps
			if strings.TrimSpace(line) == "steps:" {
				hasSteps = true
			}
		}
	}

	if inTaskJob && !hasSteps {
		t.Error("Task job must have steps to be valid GitHub Actions YAML")
	}
}

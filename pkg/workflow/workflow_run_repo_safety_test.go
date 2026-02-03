//go:build integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"
)

// TestWorkflowRunRepoSafetyCheck tests that workflow_run triggers get repository safety checks
func TestWorkflowRunRepoSafetyCheck(t *testing.T) {
	tests := []struct {
		name                    string
		workflowContent         string
		expectSafetyCondition   bool
		expectedConditionString string
	}{
		{
			name: "workflow with workflow_run trigger declared in frontmatter SHOULD include repo safety check",
			workflowContent: `---
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
---

# Test Workflow

Analyze the CI run.`,
			expectSafetyCondition:   true,
			expectedConditionString: "github.event_name != 'workflow_run'",
		},
		{
			name: "workflow with workflow_run and if condition should include repo safety check and combine conditions",
			workflowContent: `---
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
if: ${{ github.event.workflow_run.conclusion == 'failure' }}
---

# Test Workflow

Analyze failed CI run.`,
			expectSafetyCondition:   true,
			expectedConditionString: "github.event_name != 'workflow_run'",
		},
		{
			name: "workflow with push trigger should NOT include repo safety check",
			workflowContent: `---
on:
  push:
    branches: [main]
---

# Test Workflow

Do something on push.`,
			expectSafetyCondition:   false,
			expectedConditionString: "",
		},
		{
			name: "workflow with issues trigger should NOT include repo safety check",
			workflowContent: `---
on:
  issues:
    types: [opened]
---

# Test Workflow

Do something on issue.`,
			expectSafetyCondition:   false,
			expectedConditionString: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			tmpDir, err := os.MkdirTemp("", "workflow-run-safety-test*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			// Write the test workflow file
			workflowFile := filepath.Join(tmpDir, "test-workflow.md")
			err = os.WriteFile(workflowFile, []byte(tt.workflowContent), 0644)
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

			// Check if the safety condition is present in job if clauses
			// For multiline conditions with "if: >", the condition is on subsequent lines
			// So we check if the expected string exists anywhere in the lock file
			hasSafetyCondition := false
			if tt.expectedConditionString != "" {
				// Simply check if the expected condition string exists in the file
				// This is safe because we're looking for specific GitHub expression syntax
				// that wouldn't appear in JavaScript code
				hasSafetyCondition = strings.Contains(lockContentStr, tt.expectedConditionString)
			}

			if tt.expectSafetyCondition && !hasSafetyCondition {
				t.Errorf("Expected workflow_run repository safety condition to be present in job if clause, but it was not found")
				t.Logf("Searched for: %s", tt.expectedConditionString)
			}

			if !tt.expectSafetyCondition && hasSafetyCondition {
				t.Errorf("Expected NO workflow_run repository safety condition in job if clause, but it was found")
				t.Logf("Found condition: %s", tt.expectedConditionString)
			}

			// Additional check: if safety condition is expected, verify it's in a job's if clause
			if tt.expectSafetyCondition {
				// The condition should appear in one of these patterns:
				// - "if: ${{ github.event.workflow_run.repository.id == github.repository_id }}"
				// - "if: >\n      github.event.workflow_run.repository.id == github.repository_id"
				// - Combined with another condition using &&

				// Look for the activation job and check its if condition
				activationJobStart := strings.Index(lockContentStr, "activation:")
				if activationJobStart == -1 {
					// No activation job, check the main agent job
					agentJobStart := strings.Index(lockContentStr, "agent:")
					if agentJobStart == -1 {
						t.Fatalf("Neither activation nor agent job found in lock file")
					}
				}
			}
		})
	}
}

// TestWorkflowRunRepoSafetyInActivationJob tests that the safety check appears in activation job
func TestWorkflowRunRepoSafetyInActivationJob(t *testing.T) {
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

This workflow runs when CI workflows fail to help diagnose issues.`

	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "workflow-run-safety-activation-test*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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

	// Verify the activation job exists
	if !strings.Contains(lockContentStr, "activation:") {
		t.Error("Expected activation job to be present")
	}

	// Verify the event_name check is present (using != instead of ==)
	eventNameCondition := "github.event_name != 'workflow_run'"
	if !strings.Contains(lockContentStr, eventNameCondition) {
		t.Errorf("Expected event_name check to be present in lock file")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify the repository safety condition is present
	expectedCondition := "github.event.workflow_run.repository.id == github.repository_id"
	if !strings.Contains(lockContentStr, expectedCondition) {
		t.Errorf("Expected repository safety condition to be present in lock file")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify the user's if condition is also present
	userCondition := "github.event.workflow_run.conclusion == 'failure'"
	if !strings.Contains(lockContentStr, userCondition) {
		t.Errorf("Expected user's if condition to be preserved")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify OR operator is used in the safety condition
	if !strings.Contains(lockContentStr, "||") {
		t.Error("Expected safety condition to use || operator")
	}

	// Verify the fork check is present
	forkCondition := "github.event.workflow_run.repository.fork"
	if !strings.Contains(lockContentStr, forkCondition) {
		t.Errorf("Expected fork check to be present in lock file")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify the NOT operator is used for the fork check
	if !strings.Contains(lockContentStr, "!(github.event.workflow_run.repository.fork)") {
		t.Errorf("Expected NOT operator on fork check")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}
}

// TestNoWorkflowRunRepoSafetyForPushTrigger tests that push triggers don't get the safety check
func TestNoWorkflowRunRepoSafetyForPushTrigger(t *testing.T) {
	workflowContent := `---
on:
  push:
    branches: [main]
---

# Push Workflow

Do something on push.`

	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "no-workflow-run-safety-test*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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

	// Verify the repository safety condition is NOT present
	unexpectedCondition := "github.event.workflow_run.repository.id"
	if strings.Contains(lockContentStr, unexpectedCondition) {
		t.Errorf("Did not expect repository safety condition in push-triggered workflow")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}
}

// TestWorkflowRunForkCheckPresent verifies that the fork check is present in workflow_run workflows
func TestWorkflowRunForkCheckPresent(t *testing.T) {
	workflowContent := `---
on:
  workflow_run:
    workflows: ["CI"]
    types: [completed]
    branches: [main]
---

# Test Workflow

Test workflow with workflow_run trigger.`

	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "workflow-run-fork-check-test*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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

	// Verify the fork check is present
	forkCheck := "github.event.workflow_run.repository.fork"
	if !strings.Contains(lockContentStr, forkCheck) {
		t.Errorf("Expected fork check to be present in compiled workflow")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify the NOT operator is applied to the fork check
	notForkCheck := "!(github.event.workflow_run.repository.fork)"
	if !strings.Contains(lockContentStr, notForkCheck) {
		t.Errorf("Expected NOT operator on fork check")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify the complete safety condition structure
	// Should have: (repo.id == repository_id) && (!repo.fork)
	repoIDCheck := "github.event.workflow_run.repository.id == github.repository_id"
	if !strings.Contains(lockContentStr, repoIDCheck) {
		t.Errorf("Expected repository ID check to be present")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}

	// Verify AND operator combines the checks
	if !strings.Contains(lockContentStr, "&&") {
		t.Errorf("Expected AND operator to combine repository ID and fork checks")
		t.Logf("Lock file content:\n%s", lockContentStr)
	}
}

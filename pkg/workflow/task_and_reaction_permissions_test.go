//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/stringutil"

	"github.com/github/gh-aw/pkg/testutil"

	"github.com/github/gh-aw/pkg/constants"
)

func TestActivationAndAddReactionJobsPermissions(t *testing.T) {
	// Test that activation job has correct permissions when reaction is configured
	tmpDir := testutil.TempDir(t, "permissions-test")

	// Create a test workflow with reaction configured (reaction step is now in activation job)
	testContent := `---
on:
  issues:
    types: [opened]
  reaction: eyes
tools:
  github:
    allowed: [list_issues]
engine: claude
strict: false
---

# Test Workflow for Task and Add Reaction

This workflow should generate activation job with reaction permissions.

The activation job references text output: "${{ needs.activation.outputs.text }}"
`

	testFile := filepath.Join(tmpDir, "test-permissions.md")
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}

	compiler := NewCompiler()

	// Compile the workflow
	if err := compiler.CompileWorkflow(testFile); err != nil {
		t.Fatalf("Failed to compile workflow: %v", err)
	}

	// Calculate the lock file path
	lockFile := stringutil.MarkdownToLockFile(testFile)

	// Read the generated lock file
	lockContent, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatalf("Failed to read lock file: %v", err)
	}

	lockContentStr := string(lockContent)

	// Test 1: Verify activation job exists and has reaction permissions
	if !strings.Contains(lockContentStr, string(constants.ActivationJobName)+":") {
		t.Error("Expected activation job to be present in generated workflow")
	}

	// Test 2: Verify activation job behavior with local actions
	activationJobSection := extractJobSection(lockContentStr, string(constants.ActivationJobName))

	// In dev mode (default), activation job should checkout actions folder for setup action
	if !strings.Contains(activationJobSection, "Checkout actions folder") {
		t.Error("Activation job should checkout actions folder for local setup action in dev mode")
	}

	// Verify it uses GitHub API script for timestamp check (not repository checkout for that purpose)
	if !strings.Contains(activationJobSection, "check_workflow_timestamp_api.cjs") {
		t.Error("Activation job should use check_workflow_timestamp_api.cjs which calls GitHub API for timestamp check")
	}

	// Test 3: Verify activation job has contents: read permission for GitHub API access
	if !strings.Contains(activationJobSection, "contents: read") {
		t.Error("Activation job should have contents: read permission for GitHub API access")
	}

	// Test 4: Verify no separate add_reaction job exists
	if strings.Contains(lockContentStr, "add_reaction:") {
		t.Error("Expected no separate add_reaction job - reaction should be in activation job")
	}

	// Test 5: Verify activation job has required permissions for reactions
	if !strings.Contains(activationJobSection, "discussions: write") {
		t.Error("Activation job should have discussions: write permission")
	}
	if !strings.Contains(activationJobSection, "issues: write") {
		t.Error("Activation job should have issues: write permission")
	}
	if !strings.Contains(activationJobSection, "pull-requests: write") {
		t.Error("Activation job should have pull-requests: write permission")
	}

	// Test 6: Verify reaction step is in pre-activation job (moved for immediate feedback)
	preActivationJobSection := extractJobSection(lockContentStr, string(constants.PreActivationJobName))
	if !strings.Contains(preActivationJobSection, "Add eyes reaction for immediate feedback") {
		t.Error("Pre-activation job should contain the reaction step")
	}
}

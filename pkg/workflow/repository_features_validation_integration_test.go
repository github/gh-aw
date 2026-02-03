//go:build integration

package workflow

import (
	"os"
	"os/exec"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestRepositoryFeaturesValidationIntegration tests the repository features validation
// with actual GitHub API calls. This test requires:
// 1. Running in a git repository with GitHub remote
// 2. GitHub CLI (gh) authenticated
func TestRepositoryFeaturesValidationIntegration(t *testing.T) {
	// Check if gh CLI is available
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh CLI not available, skipping integration test")
	}

	// Check if gh CLI is authenticated
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		t.Skip("gh CLI not authenticated, skipping integration test")
	}

	// Get current repository
	repo, err := getCurrentRepository()
	if err != nil {
		t.Skip("Could not determine current repository, skipping integration test")
	}

	t.Logf("Testing with repository: %s", repo)

	// Test checking discussions
	t.Run("check_discussions", func(t *testing.T) {
		hasDiscussions, err := checkRepositoryHasDiscussions(repo, false)
		if err != nil {
			t.Errorf("Failed to check discussions: %v", err)
		}
		t.Logf("Repository %s has discussions enabled: %v", repo, hasDiscussions)
	})

	// Test checking issues
	t.Run("check_issues", func(t *testing.T) {
		hasIssues, err := checkRepositoryHasIssues(repo, false)
		if err != nil {
			t.Errorf("Failed to check issues: %v", err)
		}
		t.Logf("Repository %s has issues enabled: %v", repo, hasIssues)

		// Issues should be enabled for github/gh-aw
		if repo == "github/gh-aw" && !hasIssues {
			t.Error("Expected github/gh-aw to have issues enabled")
		}
	})

	// Test full validation with discussions
	t.Run("validate_with_discussions", func(t *testing.T) {
		workflowData := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{},
			},
		}

		compiler := NewCompiler()
		err := compiler.validateRepositoryFeatures(workflowData)

		// After the fix, validation should never return an error for discussions
		// It should only issue warnings and let runtime handle the actual creation
		if err != nil {
			t.Errorf("Expected no error (validation should only warn), got: %v", err)
		}

		// Log the discussion status for debugging
		hasDiscussions, checkErr := checkRepositoryHasDiscussions(repo, false)
		if checkErr != nil {
			t.Logf("Could not verify discussions status: %v", checkErr)
			return
		}
		t.Logf("Repository %s has discussions enabled: %v", repo, hasDiscussions)
	})

	// Test full validation with issues
	t.Run("validate_with_issues", func(t *testing.T) {
		workflowData := &WorkflowData{
			SafeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
		}

		compiler := NewCompiler()
		err := compiler.validateRepositoryFeatures(workflowData)

		hasIssues, checkErr := checkRepositoryHasIssues(repo, false)
		if checkErr != nil {
			t.Logf("Could not verify issues status: %v", checkErr)
			return
		}

		if hasIssues && err != nil {
			t.Errorf("Expected no error when issues are enabled, got: %v", err)
		} else if !hasIssues && err == nil {
			t.Error("Expected error when issues are disabled, got none")
		}
	})
}

// TestCompileWorkflowWithRepositoryFeatureValidation tests compiling a workflow
// that requires repository features
func TestCompileWorkflowWithRepositoryFeatureValidation(t *testing.T) {
	// Check if gh CLI is available and authenticated
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh CLI not available, skipping integration test")
	}

	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		t.Skip("gh CLI not authenticated, skipping integration test")
	}

	// Get current repository
	repo, err := getCurrentRepository()
	if err != nil {
		t.Skip("Could not determine current repository, skipping integration test")
	}

	// Create a temporary workflow with create-discussion
	tempDir := testutil.TempDir(t, "test-*")
	workflowPath := tempDir + "/test-discussion.md"

	workflowContent := `---
on:
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
safe-outputs:
  create-discussion:
    category: "general"
---

# Test Discussion Workflow

Test workflow for discussions validation.
`

	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		t.Fatalf("Failed to write test workflow: %v", err)
	}

	// Try to compile the workflow
	compiler := NewCompiler()
	compiler.SetNoEmit(true) // Don't write lock file

	err = compiler.CompileWorkflow(workflowPath)

	// After the fix, compilation should always succeed for discussions
	// Validation now only issues warnings and lets runtime handle creation attempts
	if err != nil {
		t.Errorf("Expected compilation to succeed (validation should only warn), got error: %v", err)
	}

	// Log the discussion status for debugging
	hasDiscussions, checkErr := checkRepositoryHasDiscussions(repo, false)
	if checkErr != nil {
		t.Logf("Could not verify discussions status: %v", checkErr)
		return
	}
	t.Logf("Repository %s has discussions enabled: %v", repo, hasDiscussions)
}

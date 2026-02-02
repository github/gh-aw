//go:build integration

package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestAddWorkflowsFromCurrentRepository tests that adding workflows from the current repository is prevented
func TestAddWorkflowsFromCurrentRepository(t *testing.T) {
	// Create a temporary git repository to simulate being in a repository
	tempDir := testutil.TempDir(t, "test-*")

	// Initialize a proper git repository
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Add a remote origin
	remoteCmd := exec.Command("git", "remote", "add", "origin", "https://github.com/test-owner/test-repo.git")
	remoteCmd.Dir = tempDir
	if err := remoteCmd.Run(); err != nil {
		t.Fatalf("Failed to add remote origin: %v", err)
	}

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Clear the repository slug cache to ensure fresh lookup
	ClearCurrentRepoSlugCache()

	tests := []struct {
		name          string
		workflowSpecs []string
		expectError   bool
		errorContains string
	}{
		{
			name:          "prevent adding workflow from current repository",
			workflowSpecs: []string{"test-owner/test-repo/my-workflow"},
			expectError:   true,
			errorContains: "cannot add workflows from the current repository",
		},
		{
			name:          "allow adding workflow from different repository",
			workflowSpecs: []string{"different-owner/different-repo/workflow"},
			expectError:   false, // This will still fail because package doesn't exist, but not due to current repo check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache before each test
			ClearCurrentRepoSlugCache()

			_, err := AddWorkflows(tt.workflowSpecs, 1, false, "", "", false, "", false, false, false, "", false, "")

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				// For "allow" case, we expect a different error (workflow not found, not current repo error)
				if err != nil && strings.Contains(err.Error(), "cannot add workflows from the current repository") {
					t.Errorf("Should not get current repository error for different repository, got: %v", err)
				}
			}
		})
	}
}

// TestAddWorkflowsFromCurrentRepositoryMultiple tests prevention for multiple workflows
func TestAddWorkflowsFromCurrentRepositoryMultiple(t *testing.T) {
	// Create a temporary git repository to simulate being in a repository
	tempDir := testutil.TempDir(t, "test-*")

	// Initialize a proper git repository
	initCmd := exec.Command("git", "init")
	initCmd.Dir = tempDir
	if err := initCmd.Run(); err != nil {
		t.Fatalf("Failed to initialize git repository: %v", err)
	}

	// Add a remote origin with SSH format
	remoteCmd := exec.Command("git", "remote", "add", "origin", "git@github.com:myorg/myrepo.git")
	remoteCmd.Dir = tempDir
	if err := remoteCmd.Run(); err != nil {
		t.Fatalf("Failed to add remote origin: %v", err)
	}

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Clear the repository slug cache
	ClearCurrentRepoSlugCache()

	tests := []struct {
		name          string
		workflowSpecs []string
		expectError   bool
		errorContains string
	}{
		{
			name: "prevent when first workflow is from current repository",
			workflowSpecs: []string{
				"myorg/myrepo/workflow1",
				"otherorg/otherrepo/workflow2",
			},
			expectError:   true,
			errorContains: "cannot add workflows from the current repository",
		},
		{
			name: "allow when all workflows are from different repositories",
			workflowSpecs: []string{
				"org1/repo1/workflow1",
				"org2/repo2/workflow2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear cache before each test
			ClearCurrentRepoSlugCache()

			_, err := AddWorkflows(tt.workflowSpecs, 1, false, "", "", false, "", false, false, false, "", false, "")

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorContains, err)
				}
			} else {
				// For "allow" case, we expect a different error (workflow not found, not current repo error)
				if err != nil && strings.Contains(err.Error(), "cannot add workflows from the current repository") {
					t.Errorf("Should not get current repository error for different repository, got: %v", err)
				}
			}
		})
	}
}

// TestAddWorkflowsFromCurrentRepositoryNotInGitRepo tests behavior when not in a git repository
func TestAddWorkflowsFromCurrentRepositoryNotInGitRepo(t *testing.T) {
	// Create a temporary directory without .git
	tempDir := testutil.TempDir(t, "test-*")

	// Change to the temp directory
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(oldDir)

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Clear the repository slug cache
	ClearCurrentRepoSlugCache()

	// When not in a git repo, the check should be skipped (can't determine current repo)
	// The function should proceed and fail for other reasons (e.g., workflow not found)
	_, err = AddWorkflows([]string{"some-owner/some-repo/workflow"}, 1, false, "", "", false, "", false, false, false, "", false, "")

	// Should NOT get the "cannot add workflows from the current repository" error
	if err != nil && strings.Contains(err.Error(), "cannot add workflows from the current repository") {
		t.Errorf("Should not check current repository when not in a git repo, got: %v", err)
	}
}

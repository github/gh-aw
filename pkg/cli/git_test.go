//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// Note: The following tests exist in other test files and are not duplicated here:
// - TestIsGitRepo is in commands_utils_test.go (tests isGitRepo utility)
// - TestFindGitRoot is in gitroot_test.go (tests findGitRoot utility)
// - TestEnsureGitAttributes is in gitattributes_test.go (comprehensive gitattributes tests)
//
// Note: The following tests remain in commands_compile_workflow_test.go because they test
// compile-specific workflow behavior, not just Git operations:
// - TestStageWorkflowChanges (tests staging behavior during workflow compilation)
// - TestStageGitAttributesIfChanged (tests conditional staging during compilation)

func TestGetCurrentBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git user for commits
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create initial commit to establish branch
	if err := os.WriteFile("test.txt", []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	exec.Command("git", "add", "test.txt").Run()
	if err := exec.Command("git", "commit", "-m", "Initial commit").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Get current branch
	branch, err := getCurrentBranch()
	if err != nil {
		t.Fatalf("getCurrentBranch() failed: %v", err)
	}

	// Should be on main or master branch
	if branch != "main" && branch != "master" {
		t.Logf("Note: branch name is %q (expected 'main' or 'master')", branch)
	}

	// Verify it's not empty
	if branch == "" {
		t.Error("getCurrentBranch() returned empty branch name")
	}
}

func TestGetCurrentBranchNotInRepo(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Don't initialize git - should error
	_, err = getCurrentBranch()
	if err == nil {
		t.Error("getCurrentBranch() should return error when not in git repo")
	}
}

func TestCreateAndSwitchBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create initial commit
	if err := os.WriteFile("test.txt", []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	exec.Command("git", "add", "test.txt").Run()
	if err := exec.Command("git", "commit", "-m", "Initial commit").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Create and switch to new branch
	branchName := "test-branch"
	err = createAndSwitchBranch(branchName, false)
	if err != nil {
		t.Fatalf("createAndSwitchBranch() failed: %v", err)
	}

	// Verify we're on the new branch
	currentBranch, err := getCurrentBranch()
	if err != nil {
		t.Fatalf("getCurrentBranch() failed: %v", err)
	}

	if currentBranch != branchName {
		t.Errorf("Expected to be on branch %q, got %q", branchName, currentBranch)
	}
}

func TestSwitchBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create initial commit
	if err := os.WriteFile("test.txt", []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	exec.Command("git", "add", "test.txt").Run()
	if err := exec.Command("git", "commit", "-m", "Initial commit").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Get initial branch name
	initialBranch, err := getCurrentBranch()
	if err != nil {
		t.Fatalf("getCurrentBranch() failed: %v", err)
	}

	// Create a new branch
	newBranch := "feature-branch"
	if err := exec.Command("git", "checkout", "-b", newBranch).Run(); err != nil {
		t.Fatalf("Failed to create new branch: %v", err)
	}

	// Switch back to initial branch
	err = switchBranch(initialBranch, false)
	if err != nil {
		t.Fatalf("switchBranch() failed: %v", err)
	}

	// Verify we're on the initial branch
	currentBranch, err := getCurrentBranch()
	if err != nil {
		t.Fatalf("getCurrentBranch() failed: %v", err)
	}

	if currentBranch != initialBranch {
		t.Errorf("Expected to be on branch %q, got %q", initialBranch, currentBranch)
	}
}

func TestCommitChanges(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create and stage a file
	if err := os.WriteFile("test.txt", []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := exec.Command("git", "add", "test.txt").Run(); err != nil {
		t.Fatalf("Failed to stage file: %v", err)
	}

	// Commit changes
	commitMessage := "Test commit"
	err = commitChanges(commitMessage, false)
	if err != nil {
		t.Fatalf("commitChanges() failed: %v", err)
	}

	// Verify commit was created
	cmd := exec.Command("git", "log", "--oneline", "-1")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git log: %v", err)
	}

	if !strings.Contains(string(output), commitMessage) {
		t.Errorf("Expected commit message %q not found in git log", commitMessage)
	}
}

// Note: TestStageWorkflowChanges is in commands_compile_workflow_test.go
// Note: TestStageGitAttributesIfChanged is in commands_compile_workflow_test.go

func TestPushBranchNotImplemented(t *testing.T) {
	// This test verifies the function signature exists
	// We skip actual push testing as it requires remote repository setup
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// pushBranch will fail without a remote, which is expected
	err = pushBranch("test-branch", false)
	if err == nil {
		t.Log("pushBranch() succeeded unexpectedly (might have remote configured)")
	}
	// We expect this to fail in test environment, which is fine
}

func TestCheckWorkflowFileStatus(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create .github/workflows directory
	workflowDir := ".github/workflows"
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		t.Fatalf("Failed to create workflow directory: %v", err)
	}

	workflowFile := ".github/workflows/test.md"

	// Test 1: File doesn't exist - should return empty status
	t.Run("file_not_tracked", func(t *testing.T) {
		status, err := checkWorkflowFileStatus(workflowFile)
		if err != nil {
			t.Fatalf("checkWorkflowFileStatus() failed: %v", err)
		}
		if status.IsModified || status.IsStaged || status.HasUnpushedCommits {
			t.Error("Expected empty status for untracked file")
		}
	})

	// Create and commit a workflow file
	if err := os.WriteFile(workflowFile, []byte("# Test Workflow\n"), 0644); err != nil {
		t.Fatalf("Failed to create workflow file: %v", err)
	}
	exec.Command("git", "add", workflowFile).Run()
	if err := exec.Command("git", "commit", "-m", "Add workflow").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Test 2: Clean file - no changes
	t.Run("clean_file", func(t *testing.T) {
		status, err := checkWorkflowFileStatus(workflowFile)
		if err != nil {
			t.Fatalf("checkWorkflowFileStatus() failed: %v", err)
		}
		if status.IsModified || status.IsStaged || status.HasUnpushedCommits {
			t.Error("Expected empty status for clean file")
		}
	})

	// Test 3: Modified file (unstaged changes)
	t.Run("modified_file", func(t *testing.T) {
		if err := os.WriteFile(workflowFile, []byte("# Modified Workflow\n"), 0644); err != nil {
			t.Fatalf("Failed to modify workflow file: %v", err)
		}

		status, err := checkWorkflowFileStatus(workflowFile)
		if err != nil {
			t.Fatalf("checkWorkflowFileStatus() failed: %v", err)
		}

		if !status.IsModified {
			t.Error("Expected IsModified to be true for modified file")
		}
		if status.IsStaged {
			t.Error("Expected IsStaged to be false for unstaged file")
		}

		// Clean up - restore file
		exec.Command("git", "checkout", workflowFile).Run()
	})

	// Test 4: Staged file
	t.Run("staged_file", func(t *testing.T) {
		if err := os.WriteFile(workflowFile, []byte("# Staged Workflow\n"), 0644); err != nil {
			t.Fatalf("Failed to modify workflow file: %v", err)
		}
		exec.Command("git", "add", workflowFile).Run()

		status, err := checkWorkflowFileStatus(workflowFile)
		if err != nil {
			t.Fatalf("checkWorkflowFileStatus() failed: %v", err)
		}

		if !status.IsStaged {
			t.Error("Expected IsStaged to be true for staged file")
		}

		// Clean up - unstage and restore file
		exec.Command("git", "reset", "HEAD", workflowFile).Run()
		exec.Command("git", "checkout", workflowFile).Run()
	})

	// Test 5: Both staged and modified
	t.Run("staged_and_modified", func(t *testing.T) {
		// Modify and stage
		if err := os.WriteFile(workflowFile, []byte("# Staged content\n"), 0644); err != nil {
			t.Fatalf("Failed to modify workflow file: %v", err)
		}
		exec.Command("git", "add", workflowFile).Run()

		// Modify again (unstaged change)
		if err := os.WriteFile(workflowFile, []byte("# Staged and modified\n"), 0644); err != nil {
			t.Fatalf("Failed to modify workflow file again: %v", err)
		}

		status, err := checkWorkflowFileStatus(workflowFile)
		if err != nil {
			t.Fatalf("checkWorkflowFileStatus() failed: %v", err)
		}

		if !status.IsStaged {
			t.Error("Expected IsStaged to be true")
		}
		if !status.IsModified {
			t.Error("Expected IsModified to be true")
		}

		// Clean up - unstage and restore file
		exec.Command("git", "reset", "HEAD", workflowFile).Run()
		exec.Command("git", "checkout", workflowFile).Run()
	})
}

func TestCheckWorkflowFileStatusNotInRepo(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Don't initialize git - should return empty status without error
	status, err := checkWorkflowFileStatus("test.md")
	if err != nil {
		t.Fatalf("checkWorkflowFileStatus() failed: %v", err)
	}

	// Should return empty status for non-git directory
	if status.IsModified || status.IsStaged || status.HasUnpushedCommits {
		t.Error("Expected empty status when not in git repository")
	}
}

func TestToGitRootRelativePath(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Create .github directory structure
	githubDir := tmpDir + "/.github/workflows"
	if err := os.MkdirAll(githubDir, 0755); err != nil {
		t.Fatalf("Failed to create .github directory: %v", err)
	}

	// Create a test file in .github/workflows
	testFile := githubDir + "/test.campaign.md"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Create a subdirectory for testing from different working directories
	subDir := tmpDir + "/pkg/cli"
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	t.Run("from_root_directory", func(t *testing.T) {
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to root directory: %v", err)
		}

		result := ToGitRootRelativePath(testFile)
		expected := ".github/workflows/test.campaign.md"
		if result != expected {
			t.Errorf("Expected %q, got %q", expected, result)
		}
	})

	t.Run("from_subdirectory", func(t *testing.T) {
		if err := os.Chdir(subDir); err != nil {
			t.Fatalf("Failed to change to subdirectory: %v", err)
		}

		result := ToGitRootRelativePath(testFile)
		expected := ".github/workflows/test.campaign.md"
		if result != expected {
			t.Errorf("Expected %q, got %q", expected, result)
		}
	})

	t.Run("with_relative_path_from_root", func(t *testing.T) {
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to root directory: %v", err)
		}

		relativePath := ".github/workflows/test.campaign.md"
		result := ToGitRootRelativePath(relativePath)
		expected := ".github/workflows/test.campaign.md"
		if result != expected {
			t.Errorf("Expected %q, got %q", expected, result)
		}
	})

	t.Run("with_relative_path_from_subdirectory", func(t *testing.T) {
		if err := os.Chdir(subDir); err != nil {
			t.Fatalf("Failed to change to subdirectory: %v", err)
		}

		// Relative path from subdirectory
		relativePath := "../../.github/workflows/test.campaign.md"
		result := ToGitRootRelativePath(relativePath)
		expected := ".github/workflows/test.campaign.md"
		if result != expected {
			t.Errorf("Expected %q, got %q", expected, result)
		}
	})

	t.Run("stability_across_directories", func(t *testing.T) {
		// Test from root
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("Failed to change to root directory: %v", err)
		}
		result1 := ToGitRootRelativePath(testFile)

		// Test from subdirectory
		if err := os.Chdir(subDir); err != nil {
			t.Fatalf("Failed to change to subdirectory: %v", err)
		}
		result2 := ToGitRootRelativePath(testFile)

		// Results should be identical
		if result1 != result2 {
			t.Errorf("Path not stable across directories: from root=%q, from subdir=%q", result1, result2)
		}

		expected := ".github/workflows/test.campaign.md"
		if result1 != expected {
			t.Errorf("Expected %q, got %q", expected, result1)
		}
	})
}

func TestToGitRootRelativePathWithoutGithubDir(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Create a subdirectory but NO .github directory
	subDir := tmpDir + "/subdir"
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}

	testFile := subDir + "/test.md"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Should fall back to relative path from current working directory
	result := ToGitRootRelativePath(testFile)

	// Should return a relative path (not absolute)
	if strings.HasPrefix(result, "/") || (len(result) > 2 && result[1] == ':') {
		t.Errorf("Expected relative path fallback, got absolute path: %q", result)
	}

	// Should contain the filename
	if !strings.Contains(result, "test.md") {
		t.Errorf("Expected result to contain filename, got: %q", result)
	}
}

func TestToGitRootRelativePathWithNestedGithubDir(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	// Create nested structure: root/.github and root/subproject/.github
	rootGithubDir := tmpDir + "/.github/workflows"
	if err := os.MkdirAll(rootGithubDir, 0755); err != nil {
		t.Fatalf("Failed to create root .github directory: %v", err)
	}

	subprojectGithubDir := tmpDir + "/subproject/.github/workflows"
	if err := os.MkdirAll(subprojectGithubDir, 0755); err != nil {
		t.Fatalf("Failed to create subproject .github directory: %v", err)
	}

	// Create test file in subproject
	testFile := subprojectGithubDir + "/test.md"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Change to subproject directory
	subprojectDir := tmpDir + "/subproject"
	if err := os.Chdir(subprojectDir); err != nil {
		t.Fatalf("Failed to change to subproject directory: %v", err)
	}

	// Should find the closest .github directory (in subproject)
	result := ToGitRootRelativePath(testFile)
	expected := ".github/workflows/test.md"
	if result != expected {
		t.Errorf("Expected %q, got %q (should use closest .github)", expected, result)
	}
}

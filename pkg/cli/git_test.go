//go:build !integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestIsSafeGitRevisionArg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ref  string
		want bool
	}{
		{"empty", "", false},
		{"leading dash", "-oops", false},
		{"leading double dash", "--upload-pack=evil", false},
		{"newline", "origin/main\n--help", false},
		{"unicode control character", "origin/main\u202e", false},
		{"plain branch", "main", true},
		{"remote branch", "origin/main", true},
		{"contains dash not leading", "feature-branch", true},
		{"short sha", "abc1234", true},
		{"fully qualified ref", "refs/heads/main", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isSafeGitRevisionArg(tt.ref))
		})
	}
}

func TestValidateRelPathForGit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		relPath string
		wantErr bool
	}{
		{"empty", "", true},
		{"leading dash", "-oops", true},
		{"leading double dash flag", "--upload-pack=evil", true},
		{"parent traversal", "..", true},
		{"parent traversal with subpath", "../secret.txt", true},
		{"nested parent traversal", "sub/../../secret.txt", true},
		{"absolute path", "/etc/passwd", true},
		{"plain relative path", "workflow.md", false},
		{"nested relative path", ".github/workflows/workflow.md", false},
		{"dot prefixed but within repo", "./workflow.md", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRelPathForGit(tt.relPath)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetCurrentBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git user for commits
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create initial commit to establish branch
	require.NoError(t, os.WriteFile("test.txt", []byte("test"), 0644), "create initial test file")
	exec.Command("git", "add", "test.txt").Run()
	if err := exec.Command("git", "commit", "-m", "Initial commit").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Get current branch
	branch, err := getCurrentBranch()
	require.NoError(t, err, "get current branch in git repository")

	// Should be on main or master branch
	if branch != "main" && branch != "master" {
		t.Logf("Note: branch name is %q (expected 'main' or 'master')", branch)
	}

	// Verify it's not empty
	assert.NotEmpty(t, branch, "getCurrentBranch should return a non-empty branch name")
}

func TestGetCurrentBranchNotInRepo(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Don't initialize git - should error
	_, err = getCurrentBranch()
	assert.Error(t, err, "getCurrentBranch should return an error when not in a git repository")
}

func TestCheckCleanWorkingDirectoryIgnoring(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(originalDir))
	}()

	require.NoError(t, os.Chdir(tmpDir))
	require.NoError(t, exec.Command("git", "init").Run())
	require.NoError(t, exec.Command("git", "config", "user.name", "Test User").Run())
	require.NoError(t, exec.Command("git", "config", "user.email", "test@example.com").Run())

	generatedFile := filepath.Join(".github", "skills", "agentic-workflows", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(generatedFile), 0755))
	require.NoError(t, os.WriteFile(generatedFile, []byte("generated"), 0644))

	require.NoError(t, checkCleanWorkingDirectoryIgnoring(false, []string{generatedFile}))
	require.ErrorContains(t, checkCleanWorkingDirectory(false), "working directory has uncommitted changes")

	// Staged (but not committed) init file should also be excluded.
	require.NoError(t, exec.Command("git", "add", generatedFile).Run())
	require.NoError(t, checkCleanWorkingDirectoryIgnoring(false, []string{generatedFile}))
	require.ErrorContains(t, checkCleanWorkingDirectory(false), "working directory has uncommitted changes")

	require.NoError(t, exec.Command("git", "commit", "-m", "initial commit").Run())
	require.NoError(t, os.WriteFile(generatedFile, []byte("updated"), 0644))

	require.NoError(t, checkCleanWorkingDirectoryIgnoring(false, []string{generatedFile}))
	require.ErrorContains(t, checkCleanWorkingDirectory(false), "working directory has uncommitted changes")

	require.NoError(t, os.WriteFile("README.md", []byte("user file"), 0644))
	require.ErrorContains(
		t,
		checkCleanWorkingDirectoryIgnoring(false, []string{generatedFile}),
		"working directory has uncommitted changes",
	)
}

// TestCheckCleanWorkingDirectoryIgnoringAbsolutePaths verifies that absolute
// paths are accepted when the current directory is a subdirectory of the repo.
// This is the case when the wizard is invoked from a nested directory and
// ensureAddRepositoryInitializedWithDetails returns absolute paths.
func TestCheckCleanWorkingDirectoryIgnoringAbsolutePaths(t *testing.T) {
	repoDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(originalDir))
	}()

	require.NoError(t, os.Chdir(repoDir))
	require.NoError(t, exec.Command("git", "init").Run())
	require.NoError(t, exec.Command("git", "config", "user.name", "Test User").Run())
	require.NoError(t, exec.Command("git", "config", "user.email", "test@example.com").Run())

	// Create the init file at the repo root.
	generatedFile := filepath.Join(".github", "skills", "agentic-workflows", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(generatedFile), 0755))
	require.NoError(t, os.WriteFile(generatedFile, []byte("generated"), 0644))
	absGenerated := filepath.Join(repoDir, generatedFile)

	// Create a subdirectory and cd into it to simulate a nested invocation.
	subDir := filepath.Join(repoDir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.Chdir(subDir))

	// Passing the absolute path from a nested CWD should still exclude the file.
	require.NoError(t, checkCleanWorkingDirectoryIgnoring(false, []string{absGenerated}))
	require.ErrorContains(t, checkCleanWorkingDirectory(false), "working directory has uncommitted changes")

	// An unrelated untracked file must still be detected even when the init file is excluded.
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("user file"), 0644))
	require.ErrorContains(
		t,
		checkCleanWorkingDirectoryIgnoring(false, []string{absGenerated}),
		"working directory has uncommitted changes",
	)
}

func TestCreateAndSwitchBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create initial commit
	require.NoError(t, os.WriteFile("test.txt", []byte("test"), 0644), "create initial test file")
	exec.Command("git", "add", "test.txt").Run()
	if err := exec.Command("git", "commit", "-m", "Initial commit").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Create and switch to new branch
	branchName := "test-branch"
	err = createAndSwitchBranch(branchName, false)
	require.NoError(t, err, "create and switch to new branch")

	// Verify we're on the new branch
	currentBranch, err := getCurrentBranch()
	require.NoError(t, err, "get current branch after branch switch")
	assert.Equal(t, branchName, currentBranch, "current branch should match the newly created branch")
}

func TestSwitchBranch(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create initial commit
	require.NoError(t, os.WriteFile("test.txt", []byte("test"), 0644), "create initial test file")
	exec.Command("git", "add", "test.txt").Run()
	if err := exec.Command("git", "commit", "-m", "Initial commit").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Get initial branch name
	initialBranch, err := getCurrentBranch()
	require.NoError(t, err, "get initial branch")

	// Create a new branch
	newBranch := "feature-branch"
	require.NoError(t, exec.Command("git", "checkout", "-b", newBranch).Run(), "create a new branch for switch testing")

	// Switch back to initial branch
	err = switchBranch(initialBranch, false)
	require.NoError(t, err, "switch back to the initial branch")

	// Verify we're on the initial branch
	currentBranch, err := getCurrentBranch()
	require.NoError(t, err, "get current branch after switching back")
	assert.Equal(t, initialBranch, currentBranch, "current branch should match the original branch")
}

func TestCommitChanges(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create and stage a file
	require.NoError(t, os.WriteFile("test.txt", []byte("test content"), 0644), "create test file")
	require.NoError(t, exec.Command("git", "add", "test.txt").Run(), "stage test file")

	// Commit changes
	commitMessage := "Test commit"
	err = commitChanges(commitMessage, false)
	require.NoError(t, err, "commit staged changes")

	// Verify commit was created
	cmd := exec.Command("git", "log", "--oneline", "-1")
	output, err := cmd.Output()
	require.NoError(t, err, "read latest git log entry")
	assert.Contains(t, string(output), commitMessage, "latest git log entry should contain the commit message")
}

// Note: TestStageWorkflowChanges is in commands_compile_workflow_test.go
// Note: TestStageGitAttributesIfChanged is in commands_compile_workflow_test.go

func TestPushBranchNotImplemented(t *testing.T) {
	// This test verifies the function signature exists
	// We skip actual push testing as it requires remote repository setup
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

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
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	// Configure git
	exec.Command("git", "config", "user.name", "Test User").Run()
	exec.Command("git", "config", "user.email", "test@example.com").Run()

	// Create .github/workflows directory
	workflowDir := ".github/workflows"
	require.NoError(t, os.MkdirAll(workflowDir, 0755), "create workflow directory")

	workflowFile := ".github/workflows/test.md"

	// Test 1: File doesn't exist - should return empty status
	t.Run("file_not_tracked", func(t *testing.T) {
		status, err := checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "get workflow file status for untracked file")
		assert.False(t, status.IsModified, "untracked file should not be marked modified")
		assert.False(t, status.IsStaged, "untracked file should not be marked staged")
		assert.False(t, status.HasUnpushedCommits, "untracked file should not report unpushed commits")
	})

	// Create and commit a workflow file
	require.NoError(t, os.WriteFile(workflowFile, []byte("# Test Workflow\n"), 0644), "create workflow file")
	exec.Command("git", "add", workflowFile).Run()
	if err := exec.Command("git", "commit", "-m", "Add workflow").Run(); err != nil {
		t.Skip("Failed to create initial commit")
	}

	// Test 2: Clean file - no changes
	t.Run("clean_file", func(t *testing.T) {
		status, err := checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "get workflow file status for clean file")
		assert.False(t, status.IsModified, "clean file should not be marked modified")
		assert.False(t, status.IsStaged, "clean file should not be marked staged")
		assert.False(t, status.HasUnpushedCommits, "clean file should not report unpushed commits")
	})

	// Test 3: Configured upstream with local-only commits affecting the file
	t.Run("configured_upstream_file_has_unpushed_commits", func(t *testing.T) {
		remoteDir := filepath.Join(tmpDir, "origin.git")
		require.NoError(t, exec.Command("git", "init", "--bare", remoteDir).Run(), "create bare remote repo")
		require.NoError(t, exec.Command("git", "remote", "add", "origin", remoteDir).Run(), "configure upstream remote")
		require.NoError(t, exec.Command("git", "push", "-u", "origin", "HEAD").Run(), "push initial branch to remote")

		// A commit that doesn't touch the workflow should not trigger an unpushed-commit result.
		unrelatedFile := "README.md"
		require.NoError(t, os.WriteFile(unrelatedFile, []byte("notes\n"), 0644), "create unrelated file")
		require.NoError(t, exec.Command("git", "add", unrelatedFile).Run())
		require.NoError(t, exec.Command("git", "commit", "-m", "docs: add unrelated notes").Run(), "commit unrelated change")

		status, err := checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "check workflow file status against configured upstream")
		assert.False(t, status.HasUnpushedCommits, "unrelated commit should not count for workflow file")

		// A commit that does affect the workflow should be reported when ahead of upstream.
		require.NoError(t, os.WriteFile(workflowFile, []byte("# Updated Workflow\n"), 0644), "modify workflow file")
		require.NoError(t, exec.Command("git", "add", workflowFile).Run())
		require.NoError(t, exec.Command("git", "commit", "-m", "chore: update workflow").Run(), "commit workflow change")

		status, err = checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "check workflow file status after local workflow commit")
		assert.True(t, status.HasUnpushedCommits, "workflow change ahead of upstream should be reported")
	})

	// Test 4: Modified file (unstaged changes)
	t.Run("modified_file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(workflowFile, []byte("# Modified Workflow\n"), 0644), "modify workflow file")

		status, err := checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "get workflow file status for modified file")
		assert.True(t, status.IsModified, "modified file should be marked modified")
		assert.False(t, status.IsStaged, "unstaged modified file should not be marked staged")

		// Clean up - restore file
		exec.Command("git", "checkout", workflowFile).Run()
	})

	// Test 4: Staged file
	t.Run("staged_file", func(t *testing.T) {
		require.NoError(t, os.WriteFile(workflowFile, []byte("# Staged Workflow\n"), 0644), "modify workflow file before staging")
		exec.Command("git", "add", workflowFile).Run()

		status, err := checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "get workflow file status for staged file")
		assert.True(t, status.IsStaged, "staged file should be marked staged")

		// Clean up - unstage and restore file
		exec.Command("git", "reset", "HEAD", workflowFile).Run()
		exec.Command("git", "checkout", workflowFile).Run()
	})

	// Test 5: Both staged and modified
	t.Run("staged_and_modified", func(t *testing.T) {
		// Modify and stage
		require.NoError(t, os.WriteFile(workflowFile, []byte("# Staged content\n"), 0644), "write staged workflow content")
		exec.Command("git", "add", workflowFile).Run()

		// Modify again (unstaged change)
		require.NoError(t, os.WriteFile(workflowFile, []byte("# Staged and modified\n"), 0644), "write unstaged workflow content")

		status, err := checkWorkflowFileStatus(workflowFile)
		require.NoError(t, err, "get workflow file status for staged and modified file")
		assert.True(t, status.IsStaged, "staged-and-modified file should be marked staged")
		assert.True(t, status.IsModified, "staged-and-modified file should be marked modified")

		// Clean up - unstage and restore file
		exec.Command("git", "reset", "HEAD", workflowFile).Run()
		exec.Command("git", "checkout", workflowFile).Run()
	})
}

func TestCheckWorkflowFileStatusNotInRepo(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Don't initialize git - should return empty status without error
	status, err := checkWorkflowFileStatus("test.md")
	require.NoError(t, err, "get workflow file status outside a git repository")
	assert.False(t, status.IsModified, "non-git directory should not report a modified workflow file")
	assert.False(t, status.IsStaged, "non-git directory should not report a staged workflow file")
	assert.False(t, status.HasUnpushedCommits, "non-git directory should not report unpushed commits")
}

func TestExtractHostFromRemoteURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS with embedded username (Windows-style)",
			url:      "https://bryanknox@github.com/owner/repo.git",
			expected: "github.com",
		},
		{
			name:     "HTTPS with embedded username on GHES",
			url:      "https://user@ghes.example.com/org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "HTTP with embedded username",
			url:      "http://user@ghes.example.com/org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "HTTPS with embedded username and password",
			url:      "https://user:pass@github.com/owner/repo.git",
			expected: "github.com",
		},
		{
			name:     "public GitHub HTTPS",
			url:      "https://github.com/owner/repo.git",
			expected: "github.com",
		},
		{
			name:     "public GitHub SSH scp-like",
			url:      "git@github.com:owner/repo.git",
			expected: "github.com",
		},
		{
			name:     "GHES HTTPS",
			url:      "https://ghes.example.com/org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "GHES SSH scp-like",
			url:      "git@ghes.example.com:org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "GHE SSH scp-like with custom username",
			url:      "example@example.ghe.com:example-org/example-repo.git",
			expected: "example.ghe.com",
		},
		{
			name:     "SSH scp-like without username",
			url:      "github.com:owner/repo.git",
			expected: "github.com",
		},
		{
			name:     "GHES HTTPS without .git suffix",
			url:      "https://ghes.example.com/org/repo",
			expected: "ghes.example.com",
		},
		{
			name:     "SSH URL format with user",
			url:      "ssh://git@ghes.example.com/org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "SSH URL format without user",
			url:      "ssh://ghes.example.com/org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "HTTP URL",
			url:      "http://ghes.example.com/org/repo.git",
			expected: "ghes.example.com",
		},
		{
			name:     "empty URL defaults to github.com",
			url:      "",
			expected: "github.com",
		},
		{
			name:     "unrecognized URL defaults to github.com",
			url:      "not-a-url",
			expected: "github.com",
		},
		{
			name:     "GHES with port",
			url:      "https://ghes.example.com:8443/org/repo.git",
			expected: "ghes.example.com:8443",
		},
		{
			name:     "malformed URL falls back to manual parsing with userinfo and path",
			url:      "https://baduser%zz@example.com/path",
			expected: "example.com",
		},
		{
			name:     "malformed URL falls back to manual parsing with userinfo and no path",
			url:      "https://baduser%zz@example.com",
			expected: "example.com",
		},
		{
			name:     "malformed URL falls back to manual parsing with no userinfo and no path",
			url:      "https://%zz",
			expected: "%zz",
		},
		{
			name:     "SSH scp-like with slash before colon does not match and defaults to github.com",
			url:      "some/path:notaport",
			expected: "github.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostFromRemoteURL(tt.url)
			assert.Equal(t, tt.expected, got, "extractHostFromRemoteURL should return the expected host for %q", tt.url)
		})
	}
}

func TestGetHostFromOriginRemote(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-get-host-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: failed to restore directory: %v", err)
		}
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize a git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	t.Run("no remote defaults to github.com", func(t *testing.T) {
		got := getHostFromOriginRemote()
		assert.Equal(t, "github.com", got, "getHostFromOriginRemote should default to github.com without remotes")
	})

	t.Run("public GitHub remote", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "origin", "https://github.com/owner/repo.git").Run(), "add origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "origin").Run() }()

		got := getHostFromOriginRemote()
		assert.Equal(t, "github.com", got, "getHostFromOriginRemote should return the origin host")
	})

	t.Run("GHES remote", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "origin", "https://ghes.example.com/org/repo.git").Run(), "add GHES origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "origin").Run() }()

		got := getHostFromOriginRemote()
		assert.Equal(t, "ghes.example.com", got, "getHostFromOriginRemote should return the GHES origin host")
	})

	t.Run("non-origin single remote falls back to that remote", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "upstream", "https://github.com/owner/repo.git").Run(), "add upstream remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "upstream").Run() }()

		got := getHostFromOriginRemote()
		assert.Equal(t, "github.com", got, "getHostFromOriginRemote should fall back to a single non-origin remote")
	})

	t.Run("multiple remotes without origin defaults to github.com", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "myorg", "https://github.com/myorg/repo.git").Run(), "add first non-origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "myorg").Run() }()
		require.NoError(t, exec.Command("git", "remote", "add", "other", "https://github.com/other/repo.git").Run(), "add second non-origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "other").Run() }()

		got := getHostFromOriginRemote()
		assert.Equal(t, "github.com", got, "getHostFromOriginRemote should default to github.com with multiple non-origin remotes")
	})
}

func TestResolveRemoteURL(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-resolve-remote-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: failed to restore directory: %v", err)
		}
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize a git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	t.Run("no remotes returns error", func(t *testing.T) {
		_, _, err := resolveRemoteURL("")
		assert.Error(t, err, "resolveRemoteURL should return an error when no remotes are configured")
	})

	t.Run("origin remote is used when present", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "origin", "https://github.com/owner/repo.git").Run(), "add origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "origin").Run() }()

		url, name, err := resolveRemoteURL("")
		require.NoError(t, err, "resolve remote URL with origin present")
		assert.Equal(t, "origin", name, "resolveRemoteURL should prefer the origin remote")
		assert.Equal(t, "https://github.com/owner/repo.git", url, "resolveRemoteURL should return the origin remote URL")
	})

	t.Run("single non-origin remote is used as fallback", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "myorg", "https://github.com/myorg/repo.git").Run(), "add fallback remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "myorg").Run() }()

		url, name, err := resolveRemoteURL("")
		require.NoError(t, err, "resolve remote URL with a single non-origin remote")
		assert.Equal(t, "myorg", name, "resolveRemoteURL should use the only configured non-origin remote")
		assert.Equal(t, "https://github.com/myorg/repo.git", url, "resolveRemoteURL should return the only configured non-origin remote URL")
	})

	t.Run("multiple non-origin remotes returns error", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "remote1", "https://github.com/org1/repo.git").Run(), "add first non-origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "remote1").Run() }()
		require.NoError(t, exec.Command("git", "remote", "add", "remote2", "https://github.com/org2/repo.git").Run(), "add second non-origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "remote2").Run() }()

		_, _, err := resolveRemoteURL("")
		assert.Error(t, err, "resolveRemoteURL should return an error when multiple non-origin remotes are configured")
	})

	t.Run("origin takes precedence when multiple remotes exist", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "origin", "https://github.com/owner/repo.git").Run(), "add origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "origin").Run() }()
		require.NoError(t, exec.Command("git", "remote", "add", "upstream", "https://github.com/upstream/repo.git").Run(), "add upstream remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "upstream").Run() }()

		url, name, err := resolveRemoteURL("")
		require.NoError(t, err, "resolve remote URL with origin and upstream remotes")
		assert.Equal(t, "origin", name, "resolveRemoteURL should prefer origin over other remotes")
		assert.Equal(t, "https://github.com/owner/repo.git", url, "resolveRemoteURL should return the origin URL when origin is present")
	})
}

func TestGetRepositorySlugFromRemotePreferringUpstream(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-slug-upstream-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: failed to restore directory: %v", err)
		}
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	addRemote := func(t *testing.T, name, remoteURL string) {
		t.Helper()
		if err := exec.Command("git", "remote", "get-url", name).Run(); err == nil {
			require.NoError(t, exec.Command("git", "remote", "remove", name).Run(), "remove existing %s remote", name)
		}
		require.NoError(t, exec.Command("git", "remote", "add", name, remoteURL).Run(), "add %s remote", name)
		t.Cleanup(func() {
			if err := exec.Command("git", "remote", "remove", name).Run(); err != nil {
				t.Logf("Warning: failed to remove %s remote during cleanup: %v", name, err)
			}
		})
	}

	t.Run("prefers upstream when both origin and upstream exist", func(t *testing.T) {
		addRemote(t, "origin", "https://github.com/fork/repo.git")
		addRemote(t, "upstream", "https://github.com/upstream/repo.git")

		slug := getRepositorySlugFromRemotePreferringUpstream()
		assert.Equal(t, "upstream/repo", slug, "getRepositorySlugFromRemotePreferringUpstream should prefer upstream")
	})

	t.Run("falls back to origin when upstream missing", func(t *testing.T) {
		addRemote(t, "origin", "https://github.com/myorg/myrepo.git")

		slug := getRepositorySlugFromRemotePreferringUpstream()
		assert.Equal(t, "myorg/myrepo", slug, "getRepositorySlugFromRemotePreferringUpstream should fall back to origin")
	})

	t.Run("falls back to origin when upstream is unparsable", func(t *testing.T) {
		addRemote(t, "origin", "https://github.com/myorg/myrepo.git")
		addRemote(t, "upstream", "https://example.com/upstream/repo.git")

		slug := getRepositorySlugFromRemotePreferringUpstream()
		assert.Equal(t, "myorg/myrepo", slug, "getRepositorySlugFromRemotePreferringUpstream should fall back to origin when upstream is unparsable")
	})
}

func TestGetRepositorySlugFromRemoteForPathPreferringUpstream(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-slug-upstream-path-*")
	testFilePath := filepath.Join(tmpDir, "workflow.md")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: failed to restore directory: %v", err)
		}
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	addRemote := func(t *testing.T, name, remoteURL string) {
		t.Helper()
		if err := exec.Command("git", "remote", "get-url", name).Run(); err == nil {
			require.NoError(t, exec.Command("git", "remote", "remove", name).Run(), "remove existing %s remote", name)
		}
		require.NoError(t, exec.Command("git", "remote", "add", name, remoteURL).Run(), "add %s remote", name)
		t.Cleanup(func() {
			if err := exec.Command("git", "remote", "remove", name).Run(); err != nil {
				t.Logf("Warning: failed to remove %s remote during cleanup: %v", name, err)
			}
		})
	}

	t.Run("prefers upstream for path-based resolution", func(t *testing.T) {
		addRemote(t, "origin", "https://github.com/fork/repo.git")
		addRemote(t, "upstream", "https://github.com/upstream/repo.git")

		slug := getRepositorySlugFromRemoteForPath(testFilePath)
		assert.Equal(t, "upstream/repo", slug, "getRepositorySlugFromRemoteForPath should prefer upstream")
	})
}

func TestGetRepositorySlugFromRemoteFallback(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-slug-fallback-*")

	originalDir, err := os.Getwd()
	require.NoError(t, err, "get current directory for test setup")
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			t.Logf("Warning: failed to restore directory: %v", err)
		}
	}()

	require.NoError(t, os.Chdir(tmpDir), "change to temp directory for test setup")

	// Initialize a git repo
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Skip("Git not available")
	}

	t.Run("single non-origin remote provides repo slug", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "myorg", "https://github.com/myorg/myrepo.git").Run(), "add fallback remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "myorg").Run() }()

		slug := getRepositorySlugFromRemote()
		assert.Equal(t, "myorg/myrepo", slug, "getRepositorySlugFromRemote should return the only configured non-origin slug")
	})

	t.Run("multiple non-origin remotes returns empty slug", func(t *testing.T) {
		require.NoError(t, exec.Command("git", "remote", "add", "remote1", "https://github.com/org1/repo1.git").Run(), "add first non-origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "remote1").Run() }()
		require.NoError(t, exec.Command("git", "remote", "add", "remote2", "https://github.com/org2/repo2.git").Run(), "add second non-origin remote")
		defer func() { _ = exec.Command("git", "remote", "remove", "remote2").Run() }()

		slug := getRepositorySlugFromRemote()
		assert.Empty(t, slug, "getRepositorySlugFromRemote should return an empty slug when multiple non-origin remotes exist")
	})
}

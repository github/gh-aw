//go:build !integration

package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestGitPatchFromHEADCommits tests that the patch generation script can detect
// and create patches from commits made directly to HEAD (without a named branch)
func TestGitPatchFromHEADCommits(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-patch-head-*")

	// Initialize a git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init git repo: %v\nOutput: %s", err, output)
	}

	// Configure git user for commits
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to config git email: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to config git name: %v\nOutput: %s", err, output)
	}

	// Create an initial commit (this will be our GITHUB_SHA)
	testFile1 := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(testFile1, []byte("initial content\n"), 0644); err != nil {
		t.Fatalf("Failed to write initial file: %v", err)
	}

	cmd = exec.Command("git", "add", "initial.txt")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to add initial file: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create initial commit: %v\nOutput: %s", err, output)
	}

	// Get the initial commit SHA (this simulates GITHUB_SHA)
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get initial SHA: %v\nOutput: %s", err, output)
	}
	initialSHA := strings.TrimSpace(string(output))

	// Now simulate the LLM making commits directly to HEAD
	// Commit 1: Add a new file
	testFile2 := filepath.Join(tmpDir, "new-feature.txt")
	if err := os.WriteFile(testFile2, []byte("new feature content\n"), 0644); err != nil {
		t.Fatalf("Failed to write new file: %v", err)
	}

	cmd = exec.Command("git", "add", "new-feature.txt")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to add new file: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", "Add new feature")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create first commit: %v\nOutput: %s", err, output)
	}

	// Commit 2: Modify existing file
	if err := os.WriteFile(testFile1, []byte("initial content\nupdated by LLM\n"), 0644); err != nil {
		t.Fatalf("Failed to update initial file: %v", err)
	}

	cmd = exec.Command("git", "add", "initial.txt")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to add updated file: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", "Update initial file")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create second commit: %v\nOutput: %s", err, output)
	}

	// Now run the patch generation script
	// The script creates the patch at /tmp/gh-aw/aw.patch
	patchFile := "/tmp/gh-aw/aw.patch"

	// Ensure the /tmp/gh-aw directory exists
	if err := os.MkdirAll("/tmp/gh-aw", 0755); err != nil {
		t.Fatalf("Failed to create /tmp/gh-aw directory: %v", err)
	}

	// Remove any existing patch file
	os.Remove(patchFile)

	// Create a minimal safe-outputs file (empty - no branch name)
	safeOutputsFile := filepath.Join(tmpDir, "safe-outputs.jsonl")
	if err := os.WriteFile(safeOutputsFile, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write safe-outputs file: %v", err)
	}

	// Run the patch generation script from actions/setup/sh
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "generate_git_patch.sh")
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read script file: %v", err)
	}
	scriptFile := filepath.Join(tmpDir, "generate_patch.sh")
	if err := os.WriteFile(scriptFile, scriptContent, 0755); err != nil {
		t.Fatalf("Failed to write script file: %v", err)
	}

	cmd = exec.Command("bash", scriptFile)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GH_AW_SAFE_OUTPUTS="+safeOutputsFile,
		"GITHUB_SHA="+initialSHA,
		"DEFAULT_BRANCH=main",
		"GITHUB_STEP_SUMMARY=/dev/null",
	)

	// Capture the output for debugging
	scriptOutput, err := cmd.CombinedOutput()
	t.Logf("Script output:\n%s", scriptOutput)

	if err != nil {
		t.Fatalf("Failed to run patch generation script: %v\nOutput: %s", err, scriptOutput)
	}

	// Verify the patch file was created
	if _, err := os.Stat(patchFile); os.IsNotExist(err) {
		t.Fatal("Patch file was not created")
	}

	// Read and verify the patch content
	patchContent, err := os.ReadFile(patchFile)
	if err != nil {
		t.Fatalf("Failed to read patch file: %v", err)
	}

	patchStr := string(patchContent)

	// Verify the patch contains both commits
	if !strings.Contains(patchStr, "Add new feature") {
		t.Error("Patch does not contain first commit message")
	}

	if !strings.Contains(patchStr, "Update initial file") {
		t.Error("Patch does not contain second commit message")
	}

	// Verify the patch contains file changes
	if !strings.Contains(patchStr, "new-feature.txt") {
		t.Error("Patch does not contain new file")
	}

	if !strings.Contains(patchStr, "initial.txt") {
		t.Error("Patch does not contain modified file")
	}

	// Verify patch format (should start with "From <sha>")
	if !strings.HasPrefix(patchStr, "From ") {
		t.Error("Patch does not have correct format (should start with 'From ')")
	}

	// Count commits in patch (each commit starts with "From <sha>")
	commitCount := strings.Count(patchStr, "\nFrom ")
	if strings.HasPrefix(patchStr, "From ") {
		commitCount++ // Count the first commit
	}

	if commitCount != 2 {
		t.Errorf("Expected 2 commits in patch, got %d", commitCount)
	}

	// Verify script logged the strategy being used
	if !strings.Contains(string(scriptOutput), "Strategy 2: Checking for commits on current HEAD") {
		t.Error("Script output does not indicate Strategy 2 was used")
	}

	if !strings.Contains(string(scriptOutput), "GITHUB_SHA is an ancestor of HEAD - commits were added") {
		t.Error("Script output does not confirm commits were detected")
	}

	t.Log("Successfully generated patch from HEAD commits without named branch")
}

// TestGitPatchPrefersBranchOverHEAD tests that when both a branch name and HEAD commits exist,
// the script prefers the branch-based approach (Strategy 1)
func TestGitPatchPrefersBranchOverHEAD(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := testutil.TempDir(t, "test-patch-priority-*")

	// Initialize a git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init git repo: %v\nOutput: %s", err, output)
	}

	// Configure git user
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to config git: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to config git: %v\nOutput: %s", err, output)
	}

	// Create initial commit
	testFile := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(testFile, []byte("content\n"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to add files: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to commit: %v\nOutput: %s", err, output)
	}

	// Get initial SHA
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tmpDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to get SHA: %v", err)
	}
	initialSHA := strings.TrimSpace(string(output))

	// Create a named branch
	cmd = exec.Command("git", "checkout", "-b", "feature-branch")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to create branch: %v\nOutput: %s", err, output)
	}

	// Make a commit on the branch
	if err := os.WriteFile(testFile, []byte("content\nupdated\n"), 0644); err != nil {
		t.Fatalf("Failed to update file: %v", err)
	}

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to add files: %v\nOutput: %s", err, output)
	}

	cmd = exec.Command("git", "commit", "-m", "Branch commit")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to commit: %v\nOutput: %s", err, output)
	}

	// Create safe-outputs file with branch name
	safeOutputsFile := filepath.Join(tmpDir, "safe-outputs.jsonl")
	safeOutputsContent := "{\"type\":\"create_pull_request\",\"branch\":\"feature-branch\",\"title\":\"Test\",\"body\":\"Test\"}\n"
	if err := os.WriteFile(safeOutputsFile, []byte(safeOutputsContent), 0644); err != nil {
		t.Fatalf("Failed to write safe-outputs: %v", err)
	}

	// Run the script from actions/setup/sh
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "generate_git_patch.sh")
	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read script file: %v", err)
	}
	scriptFile := filepath.Join(tmpDir, "generate_patch.sh")
	if err := os.WriteFile(scriptFile, scriptContent, 0755); err != nil {
		t.Fatalf("Failed to write script: %v", err)
	}

	// Ensure /tmp/gh-aw exists and is clean
	patchFile := "/tmp/gh-aw/aw.patch"
	if err := os.MkdirAll("/tmp/gh-aw", 0755); err != nil {
		t.Fatalf("Failed to create /tmp/gh-aw: %v", err)
	}
	os.Remove(patchFile)

	cmd = exec.Command("bash", scriptFile)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GH_AW_SAFE_OUTPUTS="+safeOutputsFile,
		"GITHUB_SHA="+initialSHA,
		"DEFAULT_BRANCH=main",
		"GITHUB_STEP_SUMMARY=/dev/null",
	)

	scriptOutput, err := cmd.CombinedOutput()
	t.Logf("Script output:\n%s", scriptOutput)

	if err != nil {
		t.Fatalf("Script failed: %v\nOutput: %s", err, scriptOutput)
	}

	// Verify Strategy 1 was used (branch-based)
	if !strings.Contains(string(scriptOutput), "Strategy 1: Using named branch from JSONL") {
		t.Error("Expected Strategy 1 to be used when branch name is provided")
	}

	if strings.Contains(string(scriptOutput), "Strategy 2: Checking for commits on current HEAD") {
		t.Error("Strategy 2 should not run when Strategy 1 succeeds")
	}
}

// TestGitPatchNoCommits tests that no patch is generated when there are no commits
func TestGitPatchNoCommits(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-patch-no-commits-*")

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to init git: %v\nOutput: %s", err, output)
	}

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create and commit a file
	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("content\n"), 0644)

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Initial")
	cmd.Dir = tmpDir
	cmd.Run()

	// Get SHA
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tmpDir
	output, _ := cmd.CombinedOutput()
	currentSHA := strings.TrimSpace(string(output))

	// Create empty safe-outputs
	safeOutputsFile := filepath.Join(tmpDir, "safe-outputs.jsonl")
	os.WriteFile(safeOutputsFile, []byte(""), 0644)

	// Run script with GITHUB_SHA = current HEAD (no new commits) from actions/setup/sh
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "generate_git_patch.sh")
	scriptContent, _ := os.ReadFile(scriptPath)
	scriptFile := filepath.Join(tmpDir, "generate_patch.sh")
	os.WriteFile(scriptFile, scriptContent, 0755)

	// Ensure /tmp/gh-aw exists and is clean
	patchFile := "/tmp/gh-aw/aw.patch"
	os.MkdirAll("/tmp/gh-aw", 0755)
	os.Remove(patchFile)

	cmd = exec.Command("bash", scriptFile)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(),
		"GH_AW_SAFE_OUTPUTS="+safeOutputsFile,
		"GITHUB_SHA="+currentSHA,
		"DEFAULT_BRANCH=main",
		"GITHUB_STEP_SUMMARY=/dev/null",
	)

	scriptOutput, _ := cmd.CombinedOutput()
	t.Logf("Script output:\n%s", scriptOutput)

	// Verify no patch was generated (patchFile was already defined above)
	if _, err := os.Stat(patchFile); err == nil {
		t.Error("Patch file should not be created when there are no commits")
	}

	// Verify the script logged that no commits were found
	if !strings.Contains(string(scriptOutput), "No commits have been made since checkout") {
		t.Error("Script should log that no commits were made")
	}
}

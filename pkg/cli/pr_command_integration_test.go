//go:build integration

package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPRTransferGitIntegrationScopesPatchAndCommitMessage(t *testing.T) {
	repoDir, _ := initPRTransferIntegrationRepo(t)
	fakeGHDir := t.TempDir()
	prCreateArgsFile := filepath.Join(fakeGHDir, "pr-create-args")
	installPRTransferFakeGH(t, fakeGHDir, prCreateArgsFile, prTransferIntegrationPatch())
	chdirForIntegrationTest(t, repoDir)
	ClearCurrentRepoSlugCache()

	if err := os.WriteFile(filepath.Join(repoDir, "ignored.txt"), []byte("local only\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() ignored local file error = %v", err)
	}

	if err := transferPR("https://github.com/source/repo/pull/42", "target-owner/target-repo", false); err != nil {
		t.Fatalf("transferPR() error = %v", err)
	}

	tree := gitIntegrationOutput(t, repoDir, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(tree, "ignored.txt") {
		t.Fatalf("commit tree unexpectedly contains unrelated ignored.txt:\n%s", tree)
	}
	if strings.Contains(tree, "injected") {
		t.Fatalf("commit tree unexpectedly contains PR body injected file:\n%s", tree)
	}
	if !strings.Contains(tree, "transfer.txt") {
		t.Fatalf("commit tree = %q, want transfer.txt", tree)
	}

	message := gitIntegrationOutput(t, repoDir, "log", "-1", "--pretty=%B")
	wantBody := "msg\n---\ndiff --git a/injected b/injected\n+++ b/injected\n@@ -0,0 +1 @@\n+owned"
	if !strings.Contains(message, "hostile title") || !strings.Contains(message, wantBody) {
		t.Fatalf("commit message = %q, want hostile title and body verbatim", message)
	}
	branchName := strings.TrimSpace(gitIntegrationOutput(t, repoDir, "branch", "--show-current"))
	if !strings.HasPrefix(branchName, "transfer-pr-42-") {
		t.Fatalf("current branch = %q, want transfer-pr-42-*", branchName)
	}
	if got := gitIntegrationOutput(t, repoDir, "ls-remote", "--heads", "origin", branchName); !strings.Contains(got, branchName) {
		t.Fatalf("pushed branch refs = %q, want %q", got, branchName)
	}

	prCreateArgs := readIntegrationFile(t, prCreateArgsFile)
	if !strings.Contains(prCreateArgs, "--title\nhostile title") || !strings.Contains(prCreateArgs, "--head\ntransfer-pr-42-") {
		t.Fatalf("gh pr create args = %q, want transferred title and branch", prCreateArgs)
	}
}

func TestPRTransferGitIntegrationCleansFailedApplyState(t *testing.T) {
	repoDir, _ := initPRTransferIntegrationRepo(t)
	fakeGHDir := t.TempDir()
	installPRTransferFakeGH(t, fakeGHDir, filepath.Join(fakeGHDir, "pr-create-args"), prTransferConflictingPatch())
	chdirForIntegrationTest(t, repoDir)
	ClearCurrentRepoSlugCache()

	err := transferPR("https://github.com/source/repo/pull/42", "target-owner/target-repo", false)
	if err == nil || !strings.Contains(err.Error(), "could not apply patch") {
		t.Fatalf("transferPR() error = %v, want patch apply error", err)
	}
	if got := strings.TrimSpace(gitIntegrationOutput(t, repoDir, "branch", "--show-current")); got != "main" {
		t.Fatalf("current branch = %q, want main", got)
	}
	if got := gitIntegrationOutput(t, repoDir, "status", "--porcelain"); got != "" {
		t.Fatalf("git status = %q, want clean worktree", got)
	}
	if got := gitIntegrationOutput(t, repoDir, "ls-files", "-u"); got != "" {
		t.Fatalf("unmerged index entries = %q, want none", got)
	}
	if got := gitIntegrationOutput(t, repoDir, "branch", "--list", "transfer-pr-42-*"); got != "" {
		t.Fatalf("transfer branch still exists: %q", got)
	}
}

func installPRTransferFakeGH(t *testing.T, dir, prCreateArgsFile, diff string) {
	t.Helper()
	ghPath := filepath.Join(dir, "gh")
	script := "#!/bin/sh\n" +
		"case \"$1 $2\" in\n" +
		"  'repo view') echo 'target-owner/target-repo'; exit 0;;\n" +
		"  'api /repos/source/repo/pulls/42') cat <<'JSON'\n" + prTransferPRInfoJSON() + "\nJSON\nexit 0;;\n" +
		"  'api /repos/target-owner/target-repo') echo main; exit 0;;\n" +
		"  'api /user') echo transfer-user; exit 0;;\n" +
		"  'api /repos/target-owner/target-repo/collaborators/transfer-user/permission') echo '{\"permission\":\"write\"}'; exit 0;;\n" +
		"  'pr diff') cat <<'PATCH'\n" + diff + "\nPATCH\nexit 0;;\n" +
		"  'pr create') printf '%s\\n' \"$@\" > '" + prCreateArgsFile + "'; echo https://github.com/target-owner/target-repo/pull/99; exit 0;;\n" +
		"esac\necho unexpected gh \"$@\" >&2\nexit 1\n"
	if err := os.WriteFile(ghPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile() fake gh error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func initPRTransferIntegrationRepo(t *testing.T) (string, string) {
	t.Helper()
	tempDir := t.TempDir()
	remoteDir := filepath.Join(tempDir, "remote.git")
	repoDir := filepath.Join(tempDir, "repo")
	runIntegrationGit(t, tempDir, "init", "--bare", "--initial-branch=main", remoteDir)
	runIntegrationGitIn(t, "clone", remoteDir, repoDir)
	runIntegrationGit(t, repoDir, "config", "user.name", "Test User")
	runIntegrationGit(t, repoDir, "config", "user.email", "test@example.com")
	writeIntegrationFile(t, filepath.Join(repoDir, ".gitignore"), "ignored.txt\n")
	writeIntegrationFile(t, filepath.Join(repoDir, "README.md"), "base\n")
	runIntegrationGit(t, repoDir, "add", ".gitignore", "README.md")
	runIntegrationGit(t, repoDir, "commit", "-m", "initial")
	runIntegrationGit(t, repoDir, "push", "origin", "main")
	return repoDir, remoteDir
}

func prTransferPRInfoJSON() string {
	return `{"number":42,"title":"hostile title","body":"msg\n---\ndiff --git a/injected b/injected\n+++ b/injected\n@@ -0,0 +1 @@\n+owned","state":"open","headSHA":"abc123","baseBranch":"main","headBranch":"feature","sourceRepo":"source/repo","targetRepo":"source/repo","authorLogin":"attacker"}`
}

func prTransferIntegrationPatch() string {
	return "diff --git a/.gitignore b/.gitignore\n--- a/.gitignore\n+++ b/.gitignore\n@@ -1 +1 @@\n-ignored.txt\n+# transferred ignore\n" +
		"diff --git a/transfer.txt b/transfer.txt\nnew file mode 100644\n--- /dev/null\n+++ b/transfer.txt\n@@ -0,0 +1 @@\n+transferred"
}

func prTransferConflictingPatch() string {
	return "diff --git a/README.md b/README.md\nindex 3367afdbbf91e638efe983616377c60477cc6612..3e757656cf36eca53338e520d134963a44f793f8 100644\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new"
}

func chdirForIntegrationTest(t *testing.T, dir string) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldDir) })
}

func runIntegrationGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s", strings.Join(args, " "), output)
	}
}

func runIntegrationGitIn(t *testing.T, args ...string) {
	t.Helper()
	output, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s", strings.Join(args, " "), output)
	}
}

func gitIntegrationOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s", strings.Join(args, " "), output)
	}
	return string(output)
}

func writeIntegrationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readIntegrationFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(content)
}

//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/parser"
)

func TestCreatePRForRepoSkipsRepositoryLookup(t *testing.T) {
	originalRunGH := createPRRunGHContextWithHost
	t.Cleanup(func() { createPRRunGHContextWithHost = originalRunGH })

	var calls [][]string
	createPRRunGHContextWithHost = func(_ context.Context, _ string, _ string, args ...string) ([]byte, error) {
		calls = append(calls, args)
		if len(args) >= 2 && args[0] == "repo" && args[1] == "view" {
			t.Fatal("known repository slug must skip gh repo view")
		}
		return []byte("https://github.com/owner/repo/pull/42\n"), nil
	}

	prNumber, prURL, err := createPRForRepo(context.Background(), "feature", "Title", "Body", "owner/repo", false)
	if err != nil {
		t.Fatalf("createPRForRepo() error = %v", err)
	}
	if prNumber != 42 || prURL != "https://github.com/owner/repo/pull/42" {
		t.Fatalf("createPRForRepo() = (%d, %q), want (42, PR URL)", prNumber, prURL)
	}
	if len(calls) != 1 {
		t.Fatalf("gh call count = %d, want 1", len(calls))
	}
	args := strings.Join(calls[0], " ")
	if !strings.Contains(args, "pr create --repo owner/repo") {
		t.Fatalf("gh args = %q, want explicit repository", args)
	}
}

func TestCreatePatchFromPRWritesOnlyDiff(t *testing.T) {
	diff := "diff --git a/file.txt b/file.txt\n--- a/file.txt\n+++ b/file.txt\n@@ -1 +1 @@\n-old\n+new\n"
	prependFakeGH(t, "if [ \"$1\" = \"pr\" ] && [ \"$2\" = \"diff\" ]; then cat <<'EOF'\n"+diff+"EOF\nexit 0\nfi\necho unexpected gh \"$@\" >&2\nexit 1\n")

	patchFile, err := createPatchFromPR("owner", "repo", &PRInfo{
		Number:      42,
		Title:       "title",
		Body:        "message\n---\ndiff --git a/injected b/injected",
		HeadSHA:     "sha",
		AuthorLogin: "author",
	}, false)
	if err != nil {
		t.Fatalf("createPatchFromPR() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(patchFile)) })

	got, err := os.ReadFile(patchFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != diff {
		t.Fatalf("patch contents = %q, want raw diff %q", got, diff)
	}
}

func TestApplyPatchToRepoScopesCommitToPatchIndex(t *testing.T) {
	repoDir := initPRTransferRepo(t)
	prependFakeGH(t, "if [ \"$1\" = \"api\" ] && [ \"$2\" = \"/repos/target-owner/target-repo\" ]; then echo main; exit 0; fi\necho unexpected gh \"$@\" >&2\nexit 1\n")
	chdirForTest(t, repoDir)

	if err := os.WriteFile(filepath.Join(repoDir, "ignored.txt"), []byte("local secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() ignored local file error = %v", err)
	}

	patchFile := filepath.Join(t.TempDir(), "pr.patch")
	patch := "diff --git a/.gitignore b/.gitignore\n--- a/.gitignore\n+++ b/.gitignore\n@@ -1 +1 @@\n-ignored.txt\n+# transferred ignore\n" +
		"diff --git a/transfer.txt b/transfer.txt\nnew file mode 100644\n--- /dev/null\n+++ b/transfer.txt\n@@ -0,0 +1 @@\n+transferred\n"
	if err := os.WriteFile(patchFile, []byte(patch), 0o600); err != nil {
		t.Fatalf("WriteFile() patch error = %v", err)
	}

	body := "msg\n---\ndiff --git a/injected b/injected\n+++ b/injected\n@@ -0,0 +1 @@\n+owned"
	branchName, err := applyPatchToRepo(patchFile, &PRInfo{
		Number:      42,
		Title:       "title",
		Body:        body,
		SourceRepo:  "source/repo",
		AuthorLogin: "author",
	}, "target-owner", "target-repo", false)
	if err != nil {
		t.Fatalf("applyPatchToRepo() error = %v", err)
	}
	if !strings.HasPrefix(branchName, "transfer-pr-42-") {
		t.Fatalf("branchName = %q, want transfer-pr-42-*", branchName)
	}

	tree := gitOutput(t, repoDir, "ls-tree", "-r", "--name-only", "HEAD")
	if strings.Contains(tree, "ignored.txt") {
		t.Fatalf("commit tree unexpectedly contains unrelated ignored.txt:\n%s", tree)
	}
	if strings.Contains(tree, "injected") {
		t.Fatalf("commit tree unexpectedly contains PR body injected file:\n%s", tree)
	}
	if !strings.Contains(tree, "transfer.txt") {
		t.Fatalf("commit tree = %q, want transfer.txt", tree)
	}

	message := gitOutput(t, repoDir, "log", "-1", "--pretty=%B")
	if !strings.Contains(message, body) {
		t.Fatalf("commit message = %q, want malicious body verbatim", message)
	}
}

func TestApplyPatchToRepoRejectsDirtyWorktree(t *testing.T) {
	repoDir := initPRTransferRepo(t)
	prependFakeGH(t, "if [ \"$1\" = \"api\" ] && [ \"$2\" = \"/repos/target-owner/target-repo\" ]; then echo main; exit 0; fi\necho unexpected gh \"$@\" >&2\nexit 1\n")
	chdirForTest(t, repoDir)

	if err := os.WriteFile(filepath.Join(repoDir, "unrelated.txt"), []byte("do not commit\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() unrelated file error = %v", err)
	}
	patchFile := filepath.Join(t.TempDir(), "pr.patch")
	if err := os.WriteFile(patchFile, []byte("diff --git a/transfer.txt b/transfer.txt\nnew file mode 100644\n--- /dev/null\n+++ b/transfer.txt\n@@ -0,0 +1 @@\n+transferred\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() patch error = %v", err)
	}

	_, err := applyPatchToRepo(patchFile, &PRInfo{Number: 42}, "target-owner", "target-repo", false)
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("applyPatchToRepo() error = %v, want uncommitted changes error", err)
	}
	if got := gitOutput(t, repoDir, "branch", "--show-current"); strings.TrimSpace(got) != "main" {
		t.Fatalf("current branch = %q, want main", strings.TrimSpace(got))
	}
}

func TestApplyPatchToRepoCleansFailedApplyState(t *testing.T) {
	repoDir := initPRTransferRepo(t)
	prependFakeGH(t, "if [ \"$1\" = \"api\" ] && [ \"$2\" = \"/repos/target-owner/target-repo\" ]; then echo main; exit 0; fi\necho unexpected gh \"$@\" >&2\nexit 1\n")
	chdirForTest(t, repoDir)

	patchFile := filepath.Join(t.TempDir(), "pr.patch")
	patch := "diff --git a/README.md b/README.md\nindex 3367afdbbf91e638efe983616377c60477cc6612..3e757656cf36eca53338e520d134963a44f793f8 100644\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n"
	if err := os.WriteFile(patchFile, []byte(patch), 0o600); err != nil {
		t.Fatalf("WriteFile() patch error = %v", err)
	}

	_, err := applyPatchToRepo(patchFile, &PRInfo{Number: 42}, "target-owner", "target-repo", false)
	if err == nil || !strings.Contains(err.Error(), "could not apply patch") {
		t.Fatalf("applyPatchToRepo() error = %v, want patch apply error", err)
	}
	if got := strings.TrimSpace(gitOutput(t, repoDir, "branch", "--show-current")); got != "main" {
		t.Fatalf("current branch = %q, want main", got)
	}
	if got := gitOutput(t, repoDir, "status", "--porcelain"); got != "" {
		t.Fatalf("git status = %q, want clean worktree", got)
	}
	if got := gitOutput(t, repoDir, "ls-files", "-u"); got != "" {
		t.Fatalf("unmerged index entries = %q, want none", got)
	}
	if got := gitOutput(t, repoDir, "branch", "--list", "transfer-pr-42-*"); got != "" {
		t.Fatalf("transfer branch still exists: %q", got)
	}
	if got := readFileString(t, filepath.Join(repoDir, "README.md")); got != "base\n" {
		t.Fatalf("README.md = %q, want restored base content", got)
	}
}

func prependFakeGH(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	ghPath := filepath.Join(dir, "gh")
	if err := os.WriteFile(ghPath, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("WriteFile() fake gh error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func initPRTransferRepo(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	remoteDir := filepath.Join(tempDir, "remote.git")
	repoDir := filepath.Join(tempDir, "repo")
	runGit(t, tempDir, "init", "--bare", "--initial-branch=main", remoteDir)
	runGitIn(t, "", "clone", remoteDir, repoDir)
	runGit(t, repoDir, "config", "user.name", "Test User")
	runGit(t, repoDir, "config", "user.email", "test@example.com")
	runGit(t, repoDir, "config", "maintenance.auto", "false")
	if err := os.WriteFile(filepath.Join(repoDir, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() .gitignore error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("base\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() README error = %v", err)
	}
	runGit(t, repoDir, "add", ".gitignore", "README.md")
	runGit(t, repoDir, "commit", "-m", "initial")
	runGit(t, repoDir, "push", "origin", "main")
	return repoDir
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Fatalf("restore Chdir() error = %v", err)
		}
	})
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %s", strings.Join(args, " "), output)
	}
	return string(output)
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(content)
}

func TestParsePRURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantPR    int
		wantErr   bool
	}{
		{
			name:      "valid GitHub PR URL",
			url:       "https://github.com/trial/repo/pull/234",
			wantOwner: "trial",
			wantRepo:  "repo",
			wantPR:    234,
			wantErr:   false,
		},
		{
			name:      "valid GitHub PR URL with hyphenated repo name",
			url:       "https://github.com/PR-OWNER/PR-REPO/pull/456",
			wantOwner: "PR-OWNER",
			wantRepo:  "PR-REPO",
			wantPR:    456,
			wantErr:   false,
		},
		{
			name:      "valid GitHub PR URL with underscores",
			url:       "https://github.com/test_owner/test_repo/pull/789",
			wantOwner: "test_owner",
			wantRepo:  "test_repo",
			wantPR:    789,
			wantErr:   false,
		},
		{
			name:    "invalid URL format",
			url:     "not-a-url",
			wantErr: true,
		},
		{
			name:      "non-GitHub URL with valid path structure",
			url:       "https://gitlab.com/owner/repo/pull/123",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantPR:    123,
			wantErr:   false,
		},
		{
			name:    "invalid GitHub URL path - missing pull",
			url:     "https://github.com/owner/repo/123",
			wantErr: true,
		},
		{
			name:    "invalid GitHub URL path - wrong format",
			url:     "https://github.com/owner/repo/pulls/123",
			wantErr: true,
		},
		{
			name:    "invalid PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:    "missing owner",
			url:     "https://github.com//repo/pull/123",
			wantErr: true,
		},
		{
			name:    "missing repo",
			url:     "https://github.com/owner//pull/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, repo, prNumber, err := parser.ParsePRURL(tt.url)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePRURL() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("ParsePRURL() unexpected error: %v", err)
				return
			}

			if owner != tt.wantOwner {
				t.Errorf("ParsePRURL() owner = %v, want %v", owner, tt.wantOwner)
			}

			if repo != tt.wantRepo {
				t.Errorf("ParsePRURL() repo = %v, want %v", repo, tt.wantRepo)
			}

			if prNumber != tt.wantPR {
				t.Errorf("ParsePRURL() prNumber = %v, want %v", prNumber, tt.wantPR)
			}
		})
	}
}

func TestPullRequestSupportsTransferAndAutomergeJSON(t *testing.T) {
	t.Parallel()

	var transferPR PRInfo
	if err := json.Unmarshal([]byte(`{"number":123,"title":"Test PR","state":"open","authorLogin":"test-author"}`), &transferPR); err != nil {
		t.Fatalf("failed to decode transfer PR: %v", err)
	}
	if transferPR.Number != 123 || transferPR.Title != "Test PR" || transferPR.State != "open" || transferPR.AuthorLogin != "test-author" {
		t.Fatalf("unexpected transfer PR: %+v", transferPR)
	}

	var automergePR PullRequest
	if err := json.Unmarshal([]byte(`{"number":456,"title":"Automerge PR","isDraft":true,"mergeable":"MERGEABLE","createdAt":"2026-08-20T12:00:00Z","updatedAt":"2026-08-20T13:00:00Z"}`), &automergePR); err != nil {
		t.Fatalf("failed to decode automerge PR: %v", err)
	}
	if automergePR.Number != 456 || !automergePR.IsDraft || automergePR.Mergeable != "MERGEABLE" {
		t.Fatalf("unexpected automerge PR: %+v", automergePR)
	}
	if want := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC); !automergePR.CreatedAt.Equal(want) {
		t.Fatalf("CreatedAt = %v, want %v", automergePR.CreatedAt, want)
	}
}

// TestNewPRCommand tests that the PR command is created properly
func TestNewPRCommand(t *testing.T) {
	t.Parallel()
	cmd := NewPRCommand()

	if cmd.Use != "pr" {
		t.Errorf("Expected command use to be 'pr', got %s", cmd.Use)
	}

	if cmd.Short != "Pull request utilities" {
		t.Errorf("Expected command short description to be 'Pull request utilities', got %s", cmd.Short)
	}
	if !strings.Contains(cmd.Long, "provides a tool for transferring pull requests") {
		t.Errorf("Expected command long description to document a single transfer tool, got %s", cmd.Long)
	}

	// Check that transfer subcommand is added
	subcommands := cmd.Commands()
	found := false
	for _, subcmd := range subcommands {
		if subcmd.Use == "transfer <pr-url>" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected 'transfer' subcommand to be added to pr command")
	}
}

// TestNewPRTransferSubcommand tests that the transfer subcommand is created properly
func TestNewPRTransferSubcommand(t *testing.T) {
	t.Parallel()
	cmd := NewPRTransferSubcommand()

	if cmd.Use != "transfer <pr-url>" {
		t.Errorf("Expected command use to be 'transfer <pr-url>', got %s", cmd.Use)
	}

	if cmd.Short != "Transfer a pull request to another repository" {
		t.Errorf("Expected command short description to match, got %s", cmd.Short)
	}

	// Check that --repo flag exists
	repoFlag := cmd.Flags().Lookup("repo")
	if repoFlag == nil {
		t.Error("Expected --repo flag to exist")
	}
}

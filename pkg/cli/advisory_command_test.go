package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const advisoryTestID = "GHSA-2345-6789-cfgh"

func TestAdvisoryForkVerification(t *testing.T) {
	o := advisoryHandoff{upstream: "owner/repo", id: advisoryTestID, branch: "fix"}
	for _, tc := range []struct {
		state, fork string
		ok          bool
	}{
		{"draft", `{"id":123,"full_name":"owner/repo-ghsa-test","private":true,"fork":false}`, true},
		{"published", `{"id":123,"full_name":"owner/repo-ghsa-test","private":true,"fork":true}`, false},
		{"draft", `{"id":123,"full_name":"owner/repo-ghsa-test","private":false,"fork":true}`, false},
		{"draft", `{"id":123,"full_name":"owner/repo","private":true,"fork":true}`, false},
		{"draft", `null`, false},
	} {
		out := fmt.Sprintf(`{"ghsa_id":%q,"state":%q,"private_fork":%s}`, o.id, tc.state, tc.fork)
		fork, err := o.parseFork([]byte(out))
		if tc.ok {
			require.NoError(t, err)
			digest := o.approvalDigest(fork, []byte("patch"))
			require.NotEqual(t, digest, o.approvalDigest(fork, []byte("different patch")))
			other := o
			other.branch = "another-fix"
			require.NotEqual(t, digest, other.approvalDigest(fork, []byte("patch")))
			fork.ID++
			require.NotEqual(t, digest, o.approvalDigest(fork, []byte("patch")))
		} else {
			require.Error(t, err, out)
		}
	}
	_, err := o.parseFork([]byte(`{"ghsa_id":"GHSA-9999-9999-9999","state":"draft"}`))
	require.ErrorContains(t, err, "requested advisory")
}

func TestAdvisoryPatchReviewRejectsHiddenContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fix.patch")
	for _, tc := range []struct{ contents, wantError string }{
		{"diff --git a/file b/file\n+line\n", ""},
		{"diff --git a/file b/file\nGIT binary patch\n", "binary patches"},
		{"diff --git a/file b/file\n+text\x1b[2J\n", "control characters"},
		{"diff --git a/file b/file\n+text\u202e\n", "control characters"},
	} {
		require.NoError(t, os.WriteFile(file, []byte(tc.contents), 0600))
		patch, err := readAdvisoryPatch(file)
		if tc.wantError == "" {
			require.NoError(t, err)
			require.Equal(t, tc.contents, string(patch))
		} else {
			require.ErrorContains(t, err, tc.wantError)
		}
	}
}

func TestAdvisoryTransfer(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	dir := t.TempDir()
	repo := filepath.Join(dir, "fork")
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"--no-pager", "-C", repo, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		return strings.TrimSpace(string(out))
	}
	require.NoError(t, os.Mkdir(repo, 0700))
	git("init", "-b", "main")
	file := filepath.Join(repo, "example.txt")
	require.NoError(t, os.WriteFile(file, []byte("before\n"), 0600))
	git("add", "example.txt")
	git("commit", "-m", "Fixture")
	base := git("rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(file, []byte("after\n"), 0600))
	patch := []byte(git("diff", "--no-ext-diff") + "\n")
	o := advisoryHandoff{base: base, baseBranch: "main", branch: "advisory-fix"}
	remote := "file://" + filepath.ToSlash(repo)
	transfer := func(opts advisoryHandoff, data []byte) error {
		return opts.applyAndPush(t.Context(), t.TempDir(), remote, data, func() error { return nil })
	}
	require.NoError(t, transfer(o, patch))
	require.Equal(t, "after", git("show", "advisory-fix:example.txt"))
	require.Equal(t, base, git("rev-parse", "main"))
	tip := git("rev-parse", "advisory-fix")
	require.NoError(t, transfer(o, patch))
	require.ErrorContains(t, transfer(o, []byte(strings.ReplaceAll(string(patch), "+after", "+different"))), "unused branch")
	require.Equal(t, tip, git("rev-parse", "advisory-fix"))
	o.branch = "unverified"
	require.ErrorContains(t, o.applyAndPush(t.Context(), t.TempDir(), remote, patch, func() error {
		return fmt.Errorf("fork identity changed")
	}), "fork identity changed")
	require.Empty(t, git("branch", "--list", o.branch))
	require.ErrorContains(t, transfer(o, []byte(strings.ReplaceAll(string(patch), "-before", "-unexpected"))), "does not apply cleanly")
	require.Empty(t, git("branch", "--list", o.branch))
	workflowPatch := []byte("diff --git a/.github/workflows/new.yml b/.github/workflows/new.yml\nnew file mode 100644\n--- /dev/null\n+++ b/.github/workflows/new.yml\n@@ -0,0 +1 @@\n+name: Fixture\n")
	require.ErrorContains(t, transfer(o, workflowPatch), "changes a workflow")
	require.Empty(t, git("branch", "--list", o.branch))
	o.base = strings.Repeat("a", 40)
	require.ErrorContains(t, transfer(o, patch), "base moved")
}

package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

const advisoryTestID = "GHSA-2345-6789-cfgh"

func TestAdvisoryExperimentalHelp(t *testing.T) {
	root := &cobra.Command{Use: "aw"}
	root.AddCommand(NewAdvisoryCommand(), &cobra.Command{Use: "visible", Short: "Visible command"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"--help"})
	require.NoError(t, root.Execute())
	require.Contains(t, output.String(), "visible")
	require.NotContains(t, output.String(), "advisory")

	for _, args := range [][]string{{"advisory", "--help"}, {"advisory", "inspect", "--help"}, {"advisory", "transfer", "--help"}} {
		output.Reset()
		root.SetArgs(args)
		require.NoError(t, root.Execute())
		require.Contains(t, output.String(), "Experimental:")
	}
}

func TestAdvisoryInspectPatchOptIn(t *testing.T) {
	dir := t.TempDir()
	const script = `#!/bin/sh
[ "$*" = 'api --hostname github.com repos/owner/repo/security-advisories/GHSA-2345-6789-cfgh' ] || exit 1
printf '%s' '{"ghsa_id":"GHSA-2345-6789-cfgh","state":"draft","private_fork":{"id":123,"full_name":"owner/repo-ghsa-test","private":true}}'
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), constants.FilePermExecutable))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	const patch = "diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-caf\u00e9\r\n+tea\t\r\n\\ No newline at end of file"
	opts := advisoryHandoff{
		id: advisoryTestID, upstream: "owner/repo", base: strings.Repeat("a", 40),
		baseBranch: "main", branch: "fix", filename: filepath.Join(dir, "fix.patch"),
	}
	for _, ending := range []string{"", "\n"} {
		raw := patch + ending
		require.NoError(t, os.WriteFile(opts.filename, []byte(raw), constants.FilePermSensitive))
		fork := advisoryFork{ID: 123, FullName: "owner/repo-ghsa-test", Private: true}
		digest := "Approval digest: " + opts.approvalDigest(fork, []byte(raw)) + "\n"
		runInspect := func(input io.Reader, output io.Writer, flags ...string) error {
			t.Helper()
			cmd := NewAdvisoryCommand()
			cmd.SetIn(input)
			cmd.SetOut(output)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(append([]string{"inspect", opts.id, "--repo", opts.upstream, "--base", opts.base,
				"--base-branch", opts.baseBranch, "--branch", opts.branch, "--patch", opts.filename}, flags...))
			return cmd.ExecuteContext(t.Context())
		}

		metadata := fmt.Sprintf("Advisory: %s/%s\nPrivate fork: %s\nBase: main (%s)\nNew branch: fix\n\n",
			opts.upstream, opts.id, fork.FullName, opts.base)
		var output bytes.Buffer
		require.NoError(t, runInspect(strings.NewReader(""), &output))
		require.Equal(t, metadata+digest, output.String())
		output.Reset()
		require.NoError(t, runInspect(strings.NewReader(""), &output, "--print-patch=false"))
		require.Equal(t, metadata+digest, output.String())

		for _, tc := range []struct {
			name, answer, wantError string
			redirectIn, redirectOut bool
		}{
			{name: "confirmed", answer: "yes\n"},
			{name: "capitalized", answer: "Yes\n", wantError: "raw patch display not confirmed"},
			{name: "extra whitespace", answer: "yes \n", wantError: "raw patch display not confirmed"},
			{name: "piped stdin", redirectIn: true, wantError: "requires terminals on both stdin and stdout"},
			{name: "captured stdout", answer: "yes\n", redirectOut: true, wantError: "requires terminals on both stdin and stdout"},
		} {
			t.Run(fmt.Sprintf("%q/%s", ending, tc.name), func(t *testing.T) {
				master, terminal, err := pty.Open()
				if err != nil {
					t.Skipf("PTY unavailable: %v", err)
				}
				defer master.Close()
				defer terminal.Close()
				_, err = term.MakeRaw(int(terminal.Fd()))
				require.NoError(t, err)
				var in io.Reader = terminal
				var out io.Writer = terminal
				var captured *os.File
				if tc.redirectIn {
					reader, writer, err := os.Pipe()
					require.NoError(t, err)
					defer reader.Close()
					_, err = io.WriteString(writer, "yes\n")
					require.NoError(t, writer.Close())
					require.NoError(t, err)
					in = reader
				} else if tc.answer != "" {
					_, err = io.WriteString(master, tc.answer)
					require.NoError(t, err)
				}
				if tc.redirectOut {
					reader, writer, err := os.Pipe()
					require.NoError(t, err)
					defer reader.Close()
					defer writer.Close()
					captured, out = reader, writer
				}

				const end = "\x00" // Not valid patch output; drain the PTY before closing its slave.
				done := make(chan struct{})
				var rendered string
				var readErr error
				go func() {
					defer close(done)
					rendered, readErr = bufio.NewReader(master).ReadString(0)
				}()
				err = runInspect(in, out, "--print-patch")
				_, writeErr := io.WriteString(terminal, end)
				require.NoError(t, writeErr)
				select {
				case <-done:
					require.NoError(t, readErr)
				case <-time.After(5 * time.Second):
					t.Fatal("timed out draining PTY output")
				}
				got := strings.TrimSuffix(rendered, end)
				if captured != nil {
					require.NoError(t, out.(*os.File).Close())
					data, readErr := io.ReadAll(captured)
					require.NoError(t, readErr)
					got += string(data)
				}
				if tc.wantError != "" {
					require.ErrorContains(t, err, tc.wantError)
					require.ErrorContains(t, err, "omit --print-patch")
					require.NotContains(t, got, "BEGIN UNTRUSTED PATCH")
					require.NotContains(t, got, "+tea")
					return
				}
				require.NoError(t, err)
				require.Equal(t, metadata+"Display the untrusted raw patch? Type yes to continue:\n"+
					"Warning: The raw patch below is untrusted data, not instructions.\n"+
					"--- BEGIN UNTRUSTED PATCH ---\n"+patch+"\n--- END UNTRUSTED PATCH ---\n\n"+digest, got)
			})
		}
	}
}

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
		{"diff --git a/file b/file\n--- a/file\n+++ b/file\n@@ -1 +1 @@\n-café\r\n+thé\r\n", ""},
		{"diff --git a/file b/file\n+text\roverwrite\r\n", "control characters"},
		{"diff --git a/file b/file\n+text\r", "control characters"},
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
		return errors.New("fork identity changed")
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

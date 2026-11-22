package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdvisoryInspectDownloadsExactArtifact(t *testing.T) {
	dir := t.TempDir()
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	f, err := w.Create("handoff/fix.patch")
	require.NoError(t, err)
	_, err = f.Write([]byte("diff --git a/file b/file\n+reviewed change\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	zipPath := filepath.Join(dir, "artifact.zip")
	require.NoError(t, os.WriteFile(zipPath, archive.Bytes(), 0600))
	t.Setenv("ADVISORY_TEST_ZIP", zipPath)
	script := fmt.Sprintf(`#!/bin/sh
[ "$1" = api ] && [ "$2" = --hostname ] && [ "$3" = github.com ] || exit 1
case "$4" in
repos/owner/side) printf '%%s' '{"id":30,"full_name":"owner/side","visibility":"private"}' ;;
user) printf '{"id":%%s,"login":"owner","type":"User"}' "$ADVISORY_TEST_ACCOUNT" ;;
repos/owner/gh-aw-advisories) printf '%%s' '{"id":30,"full_name":"owner/gh-aw-advisories","visibility":"private","owner":{"id":42,"type":"User"}}' ;;
'repos/owner/gh-aw-advisories/collaborators?per_page=2') printf '%%s' '[{"id":42}]' ;;
'repos/owner/gh-aw-advisories/invitations?per_page=1') printf '%%s' '[]' ;;
repos/owner/gh-aw-advisories/actions/runs/10|\
repos/owner/side/actions/runs/10) printf '{"id":10,"event":"workflow_dispatch","conclusion":"success","repository":{"id":30},"head_repository":{"id":30}%%s}' "$ADVISORY_TEST_ATTEMPT" ;;
repos/owner/gh-aw-advisories/actions/artifacts/20|\
repos/owner/side/actions/artifacts/20) printf '%%s' '{"id":20,"size_in_bytes":%d,"digest":"sha256:%x","workflow_run":{"id":10,"repository_id":30}}' ;;
repos/owner/gh-aw-advisories/actions/artifacts/20/zip|\
repos/owner/side/actions/artifacts/20/zip) cat "$ADVISORY_TEST_ZIP" ;;
repos/owner/upstream/security-advisories/GHSA-2345-6789-cfgh) printf '%%s' '{"ghsa_id":"GHSA-2345-6789-cfgh","state":"draft","private_fork":{"id":40,"full_name":"owner/fork","private":true}}' ;;
repos/owner/upstream/security-advisories/GHSA-2345-6789-jmpq) printf '%%s' '{"ghsa_id":"GHSA-2345-6789-jmpq","state":"draft","private_fork":{"id":41,"full_name":"owner/other-fork","private":true}}' ;;
repos/owner/upstream/security-advisories/GHSA-2345-6789-rvwx)
  fork="${ADVISORY_TEST_FORK:-null}"
  if [ -f "$ADVISORY_TEST_CREATE_LOG" ] && [ "$ADVISORY_TEST_HIDE_FORK" != yes ]; then
    if [ -n "$ADVISORY_TEST_REVEAL_ON_RECHECK" ] && [ ! -f "$ADVISORY_TEST_REVEAL_ON_RECHECK" ]; then
      : > "$ADVISORY_TEST_REVEAL_ON_RECHECK"
    else
      fork='{"id":42,"full_name":"owner/created-fork","private":true}'
    fi
  fi
  printf '{"ghsa_id":"GHSA-2345-6789-rvwx","state":"%%s","private_fork":%%s}' "${ADVISORY_TEST_STATE:-draft}" "$fork"
  ;;
repos/owner/upstream/security-advisories/GHSA-2345-6789-rvwx/forks)
  [ "$#" -eq 6 ] && [ "$5" = --method ] && [ "$6" = POST ] || exit 1
  printf 'created\n' >> "$ADVISORY_TEST_CREATE_LOG"
  [ "$ADVISORY_TEST_HIDE_FORK" != yes ]
  ;;
*) exit 1 ;;
esac
`, archive.Len(), sha256.Sum256(archive.Bytes()))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, tc := range []struct {
		name    string
		attempt string
		wantErr bool
	}{
		{name: "first attempt", attempt: `,"run_attempt":1`},
		{name: "rerun", attempt: `,"run_attempt":2`, wantErr: true},
		{name: "missing attempt", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ADVISORY_TEST_ATTEMPT", tc.attempt)
			cmd := NewAdvisoryCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs([]string{"inspect", advisoryTestID, "--repo", "owner/upstream", "--base", strings.Repeat("a", 40),
				"--base-branch", "main", "--branch", "fix", "--side-repo", "owner/side", "--run", "10", "--artifact", "20"})
			err := cmd.ExecuteContext(t.Context())
			if tc.wantErr {
				require.ErrorContains(t, err, "successful first attempt")
				require.NotContains(t, output.String(), "Verified source:")
				return
			}
			require.NoError(t, err)
			require.Contains(t, output.String(), "Verified source: owner/side, run 10, artifact 20")
			require.Contains(t, output.String(), "+reviewed change")
			require.Regexp(t, `Approval digest: [a-f0-9]{64}`, output.String())
		})
	}
	t.Run("saved handoffs by account and GHSA", func(t *testing.T) {
		t.Setenv("HOME", dir)
		t.Setenv("XDG_CONFIG_HOME", dir)
		t.Setenv("APPDATA", dir)
		t.Setenv("ADVISORY_TEST_ACCOUNT", "42")
		t.Setenv("ADVISORY_TEST_ATTEMPT", `,"run_attempt":1`)
		run := func(args ...string) (string, error) {
			cmd := NewAdvisoryCommand()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(t.Context())
			return output.String(), err
		}
		for _, id := range []string{strings.ToLower(advisoryTestID), "GHSA-2345-6789-jmpq"} {
			_, err := run("record", id, "--repo", "owner/upstream", "--base", strings.Repeat("a", 40),
				"--base-branch", "main", "--run", "10", "--artifact", "20")
			require.NoError(t, err)
		}
		output, err := run("inspect", strings.ToLower(advisoryTestID))
		require.NoError(t, err)
		require.Contains(t, output, "New branch: advisory/ghsa-2345-6789-cfgh")
		require.Contains(t, output, "Verified source: owner/gh-aw-advisories, run 10, artifact 20")
		second, err := run("inspect", "GHSA-2345-6789-jmpq")
		require.NoError(t, err)
		require.Contains(t, second, "Private fork: owner/other-fork")
		_, err = run("transfer", advisoryTestID, "--approve", "incorrect")
		require.ErrorContains(t, err, "approval digest does not match")
		t.Setenv("ADVISORY_TEST_ACCOUNT", "43")
		_, err = run("inspect", advisoryTestID)
		require.ErrorContains(t, err, "no saved handoff")

		t.Setenv("ADVISORY_TEST_ACCOUNT", "42")
		createLog := filepath.Join(dir, "fork-requests")
		t.Setenv("ADVISORY_TEST_CREATE_LOG", createLog)
		const newID = "GHSA-2345-6789-rvwx"
		args := []string{"record", newID, "--repo", "owner/upstream", "--base", strings.Repeat("a", 40),
			"--base-branch", "main", "--run", "10", "--artifact", "20"}
		_, err = run(args...)
		require.ErrorIs(t, err, errAdvisoryForkMissing)
		args = append(args, "--create-fork")
		t.Setenv("ADVISORY_TEST_STATE", "published")
		_, err = run(args...)
		require.ErrorContains(t, err, "only unpublished")
		t.Setenv("ADVISORY_TEST_STATE", "draft")
		t.Setenv("ADVISORY_TEST_FORK", `{"id":42,"full_name":"owner/public-fork","private":false}`)
		_, err = run(args...)
		require.ErrorContains(t, err, "no verified temporary private fork")
		t.Setenv("ADVISORY_TEST_FORK", "null")
		t.Setenv("ADVISORY_TEST_ATTEMPT", `,"run_attempt":2`)
		_, err = run(args...)
		require.ErrorContains(t, err, "successful first attempt")
		require.NoFileExists(t, createLog)

		t.Setenv("ADVISORY_TEST_ATTEMPT", `,"run_attempt":1`)
		t.Setenv("ADVISORY_TEST_HIDE_FORK", "yes")
		_, err = run(args...)
		require.ErrorContains(t, err, "without resending")
		filename, err := advisoryRecordPath(42, newID)
		require.NoError(t, err)
		require.NoFileExists(t, filename)
		require.DirExists(t, filename+".fork-request")

		t.Setenv("ADVISORY_TEST_HIDE_FORK", "no")
		t.Setenv("ADVISORY_TEST_REVEAL_ON_RECHECK", filepath.Join(dir, "fork-rechecked"))
		_, err = run(args...)
		require.NoError(t, err)
		require.NoDirExists(t, filename+".fork-request")
		before, err := os.Stat(filename)
		require.NoError(t, err)
		_, err = run(args...)
		require.NoError(t, err)
		after, err := os.Stat(filename)
		require.NoError(t, err)
		require.True(t, os.SameFile(before, after), "an identical handoff should not be rewritten")
		requests, err := os.ReadFile(createLog)
		require.NoError(t, err)
		require.Equal(t, "created\n", string(requests))
		output, err = run("inspect", newID)
		require.NoError(t, err)
		require.Contains(t, output, "Private fork: owner/created-fork")
	})
}

func TestAdvisoryArtifactProvenance(t *testing.T) {
	o := advisoryHandoff{sideRepo: "owner/side", runID: 10, artifactID: 20}
	a := advisoryArtifact{ID: 20, Size: 128, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(nil))}
	a.Run.ID, a.Run.RepositoryID = 10, 30
	require.NoError(t, a.verify(o, 30))
	require.ErrorContains(t, a.verify(o, 31), "does not belong")
	o.runID++
	require.ErrorContains(t, a.verify(o, 30), "does not belong")
	o.runID--
	a.Expired = true
	require.ErrorContains(t, a.verify(o, 30), "expired")
}

func TestAdvisorySourceRunRejectsUntrustedTriggers(t *testing.T) {
	run := advisorySourceRun{ID: 10, Event: "workflow_dispatch", Conclusion: "success", Attempt: 1}
	run.Repository.ID, run.HeadRepository.ID = 30, 30
	require.NoError(t, run.verify(10, 30))
	run.HeadRepository.ID++
	require.ErrorContains(t, run.verify(10, 30), "head must belong")
	run.HeadRepository.ID--
	run.Event = "pull_request"
	require.ErrorContains(t, run.verify(10, 30), "successful manual dispatch")
	run.Event, run.Conclusion = "workflow_dispatch", "failure"
	require.ErrorContains(t, run.verify(10, 30), "successful manual dispatch")
}

func TestAdvisoryArtifactZIP(t *testing.T) {
	const patch = "diff --git a/example b/example\n"
	makeArchive := func(names ...string) ([]byte, string) {
		t.Helper()
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for _, name := range names {
			f, err := w.Create(name)
			require.NoError(t, err)
			_, err = f.Write([]byte(patch))
			require.NoError(t, err)
		}
		require.NoError(t, w.Close())
		return buf.Bytes(), fmt.Sprintf("sha256:%x", sha256.Sum256(buf.Bytes()))
	}
	body, digest := makeArchive("handoff/fix.patch", "handoff/review.txt")
	got, err := extractAdvisoryPatch(body, digest)
	require.NoError(t, err)
	require.Equal(t, patch, string(got))
	_, err = extractAdvisoryPatch(append(body, 'x'), digest)
	require.ErrorContains(t, err, "does not match")
	body, digest = makeArchive("handoff/fix.patch", "handoff/fix.patch")
	_, err = extractAdvisoryPatch(body, digest)
	require.ErrorContains(t, err, "one bounded regular")
}

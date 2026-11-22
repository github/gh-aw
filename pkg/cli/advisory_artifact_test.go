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
repos/owner/side/actions/runs/10) printf '%%s' '{"id":10,"event":"workflow_dispatch","conclusion":"success","repository":{"id":30},"head_repository":{"id":30}}' ;;
repos/owner/side/actions/artifacts/20) printf '%%s' '{"id":20,"size_in_bytes":%d,"digest":"sha256:%x","workflow_run":{"id":10,"repository_id":30}}' ;;
repos/owner/side/actions/artifacts/20/zip) cat "$ADVISORY_TEST_ZIP" ;;
repos/owner/upstream/security-advisories/GHSA-2345-6789-cfgh) printf '%%s' '{"ghsa_id":"GHSA-2345-6789-cfgh","state":"draft","private_fork":{"id":40,"full_name":"owner/fork","private":true}}' ;;
*) exit 1 ;;
esac
`, archive.Len(), sha256.Sum256(archive.Bytes()))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd := NewAdvisoryCommand()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"inspect", advisoryTestID, "--repo", "owner/upstream", "--base", strings.Repeat("a", 40),
		"--base-branch", "main", "--branch", "fix", "--side-repo", "owner/side", "--run", "10", "--artifact", "20"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	require.Contains(t, output.String(), "Verified source: owner/side, run 10, artifact 20")
	require.Contains(t, output.String(), "+reviewed change")
	require.Regexp(t, `Approval digest: [a-f0-9]{64}`, output.String())
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
	run := advisorySourceRun{ID: 10, Event: "workflow_dispatch", Conclusion: "success"}
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

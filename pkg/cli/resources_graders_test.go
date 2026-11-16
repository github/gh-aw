package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchOperationalValueEvaluatorResources(t *testing.T) {
	for _, run := range []string{".github/graders/score.sh", "./graders/score.sh"} {
		t.Run(run, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, exec.Command("git", "-C", root, "init", "-q").Run())
			target := filepath.Join(root, ".github", "workflows")
			require.NoError(t, os.MkdirAll(target, 0o755))
			content := "---\ngraders:\n  operational-value:\n    enabled: false\n    run: " + run + "\n---\n"
			spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}, WorkflowPath: ".github/workflows/test.md"}
			var downloaded string
			download := func(_ context.Context, _, _, path, _ string) ([]byte, error) {
				downloaded = path
				return []byte("#!/bin/bash\nprintf '[]'\n"), nil
			}
			require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, target, false, false, nil, download))
			destination := filepath.Join(root, filepath.FromSlash(run))
			if run == "./graders/score.sh" {
				assert.Equal(t, ".github/workflows/graders/score.sh", downloaded)
				destination = filepath.Join(target, "graders", "score.sh")
			} else {
				assert.Equal(t, run, downloaded)
			}
			original, err := os.ReadFile(destination)
			require.NoError(t, err)
			require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, target, false, false, nil, download))
			require.NoError(t, os.WriteFile(destination, []byte("local edit"), 0o600))
			require.ErrorContains(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, target, false, false, nil, download), "different content")
			require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, target, false, true, nil, download))
			actual, err := os.ReadFile(destination)
			require.NoError(t, err)
			assert.Equal(t, original, actual)
		})
	}
}

func TestOperationalValueResourceDownloadErrorsAreSurfaced(t *testing.T) {
	target := t.TempDir()
	content := "---\ngraders:\n  operational-value:\n    run: ./score.sh\n---\n"
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}, WorkflowPath: ".github/workflows/test.md"}
	download := func(context.Context, string, string, string, string) ([]byte, error) {
		return nil, errors.New("download unavailable")
	}
	require.ErrorContains(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, target, false, false, nil, download), "download unavailable")
}

func TestOperationalValueJavaScriptDoesNotDownloadEvaluatorResources(t *testing.T) {
	target := t.TempDir()
	content := "---\ngraders:\n  operational-value:\n    script: return [{id: \"primary\", value: 1}]\n---\n"
	spec := &WorkflowSpec{RepoSpec: RepoSpec{RepoSlug: "owner/repo", Version: "main"}, WorkflowPath: ".github/workflows/test.md"}
	downloaded := false
	download := func(context.Context, string, string, string, string) ([]byte, error) {
		downloaded = true
		return nil, errors.New("unexpected download")
	}
	require.NoError(t, fetchAndSaveRemoteResourcesWithDownloader(t.Context(), content, spec, target, false, false, nil, download))
	assert.False(t, downloaded)
}

//go:build !integration

package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchWorkflowDependencies_UsesResolvedCommitWithoutChangingSourceRef(t *testing.T) {
	dir := testutil.TempDir(t, "add-dependencies-resolved-commit-*")
	originalDownload := downloadRemoteImportFile
	t.Cleanup(func() { downloadRemoteImportFile = originalDownload })

	const resolvedSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var downloadedPaths []string
	downloadRemoteImportFile = func(_ context.Context, owner, repo, path, ref string) ([]byte, error) {
		assert.Equal(t, "owner", owner)
		assert.Equal(t, "repo", repo)
		assert.Equal(t, resolvedSHA, ref)
		downloadedPaths = append(downloadedPaths, path)
		switch path {
		case ".github/workflows/shared/helper.md":
			return []byte("---\nimports:\n  - nested.md\n---\n# Helper\n"), nil
		case ".github/workflows/shared/nested.md":
			return []byte("# Nested helper\n"), nil
		default:
			return nil, fmt.Errorf("unexpected import path %s", path)
		}
	}

	spec := &WorkflowSpec{
		RepoSpec:     RepoSpec{RepoSlug: "owner/repo", Version: "main"},
		WorkflowPath: ".github/workflows/triage.md",
	}
	err := fetchWorkflowDependencies(context.Background(), spec, &FetchedWorkflow{CommitSHA: resolvedSHA},
		[]byte("---\nimports:\n  - shared/helper.md\n---\n# Triage\n"), dir, nil, AddOptions{})
	require.NoError(t, err)
	assert.Equal(t, "main", spec.Version, "dependency pinning must preserve the branch-tracking source ref")
	assert.Equal(t, []string{
		".github/workflows/shared/helper.md",
		".github/workflows/shared/nested.md",
	}, downloadedPaths)
	assert.FileExists(t, filepath.Join(dir, "shared", "helper.md"))
	assert.FileExists(t, filepath.Join(dir, "shared", "nested.md"))
}

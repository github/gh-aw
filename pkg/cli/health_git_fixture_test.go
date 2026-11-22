package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const healthSnapshotMetadata = `# gh-aw-metadata: {"schema_version":"v4","frontmatter_hash":"settings","body_hash":"prompt"}`

func createHealthGitFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, initTestGitRepo(root))
	_, err := runHealthGitContext(context.Background(), root, "remote", "add", "origin", "https://github.com/org/repo.git")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github/workflows"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github/workflows/example.md"), []byte("Original prompt"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github/workflows/example.lock.yml"), []byte(healthSnapshotMetadata), 0644))
	_, err = runHealthGitContext(context.Background(), root, "add", ".github/workflows")
	require.NoError(t, err)
	_, err = runHealthGitContext(context.Background(), root, "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	require.NoError(t, err)
	out, err := runHealthGitContext(context.Background(), root, "rev-parse", "HEAD")
	require.NoError(t, err)
	return root, strings.TrimSpace(string(out))
}

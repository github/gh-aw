package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthFleetIntentionalFailureUsesMatchingPinnedCheckout(t *testing.T) {
	root, _ := createHealthGitFixture(t)
	t.Chdir(root)
	source := filepath.Join(root, ".github/workflows/example.md")
	require.NoError(t, os.WriteFile(source, []byte("---\nfeatures:\n  intentional-failure: true\n---\n"), 0o644))
	_, err := runHealthGitContext(context.Background(), root, "add", ".github/workflows/example.md")
	require.NoError(t, err)
	_, err = runHealthGitContext(context.Background(), root, "-c", "commit.gpgsign=false", "commit", "-qm", "intentional")
	require.NoError(t, err)
	head, err := runHealthGitContext(context.Background(), root, "rev-parse", "HEAD")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(source, []byte("uncommitted edit"), 0o644))

	path := ".github/workflows/example.lock.yml"
	collection := &HealthCollection{Host: "github.com", repoOverride: "org/repo", pinnedBranchSHA: strings.TrimSpace(string(head))}
	assert.True(t, healthFleetIntentionalFailure(path, collection))

	mismatch := &HealthCollection{Host: "github.com", repoOverride: "other/repo", pinnedBranchSHA: strings.TrimSpace(string(head))}
	assert.False(t, healthFleetIntentionalFailure(path, mismatch))
	assert.False(t, healthFleetIntentionalFailure("../example.lock.yml", collection))
}

//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/stretchr/testify/require"
)

func TestCLIVersionCheckerUsesCodexModel(t *testing.T) {
	t.Parallel()

	repoRoot, err := gitutil.FindGitRoot()
	if err != nil {
		t.Skipf("Skipping test: not in a git repository: %v", err)
	}

	workflowPath := filepath.Join(repoRoot, ".github", "workflows", "cli-version-checker.md")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "model: openai/gpt-5.3-codex")
}

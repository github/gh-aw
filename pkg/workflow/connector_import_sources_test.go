//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func installConnectorTestSources(t *testing.T, root string) {
	t.Helper()
	files, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		target := filepath.Join(root, ".github", "workflows", "connectors", filepath.Base(file))
		if _, err := os.Stat(target); err == nil {
			continue
		} else {
			require.ErrorIs(t, err, os.ErrNotExist)
		}
		content, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0700))
		require.NoError(t, os.WriteFile(target, content, 0600))
	}
}

func TestConnectorLocalDeclarationsAreEditable(t *testing.T) {
	c, root := awConnectorCompiler(t)
	target := filepath.Join(root, ".github", "workflows", "connectors", "tracker.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0700))
	source := awConnectorSource(t)
	consumer := awConnectorHeader + "imports: [.github/workflows/connectors/tracker.md]\ntools:\n  tracker: [get-issue]\n---\nReview approved evidence.\n"
	for _, name := range []string{"EXAMPLE_READ_TOKEN", "LOCAL_REVIEW_TOKEN"} {
		content := "---\n" + strings.ReplaceAll(source, "EXAMPLE_READ_TOKEN", name) + "---\n"
		require.NoError(t, os.WriteFile(target, []byte(content), 0600))
		data, err := c.ParseWorkflowString(consumer, filepath.Join(root, ".github", "workflows", "review.md"))
		require.NoError(t, err)
		require.Equal(t, []string{name}, data.Connectors["tracker"].Agent.Secrets())
	}
}

func TestConnectorBuiltinImportsAreUnavailable(t *testing.T) {
	_, err := parser.ResolveIncludePath("@builtin:connectors/custom.md", t.TempDir(), nil)
	require.ErrorContains(t, err, "builtin file not found")
}

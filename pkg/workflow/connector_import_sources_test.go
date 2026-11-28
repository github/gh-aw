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
	consumer := awConnectorHeader + "imports:\n  - uses: \".github/workflows/connectors/tracker.md\"\ntools:\n  tracker: [get-issue]\n---\nReview approved evidence.\n"
	for _, name := range []string{"EXAMPLE_READ_TOKEN", "LOCAL_REVIEW_TOKEN"} {
		content := "---\n" + strings.ReplaceAll(source, "EXAMPLE_READ_TOKEN", name) + "---\n"
		require.NoError(t, os.WriteFile(target, []byte(content), 0600))
		data, err := c.ParseWorkflowString(consumer, filepath.Join(root, ".github", "workflows", "review.md"))
		require.NoError(t, err)
		require.Equal(t, []string{name}, data.Connectors["tracker"].Agent.Secrets())
	}
}

func TestEmbeddedConnectorDeclarationsMatchRepositorySources(t *testing.T) {
	sources, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.Len(t, builtinConnectorSources, len(sources))

	for _, source := range sources {
		id := strings.TrimSuffix(filepath.Base(source), ".md")
		sourceContent, err := os.ReadFile(source)
		require.NoError(t, err)
		embeddedContent, ok := builtinConnectorSources[id]
		require.True(t, ok, "missing generated connector %s", id)
		require.Equal(t, string(sourceContent), string(embeddedContent), "embedded connector %s is stale", id)
		require.True(t, parser.BuiltinVirtualFileExists(builtinConnectorPath(id)))
		registeredContent, err := parser.ReadFile(builtinConnectorPath(id))
		require.NoError(t, err)
		require.Equal(t, string(sourceContent), string(registeredContent))
	}
}

func TestConnectorShortNameImportsUseEmbeddedDeclarations(t *testing.T) {
	c, root := awConnectorCompiler(t)
	content := awConnectorHeader + `imports:
  - connector:linear
---
Review approved evidence.
`
	data, err := c.ParseWorkflowString(content, filepath.Join(root, ".github", "workflows", "review.md"))
	require.NoError(t, err)
	require.Empty(t, data.Connectors, "importing alone must not select a capability")

	frontmatter := map[string]any{"imports": []any{
		map[string]any{
			"uses": "connector:linear",
			"with": map[string]any{"teams": []any{"33333333-3333-4333-8333-333333333333"}},
		},
	}}
	require.NoError(t, c.normalizeImportedConnectors(frontmatter, filepath.Join(root, ".github", "workflows", "review.md"), ""))
	registrations, ok := frontmatter["connectors"].(map[string]any)
	require.True(t, ok)
	connector, ok := registrations["linear"].(map[string]any)
	require.True(t, ok)
	resources, ok := connector["resources"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"33333333-3333-4333-8333-333333333333"}, resources["teams"])
}

func TestCompilerRejectsBrokenLocalConnectorImports(t *testing.T) {
	for _, test := range []struct {
		path, message string
	}{
		{".github/workflows/connectors/missing.md", "not found"},
		{"./connectors/missing.md", "not found"},
		{".github/../../outside.md", "security"},
	} {
		for _, nested := range []bool{false, true} {
			t.Run(test.path+map[bool]string{false: "/direct", true: "/nested"}[nested], func(t *testing.T) {
				c, root := awConnectorCompiler(t)
				c.SetNoEmit(false)
				dir := filepath.Join(root, ".github", "workflows")
				require.NoError(t, os.MkdirAll(dir, 0700))
				imports := "imports:\n  - uses: \"" + test.path + "\"\n"
				if nested {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "shared.md"), []byte("---\n"+imports+"---\n"), 0600))
					imports = "imports:\n  - ./shared.md\n"
				}
				source := filepath.Join(dir, "broken.md")
				require.NoError(t, os.WriteFile(source, []byte(awConnectorHeader+imports+"---\nReview.\n"), 0600))
				require.ErrorContains(t, c.CompileWorkflow(source), test.message)
				_, err := os.Stat(filepath.Join(dir, "broken.lock.yml"))
				require.ErrorIs(t, err, os.ErrNotExist)
			})
		}
	}
}

func TestConnectorBuiltinImportsAreUnavailable(t *testing.T) {
	_, err := parser.ResolveIncludePath("@builtin:connectors/custom.md", t.TempDir(), nil)
	require.ErrorContains(t, err, "builtin file not found")
}

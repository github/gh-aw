//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotWebSearchRemovalCodemod(t *testing.T) {
	codemod := getCopilotWebSearchRemovalCodemod()
	assert.Equal(t, "copilot-web-search-removal", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.NotEmpty(t, codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
	require.NotNil(t, codemod.ApplyWithContext)

	for _, tc := range []struct {
		name    string
		engine  string
		tool    string
		removed bool
	}{
		{"Copilot CLI", "copilot", "true", true},
		{"Copilot SDK", "{id: copilot, copilot-sdk: true}", "{}", true},
		{"other engine", "codex", "true", false},
		{"disabled", "copilot", "false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content := "---\nengine: " + tc.engine + "\ntools:\n  web-search: " + tc.tool + " # search comment\n  bash: true # retain\n---\n\n# Body\n"
			fm, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)
			updated, applied, err := codemod.Apply(content, fm.Frontmatter)
			require.NoError(t, err)
			assert.Equal(t, tc.removed, applied)
			if !tc.removed {
				assert.Equal(t, content, updated)
				return
			}
			assert.Equal(t, "---\nengine: "+tc.engine+"\ntools:\n  bash: true # retain\n---\n\n# Body\n", updated)
			fm, err = parser.ExtractFrontmatterFromContent(updated)
			require.NoError(t, err)
			again, applied, err := codemod.Apply(updated, fm.Frontmatter)
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, updated, again)
		})
	}
}

func TestCopilotWebSearchRemovalCodemod_ResolvesIncludedEngine(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "engine.md"), []byte("---\nengine: copilot\n---\n"), 0o600))
	content := "---\ntools: {web-search: true, bash: true} # tools comment\n---\n@include engine.md\n"
	fm, err := parser.ExtractFrontmatterFromContent(content)
	require.NoError(t, err)
	codemod := getCopilotWebSearchRemovalCodemod()
	updated, applied, err := codemod.ApplyWithContext(content, fm.Frontmatter, filepath.Join(dir, "workflow.md"))
	require.NoError(t, err)
	require.True(t, applied)
	assert.Contains(t, updated, "tools comment")
	assert.Contains(t, updated, "bash: true")
	assert.NotContains(t, updated, "web-search")

	unresolved := "---\ntools:\n  web-search: true\n---\n@include missing.md\n"
	fm, err = parser.ExtractFrontmatterFromContent(unresolved)
	require.NoError(t, err)
	updated, applied, err = codemod.ApplyWithContext(unresolved, fm.Frontmatter, filepath.Join(dir, "workflow.md"))
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, unresolved, updated)
}

func TestCopilotWebFetchRemovalCodemodPreservesNestedFragmentWithoutEngine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".github", "workflows")
	filePath := filepath.Join(dir, "shared", "fragment.md")
	content := "---\ntools:\n  web-fetch: true\n---\n"
	fm, err := parser.ExtractFrontmatterFromContent(content)
	require.NoError(t, err)

	updated, applied, err := getCopilotWebFetchRemovalCodemod().ApplyWithContext(content, fm.Frontmatter, filePath)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, updated)

	withEngine := strings.Replace(content, "---\n", "---\nengine: copilot\n", 1)
	fm, err = parser.ExtractFrontmatterFromContent(withEngine)
	require.NoError(t, err)
	updated, applied, err = getCopilotWebFetchRemovalCodemod().ApplyWithContext(withEngine, fm.Frontmatter, filePath)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, updated, "web-fetch")
}

func TestCopilotWebSearchRemovalCodemod_FixesStrictCompilation(t *testing.T) {
	content := "---\non: workflow_dispatch\nstrict: true\nengine: copilot\ntools:\n  web-search:\n---\nSearch.\n"
	compiler := workflow.NewCompiler()
	_, err := compiler.ParseWorkflowString(content, "web-search.md")
	require.ErrorContains(t, err, "Copilot's native 'web-search' tool is unavailable")
	path := filepath.Join(t.TempDir(), "web-search.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	fixed, _, err := processWorkflowFileWithInfo(path, GetAllCodemods(), true, false)
	require.NoError(t, err)
	require.True(t, fixed)
	updated, err := os.ReadFile(path)
	require.NoError(t, err)
	compiler = workflow.NewCompiler()
	_, err = compiler.ParseWorkflowString(string(updated), path)
	require.NoError(t, err)
}

//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopilotWebFetchRemovalCodemod_Metadata(t *testing.T) {
	t.Parallel()

	codemod := getCopilotWebFetchRemovalCodemod()
	assert.Equal(t, "copilot-web-fetch-removal", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "1.0.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestCopilotWebFetchRemovalCodemod_Apply(t *testing.T) {
	t.Parallel()

	content := `---
engine:
  id: copilot
tools:
  web-fetch: true # native fetch is unavailable
  bash: ["git"]
---

# Keep this body intact.
`
	frontmatter := map[string]any{
		"engine": map[string]any{"id": "copilot"},
		"tools":  map[string]any{"web-fetch": true, "bash": []any{"git"}},
	}

	codemod := getCopilotWebFetchRemovalCodemod()
	updated, applied, err := codemod.Apply(content, frontmatter)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, `---
engine:
  id: copilot
tools:
  bash: ["git"]
---

# Keep this body intact.
`, updated)

	updatedAgain, appliedAgain, err := codemod.Apply(updated, frontmatter)
	require.NoError(t, err)
	assert.False(t, appliedAgain)
	assert.Equal(t, updated, updatedAgain)
}

func TestCopilotWebFetchRemovalCodemod_CompatibleInputsAreUnchanged(t *testing.T) {
	t.Parallel()

	codemod := getCopilotWebFetchRemovalCodemod()
	tests := []struct {
		name        string
		frontmatter map[string]any
		content     string
	}{
		{
			name: "Copilot SDK custom fetch",
			frontmatter: map[string]any{
				"engine": map[string]any{"id": "copilot", "copilot-sdk": true},
				"tools":  map[string]any{"web-fetch": true},
			},
			content: "---\nengine:\n  id: copilot\n  copilot-sdk: true\ntools:\n  web-fetch:\n---\n",
		},
		{
			name:        "other engine",
			frontmatter: map[string]any{"engine": "codex", "tools": map[string]any{"web-fetch": true}},
			content:     "---\nengine: codex\ntools:\n  web-fetch:\n---\n",
		},
		{
			name:        "explicitly disabled",
			frontmatter: map[string]any{"engine": "copilot", "tools": map[string]any{"web-fetch": false}},
			content:     "---\nengine: copilot\ntools:\n  web-fetch: false\n---\n",
		},
		{
			name:        "no web-fetch setting",
			frontmatter: map[string]any{"engine": "copilot", "tools": map[string]any{"bash": true}},
			content:     "---\nengine: copilot\ntools:\n  bash: true\n---\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			updated, applied, err := codemod.Apply(tt.content, tt.frontmatter)
			require.NoError(t, err)
			assert.False(t, applied)
			assert.Equal(t, tt.content, updated)
		})
	}
}

func TestCopilotWebFetchRemovalCodemod_FixesStrictCompilation(t *testing.T) {
	t.Parallel()

	content := `---
on: workflow_dispatch
strict: true
engine:
  id: copilot
tools:
  web-fetch:
---
Fetch content.
`
	compiler := workflow.NewCompiler()
	_, err := compiler.ParseWorkflowString(content, "copilot-web-fetch.md")
	require.ErrorContains(t, err, "Copilot's native 'web-fetch' tool is unavailable")

	path := filepath.Join(t.TempDir(), "copilot-web-fetch.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	fixed, _, err := processWorkflowFileWithInfo(path, GetAllCodemods(), true, false)
	require.NoError(t, err)
	require.True(t, fixed)

	updatedBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	updated := string(updatedBytes)
	compiler = workflow.NewCompiler()
	data, err := compiler.ParseWorkflowString(updated, "copilot-web-fetch.md")
	require.NoError(t, err)
	require.NoError(t, compiler.CompileWorkflowData(data, filepath.Join(t.TempDir(), "copilot-web-fetch.md")))
}

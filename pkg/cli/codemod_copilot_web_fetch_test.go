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

func TestCopilotWebFetchRemovalCodemod_Metadata(t *testing.T) {
	t.Parallel()

	codemod := getCopilotWebFetchRemovalCodemod()
	assert.Equal(t, "copilot-web-fetch-removal", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "1.0.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
	require.NotNil(t, codemod.ApplyWithContext)
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

func TestCopilotWebFetchRemovalCodemod_YAMLFormsAndLiteralText(t *testing.T) {
	t.Parallel()

	codemod := getCopilotWebFetchRemovalCodemod()
	tests := []struct {
		name            string
		content         string
		hasBash         bool
		expectedComment string
	}{
		{
			name: "flow mapping and quoted block key",
			content: `---
engine: copilot
tools: {web-fetch: true, bash: ["git"]} # keep tools comment
description: |
  Example:
    tools:
      'web-fetch': example
---
`,
			hasBash:         true,
			expectedComment: "# keep tools comment",
		},
		{
			name: "quoted key with sibling literal",
			content: `---
engine: copilot
tools:
  'web-fetch': true
  bash: ["git"] # keep sibling comment
description: |
  tools:
    web-fetch: example
---
`,
			hasBash:         true,
			expectedComment: "# keep sibling comment",
		},
		{
			name: "flow mapping with only web-fetch",
			content: `---
engine: copilot
tools: {web-fetch: true}
description: |
  tools:
    web-fetch: example
---
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			frontmatter, err := parser.ExtractFrontmatterFromContent(tt.content)
			require.NoError(t, err)
			updated, applied, err := codemod.Apply(tt.content, frontmatter.Frontmatter)
			require.NoError(t, err)
			require.True(t, applied)

			updatedFrontmatter, err := parser.ExtractFrontmatterFromContent(updated)
			require.NoError(t, err)
			if tt.hasBash {
				tools := updatedFrontmatter.Frontmatter["tools"].(map[string]any)
				assert.NotContains(t, tools, "web-fetch")
				assert.Contains(t, tools, "bash")
			} else {
				assert.NotContains(t, updatedFrontmatter.Frontmatter, "tools")
			}
			assert.Equal(t, frontmatter.Frontmatter["description"], updatedFrontmatter.Frontmatter["description"])
			if tt.expectedComment != "" {
				assert.Contains(t, updated, tt.expectedComment)
			}
		})
	}
}

func TestCopilotWebFetchRemovalCodemod_ResolvesIncludedEngine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		engineSource string
		importEngine bool
		expectedID   string
		copilotSDK   bool
		shouldRemove bool
	}{
		{
			name: "imported Copilot SDK",
			engineSource: `engine:
  id: copilot
  copilot-sdk: true`,
			importEngine: true,
			expectedID:   "copilot",
			copilotSDK:   true,
		},
		{
			name:         "included other engine",
			engineSource: "engine: codex",
			expectedID:   "codex",
		},
		{
			name:         "included Copilot CLI",
			engineSource: "engine: copilot",
			expectedID:   "copilot",
			shouldRemove: true,
		},
	}
	codemod := getCopilotWebFetchRemovalCodemod()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			includePath := filepath.Join(dir, "engine.md")
			include := "---\n" + tt.engineSource + "\n---\n"
			require.NoError(t, os.WriteFile(includePath, []byte(include), 0o600))

			content := `---
on: issue_comment
tools:
  web-fetch: true
---
`
			if tt.importEngine {
				content = strings.Replace(content, "---\n", "---\nimports:\n  - engine.md\n", 1)
			} else {
				content += "@include engine.md\n"
			}
			path := filepath.Join(dir, "workflow.md")
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			frontmatter, err := parser.ExtractFrontmatterFromContent(content)
			require.NoError(t, err)

			engineConfig, err := workflow.NewCompiler().ResolveEffectiveEngineConfig(content, path)
			require.NoError(t, err)
			require.NotNil(t, engineConfig)
			assert.Equal(t, tt.expectedID, engineConfig.ID)
			assert.Equal(t, tt.copilotSDK, engineConfig.CopilotSDK)

			updated, applied, err := codemod.ApplyWithContext(content, frontmatter.Frontmatter, path)
			require.NoError(t, err)
			assert.Equal(t, tt.shouldRemove, applied)
			if tt.shouldRemove {
				assert.NotContains(t, updated, "web-fetch")
			} else {
				assert.Equal(t, content, updated)
			}
		})
	}
}

func TestCopilotWebFetchRemovalCodemod_PreservesSettingWhenEngineResolutionFails(t *testing.T) {
	t.Parallel()

	content := `---
on: issue_comment
tools:
  web-fetch: true
---
@include missing-engine.md
`
	frontmatter, err := parser.ExtractFrontmatterFromContent(content)
	require.NoError(t, err)
	codemod := getCopilotWebFetchRemovalCodemod()

	updated, applied, err := codemod.ApplyWithContext(content, frontmatter.Frontmatter, filepath.Join(t.TempDir(), "workflow.md"))
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, updated)
}

//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubAppTokenPermissionsCodemodMetadata(t *testing.T) {
	t.Parallel()

	codemod := getGitHubAppTokenPermissionsCodemod()
	assert.Equal(t, "github-app-token-permissions", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "1.5.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestGitHubAppTokenPermissionsCodemodInfersReadScopes(t *testing.T) {
	t.Parallel()

	content := `---
jobs:
  inspect:
    steps:
      - id: app-token
        uses: actions/create-github-app-token@v3
        with:
          app-id: ${{ vars.APP_ID }} # keep this comment
          private-key: ${{ secrets.APP_KEY }}
      - name: Read pull request
        env:
          GH_TOKEN: ${{ steps.app-token.outputs.token }}
        run: gh pr view 123
---
# Preserve the workflow body.
`
	codemod := getGitHubAppTokenPermissionsCodemod()
	updated, applied, err := codemod.Apply(content, nil)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Contains(t, updated, "permission-pull-requests: read")
	assert.NotContains(t, updated, "permission-contents: none")
	assert.NotContains(t, updated, githubAppTokenPermissionsWarning)
	assert.Contains(t, updated, "app-id: ${{ vars.APP_ID }} # keep this comment")
	assert.Contains(t, updated, "# Preserve the workflow body.")

	again, applied, err := codemod.Apply(updated, nil)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, updated, again)
}

func TestGitHubAppTokenPermissionsCodemodAddsMarkedFallback(t *testing.T) {
	t.Parallel()

	content := `---
jobs: # keep the jobs comment
  publish:
    steps:
      - uses: actions/create-github-app-token@v3
        with:
          app-id: ${{ vars.APP_ID }}
      - name: Use token indirectly
        run: ./publish.sh
---
Keep the body.
`
	codemod := getGitHubAppTokenPermissionsCodemod()
	updated, applied, err := codemod.Apply(content, nil)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Contains(t, updated, "permission-contents: none")
	assert.Contains(t, updated, "steps: # "+githubAppTokenPermissionsWarning)
	assert.Contains(t, updated, "Keep the body.")

	again, applied, err := codemod.Apply(updated, nil)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, updated, again)
}

func TestGitHubAppTokenPermissionsCodemodDoesNotInferWritesOrReplaceExistingInputs(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		`---
jobs:
  publish:
    steps:
      - id: app-token
        uses: actions/create-github-app-token@v3
        with:
          app-id: ${{ vars.APP_ID }}
      - env:
          GH_TOKEN: ${{ steps.app-token.outputs.token }}
        run: gh pr create --title test
---`,
		`---
jobs:
  publish:
    steps:
      - uses: actions/create-github-app-token@v3
        with:
          permission-issues: write
---`,
		`---
jobs:
  publish:
    steps:
      - uses: actions/checkout@v4
---`,
	} {
		updated, applied, err := getGitHubAppTokenPermissionsCodemod().Apply(content, nil)
		require.NoError(t, err)
		if content == updated {
			assert.False(t, applied)
		} else {
			assert.True(t, applied)
			assert.Contains(t, updated, "permission-contents: none")
			assert.Contains(t, updated, githubAppTokenPermissionsWarning)
		}
	}
}

func TestFixRecursivelyProcessesNestedWorkflowFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"root.md": `---
engine: copilot
tools:
  web-fetch: true # unavailable in offline mode
  bash: ["git"]
---
Keep this body.
`,
		filepath.Join("shared", "nested", "shared.md"): `---
engine: copilot
tools: {web-fetch: true, bash: ["git"]} # keep mapping comment
---
Keep this body.
`,
	}
	for relativePath, content := range files {
		path := filepath.Join(dir, relativePath)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	discovered, err := getMarkdownWorkflowFilesRecursively(dir)
	require.NoError(t, err)
	require.Len(t, discovered, len(files))
	discoveredPaths := make(map[string]bool, len(discovered))
	for _, path := range discovered {
		relativePath, err := filepath.Rel(dir, path)
		require.NoError(t, err)
		discoveredPaths[relativePath] = true
		fixed, _, err := processWorkflowFileWithInfo(path, []Codemod{getCopilotWebFetchRemovalCodemod()}, false, false)
		require.NoError(t, err)
		assert.True(t, fixed, path)
	}
	for path := range files {
		assert.True(t, discoveredPaths[path], path)
	}
}

func TestGitHubAppTokenPermissionsCodemodVisitsAuthoredStepSections(t *testing.T) {
	t.Parallel()

	content := `---
pre-steps:
  - uses: actions/create-github-app-token@v3
steps:
  - uses: actions/create-github-app-token@v3
pre-agent-steps:
  - uses: actions/create-github-app-token@v3
post-steps:
  - uses: actions/create-github-app-token@v3
jobs:
  build:
    setup-steps:
      - uses: actions/create-github-app-token@v3
    steps:
      - uses: actions/create-github-app-token@v3
    post-steps:
      - uses: actions/create-github-app-token@v3
---`

	updated, applied, err := getGitHubAppTokenPermissionsCodemod().Apply(content, nil)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, 7, strings.Count(updated, "permission-contents: none\n"))
	assert.Contains(t, updated, "pre-steps: # "+githubAppTokenPermissionsWarning)
	assert.Contains(t, updated, "pre-agent-steps: # "+githubAppTokenPermissionsWarning)
	assert.Contains(t, updated, "post-steps: # "+githubAppTokenPermissionsWarning)
	assert.Contains(t, updated, "setup-steps: # "+githubAppTokenPermissionsWarning)
	assert.Contains(t, updated, "steps: # "+githubAppTokenPermissionsWarning)
}

func TestGitHubAppTokenPermissionsCodemodDetectsCompactAndFallbackReferences(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name           string
		consumerEnv    string
		consumerRun    string
		extraConsumer  string
		wantPermission bool
	}{
		{
			name:           "compact direct binding",
			consumerEnv:    "${{steps.app.outputs.token}}",
			consumerRun:    "gh pr view 123",
			wantPermission: true,
		},
		{
			name:        "fallback expression after canonical read",
			consumerEnv: "${{steps.app.outputs.token}}",
			consumerRun: "gh pr view 123",
			extraConsumer: `      - env:
          GH_TOKEN: ${{ steps.app.outputs.token || github.token }}
        run: gh issue create --title test`,
			wantPermission: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := `---
jobs:
  build:
    steps:
      - id: app
        uses: actions/create-github-app-token@v3
      - env:
          GH_TOKEN: ` + tt.consumerEnv + `
        run: ` + tt.consumerRun + `
` + tt.extraConsumer + `
---`
			updated, applied, err := getGitHubAppTokenPermissionsCodemod().Apply(content, nil)
			require.NoError(t, err)
			require.True(t, applied)
			if tt.wantPermission {
				assert.Contains(t, updated, "permission-pull-requests: read")
				assert.NotContains(t, updated, "permission-contents: none")
			} else {
				assert.Contains(t, updated, "permission-contents: none")
				assert.NotContains(t, updated, "permission-pull-requests: read")
			}
		})
	}
}

func TestGitHubAppTokenPermissionsCodemodDoesNotInferUnsupportedTokenUses(t *testing.T) {
	t.Parallel()

	content := `---
jobs:
  publish:
    steps:
      - id: app-token
        uses: actions/create-github-app-token@v3
        with:
          app-id: ${{ vars.APP_ID }}
      - name: Pass token to another action
        with:
          token: ${{ steps.app-token.outputs.token }}
        uses: owner/action@v1
      - env:
          GH_TOKEN: ${{ steps.app-token.outputs.token }}
        run: gh pr view 123
---`
	updated, applied, err := getGitHubAppTokenPermissionsCodemod().Apply(content, nil)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Contains(t, updated, "permission-contents: none")
	assert.Contains(t, updated, githubAppTokenPermissionsWarning)
}

func TestGitHubAppTokenPermissionsCodemodPreservesNoopForWorkflowWithoutToken(t *testing.T) {
	t.Parallel()

	content := `---
jobs:
  publish:
    steps:
      - uses: actions/checkout@v4
---`
	updated, applied, err := getGitHubAppTokenPermissionsCodemod().Apply(content, nil)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, updated)
}

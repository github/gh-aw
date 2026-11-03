//go:build !integration

package cli

import (
	"fmt"
	"os"
	"path/filepath"
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
	assert.Contains(t, updated, "jobs: # keep the jobs comment # "+githubAppTokenPermissionsWarning)
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
	for i := range 15 {
		directory := dir
		if i > 0 {
			directory = filepath.Join(dir, "shared", "nested")
		}
		path := filepath.Join(directory, fmt.Sprintf("workflow-%02d.md", i))
		content := `---
engine: copilot
tools:
  web-fetch: true # unavailable in offline mode
  bash: ["git"]
---
Keep this body.
`
		if i%2 == 1 {
			content = `---
engine: copilot
tools: {web-fetch: true, bash: ["git"]} # keep mapping comment
---
Keep this body.
`
		}
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	discovered, err := getMarkdownWorkflowFilesRecursively(dir)
	require.NoError(t, err)
	require.Len(t, discovered, 15)
	for _, path := range discovered {
		fixed, _, err := processWorkflowFileWithInfo(path, []Codemod{getCopilotWebFetchRemovalCodemod()}, false, false)
		require.NoError(t, err)
		assert.True(t, fixed, path)
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

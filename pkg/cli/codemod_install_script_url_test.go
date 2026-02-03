//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetInstallScriptURLCodemod(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	// Verify codemod metadata
	assert.Equal(t, "install-script-url-migration", codemod.ID, "Codemod ID should match")
	assert.Equal(t, "Migrate install script URL from githubnext/gh-aw to github/gh-aw", codemod.Name, "Codemod name should match")
	assert.NotEmpty(t, codemod.Description, "Codemod should have a description")
	assert.Equal(t, "0.9.0", codemod.IntroducedIn, "Codemod version should match")
	require.NotNil(t, codemod.Apply, "Codemod should have an Apply function")
}

func TestInstallScriptURLCodemod_RawGitHubUserContent(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    runs-on: ubuntu-latest
    steps:
      - name: Install gh-aw
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/main/install-gh-aw.sh | bash
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"jobs": map[string]any{
			"setup": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{
						"name": "Install gh-aw",
						"run":  "curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/main/install-gh-aw.sh | bash",
					},
				},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh", "Result should contain updated URL")
	assert.NotContains(t, result, "githubnext/gh-aw", "Result should not contain old URL")
}

func TestInstallScriptURLCodemod_RefsHeadsMain(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    steps:
      - name: Install gh-aw extension
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/refs/heads/main/install-gh-aw.sh | bash
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "https://raw.githubusercontent.com/github/gh-aw/refs/heads/main/install-gh-aw.sh", "Result should contain updated URL")
	assert.NotContains(t, result, "githubnext/gh-aw", "Result should not contain old URL")
}

func TestInstallScriptURLCodemod_ShortForm(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    steps:
      - name: Install extension
        run: gh extension install githubnext/gh-aw
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "github/gh-aw", "Result should contain updated repo")
	assert.NotContains(t, result, "githubnext/gh-aw", "Result should not contain old repo")
}

func TestInstallScriptURLCodemod_AlreadyMigrated(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    steps:
      - name: Install gh-aw
        run: curl -fsSL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes when already migrated")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

func TestInstallScriptURLCodemod_NoInstallScript(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Run tests
        run: npm test
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes when no install script found")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

func TestInstallScriptURLCodemod_MultipleOccurrences(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    steps:
      - name: Install gh-aw
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/main/install-gh-aw.sh | bash
      - name: Install extension
        run: gh extension install githubnext/gh-aw
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh", "Result should contain updated URL")
	assert.Contains(t, result, "gh extension install github/gh-aw", "Result should contain updated repo")
	assert.NotContains(t, result, "githubnext/gh-aw", "Result should not contain old references")
}

func TestInstallScriptURLCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    steps:
      - name: Install gh-aw
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/main/install-gh-aw.sh | bash
---

# Test Workflow

This workflow installs gh-aw from githubnext.

## Steps
- Download install script
- Run install script

` + "```bash" + `
curl -fsSL https://example.com/script.sh | bash
` + "```"

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "# Test Workflow", "Result should preserve markdown")
	assert.Contains(t, result, "## Steps", "Result should preserve markdown sections")
	assert.Contains(t, result, "```bash", "Result should preserve code blocks")
	assert.Contains(t, result, "This workflow installs gh-aw from githubnext", "Result should preserve markdown text")
}

func TestInstallScriptURLCodemod_PreservesIndentation(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    runs-on: ubuntu-latest
    steps:
      - name: Install gh-aw
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/main/install-gh-aw.sh | bash
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	// Check that indentation is preserved
	assert.Contains(t, result, "      - name: Install gh-aw", "Result should preserve indentation")
	assert.Contains(t, result, "        run: curl", "Result should preserve indentation")
}

func TestInstallScriptURLCodemod_DifferentBranches(t *testing.T) {
	codemod := getInstallScriptURLCodemod()

	content := `---
on: workflow_dispatch
jobs:
  setup:
    steps:
      - name: Install from develop
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/develop/install-gh-aw.sh | bash
      - name: Install from specific tag
        run: curl -fsSL https://raw.githubusercontent.com/githubnext/gh-aw/v1.0.0/install-gh-aw.sh | bash
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.Contains(t, result, "https://raw.githubusercontent.com/github/gh-aw/develop/install-gh-aw.sh", "Result should update develop branch")
	assert.Contains(t, result, "https://raw.githubusercontent.com/github/gh-aw/v1.0.0/install-gh-aw.sh", "Result should update tag reference")
	assert.NotContains(t, result, "githubnext/gh-aw", "Result should not contain old references")
}

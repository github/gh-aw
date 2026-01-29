//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCommandToSlashCommandCodemod(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	assert.Equal(t, "command-to-slash-command-migration", codemod.ID)
	assert.Equal(t, "Migrate on.command to on.slash_command", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.2.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestSlashCommandCodemod_BasicMigration(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
on:
  command: /test
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": map[string]any{
			"command": "/test",
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "slash_command: /test")
	assert.NotContains(t, result, "  command: /test")
}

func TestSlashCommandCodemod_PreservesIndentation(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
on:
  command: /deploy
  workflow_dispatch:
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"command":           "/deploy",
			"workflow_dispatch": nil,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "  slash_command: /deploy")
}

func TestSlashCommandCodemod_PreservesComment(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
on:
  command: /run  # Run the workflow
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"command": "/run",
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "slash_command: /run  # Run the workflow")
}

func TestSlashCommandCodemod_NoCommandField(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
on:
  workflow_dispatch:
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"workflow_dispatch": nil,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSlashCommandCodemod_NoOnField(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSlashCommandCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
on:
  command: /test
---

# Test Workflow

This is a test with markdown:
- Item 1
- Item 2

` + "```yaml" + `
key: value
` + "```"

	frontmatter := map[string]any{
		"on": map[string]any{
			"command": "/test",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "- Item 1")
	assert.Contains(t, result, "```yaml")
}

func TestSlashCommandCodemod_MultipleOnTriggers(t *testing.T) {
	codemod := getCommandToSlashCommandCodemod()

	content := `---
on:
  command: /build
  push:
    branches:
      - main
  workflow_dispatch:
---

# Test`

	frontmatter := map[string]any{
		"on": map[string]any{
			"command": "/build",
			"push": map[string]any{
				"branches": []any{"main"},
			},
			"workflow_dispatch": nil,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "slash_command: /build")
	assert.Contains(t, result, "push:")
	assert.Contains(t, result, "workflow_dispatch:")
}

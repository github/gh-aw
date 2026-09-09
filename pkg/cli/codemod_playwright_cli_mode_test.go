//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetPlaywrightCLIModeRemovalCodemod(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	assert.Equal(t, "playwright-cli-mode-removal", codemod.ID, "Codemod ID should match")
	assert.Equal(t, "Remove redundant 'tools.playwright.mode: cli'", codemod.Name, "Codemod name should match")
	assert.NotEmpty(t, codemod.Description, "Codemod should have a description")
	assert.Equal(t, "1.5.0", codemod.IntroducedIn, "Codemod version should match")
	require.NotNil(t, codemod.Apply, "Codemod should have an Apply function")
}

func TestPlaywrightCLIModeRemovalCodemod_NoTools(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Should not apply when no tools block")
	assert.Equal(t, content, result, "Content should be unchanged")
}

func TestPlaywrightCLIModeRemovalCodemod_NoPlaywright(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  github:
    mode: remote
---

# Test`

	frontmatter := map[string]any{
		"tools": map[string]any{
			"github": map[string]any{"mode": "remote"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Should not apply when no playwright tool")
	assert.Equal(t, content, result, "Content should be unchanged")
}

func TestPlaywrightCLIModeRemovalCodemod_NoMode(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  playwright:
    version: "0.1.18"
---

# Test`

	frontmatter := map[string]any{
		"tools": map[string]any{
			"playwright": map[string]any{"version": "0.1.18"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Should not apply when mode is absent")
	assert.Equal(t, content, result, "Content should be unchanged")
}

func TestPlaywrightCLIModeRemovalCodemod_LeavesMCPModeUntouched(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  playwright:
    mode: mcp
---

# Test`

	frontmatter := map[string]any{
		"tools": map[string]any{
			"playwright": map[string]any{"mode": "mcp"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Should not touch mode: mcp")
	assert.Equal(t, content, result, "Content should be unchanged")
}

func TestPlaywrightCLIModeRemovalCodemod_RemovesModeOnly(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  playwright:
    mode: cli
    version: "0.1.18"
  bash:
    - "date *"
---

# Test`

	frontmatter := map[string]any{
		"tools": map[string]any{
			"playwright": map[string]any{"mode": "cli", "version": "0.1.18"},
			"bash":       []any{"date *"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Should apply when mode: cli is present")
	assert.NotContains(t, result, "mode: cli", "mode: cli should be removed:\n%s", result)
	assert.Contains(t, result, `version: "0.1.18"`, "version should be preserved:\n%s", result)
	assert.Contains(t, result, "bash:", "unrelated bash tool should be preserved:\n%s", result)
}

func TestPlaywrightCLIModeRemovalCodemod_RemovesOnlyField(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  playwright:
    mode: cli
---

# Test`

	frontmatter := map[string]any{
		"tools": map[string]any{
			"playwright": map[string]any{"mode": "cli"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Should apply when mode: cli is the only field")
	assert.NotContains(t, result, "mode:", "mode field should be removed:\n%s", result)
	assert.Contains(t, result, "playwright:", "bare playwright: block should be preserved:\n%s", result)
}

func TestPlaywrightCLIModeRemovalCodemod_Idempotent(t *testing.T) {
	t.Parallel()
	codemod := getPlaywrightCLIModeRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  playwright:
---

# Test`

	frontmatter := map[string]any{
		"tools": map[string]any{
			"playwright": nil,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Should not apply to an already-migrated bare playwright block")
	assert.Equal(t, content, result, "Content should be unchanged")
}

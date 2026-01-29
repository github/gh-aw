//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSafeInputsModeCodemod(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	assert.Equal(t, "safe-inputs-mode-removal", codemod.ID)
	assert.Equal(t, "Remove deprecated safe-inputs.mode field", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.2.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestSafeInputsModeCodemod_RemovesMode(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	content := `---
on: workflow_dispatch
safe-inputs:
  mode: http
  max-size: 100KB
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-inputs": map[string]any{
			"mode":     "http",
			"max-size": "100KB",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "mode:")
	assert.Contains(t, result, "max-size: 100KB")
}

func TestSafeInputsModeCodemod_NoSafeInputsField(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	content := `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSafeInputsModeCodemod_NoModeField(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	content := `---
on: workflow_dispatch
safe-inputs:
  max-size: 100KB
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-inputs": map[string]any{
			"max-size": "100KB",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSafeInputsModeCodemod_PreservesIndentation(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	content := `---
on: workflow_dispatch
safe-inputs:
  mode: http
  max-size: 100KB
  timeout: 30s
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-inputs": map[string]any{
			"mode":     "http",
			"max-size": "100KB",
			"timeout":  "30s",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "mode:")
	assert.Contains(t, result, "  max-size: 100KB")
	assert.Contains(t, result, "  timeout: 30s")
}

func TestSafeInputsModeCodemod_PreservesComments(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	content := `---
on: workflow_dispatch
safe-inputs:
  mode: http  # HTTP mode is now the default
  max-size: 100KB  # Maximum size for inputs
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-inputs": map[string]any{
			"mode":     "http",
			"max-size": "100KB",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "mode:")
	assert.Contains(t, result, "max-size: 100KB  # Maximum size for inputs")
}

func TestSafeInputsModeCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getSafeInputsModeCodemod()

	content := `---
on: workflow_dispatch
safe-inputs:
  mode: http
---

# Test Workflow

This workflow uses safe inputs.`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-inputs": map[string]any{
			"mode": "http",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "This workflow uses safe inputs.")
}

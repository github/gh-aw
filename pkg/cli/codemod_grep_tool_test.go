//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetGrepToolRemovalCodemod(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	// Verify codemod metadata
	assert.Equal(t, "grep-tool-removal", codemod.ID, "Codemod ID should match")
	assert.Equal(t, "Remove deprecated tools.grep field", codemod.Name, "Codemod name should match")
	assert.NotEmpty(t, codemod.Description, "Codemod should have a description")
	assert.Equal(t, "0.7.0", codemod.IntroducedIn, "Codemod version should match")
	require.NotNil(t, codemod.Apply, "Codemod should have an Apply function")
}

func TestGrepToolRemovalCodemod_BasicRemoval(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  bash: ["echo", "ls"]
  grep: true
  github:
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"tools": map[string]any{
			"bash":   []any{"echo", "ls"},
			"grep":   true,
			"github": nil,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.NotContains(t, result, "grep:", "Result should not contain grep field")
	assert.Contains(t, result, "bash:", "Result should preserve bash tool")
	assert.Contains(t, result, "github:", "Result should preserve github tool")
}

func TestGrepToolRemovalCodemod_NoToolsBlock(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes when tools block doesn't exist")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

func TestGrepToolRemovalCodemod_NoGrepField(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  bash: ["echo", "ls"]
  github:
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"tools": map[string]any{
			"bash":   []any{"echo", "ls"},
			"github": nil,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes when grep field doesn't exist")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

func TestGrepToolRemovalCodemod_PreservesOtherTools(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  bash:
    - echo
    - ls
    - pwd
  grep: true
  github:
    toolsets: [default]
  playwright:
    version: "v1.41.0"
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"tools": map[string]any{
			"bash":       []any{"echo", "ls", "pwd"},
			"grep":       true,
			"github":     map[string]any{"toolsets": []any{"default"}},
			"playwright": map[string]any{"version": "v1.41.0"},
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.NotContains(t, result, "grep:", "Result should not contain grep field")
	assert.Contains(t, result, "bash:", "Result should preserve bash tool")
	assert.Contains(t, result, "github:", "Result should preserve github tool")
	assert.Contains(t, result, "playwright:", "Result should preserve playwright tool")
	assert.Contains(t, result, "toolsets: [default]", "Result should preserve github config")
}

func TestGrepToolRemovalCodemod_GrepWithFalseValue(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  bash: ["echo"]
  grep: false
permissions:
  contents: read
---

# Test Workflow`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"tools": map[string]any{
			"bash": []any{"echo"},
			"grep": false,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes even when grep is false")
	assert.NotContains(t, result, "grep:", "Result should not contain grep field")
}

func TestGrepToolRemovalCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
tools:
  grep: true
  bash: ["echo"]
---

# Test Workflow

This workflow uses grep.

## Features
- Feature 1
- Feature 2

` + "```bash" + `
echo "test"
` + "```"

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"tools": map[string]any{
			"grep": true,
			"bash": []any{"echo"},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.True(t, applied, "Codemod should report changes")
	assert.NotContains(t, result, "grep:", "Result should not contain grep field")
	assert.Contains(t, result, "# Test Workflow", "Result should preserve markdown")
	assert.Contains(t, result, "## Features", "Result should preserve markdown sections")
	assert.Contains(t, result, "```bash", "Result should preserve code blocks")
}

func TestGrepToolRemovalCodemod_ToolsAsInvalidType(t *testing.T) {
	codemod := getGrepToolRemovalCodemod()

	content := `---
on: workflow_dispatch
tools: simple_string
---

# Test Workflow`

	frontmatter := map[string]any{
		"on":    "workflow_dispatch",
		"tools": "simple_string",
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes when tools is not a map")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

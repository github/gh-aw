//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSandboxFalseToAgentFalseCodemod(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	assert.Equal(t, "sandbox-false-to-agent-false", codemod.ID)
	assert.Equal(t, "Convert sandbox: false to sandbox.agent: false", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.10.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestSandboxFalseToAgentFalseCodemod_ConvertsBooleanFalse(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	content := `---
on: workflow_dispatch
sandbox: false
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on":      "workflow_dispatch",
		"sandbox": false,
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "sandbox: false")
	assert.Contains(t, result, "sandbox:")
	assert.Contains(t, result, "  agent: false")
}

func TestSandboxFalseToAgentFalseCodemod_PreservesIndentation(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	content := `---
on: workflow_dispatch
sandbox: false
engine: copilot
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on":      "workflow_dispatch",
		"sandbox": false,
		"engine":  "copilot",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "sandbox:")
	assert.Contains(t, result, "  agent: false")
}

func TestSandboxFalseToAgentFalseCodemod_NoSandboxField(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

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

func TestSandboxFalseToAgentFalseCodemod_SandboxTrue(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	content := `---
on: workflow_dispatch
sandbox: true
---

# Test`

	frontmatter := map[string]any{
		"on":      "workflow_dispatch",
		"sandbox": true,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSandboxFalseToAgentFalseCodemod_SandboxObject(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent: awf
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"agent": "awf",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSandboxFalseToAgentFalseCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	content := `---
on: workflow_dispatch
sandbox: false
---

# Test Workflow

This workflow runs without a sandbox.`

	frontmatter := map[string]any{
		"on":      "workflow_dispatch",
		"sandbox": false,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "This workflow runs without a sandbox.")
}

func TestSandboxFalseToAgentFalseCodemod_WithStrictFalse(t *testing.T) {
	codemod := getSandboxFalseToAgentFalseCodemod()

	content := `---
on: workflow_dispatch
sandbox: false
strict: false
---

# Test`

	frontmatter := map[string]any{
		"on":      "workflow_dispatch",
		"sandbox": false,
		"strict":  false,
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "sandbox: false")
	assert.Contains(t, result, "sandbox:")
	assert.Contains(t, result, "  agent: false")
	assert.Contains(t, result, "strict: false")
}


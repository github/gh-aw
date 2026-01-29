//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSandboxAgentFalseRemovalCodemod(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	assert.Equal(t, "sandbox-agent-false-removal", codemod.ID)
	assert.Equal(t, "Remove deprecated sandbox.agent: false", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.5.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestSandboxAgentCodemod_RemovesAgentFalse(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent: false
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"agent": false,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "agent: false")
	assert.NotContains(t, result, "sandbox:")
}

func TestSandboxAgentCodemod_PreservesOtherSandboxFields(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent: false
  timeout: 30m
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"agent":   false,
			"timeout": "30m",
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "agent: false")
	assert.Contains(t, result, "sandbox:")
	assert.Contains(t, result, "timeout: 30m")
}

func TestSandboxAgentCodemod_NoSandboxField(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

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

func TestSandboxAgentCodemod_NoAgentField(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  timeout: 30m
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"timeout": "30m",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSandboxAgentCodemod_AgentTrue(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent: true
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"agent": true,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestSandboxAgentCodemod_AgentString(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

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

func TestSandboxAgentCodemod_RemovesEmptySandboxBlock(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent: false
permissions:
  contents: read
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"agent": false,
		},
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "sandbox:")
}

func TestSandboxAgentCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getSandboxAgentFalseRemovalCodemod()

	content := `---
on: workflow_dispatch
sandbox:
  agent: false
---

# Test Workflow

This workflow runs in a sandbox.`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"sandbox": map[string]any{
			"agent": false,
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "This workflow runs in a sandbox.")
}

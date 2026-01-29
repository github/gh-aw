//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAgentTaskToAgentSessionCodemod(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	assert.Equal(t, "agent-task-to-agent-session-migration", codemod.ID)
	assert.Equal(t, "Migrate create-agent-task to create-agent-session", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.4.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestAgentSessionCodemod_BasicMigration(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-agent-task:
    title: Run tests
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-agent-task": map[string]any{
				"title": "Run tests",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "create-agent-session:")
	assert.NotContains(t, result, "create-agent-task:")
}

func TestAgentSessionCodemod_PreservesIndentation(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-agent-task:
    title: Deploy
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-agent-task": map[string]any{
				"title": "Deploy",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "  create-agent-session:")
}

func TestAgentSessionCodemod_PreservesComment(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-agent-task:  # Create a new agent task
    title: Test
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-agent-task": map[string]any{
				"title": "Test",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "create-agent-session:  # Create a new agent task")
}

func TestAgentSessionCodemod_NoSafeOutputsField(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

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

func TestAgentSessionCodemod_NoAgentTaskField(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-issue:
    title: Test
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"title": "Test",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestAgentSessionCodemod_PreservesOtherFields(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-issue:
    title: Bug Report
  create-agent-task:
    title: Run tests
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"title": "Bug Report",
			},
			"create-agent-task": map[string]any{
				"title": "Run tests",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "create-issue:")
	assert.Contains(t, result, "create-agent-session:")
	assert.NotContains(t, result, "create-agent-task:")
}

func TestAgentSessionCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-agent-task:
    title: Test
---

# Test Workflow

Creates an agent session.`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-agent-task": map[string]any{
				"title": "Test",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "Creates an agent session.")
}

func TestAgentSessionCodemod_SkipsWhenSessionAlreadyExists(t *testing.T) {
	codemod := getAgentTaskToAgentSessionCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-agent-task:
    title: Old Task
  create-agent-session:
    title: New Session
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-agent-task": map[string]any{
				"title": "Old Task",
			},
			"create-agent-session": map[string]any{
				"title": "New Session",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied, "Should not apply migration when create-agent-session already exists")
	assert.Equal(t, content, result, "Content should remain unchanged")
	// Verify both fields still exist unchanged
	assert.Contains(t, result, "create-agent-task:")
	assert.Contains(t, result, "create-agent-session:")
}

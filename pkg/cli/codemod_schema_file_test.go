//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDeleteSchemaFileCodemod(t *testing.T) {
	codemod := getDeleteSchemaFileCodemod()

	// Verify codemod metadata
	assert.Equal(t, "delete-schema-file", codemod.ID, "Codemod ID should match")
	assert.Equal(t, "Delete deprecated schema file", codemod.Name, "Codemod name should match")
	assert.NotEmpty(t, codemod.Description, "Codemod should have a description")
	assert.Equal(t, "0.6.0", codemod.IntroducedIn, "Codemod version should match")
	require.NotNil(t, codemod.Apply, "Codemod should have an Apply function")
}

func TestDeleteSchemaFileCodemod_NoChanges(t *testing.T) {
	codemod := getDeleteSchemaFileCodemod()

	content := `---
on: workflow_dispatch
permissions:
  contents: read
---

# Test Workflow

This workflow doesn't need any changes.`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"permissions": map[string]any{
			"contents": "read",
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, applied, "Codemod should not report changes (handled by fix command)")
	assert.Equal(t, content, result, "Content should remain unchanged")
}

func TestDeleteSchemaFileCodemod_AlwaysReturnsUnchanged(t *testing.T) {
	// This codemod doesn't modify workflow files - the fix command handles the file deletion
	// Test various content to ensure it never makes changes

	tests := []struct {
		name        string
		content     string
		frontmatter map[string]any
	}{
		{
			name: "simple workflow",
			content: `---
on: workflow_dispatch
---

# Simple`,
			frontmatter: map[string]any{"on": "workflow_dispatch"},
		},
		{
			name: "complex workflow",
			content: `---
on:
  workflow_dispatch:
  push:
    branches: [main]
engine: copilot
permissions:
  contents: read
  issues: write
---

# Complex Workflow

With multiple sections.`,
			frontmatter: map[string]any{
				"on": map[string]any{
					"workflow_dispatch": nil,
					"push": map[string]any{
						"branches": []any{"main"},
					},
				},
				"engine": "copilot",
				"permissions": map[string]any{
					"contents": "read",
					"issues":   "write",
				},
			},
		},
		{
			name:        "empty content",
			content:     "",
			frontmatter: map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codemod := getDeleteSchemaFileCodemod()
			result, applied, err := codemod.Apply(tt.content, tt.frontmatter)

			require.NoError(t, err, "Apply should not return an error")
			assert.False(t, applied, "Codemod should never report changes")
			assert.Equal(t, tt.content, result, "Content should always remain unchanged")
		})
	}
}

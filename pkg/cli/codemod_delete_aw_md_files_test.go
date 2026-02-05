//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeleteAgenticWorkflowPromptFilesCodemod(t *testing.T) {
	codemod := getDeleteAgenticWorkflowPromptFilesCodemod()

	// Verify codemod metadata
	assert.Equal(t, "delete-aw-md-files", codemod.ID, "Codemod ID should match")
	assert.Equal(t, "Delete agentic workflow prompt markdown files", codemod.Name, "Codemod name should match")
	assert.Contains(t, codemod.Description, "downloaded from GitHub", "Description should mention GitHub downloads")
	assert.Equal(t, "0.7.0", codemod.IntroducedIn, "Codemod should be introduced in 0.7.0")

	// Test that the codemod doesn't modify workflow content
	// (The actual deletion is handled by the fix command itself)
	content := `---
on: push
---

# Test Workflow

This is a test workflow.
`
	frontmatter := map[string]any{
		"on": "push",
	}

	result, changed, err := codemod.Apply(content, frontmatter)
	require.NoError(t, err, "Apply should not return an error")
	assert.False(t, changed, "Codemod should not mark content as changed")
	assert.Equal(t, content, result, "Codemod should not modify workflow content")
}

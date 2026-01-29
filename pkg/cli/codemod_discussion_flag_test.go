//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDiscussionFlagRemovalCodemod(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	assert.Equal(t, "add-comment-discussion-removal", codemod.ID)
	assert.Equal(t, "Remove deprecated add-comment.discussion field", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.3.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestDiscussionFlagCodemod_RemovesDiscussionFlag(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  add-comment:
    hide-older-comments: true
    discussion: true
    max: 2
  create-issue:
    expires: 2h
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{
				"hide-older-comments": true,
				"discussion":          true,
				"max":                 2,
			},
			"create-issue": map[string]any{
				"expires": "2h",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "discussion:")
	assert.Contains(t, result, "hide-older-comments: true")
	assert.Contains(t, result, "max: 2")
	assert.Contains(t, result, "expires: 2h")
}

func TestDiscussionFlagCodemod_NoSafeOutputsField(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

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

func TestDiscussionFlagCodemod_NoAddCommentField(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-issue:
    expires: 2h
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"expires": "2h",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestDiscussionFlagCodemod_NoDiscussionField(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  add-comment:
    hide-older-comments: true
    max: 2
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{
				"hide-older-comments": true,
				"max":                 2,
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestDiscussionFlagCodemod_PreservesIndentation(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  add-comment:
    hide-older-comments: true
    discussion: true
    max: 2
  create-issue:
    expires: 2h
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{
				"hide-older-comments": true,
				"discussion":          true,
				"max":                 2,
			},
			"create-issue": map[string]any{
				"expires": "2h",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "discussion:")
	assert.Contains(t, result, "  add-comment:")
	assert.Contains(t, result, "    hide-older-comments: true")
	assert.Contains(t, result, "    max: 2")
}

func TestDiscussionFlagCodemod_PreservesComments(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  add-comment:
    hide-older-comments: true  # Hide older comments
    discussion: true  # Target discussions
    max: 2  # Maximum comments
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{
				"hide-older-comments": true,
				"discussion":          true,
				"max":                 2,
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.NotContains(t, result, "discussion:")
	assert.Contains(t, result, "hide-older-comments: true  # Hide older comments")
	assert.Contains(t, result, "max: 2  # Maximum comments")
}

func TestDiscussionFlagCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  add-comment:
    discussion: true
---

# Test Workflow

This workflow uses add-comment with discussion support.`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{
				"discussion": true,
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "This workflow uses add-comment with discussion support.")
}

func TestDiscussionFlagCodemod_MultipleFields(t *testing.T) {
	codemod := getDiscussionFlagRemovalCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  add-comment:
    max: 1
    target: "*"
    discussion: true
    hide-older-comments: false
    target-repo: "owner/repo"
  create-discussion:
    expires: 24h
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"add-comment": map[string]any{
				"max":                 1,
				"target":              "*",
				"discussion":          true,
				"hide-older-comments": false,
				"target-repo":         "owner/repo",
			},
			"create-discussion": map[string]any{
				"expires": "24h",
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	// Check that "discussion: true" is not present (but "create-discussion:" is still there)
	assert.NotContains(t, result, "    discussion: true")
	assert.Contains(t, result, "max: 1")
	assert.Contains(t, result, "target: \"*\"")
	assert.Contains(t, result, "hide-older-comments: false")
	assert.Contains(t, result, "target-repo: \"owner/repo\"")
	assert.Contains(t, result, "create-discussion:")
	assert.Contains(t, result, "expires: 24h")
}

//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUploadAssetsCodemod(t *testing.T) {
	codemod := getUploadAssetsCodemod()

	assert.Equal(t, "upload-assets-to-upload-asset-migration", codemod.ID)
	assert.Equal(t, "Migrate upload-assets to upload-asset", codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.Equal(t, "0.3.0", codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestUploadAssetsCodemod_BasicMigration(t *testing.T) {
	codemod := getUploadAssetsCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  upload-assets:
    - path: dist/
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"upload-assets": []any{
				map[string]any{"path": "dist/"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "upload-asset:")
	assert.NotContains(t, result, "upload-assets:")
}

func TestUploadAssetsCodemod_PreservesIndentation(t *testing.T) {
	codemod := getUploadAssetsCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  upload-assets:
    - path: build/
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"upload-assets": []any{
				map[string]any{"path": "build/"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "  upload-asset:")
}

func TestUploadAssetsCodemod_PreservesComment(t *testing.T) {
	codemod := getUploadAssetsCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  upload-assets:  # Upload build artifacts
    - path: dist/
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"upload-assets": []any{
				map[string]any{"path": "dist/"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "upload-asset:  # Upload build artifacts")
}

func TestUploadAssetsCodemod_NoSafeOutputsField(t *testing.T) {
	codemod := getUploadAssetsCodemod()

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

func TestUploadAssetsCodemod_NoUploadAssetsField(t *testing.T) {
	codemod := getUploadAssetsCodemod()

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

func TestUploadAssetsCodemod_PreservesOtherFields(t *testing.T) {
	codemod := getUploadAssetsCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  create-issue:
    title: Test
  upload-assets:
    - path: dist/
---

# Test`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"title": "Test",
			},
			"upload-assets": []any{
				map[string]any{"path": "dist/"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "create-issue:")
	assert.Contains(t, result, "upload-asset:")
	assert.NotContains(t, result, "upload-assets:")
}

func TestUploadAssetsCodemod_PreservesMarkdown(t *testing.T) {
	codemod := getUploadAssetsCodemod()

	content := `---
on: workflow_dispatch
safe-outputs:
  upload-assets:
    - path: dist/
---

# Test Workflow

Uploads assets to release.`

	frontmatter := map[string]any{
		"on": "workflow_dispatch",
		"safe-outputs": map[string]any{
			"upload-assets": []any{
				map[string]any{"path": "dist/"},
			},
		},
	}

	result, applied, err := codemod.Apply(content, frontmatter)

	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "# Test Workflow")
	assert.Contains(t, result, "Uploads assets to release.")
}

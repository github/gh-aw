package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestReviewPolicyCodemodMetadata(t *testing.T) {
	codemod := getRequestReviewPolicyCodemod()
	assert.Equal(t, "request-review-policy-normalization", codemod.ID)
	assert.NotEmpty(t, codemod.Name)
	assert.NotEmpty(t, codemod.Description)
	assert.NotEmpty(t, codemod.IntroducedIn)
	require.NotNil(t, codemod.Apply)
}

func TestRequestReviewPolicyCodemodMigratesScalarAndObjectForms(t *testing.T) {
	content := `---
safe-outputs:
  create-pull-request:
    protected-files: request_review # review policy
  push-to-pull-request-branch:
    protected-files:
      policy: request_review
---

# Body
`
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-pull-request": map[string]any{"protected-files": "request_review"},
			"push-to-pull-request-branch": map[string]any{
				"protected-files": map[string]any{"policy": "request_review"},
			},
		},
	}

	result, applied, err := getRequestReviewPolicyCodemod().Apply(content, frontmatter)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "protected-files: request-review # review policy")
	assert.Contains(t, result, "      policy: request-review")
	assert.Contains(t, result, "# Body")
}

func TestRequestReviewPolicyCodemodNoOpAndIdempotent(t *testing.T) {
	content := "---\nsafe-outputs:\n  create-pull-request:\n    protected-files: request-review\n---\n\n# Body\n"
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-pull-request": map[string]any{"protected-files": "request-review"},
		},
	}

	codemod := getRequestReviewPolicyCodemod()
	result, applied, err := codemod.Apply(content, frontmatter)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.Equal(t, content, result)
}

func TestRequestReviewPolicyCodemodPreservesUnrelatedValues(t *testing.T) {
	content := `---
safe-outputs:
  create-pull-request:
    protected-files:
      policy: request_review
      exclude:
        - README.md
  add-comment:
    body: request_review
---
`
	frontmatter := map[string]any{
		"safe-outputs": map[string]any{
			"create-pull-request": map[string]any{
				"protected-files": map[string]any{"policy": "request_review"},
			},
		},
	}

	result, applied, err := getRequestReviewPolicyCodemod().Apply(content, frontmatter)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.Contains(t, result, "body: request_review")
	assert.Contains(t, result, "- README.md")
}

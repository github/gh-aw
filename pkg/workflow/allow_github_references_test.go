//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowGitHubReferencesConfig(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    []string
	}{
		{
			name: "allow current repo only",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{"repo"},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{"repo"},
		},
		{
			name: "allow multiple repos",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{"repo", "org/repo2", "org/repo3"},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{"repo", "org/repo2", "org/repo3"},
		},
		{
			name: "no restrictions (empty array)",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{}, // Empty array should be preserved (means escape all)
		},
		{
			name: "no allowed-github-references field",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"create-issue": map[string]any{},
				},
			},
			expected: nil,
		},
		{
			name: "allow repos with hyphens",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{"my-org/my-repo", "other-org/other-repo"},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{"my-org/my-repo", "other-org/other-repo"},
		},
		{
			name: "allow repos with underscores and dots",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{"my-org/my.repo", "test-org/test.repo.v2"},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{"my-org/my.repo", "test-org/test.repo.v2"},
		},
		{
			name: "single specific repo without 'repo' keyword",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{"octocat/hello-world"},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{"octocat/hello-world"},
		},
		{
			name: "mix of 'repo' keyword and specific repos",
			frontmatter: map[string]any{
				"safe-outputs": map[string]any{
					"allowed-github-references": []any{"repo", "microsoft/vscode", "github/copilot"},
					"create-issue":              map[string]any{},
				},
			},
			expected: []string{"repo", "microsoft/vscode", "github/copilot"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompilerWithVersion("1.0.0")
			config := c.extractSafeOutputsConfig(tt.frontmatter)
			require.NotNil(t, config, "extractSafeOutputsConfig() should not return nil")

			if tt.expected == nil {
				assert.Nil(t, config.AllowGitHubReferences, "AllowGitHubReferences should be nil")
			} else {
				require.NotNil(t, config.AllowGitHubReferences, "AllowGitHubReferences should not be nil")
				assert.Equal(t, tt.expected, config.AllowGitHubReferences, "AllowGitHubReferences should match expected")
			}
		})
	}
}

//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateFrontmatterPlugins(t *testing.T) {
	t.Run("accepts string entries", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{"octo-org/agent-plugin@main"},
		})
		require.NoError(t, err)
	})

	t.Run("no-op without plugins field", func(t *testing.T) {
		require.NoError(t, validateFrontmatterPlugins(map[string]any{}))
	})

	t.Run("rejects non-array plugins", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{"plugins": "not-an-array"})
		require.ErrorContains(t, err, "plugins must be an array")
	})

	t.Run("accepts object form with github-token", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{
					"plugin":       "octo-org/private-plugin@main",
					"github-token": "${{ secrets.PRIVATE_PLUGIN_TOKEN }}",
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("rejects object form with steps output github-token", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{
					"plugin":       "octo-org/private-plugin@main",
					"github-token": "${{ steps.fetch_token.outputs.token }}",
				},
			},
		})
		require.ErrorContains(t, err, "plugins[0].github-token must be a valid GitHub token expression")
	})

	t.Run("accepts object form with github-app", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{
					"plugin": "octo-org/private-marketplace/plugins/example@main",
					"github-app": map[string]any{
						"client-id":   "${{ vars.PLUGIN_APP_CLIENT_ID }}",
						"private-key": "${{ secrets.PLUGIN_APP_PRIVATE_KEY }}",
					},
				},
			},
		})
		require.NoError(t, err)
	})

	t.Run("rejects github-token and github-app together", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{
					"plugin":       "octo-org/private-plugin@main",
					"github-token": "${{ secrets.PRIVATE_PLUGIN_TOKEN }}",
					"github-app": map[string]any{
						"client-id":   "${{ vars.PLUGIN_APP_CLIENT_ID }}",
						"private-key": "${{ secrets.PLUGIN_APP_PRIVATE_KEY }}",
					},
				},
			},
		})
		require.ErrorContains(t, err, "mutually exclusive")
	})

	t.Run("rejects object form without plugin", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{"github-token": "${{ secrets.PRIVATE_PLUGIN_TOKEN }}"},
			},
		})
		require.ErrorContains(t, err, "plugins[0].plugin is required")
	})

	t.Run("rejects object form github-app without private-key", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{
					"plugin": "octo-org/private-plugin@main",
					"github-app": map[string]any{
						"client-id": "${{ vars.PLUGIN_APP_CLIENT_ID }}",
					},
				},
			},
		})
		require.ErrorContains(t, err, "plugins[0].github-app")
	})

	t.Run("rejects object form with unknown fields", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{
			"plugins": []any{
				map[string]any{
					"plugin": "octo-org/private-plugin@main",
					"bogus":  "value",
				},
			},
		})
		require.ErrorContains(t, err, "plugins[0].bogus is not supported")
	})

	t.Run("rejects non-string non-object entries", func(t *testing.T) {
		err := validateFrontmatterPlugins(map[string]any{"plugins": []any{42}})
		require.Error(t, err)
	})
}

func TestParseRawPluginReferences(t *testing.T) {
	refs := parseRawPluginReferences([]any{
		"octo-org/agent-plugin@main",
		map[string]any{
			"plugin":       "octo-org/private-plugin@main",
			"github-token": "${{ secrets.PRIVATE_PLUGIN_TOKEN }}",
		},
		map[string]any{
			"plugin": "octo-org/private-marketplace/plugins/example@main",
			"github-app": map[string]any{
				"client-id":   "${{ vars.PLUGIN_APP_CLIENT_ID }}",
				"private-key": "${{ secrets.PLUGIN_APP_PRIVATE_KEY }}",
			},
		},
	})

	require.Len(t, refs, 3)
	assert.Equal(t, "octo-org/agent-plugin@main", refs[0].Plugin)
	assert.Empty(t, refs[0].GitHubToken)
	assert.Nil(t, refs[0].GitHubApp)

	assert.Equal(t, "octo-org/private-plugin@main", refs[1].Plugin)
	assert.Equal(t, "${{ secrets.PRIVATE_PLUGIN_TOKEN }}", refs[1].GitHubToken)

	assert.Equal(t, "octo-org/private-marketplace/plugins/example@main", refs[2].Plugin)
	require.NotNil(t, refs[2].GitHubApp)
	assert.Equal(t, "${{ vars.PLUGIN_APP_CLIENT_ID }}", refs[2].GitHubApp.AppID)
	assert.Equal(t, "${{ secrets.PLUGIN_APP_PRIVATE_KEY }}", refs[2].GitHubApp.PrivateKey)
}

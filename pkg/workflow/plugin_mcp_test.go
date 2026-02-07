//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractPluginsFromFrontmatter_WithMCPConfig(t *testing.T) {
	tests := []struct {
		name               string
		frontmatter        map[string]any
		expectedRepos      []string
		expectedToken      string
		expectedMCPConfigs map[string]*PluginMCPConfig
	}{
		{
			name: "Array format with simple string plugin",
			frontmatter: map[string]any{
				"plugins": []any{"github/plugin1", "github/plugin2"},
			},
			expectedRepos:      []string{"github/plugin1", "github/plugin2"},
			expectedToken:      "",
			expectedMCPConfigs: map[string]*PluginMCPConfig{},
		},
		{
			name: "Array format with plugin object containing MCP config",
			frontmatter: map[string]any{
				"plugins": []any{
					"github/simple-plugin",
					map[string]any{
						"id": "github/mcp-plugin",
						"mcp": map[string]any{
							"env": map[string]any{
								"API_KEY": "${{ secrets.API_KEY }}",
								"API_URL": "https://api.example.com",
							},
						},
					},
				},
			},
			expectedRepos: []string{"github/simple-plugin", "github/mcp-plugin"},
			expectedToken: "",
			expectedMCPConfigs: map[string]*PluginMCPConfig{
				"github/mcp-plugin": {
					Env: map[string]string{
						"API_KEY": "${{ secrets.API_KEY }}",
						"API_URL": "https://api.example.com",
					},
				},
			},
		},
		{
			name: "Object format with custom token and mixed plugin types",
			frontmatter: map[string]any{
				"plugins": map[string]any{
					"repos": []any{
						"github/simple-plugin",
						map[string]any{
							"id": "github/mcp-plugin",
							"mcp": map[string]any{
								"env": map[string]any{
									"SECRET_KEY": "${{ secrets.SECRET_KEY }}",
								},
							},
						},
					},
					"github-token": "${{ secrets.CUSTOM_TOKEN }}",
				},
			},
			expectedRepos: []string{"github/simple-plugin", "github/mcp-plugin"},
			expectedToken: "${{ secrets.CUSTOM_TOKEN }}",
			expectedMCPConfigs: map[string]*PluginMCPConfig{
				"github/mcp-plugin": {
					Env: map[string]string{
						"SECRET_KEY": "${{ secrets.SECRET_KEY }}",
					},
				},
			},
		},
		{
			name: "Multiple plugins with different MCP configs",
			frontmatter: map[string]any{
				"plugins": []any{
					map[string]any{
						"id": "github/plugin1",
						"mcp": map[string]any{
							"env": map[string]any{
								"API_KEY_1": "${{ secrets.API_KEY_1 }}",
							},
						},
					},
					map[string]any{
						"id": "github/plugin2",
						"mcp": map[string]any{
							"env": map[string]any{
								"API_KEY_2": "${{ secrets.API_KEY_2 }}",
							},
						},
					},
				},
			},
			expectedRepos: []string{"github/plugin1", "github/plugin2"},
			expectedToken: "",
			expectedMCPConfigs: map[string]*PluginMCPConfig{
				"github/plugin1": {
					Env: map[string]string{
						"API_KEY_1": "${{ secrets.API_KEY_1 }}",
					},
				},
				"github/plugin2": {
					Env: map[string]string{
						"API_KEY_2": "${{ secrets.API_KEY_2 }}",
					},
				},
			},
		},
		{
			name: "Plugin object with URL but no MCP config",
			frontmatter: map[string]any{
				"plugins": []any{
					map[string]any{
						"id": "github/simple-plugin",
					},
				},
			},
			expectedRepos:      []string{"github/simple-plugin"},
			expectedToken:      "",
			expectedMCPConfigs: map[string]*PluginMCPConfig{},
		},
		{
			name:               "No plugins defined",
			frontmatter:        map[string]any{},
			expectedRepos:      nil,
			expectedToken:      "",
			expectedMCPConfigs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pluginInfo := extractPluginsFromFrontmatter(tt.frontmatter)

			var repos []string
			var token string
			var mcpConfigs map[string]*PluginMCPConfig

			if pluginInfo != nil {
				repos = pluginInfo.Plugins
				token = pluginInfo.CustomToken
				mcpConfigs = pluginInfo.MCPConfigs
			}

			assert.Equal(t, tt.expectedRepos, repos, "Extracted plugin repos should match expected")
			assert.Equal(t, tt.expectedToken, token, "Extracted plugin token should match expected")

			if tt.expectedMCPConfigs == nil {
				if mcpConfigs != nil {
					assert.Empty(t, mcpConfigs, "MCP configs should be empty when none expected")
				}
			} else {
				require.NotNil(t, mcpConfigs, "MCP configs should not be nil")
				assert.Len(t, mcpConfigs, len(tt.expectedMCPConfigs), "Number of MCP configs should match")

				for id, expectedConfig := range tt.expectedMCPConfigs {
					actualConfig, exists := mcpConfigs[id]
					assert.True(t, exists, "MCP config for %s should exist", id)
					if exists {
						assert.Equal(t, expectedConfig.Env, actualConfig.Env, "Env vars for %s should match", id)
					}
				}
			}
		})
	}
}

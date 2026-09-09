//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilterEnvForSecrets tests the FilterEnvForSecrets function
func TestFilterEnvForSecrets(t *testing.T) {
	tests := []struct {
		name           string
		env            map[string]string
		allowedSecrets []string
		wantKeys       []string
		wantRemoved    int
	}{
		{
			name: "allows whitelisted secrets",
			env: map[string]string{
				"COPILOT_GITHUB_TOKEN": "${{ secrets.COPILOT_GITHUB_TOKEN }}",
				"MCP_GATEWAY_AGENT_ID": "${{ secrets.MCP_GATEWAY_AGENT_ID }}",
				"NORMAL_ENV_VAR":       "some-value",
			},
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN", "MCP_GATEWAY_AGENT_ID"},
			wantKeys:       []string{"COPILOT_GITHUB_TOKEN", "MCP_GATEWAY_AGENT_ID", "NORMAL_ENV_VAR"},
			wantRemoved:    0,
		},
		{
			name: "allows overriding engine env var with a differently-named secret",
			env: map[string]string{
				// User overrides the engine token with their own org secret
				"COPILOT_GITHUB_TOKEN": "${{ secrets.MY_ORG_COPILOT_TOKEN }}",
				"NORMAL_ENV_VAR":       "some-value",
			},
			// COPILOT_GITHUB_TOKEN is in the allowed list (as an env var key),
			// so the custom secret MY_ORG_COPILOT_TOKEN should pass through.
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN"},
			wantKeys:       []string{"COPILOT_GITHUB_TOKEN", "NORMAL_ENV_VAR"},
			wantRemoved:    0,
		},
		{
			name: "does not allow non-engine secrets even when key matches nothing",
			env: map[string]string{
				"COPILOT_GITHUB_TOKEN": "${{ secrets.COPILOT_GITHUB_TOKEN }}",
				"RANDOM_SECRET":        "${{ secrets.RANDOM_SECRET }}",
			},
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN"},
			wantKeys:       []string{"COPILOT_GITHUB_TOKEN"},
			wantRemoved:    1,
		},
		{
			name: "filters unauthorized secrets",
			env: map[string]string{
				"COPILOT_GITHUB_TOKEN": "${{ secrets.COPILOT_GITHUB_TOKEN }}",
				"UNAUTHORIZED_SECRET":  "${{ secrets.UNAUTHORIZED_SECRET }}",
				"NORMAL_ENV_VAR":       "some-value",
			},
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN"},
			wantKeys:       []string{"COPILOT_GITHUB_TOKEN", "NORMAL_ENV_VAR"},
			wantRemoved:    1,
		},
		{
			name: "allows non-secret env vars",
			env: map[string]string{
				"PATH":                 "/usr/bin",
				"GITHUB_WORKSPACE":     "${{ github.workspace }}",
				"COPILOT_GITHUB_TOKEN": "${{ secrets.COPILOT_GITHUB_TOKEN }}",
			},
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN"},
			wantKeys:       []string{"PATH", "GITHUB_WORKSPACE", "COPILOT_GITHUB_TOKEN"},
			wantRemoved:    0,
		},
		{
			name: "handles secrets with fallback expressions",
			env: map[string]string{
				"API_KEY":    "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}",
				"OTHER_VAR":  "value",
				"BAD_SECRET": "${{ secrets.BAD_SECRET || secrets.OTHER_BAD }}",
			},
			allowedSecrets: []string{"CODEX_API_KEY"},
			wantKeys:       []string{"API_KEY", "OTHER_VAR"},
			wantRemoved:    1,
		},
		{
			name:           "empty env returns empty",
			env:            map[string]string{},
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN"},
			wantKeys:       []string{},
			wantRemoved:    0,
		},
		{
			name: "no allowed secrets filters all secrets",
			env: map[string]string{
				"SECRET_ONE": "${{ secrets.SECRET_ONE }}",
				"SECRET_TWO": "${{ secrets.SECRET_TWO }}",
				"NORMAL_VAR": "value",
			},
			allowedSecrets: []string{},
			wantKeys:       []string{"NORMAL_VAR"},
			wantRemoved:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterEnvForSecrets(tt.env, tt.allowedSecrets)

			// Check that the expected keys are present
			assert.Len(t, result, len(tt.wantKeys), "Expected %d keys, got %d", len(tt.wantKeys), len(result))

			for _, key := range tt.wantKeys {
				_, exists := result[key]
				assert.True(t, exists, "Expected key %s to be present in filtered env", key)
			}

			// Check that the removed count is correct
			removedCount := len(tt.env) - len(result)
			assert.Equal(t, tt.wantRemoved, removedCount, "Expected %d secrets to be removed, got %d", tt.wantRemoved, removedCount)
		})
	}
}

// TestGetRequiredSecretNames_Copilot tests CopilotEngine.GetRequiredSecretNames
func TestGetRequiredSecretNames_Copilot(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("basic secrets without MCP", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include COPILOT_GITHUB_TOKEN plus the three BYOK provider secret keys
		// (COPILOT_PROVIDER_BASE_URL, COPILOT_PROVIDER_API_KEY, COPILOT_PROVIDER_BEARER_TOKEN)
		// which are always listed so that strict-mode validation recognises them as engine credentials.
		require.Len(t, secrets, 4)
		assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
		assert.Contains(t, secrets, "COPILOT_PROVIDER_BASE_URL")
		assert.Contains(t, secrets, "COPILOT_PROVIDER_API_KEY")
		assert.Contains(t, secrets, "COPILOT_PROVIDER_BEARER_TOKEN")
	})

	t.Run("includes MCP gateway agent ID when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include COPILOT_GITHUB_TOKEN, MCP_GATEWAY_AGENT_ID, and GITHUB_MCP_SERVER_TOKEN
		assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
		assert.Contains(t, secrets, "MCP_GATEWAY_AGENT_ID")
		assert.Contains(t, secrets, "GITHUB_MCP_SERVER_TOKEN")
	})

	t.Run("includes mcp-scripts secrets", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
			MCPScripts: &MCPScriptsConfig{
				Mode: "http",
				Tools: map[string]*MCPScriptToolConfig{
					"api": {
						Env: map[string]string{
							"API_TOKEN": "${{ secrets.API_TOKEN }}",
						},
					},
				},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include COPILOT_GITHUB_TOKEN and API_TOKEN
		assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
		assert.Contains(t, secrets, "API_TOKEN")
	})

	t.Run("model-provider=openai switches core secret requirements", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
			EngineConfig: &EngineConfig{
				LLMProvider: LLMProviderOpenAI,
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)
		assert.Contains(t, secrets, "CODEX_API_KEY")
		assert.Contains(t, secrets, "OPENAI_API_KEY")
		assert.NotContains(t, secrets, "COPILOT_GITHUB_TOKEN")
	})
}

// TestGetRequiredSecretNames_Claude tests ClaudeEngine.GetRequiredSecretNames
func TestGetRequiredSecretNames_Claude(t *testing.T) {
	engine := NewClaudeEngine()

	t.Run("basic secrets without MCP", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should only include ANTHROPIC_API_KEY
		require.Len(t, secrets, 1)
		assert.Contains(t, secrets, "ANTHROPIC_API_KEY")
	})

	t.Run("includes MCP gateway agent ID when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include ANTHROPIC_API_KEY and MCP_GATEWAY_AGENT_ID
		assert.Contains(t, secrets, "ANTHROPIC_API_KEY")
		assert.Contains(t, secrets, "MCP_GATEWAY_AGENT_ID")
	})

	t.Run("WIF: skips ANTHROPIC_API_KEY when github-oidc+anthropic configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
			EngineConfig: &EngineConfig{
				Auth: &EngineAuthConfig{
					Type:     "github-oidc",
					Provider: "anthropic",
				},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		assert.NotContains(t, secrets, "ANTHROPIC_API_KEY")
	})

	t.Run("WIF: still includes MCP secrets when WIF and MCP both configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
			EngineConfig: &EngineConfig{
				Auth: &EngineAuthConfig{
					Type:     "github-oidc",
					Provider: "anthropic",
				},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		assert.NotContains(t, secrets, "ANTHROPIC_API_KEY")
		assert.Contains(t, secrets, "MCP_GATEWAY_AGENT_ID")
	})

	t.Run("model-provider=github switches required secret to copilot token", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
			EngineConfig: &EngineConfig{
				LLMProvider: LLMProviderGitHub,
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)
		assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
		assert.NotContains(t, secrets, "ANTHROPIC_API_KEY")
	})
}

// TestGetRequiredSecretNames_Codex tests CodexEngine.GetRequiredSecretNames
func TestGetRequiredSecretNames_Codex(t *testing.T) {
	engine := NewCodexEngine()

	t.Run("basic secrets without MCP", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include CODEX_API_KEY and OPENAI_API_KEY
		require.Len(t, secrets, 2)
		assert.Contains(t, secrets, "CODEX_API_KEY")
		assert.Contains(t, secrets, "OPENAI_API_KEY")
	})

	t.Run("includes MCP gateway agent ID when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"example": map[string]any{
					"type":    "http",
					"url":     "https://example.com/mcp",
					"allowed": []any{"read"},
				},
			},
			ParsedTools: &ToolsConfig{},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include Codex secrets and MCP_GATEWAY_AGENT_ID
		assert.Contains(t, secrets, "CODEX_API_KEY")
		assert.Contains(t, secrets, "OPENAI_API_KEY")
		assert.Contains(t, secrets, "MCP_GATEWAY_AGENT_ID")
	})
}

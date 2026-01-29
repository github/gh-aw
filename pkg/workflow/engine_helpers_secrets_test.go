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
				"MCP_GATEWAY_API_KEY":  "${{ secrets.MCP_GATEWAY_API_KEY }}",
				"NORMAL_ENV_VAR":       "some-value",
			},
			allowedSecrets: []string{"COPILOT_GITHUB_TOKEN", "MCP_GATEWAY_API_KEY"},
			wantKeys:       []string{"COPILOT_GITHUB_TOKEN", "MCP_GATEWAY_API_KEY", "NORMAL_ENV_VAR"},
			wantRemoved:    0,
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

		// Should only include COPILOT_GITHUB_TOKEN
		require.Len(t, secrets, 1)
		assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
	})

	t.Run("includes MCP gateway API key when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include COPILOT_GITHUB_TOKEN, MCP_GATEWAY_API_KEY, and GITHUB_MCP_SERVER_TOKEN
		assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN")
		assert.Contains(t, secrets, "MCP_GATEWAY_API_KEY")
		assert.Contains(t, secrets, "GITHUB_MCP_SERVER_TOKEN")
	})

	t.Run("includes safe-inputs secrets", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
			SafeInputs: &SafeInputsConfig{
				Mode: "http",
				Tools: map[string]*SafeInputToolConfig{
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

		// Should include ANTHROPIC_API_KEY and CLAUDE_CODE_OAUTH_TOKEN
		require.Len(t, secrets, 2)
		assert.Contains(t, secrets, "ANTHROPIC_API_KEY")
		assert.Contains(t, secrets, "CLAUDE_CODE_OAUTH_TOKEN")
	})

	t.Run("includes MCP gateway API key when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include Claude secrets and MCP_GATEWAY_API_KEY
		assert.Contains(t, secrets, "ANTHROPIC_API_KEY")
		assert.Contains(t, secrets, "CLAUDE_CODE_OAUTH_TOKEN")
		assert.Contains(t, secrets, "MCP_GATEWAY_API_KEY")
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

	t.Run("includes MCP gateway API key when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"playwright": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				Playwright: &PlaywrightToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should include Codex secrets and MCP_GATEWAY_API_KEY
		assert.Contains(t, secrets, "CODEX_API_KEY")
		assert.Contains(t, secrets, "OPENAI_API_KEY")
		assert.Contains(t, secrets, "MCP_GATEWAY_API_KEY")
	})
}

// TestGetRequiredSecretNames_Custom tests CustomEngine.GetRequiredSecretNames
func TestGetRequiredSecretNames_Custom(t *testing.T) {
	engine := NewCustomEngine()

	t.Run("no secrets without MCP", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools:       map[string]any{},
			ParsedTools: &ToolsConfig{},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Custom engine has no predefined secrets
		require.Empty(t, secrets)
	})

	t.Run("includes MCP gateway API key when MCP servers present", func(t *testing.T) {
		workflowData := &WorkflowData{
			Tools: map[string]any{
				"github": map[string]any{},
			},
			ParsedTools: &ToolsConfig{
				GitHub: &GitHubToolConfig{},
			},
		}

		secrets := engine.GetRequiredSecretNames(workflowData)

		// Should only include MCP_GATEWAY_API_KEY
		require.Len(t, secrets, 1)
		assert.Contains(t, secrets, "MCP_GATEWAY_API_KEY")
	})
}

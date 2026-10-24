//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractAPITargetHost tests the extractAPITargetHost function that extracts
// hostnames from custom API base URLs in engine.env
func TestExtractAPITargetHost(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		envVar       string
		expected     string
	}{
		{
			name: "extracts hostname from HTTPS URL with path",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://llm-router.internal.example.com/v1",
					},
				},
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "llm-router.internal.example.com",
		},
		{
			name: "extracts hostname from HTTP URL with port and path",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"ANTHROPIC_BASE_URL": "http://localhost:8080/v1",
					},
				},
			},
			envVar:   "ANTHROPIC_BASE_URL",
			expected: "localhost:8080",
		},
		{
			name: "handles hostname without protocol or path",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": "api.openai.com",
					},
				},
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "api.openai.com",
		},
		{
			name: "handles hostname with port but no protocol",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": "localhost:8000",
					},
				},
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "localhost:8000",
		},
		{
			name: "returns empty string when env var not set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OTHER_VAR": "value",
					},
				},
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "",
		},
		{
			name: "returns empty string when engine config is nil",
			workflowData: &WorkflowData{
				EngineConfig: nil,
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "",
		},
		{
			name:         "returns empty string when workflow data is nil",
			workflowData: nil,
			envVar:       "OPENAI_BASE_URL",
			expected:     "",
		},
		{
			name: "returns empty string for empty URL",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": "",
					},
				},
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "",
		},
		{
			name: "extracts Azure OpenAI endpoint hostname",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": "https://my-resource.openai.azure.com/openai/deployments/gpt-4",
					},
				},
			},
			envVar:   "OPENAI_BASE_URL",
			expected: "my-resource.openai.azure.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractAPITargetHost(tt.workflowData, tt.envVar)
			assert.Equal(t, tt.expected, result, "Extracted hostname should match expected value")
		})
	}
}

// TestAWFCustomAPITargetFlags tests that BuildAWFConfigJSON includes custom API targets
// when OPENAI_BASE_URL or ANTHROPIC_BASE_URL are configured in engine.env.
// With config file support (default AWF version), API targets move to the JSON config
// rather than being emitted as --*-api-target CLI flags.

func TestAWFCustomAPITargetFlags(t *testing.T) {
	t.Run("includes openai target in config JSON when OPENAI_BASE_URL is configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
				Env: map[string]string{
					"OPENAI_BASE_URL": "https://llm-router.internal.example.com/v1",
					"OPENAI_API_KEY":  "${{ secrets.LLM_ROUTER_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "codex",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		// API targets are in the JSON config file, not in CLI args
		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, `"openai"`, "Should include openai target in config JSON")
		assert.Contains(t, awfConfigJSON, "llm-router.internal.example.com", "Should include custom hostname in config JSON")

		// --openai-api-target should NOT appear as a CLI flag
		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--openai-api-target", "Should not emit --openai-api-target as CLI flag when config file is used")
	})

	t.Run("includes anthropic target in config JSON when ANTHROPIC_BASE_URL is configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
				Env: map[string]string{
					"ANTHROPIC_BASE_URL": "https://claude-proxy.internal.company.com",
					"ANTHROPIC_API_KEY":  "${{ secrets.CLAUDE_PROXY_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "claude",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, `"anthropic"`, "Should include anthropic target in config JSON")
		assert.Contains(t, awfConfigJSON, "claude-proxy.internal.company.com", "Should include custom hostname in config JSON")

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--anthropic-api-target", "Should not emit --anthropic-api-target as CLI flag when config file is used")
	})

	t.Run("does not include api targets in config JSON when using default URLs", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
				// No custom OPENAI_BASE_URL
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "codex",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.NotContains(t, awfConfigJSON, `"openai"`, "Should not include openai target when not configured")
		assert.NotContains(t, awfConfigJSON, `"anthropic"`, "Should not include anthropic target when not configured")

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--openai-api-target", "Should not include --openai-api-target when not configured")
		assert.NotContains(t, argsStr, "--anthropic-api-target", "Should not include --anthropic-api-target when not configured")
	})

	t.Run("includes both api targets in config JSON when both are configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "custom",
				Env: map[string]string{
					"OPENAI_BASE_URL":    "https://openai-proxy.company.com/v1",
					"ANTHROPIC_BASE_URL": "https://anthropic-proxy.company.com",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "custom",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, `"openai"`, "Should include openai target")
		assert.Contains(t, awfConfigJSON, "openai-proxy.company.com", "Should include OpenAI custom hostname")
		assert.Contains(t, awfConfigJSON, `"anthropic"`, "Should include anthropic target")
		assert.Contains(t, awfConfigJSON, "anthropic-proxy.company.com", "Should include Anthropic custom hostname")

		// API targets should not appear as CLI flags
		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--openai-api-target", "Should not emit --openai-api-target as CLI flag")
		assert.NotContains(t, argsStr, "--anthropic-api-target", "Should not emit --anthropic-api-target as CLI flag")
	})
}

func TestExtractAPITargetAuthHeader(t *testing.T) {
	makeWorkflowData := func(provider, authHeader string) *WorkflowData {
		return &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Targets: map[string]*AgentAPIProxyTargetConfig{
						provider: {AuthHeader: authHeader},
					},
				},
			},
		}
	}

	t.Run("returns authHeader for openai provider", func(t *testing.T) {
		result := extractAPITargetAuthHeader(makeWorkflowData("openai", "api-key"), "openai")
		assert.Equal(t, "api-key", result)
	})

	t.Run("returns authHeader for anthropic provider", func(t *testing.T) {
		result := extractAPITargetAuthHeader(makeWorkflowData("anthropic", "x-custom-header"), "anthropic")
		assert.Equal(t, "x-custom-header", result)
	})

	t.Run("returns empty string when sandbox config is absent", func(t *testing.T) {
		wd := &WorkflowData{EngineConfig: &EngineConfig{ID: "codex"}}
		assert.Empty(t, extractAPITargetAuthHeader(wd, "openai"))
	})

	t.Run("returns empty string when provider is absent", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{
					Targets: map[string]*AgentAPIProxyTargetConfig{},
				},
			},
		}
		assert.Empty(t, extractAPITargetAuthHeader(wd, "openai"))
	})

	t.Run("returns empty string for nil WorkflowData", func(t *testing.T) {
		assert.Empty(t, extractAPITargetAuthHeader(nil, "openai"))
	})

	t.Run("returns empty string when targets is nil", func(t *testing.T) {
		wd := &WorkflowData{
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{},
			},
		}
		assert.Empty(t, extractAPITargetAuthHeader(wd, "openai"))
	})
}

// TestExtractAPIBasePath tests the extractAPIBasePath function that extracts
// path components from custom API base URLs in engine.env

func TestExtractAPIBasePath(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"databricks serving endpoint", "https://host.com/serving-endpoints", "/serving-endpoints"},
		{"azure openai deployment", "https://host.com/openai/deployments/gpt-4", "/openai/deployments/gpt-4"},
		{"simple path", "https://host.com/v1", "/v1"},
		{"trailing slash stripped", "https://host.com/api/", "/api"},
		{"multiple trailing slashes stripped", "https://host.com/api///", "/api"},
		{"no path", "https://host.com", ""},
		{"bare hostname", "host.com", ""},
		{"root path only", "https://host.com/", ""},
		{"query string stripped", "https://host.com/api?param=value", "/api"},
		{"fragment stripped", "https://host.com/api#section", "/api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				EngineConfig: &EngineConfig{
					Env: map[string]string{
						"OPENAI_BASE_URL": tt.url,
					},
				},
			}
			result := extractAPIBasePath(workflowData, "OPENAI_BASE_URL")
			assert.Equal(t, tt.expected, result, "Extracted base path should match expected value")
		})
	}

	t.Run("returns empty string when workflow data is nil", func(t *testing.T) {
		result := extractAPIBasePath(nil, "OPENAI_BASE_URL")
		assert.Empty(t, result, "Should return empty string for nil workflow data")
	})

	t.Run("returns empty string when engine config is nil", func(t *testing.T) {
		workflowData := &WorkflowData{EngineConfig: nil}
		result := extractAPIBasePath(workflowData, "OPENAI_BASE_URL")
		assert.Empty(t, result, "Should return empty string when engine config is nil")
	})

	t.Run("returns empty string when env var not set", func(t *testing.T) {
		workflowData := &WorkflowData{
			EngineConfig: &EngineConfig{
				Env: map[string]string{"OTHER_VAR": "value"},
			},
		}
		result := extractAPIBasePath(workflowData, "OPENAI_BASE_URL")
		assert.Empty(t, result, "Should return empty string when env var not set")
	})
}

// TestAWFBasePathFlags tests that BuildAWFArgs includes --openai-api-base-path and
// --anthropic-api-base-path when the configured URLs contain a path component.
// Note: API targets (hosts) move to the JSON config file, while base paths remain
// as CLI flags — they are not yet represented in the AWF config file schema.

func TestAWFBasePathFlags(t *testing.T) {
	t.Run("includes openai-api-base-path when OPENAI_BASE_URL has path component", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
				Env: map[string]string{
					"OPENAI_BASE_URL": "https://stone-dataplatform.cloud.databricks.com/serving-endpoints",
					"OPENAI_API_KEY":  "${{ secrets.DATABRICKS_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "codex",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		// Base path is still a CLI flag (not in config file schema yet)
		assert.Contains(t, argsStr, "--openai-api-base-path", "Should include --openai-api-base-path flag")
		assert.Contains(t, argsStr, "/serving-endpoints", "Should include the path component")

		// API target (host) is now in the config JSON
		assert.NotContains(t, argsStr, "--openai-api-target", "API target should be in config JSON, not CLI args")
		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, "stone-dataplatform.cloud.databricks.com", "Target host should be in config JSON")
	})

	t.Run("includes anthropic-api-base-path when ANTHROPIC_BASE_URL has path component", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
				Env: map[string]string{
					"ANTHROPIC_BASE_URL": "https://proxy.company.com/anthropic/v1",
					"ANTHROPIC_API_KEY":  "${{ secrets.ANTHROPIC_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "claude",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		// Base path is still a CLI flag
		assert.Contains(t, argsStr, "--anthropic-api-base-path", "Should include --anthropic-api-base-path flag")
		assert.Contains(t, argsStr, "/anthropic/v1", "Should include the path component")

		// API target (host) is now in the config JSON
		assert.NotContains(t, argsStr, "--anthropic-api-target", "API target should be in config JSON, not CLI args")
		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, "proxy.company.com", "Target host should be in config JSON")
	})

	t.Run("does not include base-path flags when URLs have no path", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
				Env: map[string]string{
					"OPENAI_BASE_URL":    "https://openai-proxy.company.com",
					"ANTHROPIC_BASE_URL": "https://anthropic-proxy.company.com",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{Enabled: true},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "codex",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		assert.NotContains(t, argsStr, "--openai-api-base-path", "Should not include --openai-api-base-path when no path in URL")
		assert.NotContains(t, argsStr, "--anthropic-api-base-path", "Should not include --anthropic-api-base-path when no path in URL")
	})
}

// TestBuildAWFArgsAuditDir tests that audit-dir and proxy-logs-dir are emitted in config,
// not CLI flags, for both standard and ARC/DinD workflows.

func TestEngineExecutionWithCustomAPITarget(t *testing.T) {
	t.Run("Codex engine includes openai target in config JSON when OPENAI_BASE_URL is configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
				Env: map[string]string{
					"OPENAI_BASE_URL": "https://llm-router.internal.example.com/v1",
					"OPENAI_API_KEY":  "${{ secrets.LLM_ROUTER_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCodexEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Should generate execution steps")

		stepContent := strings.Join(steps[0], "\n")

		// API target is in the JSON config (in the printf command), not as a CLI flag
		assert.Contains(t, stepContent, `\"openai\"`, "Should include openai target in config JSON")
		assert.Contains(t, stepContent, "llm-router.internal.example.com", "Should include custom hostname in config JSON")
		assert.NotContains(t, stepContent, "--openai-api-target", "Should not emit --openai-api-target as CLI flag")
	})

	t.Run("Claude engine includes anthropic target in config JSON when ANTHROPIC_BASE_URL is configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
				Env: map[string]string{
					"ANTHROPIC_BASE_URL": "https://claude-proxy.internal.company.com",
					"ANTHROPIC_API_KEY":  "${{ secrets.CLAUDE_PROXY_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewClaudeEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		assert.NotEmpty(t, steps, "Should generate execution steps")

		stepContent := strings.Join(steps[0], "\n")

		// API target is in the JSON config (in the printf command), not as a CLI flag
		assert.Contains(t, stepContent, `\"anthropic\"`, "Should include anthropic target in config JSON")
		assert.Contains(t, stepContent, "claude-proxy.internal.company.com", "Should include custom hostname in config JSON")
		assert.NotContains(t, stepContent, "--anthropic-api-target", "Should not emit --anthropic-api-target as CLI flag")
	})
}

// TestGetCopilotAPITarget tests the GetCopilotAPITarget helper that resolves the effective
// Copilot API target from engine.api-target or supported Copilot base URL env vars.

func TestGetCopilotAPITarget(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     string
	}{
		{
			name: "engine.api-target takes precedence over GITHUB_COPILOT_BASE_URL",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "api.acme.ghe.com",
					Env: map[string]string{
						"GITHUB_COPILOT_BASE_URL": "https://other.endpoint.com",
					},
				},
			},
			expected: "api.acme.ghe.com",
		},
		{
			name: "GITHUB_COPILOT_BASE_URL used as fallback when api-target not set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						"GITHUB_COPILOT_BASE_URL": "https://copilot-api.contoso-aw.ghe.com",
					},
				},
			},
			expected: "copilot-api.contoso-aw.ghe.com",
		},
		{
			name: "GITHUB_COPILOT_BASE_URL with path extracts hostname only",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						"GITHUB_COPILOT_BASE_URL": "https://copilot-proxy.corp.example.com/v1",
					},
				},
			},
			expected: "copilot-proxy.corp.example.com",
		},
		{
			name: "literal COPILOT_PROVIDER_BASE_URL used as final fallback when other target sources are unset",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "http://host.docker.internal:11434/v1",
					},
				},
			},
			expected: "host.docker.internal:11434",
		},
		{
			name: "empty when neither api-target nor supported copilot base url env vars are set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
			},
			expected: "",
		},
		{
			name: "empty when COPILOT_PROVIDER_BASE_URL is a GitHub expression",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "${{ secrets.PROVIDER_BASE_URL }}",
					},
				},
			},
			expected: "",
		},
		{
			name:         "empty when workflowData is nil",
			workflowData: nil,
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCopilotAPITarget(tt.workflowData)
			assert.Equal(t, tt.expected, result, "GetCopilotAPITarget should return expected hostname")
		})
	}
}

func TestIsCopilotBYOKMode(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		sandboxEnabled bool
		expected       bool
	}{
		{
			name:           "false when no BYOK signals with sandbox enabled",
			workflowData:   &WorkflowData{EngineConfig: &EngineConfig{ID: "copilot"}},
			sandboxEnabled: true,
			expected:       false,
		},
		{
			name: "true via COPILOT_PROVIDER_BASE_URL when non-empty even with sandbox disabled",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "https://api.openai.com/v1",
					},
				},
			},
			sandboxEnabled: false,
			expected:       true,
		},
		{
			name: "false when COPILOT_PROVIDER_BASE_URL is empty",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "",
					},
				},
			},
			sandboxEnabled: true,
			expected:       false,
		},
		{
			name: "true for non-github provider when sandbox enabled",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:          "copilot",
					LLMProvider: LLMProviderAnthropic,
				},
			},
			sandboxEnabled: true,
			expected:       true,
		},
		{
			name: "false for non-github provider when sandbox disabled",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:          "copilot",
					LLMProvider: LLMProviderAnthropic,
				},
			},
			sandboxEnabled: false,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isCopilotBYOKMode(tt.workflowData, tt.sandboxEnabled))
		})
	}
}

func TestIsCopilotCustomConfig(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     bool
	}{
		{
			name: "not customized when no custom provider or target is set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{ID: "copilot"},
			},
			expected: false,
		},
		{
			name: "customized when COPILOT_PROVIDER_BASE_URL is set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "https://api.openai.com/v1",
					},
				},
			},
			expected: true,
		},
		{
			name: "not customized when COPILOT_PROVIDER_BASE_URL is empty",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "",
					},
				},
			},
			expected: false,
		},
		{
			name: "customized when model-provider gateway is enabled with firewall",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:          "copilot",
					LLMProvider: LLMProviderAnthropic,
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			},
			expected: true,
		},
		{
			name: "not customized when model-provider is non-github but firewall is disabled",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:          "copilot",
					LLMProvider: LLMProviderAnthropic,
				},
			},
			expected: false,
		},
		{
			name: "customized when engine.api-target is set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "api.acme.ghe.com",
				},
			},
			expected: true,
		},
		{
			name: "customized when GITHUB_COPILOT_BASE_URL is set",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						"GITHUB_COPILOT_BASE_URL": "https://copilot-api.contoso-aw.ghe.com",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isCopilotCustomConfig(tt.workflowData))
		})
	}
}

func TestBuildAWFConfigJSONIncludesCopilotLiteralBYOKTarget(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		githubURL string
		apiTarget string
		version   string
		wantHost  string
		wantHosts []string
	}{
		{
			name:      "HTTP scheme and port with default AWF",
			baseURL:   "http://host.docker.internal:11434/v1",
			wantHost:  "http://host.docker.internal:11434",
			wantHosts: []string{"host.docker.internal:11434"},
		},
		{
			name:      "HTTP at minimum supported AWF",
			baseURL:   "http://host.docker.internal:11434/v1",
			version:   string(constants.AWFHTTPAPITargetMinVersion),
			wantHost:  "http://host.docker.internal:11434",
			wantHosts: []string{"host.docker.internal:11434"},
		},
		{
			name:      "HTTP with observed AWF version",
			baseURL:   "http://host.docker.internal:11434/v1",
			version:   "v0.28.70",
			wantHost:  "http://host.docker.internal:11434",
			wantHosts: []string{"host.docker.internal:11434"},
		},
		{
			name:      "older AWF retains bare host and port",
			baseURL:   "http://host.docker.internal:11434/v1",
			version:   "v0.28.12",
			wantHost:  "host.docker.internal:11434",
			wantHosts: []string{"host.docker.internal:11434"},
		},
		{
			name:      "unknown AWF version retains bare host and port",
			baseURL:   "http://host.docker.internal:11434/v1",
			version:   "custom-build",
			wantHost:  "host.docker.internal:11434",
			wantHosts: []string{"host.docker.internal:11434"},
		},
		{
			name:      "latest AWF preserves HTTP",
			baseURL:   "http://host.docker.internal:11434/v1",
			version:   "latest",
			wantHost:  "http://host.docker.internal:11434",
			wantHosts: []string{"host.docker.internal:11434"},
		},
		{
			name:      "HTTPS retains bare host and port",
			baseURL:   "https://provider.example.com:8443/v1",
			wantHost:  "provider.example.com:8443",
			wantHosts: []string{"provider.example.com:8443"},
		},
		{
			name:      "bare host remains unchanged",
			baseURL:   "provider.example.com:8080/v1",
			wantHost:  "provider.example.com:8080",
			wantHosts: []string{"provider.example.com:8080"},
		},
		{
			name:      "explicit api-target takes precedence over both URLs",
			baseURL:   "http://host.docker.internal:11434/v1",
			githubURL: "http://copilot.example.com:8080/v1",
			apiTarget: "api.acme.ghe.com",
			wantHost:  "api.acme.ghe.com",
			wantHosts: []string{"host.docker.internal:11434", "api.acme.ghe.com"},
		},
		{
			name:      "GitHub base URL takes precedence and remains host-only",
			baseURL:   "https://provider.example.com/v1",
			githubURL: "http://copilot.example.com:8080/v1",
			wantHost:  "copilot.example.com:8080",
			wantHosts: []string{"provider.example.com", "copilot.example.com:8080"},
		},
		{
			name:      "non-BYOK HTTP GitHub base URL remains host-only",
			githubURL: "http://copilot.example.com:8080/v1",
			wantHost:  "copilot.example.com:8080",
			wantHosts: []string{"copilot.example.com:8080"},
		},
		{
			name:      "GitHub HTTPS URL is not changed by HTTP provider URL",
			baseURL:   "http://host.docker.internal:11434/v1",
			githubURL: "https://copilot.example.com",
			wantHost:  "copilot.example.com",
			wantHosts: []string{"host.docker.internal:11434", "copilot.example.com"},
		},
		{
			name:    "provider expression is not a literal target",
			baseURL: "${{ secrets.PROVIDER_BASE_URL }}",
		},
		{
			name:    "HTTP URL containing expression is not a literal target",
			baseURL: "http://${{ vars.PROVIDER_HOST }}:11434/v1",
		},
		{
			name: "default Copilot has no target override",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: tt.apiTarget,
					Env: map[string]string{
						constants.CopilotProviderBaseURL: tt.baseURL,
						"GITHUB_COPILOT_BASE_URL":        tt.githubURL,
					},
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true, Version: tt.version},
				},
			}
			config := AWFCommandConfig{
				EngineName:     "copilot",
				AllowedDomains: NewCopilotEngine().buildCopilotAllowedDomains(data),
				WorkflowData:   data,
			}
			jsonStr, err := BuildAWFConfigJSON(config)
			require.NoError(t, err)

			var parsed AWFConfigFile
			require.NoError(t, json.Unmarshal([]byte(jsonStr), &parsed))
			require.NotNil(t, parsed.APIProxy)
			if tt.wantHost == "" {
				assert.NotContains(t, parsed.APIProxy.Targets, "copilot")
			} else {
				require.Contains(t, parsed.APIProxy.Targets, "copilot")
				assert.Equal(t, tt.wantHost, parsed.APIProxy.Targets["copilot"].Host)
			}
			assert.Equal(t, tt.wantHosts, GetCopilotAllowlistTargets(data))
			stepContent := strings.Join(NewCopilotEngine().GetExecutionSteps(data, "test.log")[0], "\n")
			if tt.baseURL == "" {
				assert.False(t, isCopilotBYOKMode(data, true))
				assert.Contains(t, stepContent, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}")
				assert.Contains(t, stepContent, "--exclude-env COPILOT_GITHUB_TOKEN")
			} else {
				assert.NotContains(t, stepContent, "COPILOT_GITHUB_TOKEN:")
			}
			for _, host := range tt.wantHosts {
				require.NotNil(t, parsed.Network)
				assert.Contains(t, parsed.Network.AllowDomains, host)
			}
			if parsed.Network != nil {
				for _, host := range parsed.Network.AllowDomains {
					assert.NotContains(t, host, "://", "firewall allowlist must remain host-only")
				}
			}
		})
	}
}

func TestGetCopilotAllowlistTargets(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		expected     []string
	}{
		{
			name: "includes BYOK provider host and api-target when both are configured",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "api.acme.ghe.com",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "https://llm.corp.example.com/v1",
					},
				},
			},
			expected: []string{"llm.corp.example.com", "api.acme.ghe.com"},
		},
		{
			name: "includes only BYOK provider host when no copilot api target is configured",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "http://localhost:11434/v1",
					},
				},
			},
			expected: []string{"localhost:11434"},
		},
		{
			name: "deduplicates identical provider and api targets",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:        "copilot",
					APITarget: "llm.corp.example.com",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "https://llm.corp.example.com/v1",
					},
				},
			},
			expected: []string{"llm.corp.example.com"},
		},
		{
			name: "skips provider host extraction when BYOK base URL is a GitHub expression",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						constants.CopilotProviderBaseURL: "${{ secrets.PROVIDER_BASE_URL }}",
					},
				},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetCopilotAllowlistTargets(tt.workflowData), "GetCopilotAllowlistTargets should return expected targets for %s", tt.name)
		})
	}
}

// TestCopilotEngineIncludesCopilotAPITargetFromEnvVar tests that the Copilot engine execution
// step includes the copilot API target in the JSON config when GITHUB_COPILOT_BASE_URL is
// configured in engine.env.

func TestCopilotEngineIncludesCopilotAPITargetFromEnvVar(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				"GITHUB_COPILOT_BASE_URL": "https://copilot-api.contoso-aw.ghe.com",
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		},
	}

	engine := NewCopilotEngine()
	steps := engine.GetExecutionSteps(workflowData, "test.log")

	assert.NotEmpty(t, steps, "Should generate execution steps")

	stepContent := strings.Join(steps[0], "\n")

	// With config file support, Copilot API target is in the JSON config (not as CLI flag)
	assert.Contains(t, stepContent, `\"copilot\"`, "Should include copilot target in config JSON")
	assert.Contains(t, stepContent, "copilot-api.contoso-aw.ghe.com", "Should include custom Copilot hostname in config JSON")
	assert.NotContains(t, stepContent, "--copilot-api-target", "Should not emit --copilot-api-target as CLI flag")
}

// TestAWFSupportsExcludeEnv verifies that --exclude-env is only enabled for AWF v0.25.3+.

func TestGetGeminiAPITarget(t *testing.T) {
	tests := []struct {
		name         string
		workflowData *WorkflowData
		engineName   string
		expected     string
	}{
		{
			name: "returns default target for gemini engine with no custom URL",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "gemini",
				},
			},
			engineName: "gemini",
			expected:   "generativelanguage.googleapis.com",
		},
		{
			name: "custom GEMINI_API_BASE_URL takes precedence over default",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "gemini",
					Env: map[string]string{
						"GEMINI_API_BASE_URL": "https://gemini-proxy.internal.company.com/v1",
					},
				},
			},
			engineName: "gemini",
			expected:   "gemini-proxy.internal.company.com",
		},
		{
			name: "returns empty for non-gemini engine without custom URL",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "claude",
				},
			},
			engineName: "claude",
			expected:   "",
		},
		{
			name:         "returns empty when workflowData is nil",
			workflowData: nil,
			engineName:   "gemini",
			expected:     "generativelanguage.googleapis.com",
		},
		{
			name: "returns custom target for non-gemini engine with GEMINI_API_BASE_URL",
			workflowData: &WorkflowData{
				EngineConfig: &EngineConfig{
					ID: "custom",
					Env: map[string]string{
						"GEMINI_API_BASE_URL": "https://custom-proxy.example.com",
					},
				},
			},
			engineName: "custom",
			expected:   "custom-proxy.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetGeminiAPITarget(tt.workflowData, tt.engineName)
			assert.Equal(t, tt.expected, result, "GetGeminiAPITarget should return expected hostname")
		})
	}
}

// TestAWFGeminiAPITargetFlags tests that BuildAWFConfigJSON includes --gemini target
// for the Gemini engine with default and custom endpoints, while base paths remain CLI flags.

func TestAWFGeminiAPITargetFlags(t *testing.T) {
	t.Run("includes default gemini target in config JSON for gemini engine", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "gemini",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "gemini",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		// Gemini target is in the JSON config, not in CLI args
		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, `"gemini"`, "Should include gemini target in config JSON")
		assert.Contains(t, awfConfigJSON, "generativelanguage.googleapis.com", "Should include default Gemini API hostname")

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--gemini-api-target", "Should not emit --gemini-api-target as CLI flag")
	})

	t.Run("includes custom gemini target in config JSON when GEMINI_API_BASE_URL is configured", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "gemini",
				Env: map[string]string{
					"GEMINI_API_BASE_URL": "https://gemini-proxy.internal.company.com/v1",
					"GEMINI_API_KEY":      "${{ secrets.GEMINI_PROXY_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "gemini",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.Contains(t, awfConfigJSON, `"gemini"`, "Should include gemini target in config JSON")
		assert.Contains(t, awfConfigJSON, "gemini-proxy.internal.company.com", "Should include custom Gemini hostname")

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--gemini-api-target", "Should not emit --gemini-api-target as CLI flag")
	})

	t.Run("does not include gemini target for non-gemini engine without custom URL", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "claude",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		awfConfigJSON, err := BuildAWFConfigJSON(config)
		require.NoError(t, err, "BuildAWFConfigJSON should succeed")
		assert.NotContains(t, awfConfigJSON, `"gemini"`, "Should not include gemini target for non-gemini engine")

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "--gemini-api-target", "Should not include --gemini-api-target for non-gemini engine")
	})

	t.Run("includes gemini-api-base-path when custom URL has path component", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "gemini",
				Env: map[string]string{
					"GEMINI_API_BASE_URL": "https://gemini-proxy.company.com/serving-endpoints",
					"GEMINI_API_KEY":      "${{ secrets.GEMINI_PROXY_KEY }}",
				},
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		config := AWFCommandConfig{
			EngineName:     "gemini",
			WorkflowData:   workflowData,
			AllowedDomains: "github.com",
		}

		args := BuildAWFArgs(config)
		argsStr := strings.Join(args, " ")

		// Base path remains as a CLI flag (not in config file schema yet)
		assert.Contains(t, argsStr, "--gemini-api-base-path", "Should include --gemini-api-base-path flag")
		assert.Contains(t, argsStr, "/serving-endpoints", "Should include the path component")
	})
}

// TestGeminiEngineIncludesGeminiAPITarget tests that the Gemini engine execution
// step includes the gemini API target in the JSON config when firewall is enabled.

func TestGeminiEngineIncludesGeminiAPITarget(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID: "gemini",
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{
				Enabled: true,
			},
		},
	}

	engine := NewGeminiEngine()
	steps := engine.GetExecutionSteps(workflowData, "test.log")

	if len(steps) < 2 {
		t.Fatal("Expected at least two execution steps (settings + execution)")
	}

	// steps[0] = Write Gemini Config, steps[1] = Execute Gemini CLI
	stepContent := strings.Join(steps[1], "\n")

	// With config file support, Gemini target is in the JSON config (not as CLI flag)
	assert.Contains(t, stepContent, `\"gemini\"`, "Should include gemini target in config JSON")
	assert.Contains(t, stepContent, "generativelanguage.googleapis.com", "Should include default Gemini API hostname")
	assert.NotContains(t, stepContent, "--gemini-api-target", "Should not emit --gemini-api-target as CLI flag")
}

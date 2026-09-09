//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractAgentSandboxConfigVersion(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.version from object format", func(t *testing.T) {
		agentObj := map[string]any{
			"id":      "awf",
			"version": "v0.30.1",
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Equal(t, "v0.30.1", config.Version, "Should extract sandbox.agent.version")
	})
}

func TestExtractMCPGatewayConfigAgentID(t *testing.T) {
	compiler := &Compiler{}

	config := compiler.extractMCPGatewayConfig(map[string]any{
		"container": "ghcr.io/github/gh-aw-mcpg",
		"agent-id":  "configured-agent-id",
	})

	require.NotNil(t, config, "Should extract MCP gateway config")
	assert.Equal(t, "configured-agent-id", config.AgentID, "Should extract sandbox.mcp.agent-id")
}

func TestExtractAgentSandboxConfigPlatform(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.platform from object format", func(t *testing.T) {
		agentObj := map[string]any{
			"id":       "awf",
			"platform": "ghes",
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Equal(t, "ghes", config.Platform, "Should extract sandbox.agent.platform")
	})
}

func TestExtractAgentSandboxConfigRuntimeInstall(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.runtime-install from object format", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{
			"id":              "awf",
			"runtime-install": false,
		})

		require.NotNil(t, config, "Should extract agent sandbox config")
		require.NotNil(t, config.RuntimeInstall, "Should extract sandbox.agent.runtime-install")
		assert.False(t, *config.RuntimeInstall, "Should preserve sandbox.agent.runtime-install value")
	})

	t.Run("runtime-install is nil when absent", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{"id": "awf"})

		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Nil(t, config.RuntimeInstall, "RuntimeInstall should be nil when not configured")
	})
}

func TestExtractAgentSandboxConfigRuntimeProfile(t *testing.T) {
	compiler := &Compiler{}

	t.Run("omitted runtime resolves to the secure docker profile", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{"id": "awf"})
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Equal(t, AgentRuntime(""), config.Runtime, "Runtime should stay unset when omitted")

		profile := resolveSandboxRuntimeProfile(config)
		assert.Equal(t, AgentRuntimeDocker, profile.Runtime, "Omitted runtime should resolve to the docker profile")
		assert.True(t, profile.NetworkIsolation, "Default profile must keep network isolation")
		assert.True(t, profile.Rootless, "Default profile must run AWF rootless")
		assert.False(t, profile.LegacySecurity, "Default profile must not enable legacy security")
	})

	t.Run("extracts sandbox.agent.runtime: docker-sudo-iptables", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{
			"id":      "awf",
			"runtime": string(AgentRuntimeDockerSudoIptables),
		})
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Equal(t, AgentRuntimeDockerSudoIptables, config.Runtime)

		profile := resolveSandboxRuntimeProfile(config)
		assert.True(t, profile.LegacySecurity, "docker-sudo-iptables must enable legacy security")
		assert.True(t, profile.SupportsHostAccess, "docker-sudo-iptables must allow host access")
		assert.False(t, profile.Rootless, "docker-sudo-iptables runs AWF with sudo")
	})

	t.Run("removed sudo and legacy-security fields are ignored", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{
			"id":              "awf",
			"sudo":            true,
			"legacy-security": "enable",
		})
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Equal(t, AgentRuntime(""), config.Runtime, "Removed fields must not change the runtime profile")
		assert.False(t, resolveSandboxRuntimeProfile(config).LegacySecurity,
			"Removed fields must not re-enable legacy security")
	})
}

func TestExtractAgentSandboxConfigAllowHostPorts(t *testing.T) {
	compiler := &Compiler{}

	config := compiler.extractAgentSandboxConfig(map[string]any{
		"id":               "awf",
		"allow-host-ports": []any{8080, 9090},
	})

	require.NotNil(t, config, "Should extract agent sandbox config")
	assert.Equal(t, []int{8080, 9090}, config.AllowHostPorts)
}

func TestExtractAgentSandboxConfigModelFallback(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.model-fallback false", func(t *testing.T) {
		agentObj := map[string]any{
			"id":             "awf",
			"model-fallback": false,
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		require.NotNil(t, config.ModelFallback, "Should extract model-fallback")
		assert.Equal(t, "false", config.ModelFallback.String(), "Should normalize false to string form")
	})

	t.Run("extracts sandbox.agent.model-fallback true", func(t *testing.T) {
		agentObj := map[string]any{
			"id":             "awf",
			"model-fallback": true,
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		require.NotNil(t, config.ModelFallback, "Should extract model-fallback")
		assert.Equal(t, "true", config.ModelFallback.String(), "Should normalize true to string form")
	})

	t.Run("extracts sandbox.agent.model-fallback expression", func(t *testing.T) {
		expr := "${{ inputs.model-fallback }}"
		agentObj := map[string]any{
			"id":             "awf",
			"model-fallback": expr,
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		require.NotNil(t, config.ModelFallback, "Should extract model-fallback")
		assert.Equal(t, expr, config.ModelFallback.String(), "Should preserve expression")
	})

	t.Run("model-fallback is nil when absent", func(t *testing.T) {
		agentObj := map[string]any{
			"id": "awf",
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Nil(t, config.ModelFallback, "ModelFallback should be nil when not configured")
	})

	t.Run("model-fallback is nil when value is not a boolean or expression", func(t *testing.T) {
		agentObj := map[string]any{
			"id":             "awf",
			"model-fallback": "not-an-expression",
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Nil(t, config.ModelFallback, "ModelFallback should be nil for invalid strings")
	})

	t.Run("model-fallback is nil when value is an object", func(t *testing.T) {
		agentObj := map[string]any{
			"id":             "awf",
			"model-fallback": map[string]any{"enabled": false},
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Nil(t, config.ModelFallback, "ModelFallback should be nil for object value")
	})
}

func TestExtractAgentSandboxConfigTokenSteering(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.token-steering false", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{
			"id":             "awf",
			"token-steering": false,
		})

		require.NotNil(t, config)
		require.NotNil(t, config.TokenSteering)
		assert.False(t, *config.TokenSteering)
	})

	t.Run("token-steering is nil when absent", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{"id": "awf"})

		require.NotNil(t, config)
		assert.Nil(t, config.TokenSteering)
	})
}

func TestExtractAgentSandboxConfigCACert(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.ca-cert", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{
			"id":      "awf",
			"ca-cert": "/etc/ssl/certs/internal-ca.pem",
		})

		require.NotNil(t, config)
		assert.Equal(t, "/etc/ssl/certs/internal-ca.pem", config.CACert)
	})

	t.Run("ca-cert is empty when absent", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{"id": "awf"})

		require.NotNil(t, config)
		assert.Empty(t, config.CACert)
	})

	t.Run("ca-cert is empty when value is not a string", func(t *testing.T) {
		config := compiler.extractAgentSandboxConfig(map[string]any{
			"id":      "awf",
			"ca-cert": 42,
		})

		require.NotNil(t, config)
		assert.Empty(t, config.CACert)
	})
}

func TestExtractAgentSandboxConfigMemory(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts sandbox.agent.memory string", func(t *testing.T) {
		agentObj := map[string]any{
			"id":     "awf",
			"memory": "48g",
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Equal(t, "48g", config.Memory, "Should extract sandbox.agent.memory")
	})

	t.Run("memory is empty when absent", func(t *testing.T) {
		agentObj := map[string]any{
			"id": "awf",
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Empty(t, config.Memory, "Memory should be empty when not configured")
	})

	t.Run("ignores non-string memory value", func(t *testing.T) {
		agentObj := map[string]any{
			"id":     "awf",
			"memory": 48,
		}

		config := compiler.extractAgentSandboxConfig(agentObj)
		require.NotNil(t, config, "Should extract agent sandbox config")
		assert.Empty(t, config.Memory, "Memory should be empty for non-string value")
	})
}

func TestCompileWorkflowPassesSandboxAgentMemoryToAWF(t *testing.T) {
	tmpDir := testutil.TempDir(t, "sandbox-agent-memory-*")
	workflowPath := filepath.Join(tmpDir, "memory-limit.md")

	workflowContent := `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
sandbox:
  agent:
    memory: 48g
---

# Memory limit
`

	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0o644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath))

	lockPath := filepath.Join(tmpDir, "memory-limit.lock.yml")
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err, "expected compiled lock file to be generated")

	lockYAML := string(lockContent)
	assert.Contains(t, lockYAML, "--memory-limit", "compiled lock file should include --memory-limit flag")
	assert.Regexp(t, `--memory-limit\s+48g`, lockYAML, "compiled lock file should pass --memory-limit 48g to AWF")
}

func TestExtractDefaultAiCreditsPricingFromModels(t *testing.T) {
	t.Run("extracts zero pricing for self-hosted BYOK model", func(t *testing.T) {
		frontmatter := map[string]any{
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":  float64(0),
					"output": float64(0),
				},
			},
		}

		pricing := extractDefaultAiCreditsPricingFromModels(frontmatter)
		require.NotNil(t, pricing, "Should extract default-ai-credits-pricing")
		assert.InDelta(t, 0.0, pricing.Input, 1e-9, "Input should be 0")
		assert.InDelta(t, 0.0, pricing.Output, 1e-9, "Output should be 0")
	})

	t.Run("extracts non-zero pricing", func(t *testing.T) {
		frontmatter := map[string]any{
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":       float64(3.0),
					"output":      float64(15.0),
					"cache_read":  float64(0.3),
					"cache_write": float64(3.0),
				},
			},
		}

		pricing := extractDefaultAiCreditsPricingFromModels(frontmatter)
		require.NotNil(t, pricing, "Should extract default-ai-credits-pricing")
		assert.InDelta(t, 3.0, pricing.Input, 1e-9, "Input should be 3.0")
		assert.InDelta(t, 15.0, pricing.Output, 1e-9, "Output should be 15.0")
		require.NotNil(t, pricing.CachedInput, "CachedInput should be extracted when cache_read is set")
		require.NotNil(t, pricing.CacheWrite, "CacheWrite should be extracted when cache_write is set")
		assert.InDelta(t, 0.3, *pricing.CachedInput, 1e-9, "CachedInput should be 0.3")
		assert.InDelta(t, 3.0, *pricing.CacheWrite, 1e-9, "CacheWrite should be 3.0")
	})

	t.Run("default-ai-credits-pricing is nil when absent", func(t *testing.T) {
		frontmatter := map[string]any{
			"models": map[string]any{},
		}

		pricing := extractDefaultAiCreditsPricingFromModels(frontmatter)
		assert.Nil(t, pricing, "Should be nil when default-ai-credits-pricing is absent")
	})

	t.Run("default-ai-credits-pricing is nil when models is absent", func(t *testing.T) {
		frontmatter := map[string]any{}

		pricing := extractDefaultAiCreditsPricingFromModels(frontmatter)
		assert.Nil(t, pricing, "Should be nil when models is absent")
	})

	t.Run("default-ai-credits-pricing is nil when value is not an object", func(t *testing.T) {
		frontmatter := map[string]any{
			"models": map[string]any{
				"default-ai-credits-pricing": "not-an-object",
			},
		}

		pricing := extractDefaultAiCreditsPricingFromModels(frontmatter)
		assert.Nil(t, pricing, "Should be nil for non-object value")
	})

	t.Run("extracts integer pricing values via toFloat64", func(t *testing.T) {
		frontmatter := map[string]any{
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":  int(2),
					"output": int(10),
				},
			},
		}

		pricing := extractDefaultAiCreditsPricingFromModels(frontmatter)
		require.NotNil(t, pricing, "Should extract default-ai-credits-pricing")
		assert.InDelta(t, 2.0, pricing.Input, 1e-9, "Input should be 2")
		assert.InDelta(t, 10.0, pricing.Output, 1e-9, "Output should be 10")
	})
}

func TestResolveDefaultAiCreditsPricing(t *testing.T) {
	t.Run("falls back to imported default pricing when main is absent", func(t *testing.T) {
		frontmatter := map[string]any{}
		imported := map[string]any{
			"input":  5.0,
			"output": 25.0,
		}

		pricing := resolveDefaultAiCreditsPricing(frontmatter, imported)
		require.NotNil(t, pricing)
		assert.InDelta(t, 5.0, pricing.Input, 1e-9)
		assert.InDelta(t, 25.0, pricing.Output, 1e-9)
	})

	t.Run("main workflow pricing overrides imported default pricing", func(t *testing.T) {
		frontmatter := map[string]any{
			"models": map[string]any{
				"default-ai-credits-pricing": map[string]any{
					"input":  1.0,
					"output": 2.0,
				},
			},
		}
		imported := map[string]any{
			"input":  5.0,
			"output": 25.0,
		}

		pricing := resolveDefaultAiCreditsPricing(frontmatter, imported)
		require.NotNil(t, pricing)
		assert.InDelta(t, 1.0, pricing.Input, 1e-9)
		assert.InDelta(t, 2.0, pricing.Output, 1e-9)
	})
}

// TestExtractMCPGatewayConfigPayloadFields tests extraction of payload-related fields
// from MCP gateway frontmatter configuration
func TestExtractMCPGatewayConfigPayloadFields(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts payloadDir using camelCase key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":  "ghcr.io/github/gh-aw-mcpg",
			"payloadDir": "/custom/payloads",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, "/custom/payloads", config.PayloadDir, "Should extract payloadDir")
	})

	t.Run("extracts payloadDir using kebab-case key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":   "ghcr.io/github/gh-aw-mcpg",
			"payload-dir": "/custom/payloads",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, "/custom/payloads", config.PayloadDir, "Should extract payload-dir")
	})

	t.Run("extracts payloadPathPrefix using camelCase key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":         "ghcr.io/github/gh-aw-mcpg",
			"payloadPathPrefix": "/workspace/payloads",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, "/workspace/payloads", config.PayloadPathPrefix, "Should extract payloadPathPrefix")
	})

	t.Run("extracts payloadPathPrefix using kebab-case key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":           "ghcr.io/github/gh-aw-mcpg",
			"payload-path-prefix": "/workspace/payloads",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, "/workspace/payloads", config.PayloadPathPrefix, "Should extract payload-path-prefix")
	})

	t.Run("extracts payloadSizeThreshold using camelCase key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":            "ghcr.io/github/gh-aw-mcpg",
			"payloadSizeThreshold": 65536,
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, 65536, config.PayloadSizeThreshold, "Should extract payloadSizeThreshold")
	})

	t.Run("extracts payloadSizeThreshold using kebab-case key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":              "ghcr.io/github/gh-aw-mcpg",
			"payload-size-threshold": 65536,
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, 65536, config.PayloadSizeThreshold, "Should extract payload-size-threshold")
	})

	t.Run("extracts payloadSizeThreshold as float64 (YAML default numeric type)", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":            "ghcr.io/github/gh-aw-mcpg",
			"payloadSizeThreshold": float64(65536),
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, 65536, config.PayloadSizeThreshold, "Should extract payloadSizeThreshold from float64")
	})

	t.Run("extracts all payload fields together", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":            "ghcr.io/github/gh-aw-mcpg",
			"payloadDir":           "/custom/payloads",
			"payloadPathPrefix":    "/workspace/payloads",
			"payloadSizeThreshold": 1048576,
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, "/custom/payloads", config.PayloadDir, "Should extract payloadDir")
		assert.Equal(t, "/workspace/payloads", config.PayloadPathPrefix, "Should extract payloadPathPrefix")
		assert.Equal(t, 1048576, config.PayloadSizeThreshold, "Should extract payloadSizeThreshold")
	})

	t.Run("leaves payload fields zero/empty when not specified", func(t *testing.T) {
		mcpObj := map[string]any{
			"container": "ghcr.io/github/gh-aw-mcpg",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Empty(t, config.PayloadDir, "PayloadDir should be empty when not specified")
		assert.Empty(t, config.PayloadPathPrefix, "PayloadPathPrefix should be empty when not specified")
		assert.Equal(t, 0, config.PayloadSizeThreshold, "PayloadSizeThreshold should be 0 when not specified")
	})
}

// TestExtractMCPGatewayConfigTrustedBots tests extraction of trustedBots from MCP gateway frontmatter
func TestExtractMCPGatewayConfigTrustedBots(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts trustedBots using camelCase key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":   "ghcr.io/github/gh-aw-mcpg",
			"trustedBots": []any{"github-actions[bot]", "copilot-swe-agent[bot]"},
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, []string{"github-actions[bot]", "copilot-swe-agent[bot]"}, config.TrustedBots, "Should extract trustedBots")
	})

	t.Run("extracts trustedBots using kebab-case key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":    "ghcr.io/github/gh-aw-mcpg",
			"trusted-bots": []any{"github-actions[bot]"},
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, []string{"github-actions[bot]"}, config.TrustedBots, "Should extract trusted-bots")
	})

	t.Run("leaves trustedBots nil when not specified", func(t *testing.T) {
		mcpObj := map[string]any{
			"container": "ghcr.io/github/gh-aw-mcpg",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Nil(t, config.TrustedBots, "TrustedBots should be nil when not specified")
	})
}

// TestExtractMCPGatewayConfigKeepaliveInterval tests extraction of keepalive-interval from MCP gateway frontmatter
func TestExtractMCPGatewayConfigKeepaliveInterval(t *testing.T) {
	compiler := &Compiler{}

	t.Run("extracts keepaliveInterval using camelCase key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":         "ghcr.io/github/gh-aw-mcpg",
			"keepaliveInterval": 300,
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, 300, config.KeepaliveInterval, "Should extract keepaliveInterval")
	})

	t.Run("extracts keepalive-interval using kebab-case key", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":          "ghcr.io/github/gh-aw-mcpg",
			"keepalive-interval": 600,
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, 600, config.KeepaliveInterval, "Should extract keepalive-interval")
	})

	t.Run("extracts -1 to disable keepalive", func(t *testing.T) {
		mcpObj := map[string]any{
			"container":         "ghcr.io/github/gh-aw-mcpg",
			"keepaliveInterval": -1,
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, -1, config.KeepaliveInterval, "Should extract -1 as keepalive disabled sentinel")
	})

	t.Run("leaves keepaliveInterval as 0 when not specified", func(t *testing.T) {
		mcpObj := map[string]any{
			"container": "ghcr.io/github/gh-aw-mcpg",
		}
		config := compiler.extractMCPGatewayConfig(mcpObj)
		require.NotNil(t, config, "Should extract MCP gateway config")
		assert.Equal(t, 0, config.KeepaliveInterval, "KeepaliveInterval should be 0 when not specified")
	})
}

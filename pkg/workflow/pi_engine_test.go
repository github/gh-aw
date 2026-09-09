//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPiEngine(t *testing.T) {
	engine := NewPiEngine()
	require.NotNil(t, engine, "NewPiEngine should return a non-nil engine")
	assert.Equal(t, "pi", engine.GetID(), "Engine ID should be 'pi'")
	assert.Equal(t, "Pi", engine.GetDisplayName(), "Display name should be 'Pi'")
	assert.False(t, engine.IsExperimental(), "Pi engine should not be experimental")
	capabilities := engine.GetCapabilities()
	assert.True(t, capabilities.ToolsAllowlist, "Pi should support tools allowlist (needed for gh-proxy/cli-proxy settings)")
	assert.False(t, capabilities.MCP, "Pi should not support MCP directly")
	assert.True(t, capabilities.MaxTurns, "Pi should support max turns")
}

func TestPiEngine_GetModelEnvVarName(t *testing.T) {
	engine := NewPiEngine()
	assert.Equal(t, "PI_MODEL", engine.GetModelEnvVarName(), "Model env var should be PI_MODEL")
}

func TestPiEngine_ResolveLLMProvider_DefaultGitHub(t *testing.T) {
	engine := NewPiEngine()
	assert.Equal(t, LLMProviderGitHub, engine.ResolveLLMProvider(&WorkflowData{EngineConfig: &EngineConfig{ID: "pi"}}))
}

func TestPiEngine_GetRequiredSecretNames(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{Name: "test-workflow"}
	secrets := engine.GetRequiredSecretNames(workflowData)
	assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN", "Required secrets should include COPILOT_GITHUB_TOKEN")
	assert.NotContains(t, secrets, "PI_API_KEY", "Required secrets should not include PI_API_KEY")
}

func TestPiEngine_GetRequiredSecretNames_CopilotProvider(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/claude-sonnet-4-20250514",
		EngineConfig: &EngineConfig{ID: "pi"},
	}
	secrets := engine.GetRequiredSecretNames(workflowData)
	assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN", "copilot/ prefix should require COPILOT_GITHUB_TOKEN")
}

func TestPiEngine_GetRequiredSecretNames_AnthropicProvider(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "anthropic/claude-sonnet-4-20250514",
		EngineConfig: &EngineConfig{ID: "pi"},
	}
	secrets := engine.GetRequiredSecretNames(workflowData)
	assert.Contains(t, secrets, "ANTHROPIC_API_KEY", "anthropic/ prefix should require ANTHROPIC_API_KEY")
	assert.NotContains(t, secrets, "COPILOT_GITHUB_TOKEN", "anthropic/ prefix should not require COPILOT_GITHUB_TOKEN")
}

func TestPiEngine_GetRequiredSecretNames_CodexProvider(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "codex/gpt-4o",
		EngineConfig: &EngineConfig{ID: "pi"},
	}
	secrets := engine.GetRequiredSecretNames(workflowData)
	assert.Contains(t, secrets, "CODEX_API_KEY", "codex/ prefix should require CODEX_API_KEY")
	assert.Contains(t, secrets, "OPENAI_API_KEY", "codex/ prefix should also require OPENAI_API_KEY (from Codex backend profile)")
}

func TestPiEngine_GetRequiredSecretNames_NoPrefix(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "claude-sonnet-4-20250514",
		EngineConfig: &EngineConfig{ID: "pi"},
	}
	secrets := engine.GetRequiredSecretNames(workflowData)
	assert.Contains(t, secrets, "COPILOT_GITHUB_TOKEN", "bare model (no prefix) should default to COPILOT_GITHUB_TOKEN")
}

func TestPiEngine_GetLogParserScriptId(t *testing.T) {
	engine := NewPiEngine()
	assert.Equal(t, "parse_pi_log", engine.GetLogParserScriptId(), "Log parser script ID should be parse_pi_log")
}

func TestPiEngine_GetLogFileForParsing(t *testing.T) {
	engine := NewPiEngine()
	assert.Equal(t, PiStreamingLogFile, engine.GetLogFileForParsing(), "Log file for parsing should be PiStreamingLogFile")
}

func TestPiEngine_GetAgentManifestFiles(t *testing.T) {
	engine := NewPiEngine()
	files := engine.GetAgentManifestFiles()
	assert.Contains(t, files, "PI.md", "Manifest files should include PI.md")
	assert.Contains(t, files, "AGENTS.md", "Manifest files should include AGENTS.md")
}

func TestPiEngine_GetAgentManifestPathPrefixes(t *testing.T) {
	engine := NewPiEngine()
	prefixes := engine.GetAgentManifestPathPrefixes()
	assert.Contains(t, prefixes, ".pi/", "Path prefixes should include .pi/")
}

func TestPiEngine_GetDeclaredOutputFiles(t *testing.T) {
	engine := NewPiEngine()
	files := engine.GetDeclaredOutputFiles()
	assert.Contains(t, files, PiStreamingLogFile, "Declared output files should include the streaming log")
}

func TestPiEngine_GetInstallationSteps_NoCustomCommand(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: &EngineConfig{ID: "pi"},
	}
	steps := engine.GetInstallationSteps(workflowData)
	assert.NotEmpty(t, steps, "Installation steps should not be empty")

	// The steps should reference @earendil-works/pi-coding-agent
	found := false
	for _, step := range steps {
		for _, line := range step {
			if strings.Contains(line, "@earendil-works/pi-coding-agent") {
				found = true
				break
			}
		}
	}
	assert.True(t, found, "Installation steps should install @earendil-works/pi-coding-agent")

	var rendered strings.Builder
	for _, step := range steps {
		rendered.WriteString(strings.Join(step, "\n"))
		rendered.WriteString("\n")
	}
	assert.NotContains(t, rendered.String(), "NPM_CONFIG_MIN_RELEASE_AGE", "Pi installation should not set the npm release-age cooldown")
}

func TestPiEngine_GetInstallationSteps_WithCustomCommand(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: &EngineConfig{ID: "pi", Command: "/custom/pi"},
	}
	steps := engine.GetInstallationSteps(workflowData)
	assert.Empty(t, steps, "Installation steps should be skipped when custom command is set")
}

func TestPiEngine_GetInstallationSteps_CustomCommandWithExtensions(t *testing.T) {
	engine := NewPiEngine()
	steps := engine.GetInstallationSteps(&WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:         "pi",
			Command:    "/custom/pi",
			Extensions: []string{"@pi/web-search"},
		},
	})
	stepContent := strings.Join(flattenSteps(steps), "\n")

	assert.Contains(t, stepContent, "/custom/pi install @pi/web-search")
	assert.NotContains(t, stepContent, "@earendil-works/pi-coding-agent")
}

func TestPiEngine_GetInstallationSteps_WithExtensions(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:         "pi",
			Extensions: []string{"@pi/web-search", "@pi/file-browser"},
		},
	}
	steps := engine.GetInstallationSteps(workflowData)
	require.NotEmpty(t, steps, "Steps should not be empty with extensions")

	// Find extension install steps
	var extensionSteps []GitHubActionStep
	for _, step := range steps {
		for _, line := range step {
			if strings.Contains(line, "Install Pi extension") {
				extensionSteps = append(extensionSteps, step)
				break
			}
		}
	}
	assert.Len(t, extensionSteps, 2, "Should have 2 extension install steps")
}

func TestPiEngine_GetExecutionSteps_Basic(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		EngineConfig: &EngineConfig{ID: "pi"},
		ParsedTools:  NewTools(map[string]any{}),
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	assert.Contains(t, stepText, "Execute Pi CLI", "Step should be named 'Execute Pi CLI'")
	assert.Contains(t, stepText, "--print", "Step should use --print flag (non-interactive mode)")
	assert.Contains(t, stepText, "--mode json", "Step should use --mode json for structured JSONL output")
	assert.NotContains(t, stepText, "pi run", "Step should not use the removed 'pi run' subcommand")
	assert.NotContains(t, stepText, "--json-log", "Step should not use the removed --json-log flag")
	assert.Contains(t, stepText, "agentic_execution", "Step should have agentic_execution id")
	assert.Contains(t, stepText, "pi_provider.cjs", "Step should load the provider extension")
	assert.Contains(t, stepText, "pi_steering_extension.cjs", "Step should automatically load the steering extension")
	assert.Contains(t, stepText, "shell_harness.cjs", "Step should run Pi through the shared shell harness")
	assert.Contains(t, stepText, "GH_AW_TIMEOUT_MINUTES: 20", "Step should expose the timeout to the shared harness")
}

func TestPiEngine_GetExecutionSteps_WithModel(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/claude-sonnet-4",
		EngineConfig: &EngineConfig{ID: "pi"},
		ParsedTools:  NewTools(map[string]any{}),
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.NotEmpty(t, steps, "Steps should not be empty")

	stepText := strings.Join(steps[0], "\n")
	// When firewall is not enabled, Pi is invoked with the --model flag using the
	// native github-copilot provider (Pi's built-in provider for GitHub Copilot).
	assert.Contains(t, stepText, "--model", "Step should pass --model flag to Pi CLI")
	assert.Contains(t, stepText, "github-copilot", "Non-firewall copilot model should use github-copilot/ provider prefix")
	assert.Contains(t, stepText, "claude-sonnet-4", "Step should include the model ID portion")
	assert.Contains(t, stepText, "GH_AW_PI_MODEL", "Step should expose the original workflow model to Pi extensions")
	assert.NotContains(t, stepText, "\n          PI_MODEL:", "Step should not set PI_MODEL in the environment when the CLI model is passed via --model")
}

func TestPiEngine_GetExecutionSteps_IgnoresRedundantYoloArg(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			ID:   "pi",
			Args: []string{"--yolo", "--custom-flag", "value", "--yolo=true"},
		},
		ParsedTools: NewTools(map[string]any{}),
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.NotEmpty(t, steps, "Steps should not be empty")

	stepText := strings.Join(steps[0], "\n")
	assert.NotContains(t, stepText, "--yolo", "Pi should not pass a redundant --yolo flag")
	assert.Contains(t, stepText, "--custom-flag value", "Pi should preserve non-yolo engine args")
}

func TestFilterPiArgs(t *testing.T) {
	t.Run("empty args", func(t *testing.T) {
		require.Empty(t, filterPiArgs(nil))
		require.Empty(t, filterPiArgs([]string{}))
	})

	t.Run("drops yolo variants only", func(t *testing.T) {
		filtered := filterPiArgs([]string{"--yolo", "--custom-flag", "value", "--yolo=true", "--yolo=false"})
		assert.Equal(t, []string{"--custom-flag", "value"}, filtered)
	})

	t.Run("drops all redundant args", func(t *testing.T) {
		filtered := filterPiArgs([]string{"--yolo", "--yolo=false"})
		assert.Equal(t, []string{}, filtered)
	})
}

func TestResolvePiGatewaySecretEnvVar(t *testing.T) {
	t.Run("uses first core secret when present", func(t *testing.T) {
		profile := universalLLMBackendProfile{coreSecretNames: []string{"CUSTOM_API_KEY", "SECOND"}}
		assert.Equal(t, "CUSTOM_API_KEY", resolvePiGatewaySecretEnvVar(profile, UniversalLLMBackendAnthropic))
	})

	t.Run("falls back to backend defaults when core secrets are empty", func(t *testing.T) {
		profile := universalLLMBackendProfile{}
		assert.Equal(t, "ANTHROPIC_API_KEY", resolvePiGatewaySecretEnvVar(profile, UniversalLLMBackendAnthropic))
		assert.Equal(t, "CODEX_API_KEY", resolvePiGatewaySecretEnvVar(profile, UniversalLLMBackendCodex))
		assert.Equal(t, "COPILOT_GITHUB_TOKEN", resolvePiGatewaySecretEnvVar(profile, UniversalLLMBackendCopilot))
	})
}

func TestPiEngine_GetExecutionSteps_ProviderPrefixCopilot(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/claude-sonnet-4-20250514",
		EngineConfig: &EngineConfig{ID: "pi"},
		ParsedTools:  NewTools(map[string]any{}),
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	assert.Contains(t, stepText, "COPILOT_GITHUB_TOKEN", "copilot/ prefix should inject COPILOT_GITHUB_TOKEN")
	// OPENAI_API_KEY must not be injected: Pi reads it and routes to api.openai.com,
	// bypassing the github-copilot provider and the AWF firewall.
	assert.NotContains(t, stepText, "OPENAI_API_KEY", "copilot/ prefix must not inject OPENAI_API_KEY (causes Pi to use OpenAI instead of github-copilot)")
	assert.Contains(t, stepText, "pi_provider.cjs", "Step should load the provider extension")
	assert.Contains(t, stepText, "--model", "Step should pass --model flag to Pi CLI")
}

func TestPiEngine_GetExecutionSteps_ProviderPrefixAnthropic(t *testing.T) {
	engine := NewPiEngine()
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "anthropic/claude-sonnet-4-20250514",
		EngineConfig: &EngineConfig{ID: "pi"},
		ParsedTools:  NewTools(map[string]any{}),
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	assert.Contains(t, stepText, "ANTHROPIC_API_KEY", "anthropic/ prefix should inject ANTHROPIC_API_KEY")
	assert.NotContains(t, stepText, "COPILOT_GITHUB_TOKEN", "anthropic/ prefix should not inject COPILOT_GITHUB_TOKEN")
}

func TestPiEngine_ImplementsCodingAgentEngine(t *testing.T) {
	var _ CodingAgentEngine = NewPiEngine()
}

func TestPiEngine_GetExecutionSteps_FirewallCopilotProvider(t *testing.T) {
	engine := NewPiEngine()
	toolsRaw := map[string]any{
		"github":    map[string]any{"mode": "gh-proxy"},
		"cli-proxy": true,
	}
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/claude-sonnet-4-20250514",
		EngineConfig: &EngineConfig{ID: "pi"},
		Tools:        toolsRaw,
		ParsedTools:  NewTools(toolsRaw),
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	// When firewall is enabled, Pi uses models.json to route through the api-proxy gateway.
	assert.Contains(t, stepText, "PI_CODING_AGENT_DIR", "Firewall mode should set PI_CODING_AGENT_DIR for models.json config")
	assert.Contains(t, stepText, "shell_harness.cjs", "Firewall mode should run Pi through the shared shell harness")
	assert.Contains(t, stepText, "GH_AW_NODE_BIN=$(command -v node 2>/dev/null || true)", "Firewall mode should capture node path before AWF chroot execution")
	assert.Contains(t, stepText, "export GH_AW_NODE_BIN", "Firewall mode should export GH_AW_NODE_BIN for AWF container")
	assert.Contains(t, stepText, "PI_CODING_AGENT_DIR: /tmp/gh-aw/pi-agent-dir", "PI_CODING_AGENT_DIR should point to the models.json directory")
	assert.Contains(t, stepText, "models.json", "Firewall mode should write a models.json gateway config")
	assert.Contains(t, stepText, "aw-gateway", "Firewall mode should register the aw-gateway provider in models.json")
	assert.Contains(t, stepText, "claude-sonnet-4-20250514", "Step should include the model ID in models.json")
	// AWF config JSON embedded in step must enable the api-proxy sidecar.
	assert.Contains(t, stepText, `\"enabled\":true`, "Firewall mode should enable the api-proxy in AWF config JSON")
	// The models.json is generated at runtime by pi_models_json.cjs, which resolves the
	// live api-proxy port via AWF's /reflect endpoint and falls back to the compile-time
	// gatewayPort otherwise. Verify the setup command exports the expected inputs.
	assert.Contains(t, stepText, "pi_models_json.cjs", "Firewall mode should invoke pi_models_json.cjs to generate models.json at runtime")
	assert.Contains(t, stepText, "GH_AW_PI_MODEL_ID=claude-sonnet-4-20250514", "Should export the model ID for pi_models_json.cjs")
	assert.Contains(t, stepText, "GH_AW_PI_GATEWAY_SECRET_ENV=COPILOT_GITHUB_TOKEN", "Should export the gateway secret env var name for pi_models_json.cjs")
	assert.Contains(t, stepText, fmt.Sprintf("GH_AW_PI_GATEWAY_FALLBACK_PORT=%d", constants.CopilotLLMGatewayPort), "Should export the compile-time fallback port")
	assert.Contains(t, stepText, "GH_AW_LLM_PROVIDER=github", "Should export the reflect provider name for pi_models_json.cjs")
}

func TestPiEngine_GetExecutionSteps_FirewallAnthropicProvider(t *testing.T) {
	engine := NewPiEngine()
	toolsRaw := map[string]any{
		"github":    map[string]any{"mode": "gh-proxy"},
		"cli-proxy": true,
	}
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "anthropic/claude-opus-4-20251101",
		EngineConfig: &EngineConfig{ID: "pi"},
		Tools:        toolsRaw,
		ParsedTools:  NewTools(toolsRaw),
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	assert.Contains(t, stepText, "PI_CODING_AGENT_DIR", "Firewall mode should set PI_CODING_AGENT_DIR for models.json config")
	assert.Contains(t, stepText, "aw-gateway", "Firewall mode should register the aw-gateway provider in models.json")
	assert.Contains(t, stepText, "aw-gateway/claude-opus-4-20251101", "Firewall mode should route model via aw-gateway provider")
	assert.NotContains(t, stepText, " --model anthropic/claude-opus-4-20251101", "Firewall mode must not use native provider resolution")
	assert.Contains(t, stepText, "claude-opus-4-20251101", "Step should include the model ID in models.json")
	assert.Contains(t, stepText, `\"enabled\":true`, "Firewall mode should enable the api-proxy in AWF config JSON")
	// Anthropic provider routes through the Claude LLM gateway port as a fallback; the live
	// port is resolved at runtime via /reflect by pi_models_json.cjs.
	assert.Contains(t, stepText, "pi_models_json.cjs", "Firewall mode should invoke pi_models_json.cjs to generate models.json at runtime")
	assert.Contains(t, stepText, "GH_AW_PI_MODEL_ID=claude-opus-4-20251101", "Should export the model ID for pi_models_json.cjs")
	assert.Contains(t, stepText, "GH_AW_PI_GATEWAY_SECRET_ENV=ANTHROPIC_API_KEY", "Should export the gateway secret env var name for pi_models_json.cjs")
	assert.Contains(t, stepText, fmt.Sprintf("GH_AW_PI_GATEWAY_FALLBACK_PORT=%d", constants.ClaudeLLMGatewayPort), "Should export the compile-time fallback port")
	assert.Contains(t, stepText, "GH_AW_LLM_PROVIDER=anthropic", "Should export the reflect provider name for pi_models_json.cjs")
}

func TestPiEngine_GetExecutionSteps_FirewallCodexProvider(t *testing.T) {
	engine := NewPiEngine()
	toolsRaw := map[string]any{
		"github":    map[string]any{"mode": "gh-proxy"},
		"cli-proxy": true,
	}
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "codex/gpt-4.1",
		EngineConfig: &EngineConfig{ID: "pi"},
		Tools:        toolsRaw,
		ParsedTools:  NewTools(toolsRaw),
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	assert.Contains(t, stepText, "PI_CODING_AGENT_DIR", "Firewall mode should set PI_CODING_AGENT_DIR for models.json config")
	assert.Contains(t, stepText, "aw-gateway", "Firewall mode should register the aw-gateway provider in models.json")
	assert.Contains(t, stepText, "aw-gateway/gpt-4.1", "Firewall mode should route model via aw-gateway provider")
	assert.NotContains(t, stepText, " --model openai/gpt-4.1", "Firewall mode must not use native provider resolution")
	assert.Contains(t, stepText, "gpt-4.1", "Step should include the model ID in models.json")
	assert.Contains(t, stepText, `\"enabled\":true`, "Firewall mode should enable the api-proxy in AWF config JSON")
	// Codex/OpenAI provider routes through the Codex LLM gateway port as a fallback; the live
	// port is resolved at runtime via /reflect by pi_models_json.cjs.
	assert.Contains(t, stepText, "pi_models_json.cjs", "Firewall mode should invoke pi_models_json.cjs to generate models.json at runtime")
	assert.Contains(t, stepText, "GH_AW_PI_MODEL_ID=gpt-4.1", "Should export the model ID for pi_models_json.cjs")
	assert.Contains(t, stepText, "GH_AW_PI_GATEWAY_SECRET_ENV=CODEX_API_KEY", "Should export the gateway secret env var name for pi_models_json.cjs")
	assert.Contains(t, stepText, fmt.Sprintf("GH_AW_PI_GATEWAY_FALLBACK_PORT=%d", constants.CodexLLMGatewayPort), "Should export the compile-time fallback port")
	assert.Contains(t, stepText, "GH_AW_LLM_PROVIDER=openai", "Should export the reflect provider name for pi_models_json.cjs")
}

// TestPiEngine_GetExecutionSteps_FirewallCopilotProvider_CopilotRequestsWrite verifies that
// when copilot-requests: write permission is set, Pi still routes LLM calls through the AWF
// api-proxy gateway (models.json) instead of using the native github-copilot provider.
// Without this, Pi would bypass the firewall and call api.individual.githubcopilot.com
// directly, which is blocked, causing a "no safe outputs" failure.
func TestPiEngine_GetExecutionSteps_FirewallCopilotProvider_CopilotRequestsWrite(t *testing.T) {
	engine := NewPiEngine()
	toolsRaw := map[string]any{
		"github":    map[string]any{"mode": "gh-proxy"},
		"cli-proxy": true,
	}
	workflowData := &WorkflowData{
		Name:         "test-workflow",
		Model:        "copilot/gpt-5.4",
		EngineConfig: &EngineConfig{ID: "pi"},
		Tools:        toolsRaw,
		ParsedTools:  NewTools(toolsRaw),
		Permissions:  "permissions:\n  copilot-requests: write",
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}
	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/agent-stdio.log")
	require.Len(t, steps, 1, "Should produce exactly one execution step")

	stepText := strings.Join(steps[0], "\n")
	// Pi must route through the AWF api-proxy gateway even when copilot-requests: write is set.
	// The native provider (github-copilot/gpt-5.4) hits api.individual.githubcopilot.com
	// directly, which is blocked by the firewall.
	assert.Contains(t, stepText, "PI_CODING_AGENT_DIR", "Firewall mode should set PI_CODING_AGENT_DIR for models.json config")
	assert.Contains(t, stepText, "models.json", "Firewall mode should write a models.json gateway config")
	assert.Contains(t, stepText, "aw-gateway", "Should use aw-gateway provider (api-proxy) not native github-copilot provider")
	assert.NotContains(t, stepText, "github-copilot/gpt-5.4", "Should not pass github-copilot provider directly to Pi CLI (would bypass firewall)")
	assert.Contains(t, stepText, "aw-gateway/gpt-5.4", "Should use aw-gateway/gpt-5.4 model flag")
	// The models.json must route through the Copilot gateway port using COPILOT_GITHUB_TOKEN
	// (set to ${{ github.token }} in copilot-requests: write mode); pi_models_json.cjs
	// resolves the live port via /reflect at runtime and falls back to this compile-time port.
	assert.Contains(t, stepText, "pi_models_json.cjs", "Firewall mode should invoke pi_models_json.cjs to generate models.json at runtime")
	assert.Contains(t, stepText, "GH_AW_PI_MODEL_ID=gpt-5.4", "Should export the model ID for pi_models_json.cjs")
	assert.Contains(t, stepText, "GH_AW_PI_GATEWAY_SECRET_ENV=COPILOT_GITHUB_TOKEN", "Should export the gateway secret env var name for pi_models_json.cjs")
	assert.Contains(t, stepText, fmt.Sprintf("GH_AW_PI_GATEWAY_FALLBACK_PORT=%d", constants.CopilotLLMGatewayPort), "Should export the compile-time fallback port")
	assert.Contains(t, stepText, "GH_AW_LLM_PROVIDER=github", "Should export the reflect provider name for pi_models_json.cjs")
}

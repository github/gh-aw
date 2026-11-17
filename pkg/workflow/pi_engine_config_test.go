//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPiEngineConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantError bool
	}{
		{"settings", `{"settings":{"defaultThinkingLevel":"high"}}`, false},
		{"persistent session", `{"session":{"enabled":true,"id":"run-1","export":true}}`, false},
		{"invalid JSON", `{`, true},
		{"null", `null`, true},
		{"unknown field", `{"other":{}}`, true},
		{"invalid settings", `{"settings":false}`, true},
		{"resume without persistence", `{"session":{"resume":"session.jsonl"}}`, true},
		{"conflicting resume", `{"session":{"enabled":true,"resume":"a","fork":"b"}}`, true},
		{"model metadata", `{"model":{"api":"openai-responses","reasoning":true,"input":["text","image"],"contextWindow":128000,"maxTokens":32000}}`, false},
		{"model typo", `{"model":{"maxTokenz":32000}}`, true},
		{"unsupported API", `{"model":{"api":"unsupported"}}`, true},
		{"invalid context limit", `{"model":{"contextWindow":0}}`, true},
		{"invalid reasoning type", `{"model":{"reasoning":"yes"}}`, true},
		{"invalid input type", `{"model":{"input":["audio"]}}`, true},
		{"invalid cache metadata", `{"model":{"promptCache":{"short":"300"}}}`, true},
		{"MCP exposure", `{"mcp":{"exposure":"codemode","toolExposure":{"github":{"get_*":"direct","delete_*":"hidden"}}}}`, false},
		{"MCP typo", `{"mcp":{"exposur":"direct"}}`, true},
		{"unsupported exposure", `{"mcp":{"exposure":"unsupported"}}`, true},
		{"tool exposure list", `{"mcp":{"toolExposure":[]}}`, true},
		{"flat tool exposure", `{"mcp":{"toolExposure":{"github":"direct"}}}`, true},
		{"invalid tool exposure", `{"mcp":{"toolExposure":{"github":{"get_*":"unsupported"}}}}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := NewCompiler().validatePiEngineConfig(&WorkflowData{EngineConfig: &EngineConfig{ID: "pi", Config: test.config}})
			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPiEngineDefaultMCPExposure(t *testing.T) {
	for _, test := range []struct {
		name   string
		config string
		want   string
	}{
		{"omitted", "", `{"mcp":{"exposure":"direct"}}`},
		{"empty", `{}`, `{"mcp":{"exposure":"direct"}}`},
		{"empty MCP", `{"mcp":{}}`, `{"mcp":{"exposure":"direct"}}`},
		{"other settings", `{"settings":{"defaultThinkingLevel":"high"},"model":{"maxTokens":32000},"session":{"enabled":true}}`, `{"settings":{"defaultThinkingLevel":"high"},"model":{"maxTokens":32000},"session":{"enabled":true},"mcp":{"exposure":"direct"}}`},
		{"tool overrides", `{"mcp":{"toolExposure":{"github":{"delete_*":"hidden"}}}}`, `{"mcp":{"exposure":"direct","toolExposure":{"github":{"delete_*":"hidden"}}}}`},
		{"explicit direct", `{"mcp":{"exposure":"direct"}}`, `{"mcp":{"exposure":"direct"}}`},
		{"explicit deferred", `{"mcp":{"exposure":"deferred"}}`, `{"mcp":{"exposure":"deferred"}}`},
		{"explicit codemode", `{"mcp":{"exposure":"codemode"}}`, `{"mcp":{"exposure":"codemode"}}`},
		{"explicit hidden", `{"mcp":{"exposure":"hidden"}}`, `{"mcp":{"exposure":"hidden"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: "pi", Config: test.config}}
			require.NoError(t, validatePiJSONConfig(test.config))
			env := make(map[string]string)
			NewPiEngine().applyPiConfigEnv(env, data)
			assert.JSONEq(t, test.want, env["GH_AW_PI_CONFIG"])
			assert.Equal(t, test.config, data.EngineConfig.Config, "defaulting must not mutate authored configuration")
		})
	}
	t.Run("no engine config", func(t *testing.T) {
		env := make(map[string]string)
		NewPiEngine().applyPiConfigEnv(env, &WorkflowData{})
		assert.JSONEq(t, `{"mcp":{"exposure":"direct"}}`, env["GH_AW_PI_CONFIG"])
	})
}

func TestPiEngineMCPExposureAcrossDrivers(t *testing.T) {
	for _, driver := range []string{"", "pi_agent_core_driver.cjs", "pi_rpc_driver.cjs"} {
		t.Run(driver, func(t *testing.T) {
			data := &WorkflowData{EngineConfig: &EngineConfig{ID: "pi", Driver: driver}}
			steps := NewPiEngine().GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
			assert.Contains(t, strings.Join(steps[0], "\n"), `GH_AW_PI_CONFIG: '{"mcp":{"exposure":"direct"}}'`)
		})
	}
}

func TestPiEngineNativeProvidersAndFederation(t *testing.T) {
	engine := NewPiEngine()
	google := &WorkflowData{Model: "google/gemini-2.5-pro", EngineConfig: &EngineConfig{ID: "pi"}}
	assert.Contains(t, engine.GetRequiredSecretNames(google), "GEMINI_API_KEY")
	assert.Equal(t, "google", engine.ResolveLLMProvider(google).String())
	assert.Equal(t, "google", piConfiguredProvider(google))
	domains, err := GetDefaultDomainsForEngine(constants.PiEngine, google.Model)
	require.NoError(t, err)
	assert.Contains(t, domains, "generativelanguage.googleapis.com")
	transportDomains, err := GetAllowedDomainsForEngineWithModel(constants.PiEngine, google.Model,
		&NetworkPermissions{Allowed: []string{"pi"}}, nil, nil)
	require.NoError(t, err)
	assert.NotContains(t, transportDomains, "generativelanguage.googleapis.com")
	for _, tt := range []struct {
		model, domain string
	}{
		{"copilot/claude-sonnet-4", "api.githubcopilot.com"},
		{"anthropic/claude-sonnet-4", "api.anthropic.com"},
		{"openai/gpt-4.1", "api.openai.com"},
		{"google/gemini-2.5-pro", "generativelanguage.googleapis.com"},
	} {
		providerDomains, err := GetDefaultDomainsForEngine(constants.PiEngine, tt.model)
		require.NoError(t, err)
		assert.Contains(t, providerDomains, tt.domain, "provider endpoint metadata for %s", tt.model)
	}
	for _, tt := range []struct {
		model, optIn, domain string
	}{
		{"copilot/claude-sonnet-4", "copilot-vendor", "api.githubcopilot.com"},
		{"openai/gpt-4.1", "codex-vendor", "api.openai.com"},
		{google.Model, "gemini-vendor", "generativelanguage.googleapis.com"},
	} {
		directDomains, err := GetAllowedDomainsForEngineWithModel(constants.PiEngine, tt.model,
			&NetworkPermissions{Allowed: []string{"pi", tt.optIn}}, nil, nil)
		require.NoError(t, err)
		assert.Contains(t, directDomains, tt.domain)
	}
	google.EngineConfig.Auth = &EngineAuthConfig{Type: "github-oidc", Provider: "gcp"}
	google.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}
	assert.NotContains(t, engine.GetRequiredSecretNames(google), "GEMINI_API_KEY")
}

func TestPiEngineSessionAndBareArguments(t *testing.T) {
	engine := NewPiEngine()
	data := &WorkflowData{EngineConfig: &EngineConfig{ID: "pi", Bare: true, Config: `{"session":{"enabled":true,"id":"run-1"}}`}}
	args := engine.buildPiArgs(data)
	assert.NotContains(t, args, "--no-session")
	assert.Contains(t, args, "--session-id")
	assert.Contains(t, args, "--no-context-files")
	assert.Contains(t, args, "--no-approve")
}

func TestPiEngineRejectsUnroutableProvider(t *testing.T) {
	data := &WorkflowData{
		Model:              "openrouter/anthropic/claude-sonnet-4",
		EngineConfig:       &EngineConfig{ID: "pi"},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	require.ErrorContains(t, NewCompiler().validatePiEngineConfig(data), "credential-isolated AWF route")
	data.EngineConfig.LLMProvider = LLMProviderOpenAI
	require.NoError(t, NewCompiler().validatePiEngineConfig(data))
}

func TestPiEngineToolPolicyAndRPC(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "pi", Driver: "pi_rpc_driver.cjs", MaxToolCalls: "20"},
		Tools:        map[string]any{"bash": []any{"echo"}, "edit": false},
	}
	require.NoError(t, validateMaxToolCallsConfig(data.EngineConfig, NewPiEngine()))
	steps := NewPiEngine().GetExecutionSteps(data, "/tmp/gh-aw/agent-stdio.log")
	rendered := strings.Join(steps[0], "\n")
	assert.Contains(t, rendered, "pi_rpc_driver.cjs")
	assert.Contains(t, rendered, "GH_AW_MAX_TOOL_CALLS: 20")
	assert.Contains(t, rendered, "GH_AW_PI_TOOL_POLICY")
	assert.Contains(t, piToolPolicyJSON(data), `"bash":["echo"]`)
}

func TestPiEngineToolPolicyAllowsMountedCLIs(t *testing.T) {
	tools := map[string]any{"bash": []any{"echo"}}
	data := &WorkflowData{
		EngineConfig: &EngineConfig{ID: "pi"},
		Tools:        tools,
		ParsedTools:  NewTools(tools),
		SafeOutputs:  &SafeOutputsConfig{NoOp: &NoOpConfig{}},
	}
	policy := piToolPolicyJSON(data)
	assert.Contains(t, policy, `"safeoutputs:*"`)
	assert.Equal(t, []any{"echo"}, data.Tools["bash"], "the workflow allowlist must not be mutated")

	tools["bash"] = []any{"*"}
	assert.NotContains(t, piToolPolicyJSON(data), "safeoutputs:*", "unrestricted bash needs no mounted CLI entries")
}

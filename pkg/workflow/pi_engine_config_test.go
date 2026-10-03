//go:build !integration

package workflow

import (
	"strings"
	"testing"

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

func TestPiEngineNativeProvidersAndFederation(t *testing.T) {
	engine := NewPiEngine()
	google := &WorkflowData{Model: "google/gemini-2.5-pro", EngineConfig: &EngineConfig{ID: "pi"}}
	assert.Contains(t, engine.GetRequiredSecretNames(google), "GEMINI_API_KEY")
	assert.Equal(t, "google", engine.ResolveLLMProvider(google).String())
	assert.Equal(t, "google", piConfiguredProvider(google))
	domains, err := getPiDefaultDomains(google.Model)
	require.NoError(t, err)
	assert.Contains(t, domains, "generativelanguage.googleapis.com")
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

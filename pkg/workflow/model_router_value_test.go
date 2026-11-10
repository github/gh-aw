//go:build !integration

package workflow

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

const routerValueBase = "---\non: workflow_dispatch\npermissions:\n  contents: read\n"

var awInfoModelConfigPattern = regexp.MustCompile(`GH_AW_INFO_MODEL_CONFIG: '([^']*)'`)

// routedObjective returns the routing objective and candidate models of the
// compiled workflow's (single) routed AWF config.
func routedObjective(t *testing.T, c defaultRoutingCompilation) (map[string]any, any) {
	t.Helper()
	require.NoError(t, c.err)
	configs := c.awfConfigs(t)
	require.Len(t, configs, 1, "an explicit router value must not be runtime-guarded")
	routing, ok := configs[0]["apiProxy"].(map[string]any)["routing"].(map[string]any)
	require.True(t, ok, "routing config must be emitted")
	return routing["objective"].(map[string]any), routing["candidateModels"]
}

func TestCompileRouterModelValue_Accepted(t *testing.T) {
	for _, tc := range []struct {
		name       string
		files      map[string]string
		mode       string
		requested  string
		candidates any
		disallowed any
	}{
		{
			name:      "model: router",
			files:     map[string]string{"main.md": routerValueBase + "model: router\nengine: copilot\n---\nReport.\n"},
			mode:      "auto",
			requested: "router",
		},
		{
			name:      "model: router/economy",
			files:     map[string]string{"main.md": routerValueBase + "model: router/economy\nengine: copilot\n---\nReport.\n"},
			mode:      "economy",
			requested: "router/economy",
		},
		{
			name:      "engine.model router/balanced",
			files:     map[string]string{"main.md": routerValueBase + "engine:\n  id: copilot\n  model: router/balanced\n---\nReport.\n"},
			mode:      "balanced",
			requested: "router/balanced",
		},
		{
			name:      "engine.model router value wins over top-level pinned model",
			files:     map[string]string{"main.md": routerValueBase + "model: gpt-5.6-luna\nengine:\n  id: copilot\n  model: router/robust\n---\nReport.\n"},
			mode:      "robust",
			requested: "router/robust",
		},
		{
			name:      "engine.model router value wins over top-level router value",
			files:     map[string]string{"main.md": routerValueBase + "model: router/economy\nengine:\n  id: copilot\n  model: router/balanced\n---\nReport.\n"},
			mode:      "balanced",
			requested: "router/balanced",
		},
		{
			name:       "claude on github keeps the claude family",
			files:      map[string]string{"main.md": routerValueBase + "model: router\nengine:\n  id: claude\n  model-provider: github\n---\nReport.\n"},
			mode:       "auto",
			requested:  "router",
			candidates: []any{"github-copilot/claude-*"},
		},
		{
			name:       "models.blocked narrows the routed choice",
			files:      map[string]string{"main.md": routerValueBase + "model: router/robust\nengine: copilot\nmodels:\n  blocked: [\"claude-opus-*\"]\n---\nReport.\n"},
			mode:       "robust",
			requested:  "router/robust",
			disallowed: []any{"claude-opus-*"},
		},
		{
			name: "router value in an imported shared file",
			files: map[string]string{
				"main.md":           routerValueBase + "engine: copilot\nimports:\n  - shared/routing.md\n---\nReport.\n",
				"shared/routing.md": "---\nmodel: router/economy\n---\n# Shared\n",
			},
			mode:      "economy",
			requested: "router/economy",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileDefaultRoutingFiles(t, tc.files)
			objective, candidates := routedObjective(t, compiled)
			require.Equal(t, map[string]any{"goal": "cost", "mode": tc.mode}, objective)
			if tc.candidates != nil {
				require.Equal(t, tc.candidates, candidates)
			}
			if tc.disallowed != nil {
				proxy := compiled.awfConfigs(t)[0]["apiProxy"].(map[string]any)
				require.Equal(t, tc.disallowed, proxy["disallowedModels"])
			}
			require.Empty(t, compiled.data.Model, "a router value is not a model")
			require.NotContains(t, compiled.stderr, defaultRoutingNoticeMarker, "an explicit router value is not the implicit default")
			require.NotContains(t, compiled.lock, "id: resolve-model-routing", "an explicit router value is not runtime-guarded")
			require.Contains(t, compiled.lock, `"requested":"`+tc.requested+`"`)
		})
	}
}

func TestCompileRouterModelValue_ImportedValueLosesToOwnModel(t *testing.T) {
	compiled := compileDefaultRoutingFiles(t, map[string]string{
		"main.md":           routerValueBase + "model: gpt-5.6-luna\nengine: copilot\nimports:\n  - shared/routing.md\n---\nReport.\n",
		"shared/routing.md": "---\nmodel: router/economy\n---\n# Shared\n",
	})
	require.NoError(t, compiled.err)
	require.Equal(t, "gpt-5.6-luna", compiled.data.Model)
	require.NotContains(t, compiled.lock, `\"routing\":{`)
	require.NotContains(t, compiled.lock, "router/economy")
}

// A router value compiles to the same routing configuration as the matching
// engine.model-routing block; only the recorded requested value differs.
func TestCompileRouterModelValue_EquivalentToModelRoutingBlock(t *testing.T) {
	for _, tc := range []struct{ value, mode string }{
		{"router", "auto"},
		{"router/economy", "economy"},
		{"router/balanced", "balanced"},
		{"router/robust", "robust"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			routed := compileDefaultRoutingSource(t, routerValueBase+"model: "+tc.value+"\nengine: copilot\n---\nReport.\n")
			explicit := compileDefaultRoutingSource(t, routerValueBase+"engine:\n  id: copilot\n  model-routing:\n    goal: cost\n    mode: "+tc.mode+"\n---\nReport.\n")
			require.NoError(t, routed.err)
			require.NoError(t, explicit.err)
			require.Equal(t, explicit.awfConfigs(t), routed.awfConfigs(t))

			routedConfig := lockModelConfig(t, routed.lock)
			explicitConfig := lockModelConfig(t, explicit.lock)
			routedRouting := routedConfig["routing"].(map[string]any)
			require.Equal(t, tc.value, routedRouting["requested"])
			delete(routedRouting, "requested")
			require.Equal(t, explicitConfig, routedConfig)
		})
	}
}

func lockModelConfig(t *testing.T, lock string) map[string]any {
	t.Helper()
	match := awInfoModelConfigPattern.FindStringSubmatch(lock)
	require.NotNil(t, match, "GH_AW_INFO_MODEL_CONFIG must be emitted")
	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(match[1]), &config))
	return config
}

func TestCompileRouterModelValue_Rejected(t *testing.T) {
	routingBlock := "  model-routing:\n    goal: cost\n    mode: economy\n"
	for _, tc := range []struct {
		name      string
		files     map[string]string
		wantError []string
	}{
		{name: "router/fast", files: map[string]string{"main.md": routerValueBase + "model: router/fast\nengine: copilot\n---\nReport.\n"},
			wantError: []string{"unknown router value", "router, router/economy, router/balanced, router/robust"}},
		{name: "router/auto", files: map[string]string{"main.md": routerValueBase + "model: router/auto\nengine: copilot\n---\nReport.\n"},
			wantError: []string{"unknown router value", "router, router/economy, router/balanced, router/robust"}},
		{name: "router/Economy", files: map[string]string{"main.md": routerValueBase + "model: router/Economy\nengine: copilot\n---\nReport.\n"},
			wantError: []string{"unknown router value", "router, router/economy, router/balanced, router/robust"}},
		{name: "unknown router value in engine.model", files: map[string]string{"main.md": routerValueBase + "engine:\n  id: copilot\n  model: router/fast\n---\nReport.\n"},
			wantError: []string{"unknown router value"}},
		{name: "effort parameter", files: map[string]string{"main.md": routerValueBase + "model: router/economy?effort=high\nengine: copilot\n---\nReport.\n"},
			wantError: []string{"router values do not take parameters", "pin a model to set the effort"}},
		{name: "with engine.model-routing", files: map[string]string{"main.md": routerValueBase + "model: router/economy\nengine:\n  id: copilot\n" + routingBlock + "---\nReport.\n"},
			wantError: []string{"a router value and engine.model-routing both choose model routing"}},
		{name: "with imported engine.model-routing", files: map[string]string{
			"main.md":           routerValueBase + "model: router\nimports:\n  - shared/routing.md\n---\nReport.\n",
			"shared/routing.md": "---\nengine:\n  id: copilot\n" + routingBlock + "---\n# Shared\n",
		}, wantError: []string{"a router value and engine.model-routing both choose model routing"}},
		{name: "with engine.model-routing false", files: map[string]string{"main.md": routerValueBase + "model: router\nengine:\n  id: copilot\n  model-routing: false\n---\nReport.\n"},
			wantError: []string{"cannot be combined with engine.model-routing: false"}},
		{name: "with engine.fallback-models", files: map[string]string{"main.md": routerValueBase + "model: router\nengine:\n  id: copilot\n  fallback-models: [gpt-5.6-luna]\n---\nReport.\n"},
			wantError: []string{"cannot be combined with engine.fallback-models"}},
		{name: "sub-agent", files: map[string]string{"main.md": routerValueBase + "engine: copilot\n---\n# Main\n\n## agent: `helper`\n---\nmodel: router/economy\n---\nWork.\n"},
			wantError: []string{"only the main agent can be routed", `sub-agent "helper" model`}},
		{name: "threat detection", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nsafe-outputs:\n  create-issue:\n  threat-detection:\n    model: router/economy\n---\nReport.\n"},
			wantError: []string{"only the main agent can be routed", "safe-outputs.threat-detection.model"}},
		{name: "threat detection engine.model", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nsafe-outputs:\n  create-issue:\n  threat-detection:\n    engine:\n      id: copilot\n      model: router\n---\nReport.\n"},
			wantError: []string{"only the main agent can be routed", "safe-outputs.threat-detection.model"}},
		{name: "evals", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nevals:\n  model: router\n  questions:\n    - id: useful\n      question: Was the result useful?\n---\nReport.\n"},
			wantError: []string{"only the main agent can be routed", "evals.model"}},
		{name: "models.allowed", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nmodels:\n  allowed: [router/economy]\n---\nReport.\n"},
			wantError: []string{"router values are not models", "models.allowed"}},
		{name: "models.blocked", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nmodels:\n  blocked: [router]\n---\nReport.\n"},
			wantError: []string{"router values are not models", "models.blocked"}},
		{name: "fallback-models", files: map[string]string{"main.md": routerValueBase + "model: gpt-5.6-luna\nengine:\n  id: copilot\n  fallback-models: [router/robust]\n---\nReport.\n"},
			wantError: []string{"router values are not models", "engine.fallback-models"}},
		{name: "models alias entry", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nmodels:\n  cheap: [router/economy]\n---\nReport.\n"},
			wantError: []string{"router values are not models", "models.cheap"}},
		{name: "models alias named router", files: map[string]string{"main.md": routerValueBase + "engine: copilot\nmodels:\n  router: [gpt-5.6-luna]\n---\nReport.\n"},
			wantError: []string{"router is reserved and cannot be defined as a model alias"}},
		{name: "gemini", files: map[string]string{"main.md": routerValueBase + "model: router\nengine: gemini\n---\nReport.\n"},
			wantError: []string{"supported only by Copilot, Claude, Codex, and pi"}},
		{name: "sandbox.agent false", files: map[string]string{"main.md": "---\non: workflow_dispatch\nstrict: false\npermissions:\n  contents: read\nfeatures:\n  dangerously-disable-sandbox-agent: true\nsandbox:\n  agent: false\nmodel: router\nengine: copilot\n---\nReport.\n"},
			wantError: []string{"requires the AWF firewall"}},
		{name: "endpoint outside GitHub Copilot", files: map[string]string{"main.md": routerValueBase + "model: router\nengine:\n  id: copilot\n  model-provider: openai\n---\nReport.\n"},
			wantError: []string{`resolves to the "openai" provider`}},
		{name: "custom api-target", files: map[string]string{"main.md": routerValueBase + "model: router\nengine:\n  id: claude\n  api-target: api.example.com\n---\nReport.\n"},
			wantError: []string{"native HTTPS endpoint"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileDefaultRoutingFiles(t, tc.files)
			require.Error(t, compiled.err)
			for _, want := range tc.wantError {
				require.ErrorContains(t, compiled.err, want)
			}
		})
	}
}

func TestIsReservedRouterModelName(t *testing.T) {
	for model, want := range map[string]bool{
		"router":                  true,
		"router/economy":          true,
		"Router/Economy":          true,
		"router/fast":             true,
		"router?effort=high":      true,
		"openrouter/auto":         false,
		"copilot/router":          false,
		"routerless":              false,
		"gpt-6.1-sol":             false,
		"${{ vars.MODEL }}":       false,
		"router/${{ vars.MODE }}": false,
		"":                        false,
	} {
		require.Equal(t, want, isReservedRouterModelName(model), model)
	}
}

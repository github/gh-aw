package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAWFNativeFallbackModels(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "codex", "gemini", "pi"} {
		t.Run(id, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig:       &EngineConfig{ID: id, FallbackModels: []string{"secondary", "last"}},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
			}
			require.NoError(t, validateEngineFallbackModels(data))
			require.Equal(t, []string{"secondary", "last"}, nativeAWFFallbackModels(data))
			env := map[string]string{}
			applyEngineHarnessRetryEnv(env, data)
			require.Equal(t, "1", env["GH_AW_NATIVE_FALLBACK_MODELS"])
			require.NotContains(t, env, "GH_AW_FALLBACK_MODELS")
			config, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: id, WorkflowData: data})
			require.NoError(t, err)
			var got struct {
				APIProxy struct {
					FallbackModels []string `json:"fallbackModels"`
					ModelFallback  struct {
						Enabled bool `json:"enabled"`
					} `json:"modelFallback"`
				} `json:"apiProxy"`
			}
			require.NoError(t, json.Unmarshal([]byte(config), &got))
			require.Equal(t, []string{"secondary", "last"}, got.APIProxy.FallbackModels)
			require.False(t, got.APIProxy.ModelFallback.Enabled)
		})
	}
}

func TestAWFNativeFallbackGates(t *testing.T) {
	for _, tc := range []struct {
		name, version, image string
		models               []string
		aliases              map[string][]string
	}{
		{"old firewall", "v0.28.30", "", []string{"secondary"}, nil},
		{"old proxy image", "", "ghcr.io/github/gh-aw-firewall/api-proxy:0.28.30", []string{"secondary"}, nil},
		{"unknown proxy version", "", "ghcr.io/github/gh-aw-firewall/api-proxy:development", []string{"secondary"}, nil},
		{"cross provider", "", "", []string{"openai/secondary"}, nil},
		{"runtime alias", "", "", []string{"recovery"}, map[string][]string{"recovery": {"copilot/secondary"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig:       &EngineConfig{ID: "copilot", FallbackModels: tc.models},
				ModelMappings:      tc.aliases,
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: tc.version}},
			}
			if tc.image != "" {
				data.SandboxConfig = &SandboxConfig{Agent: &AgentSandboxConfig{Images: map[string]string{awfImageRoleAPIProxy: tc.image + "@sha256:" + strings.Repeat("a", 64)}}}
			}
			require.Empty(t, nativeAWFFallbackModels(data))
			env := map[string]string{}
			applyEngineHarnessRetryEnv(env, data)
			require.Contains(t, env, "GH_AW_FALLBACK_MODELS")
			require.NotContains(t, env, "GH_AW_NATIVE_FALLBACK_MODELS")
			config, err := BuildAWFConfigJSON(AWFCommandConfig{EngineName: "copilot", WorkflowData: data})
			require.NoError(t, err)
			require.NotContains(t, config, `"fallbackModels"`)
		})
	}
}

func TestAWFNativeFallbackProviderPrefixes(t *testing.T) {
	data := &WorkflowData{
		EngineConfig:       &EngineConfig{ID: "codex", FallbackModels: []string{"openai/secondary", "last"}},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	require.Equal(t, []string{"secondary", "last"}, nativeAWFFallbackModels(data))
	data.EngineConfig.ID = "gemini"
	data.EngineConfig.FallbackModels = []string{"google/secondary", "gemini/last"}
	require.Equal(t, []string{"secondary", "last"}, nativeAWFFallbackModels(data))
	data.EngineConfig.FallbackModels = []string{"anthropic/last"}
	require.Error(t, validateEngineFallbackModels(data))
}

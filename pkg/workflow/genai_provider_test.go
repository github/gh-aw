//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveBYOKGenAIProviderName(t *testing.T) {
	firewall := &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}}
	tests := []struct {
		name    string
		engine  *EngineConfig
		network *NetworkPermissions
		want    string
	}{
		{name: "github-hosted copilot", engine: &EngineConfig{ID: "copilot"}, want: ""},
		{name: "non-copilot engine", engine: &EngineConfig{ID: "codex", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://x.openai.azure.com/openai/v1"}}, want: ""},
		{name: "azure ai gateway host", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://example.ai.gateway.azure.com/default/models/openai/v1", "COPILOT_PROVIDER_TYPE": "openai"}}, want: "azure.ai.openai"},
		{name: "azure apim host with port", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://gw.azure-api.net:8443/openai/v1"}}, want: "azure.ai.openai"},
		{name: "azure auth with expression url", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "${{ vars.GATEWAY_URL }}"}, Auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure"}}, want: "azure.ai.openai"},
		{name: "azure provider type", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "${{ vars.GATEWAY_URL }}", "COPILOT_PROVIDER_TYPE": "azure"}}, want: "azure.ai.openai"},
		{name: "generic openai-compatible", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "http://localhost:11434/v1"}}, want: "openai"},
		{name: "anthropic provider type", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://api.anthropic.com", "COPILOT_PROVIDER_TYPE": "anthropic"}}, want: "anthropic"},
		{name: "gcp auth with base url", engine: &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://gateway.example.com/v1"}, Auth: &EngineAuthConfig{Type: "github-oidc", Provider: "gcp"}}, want: "openai"},
		{name: "engine provider override", engine: &EngineConfig{ID: "copilot", LLMProvider: LLMProviderAnthropic}, network: firewall, want: "anthropic"},
		{name: "gcp engine provider override", engine: &EngineConfig{ID: "copilot", LLMProvider: "gcp"}, network: firewall, want: "gcp.gen_ai"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBYOKGenAIProviderName(&WorkflowData{EngineConfig: tt.engine, NetworkPermissions: tt.network})
			assert.Equal(t, tt.want, got)
		})
	}
	assert.Empty(t, resolveBYOKGenAIProviderName(nil))
}

func TestCompiledBYOKWorkflowRecordsGenAIProvider(t *testing.T) {
	compile := func(t *testing.T, env string) string {
		t.Helper()
		return compileWorkflowAndReadLock(t, `---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: copilot
`+env+`---
# Test
`)
	}

	byok := compile(t, "  env:\n    COPILOT_PROVIDER_BASE_URL: https://example.ai.gateway.azure.com/default/models/openai/v1\n")
	assert.Contains(t, byok, `GH_AW_INFO_GENAI_PROVIDER: "azure.ai.openai"`)
	require.Greater(t, strings.Count(byok, "GH_AW_INFO_GENAI_PROVIDER"), 1, "setup steps should carry the provider for job spans")

	github := compile(t, "")
	assert.NotContains(t, github, "GH_AW_INFO_GENAI_PROVIDER")
}

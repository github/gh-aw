//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexAzureOIDCAuthentication(t *testing.T) {
	for _, tt := range []struct {
		name     string
		auth     *EngineAuthConfig
		provider LLMProvider
		firewall bool
		keyless  bool
		authMode string
	}{
		{name: "Entra behind AWF", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureTenantID: "tenant", AzureClientID: "client"}, firewall: true, keyless: true, authMode: "azure"},
		{name: "OpenAI WIF behind AWF", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, firewall: true, keyless: true, authMode: "openai"},
		{name: "API key", firewall: true},
		{name: "Entra without AWF", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureTenantID: "tenant", AzureClientID: "client"}},
		{name: "other OIDC", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "anthropic"}, firewall: true},
		{name: "other auth", auth: &EngineAuthConfig{Type: "static", Provider: "azure"}, firewall: true},
		{name: "GitHub inference", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureTenantID: "tenant", AzureClientID: "client"}, provider: LLMProviderGitHub, firewall: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig:       &EngineConfig{ID: "codex", LLMProvider: tt.provider, Auth: tt.auth},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: tt.firewall}},
				Tools:              map[string]any{"github": map[string]any{}},
				ParsedTools:        &ToolsConfig{GitHub: &GitHubToolConfig{}},
			}
			engine := NewCodexEngine()
			secrets := engine.GetRequiredSecretNames(data)
			assert.Contains(t, secrets, "MCP_GATEWAY_AGENT_ID")
			config, err := engine.buildNativeConfig(data, nil)
			require.NoError(t, err)
			env := engine.buildCodexExecutionEnv(data, tt.firewall, false, "GH_AW_MODEL_AGENT_CODEX")
			if tt.keyless {
				assert.NotContains(t, secrets, "CODEX_API_KEY")
				assert.NotContains(t, secrets, "OPENAI_API_KEY")
				assert.Empty(t, engine.GetSecretValidationStep(data))
				assert.NotContains(t, env, "CODEX_API_KEY")
				assert.NotContains(t, env, "OPENAI_API_KEY")
				assert.Equal(t, tt.authMode, env["GH_AW_CODEX_OIDC_AUTH"])
				proxy := config.Defaults["model_providers"].(map[string]any)[codexOpenAIProxyProviderID].(map[string]any)
				assert.NotContains(t, proxy, "env_key")
				assert.Equal(t, false, proxy["requires_openai_auth"])
				assert.Equal(t, "responses", proxy["wire_api"])
				excluded := ComputeAWFExcludeEnvVarNames(data, []string{"CODEX_API_KEY", "OPENAI_API_KEY"})
				assert.Contains(t, excluded, "ACTIONS_ID_TOKEN_REQUEST_TOKEN")
				assert.Contains(t, excluded, "ACTIONS_ID_TOKEN_REQUEST_URL")
			} else {
				assert.NotEmpty(t, engine.GetSecretValidationStep(data))
				assert.NotContains(t, env, "GH_AW_CODEX_OIDC_AUTH")
				if tt.provider != LLMProviderGitHub {
					assert.Contains(t, secrets, "CODEX_API_KEY")
					assert.Contains(t, env, "CODEX_API_KEY")
				}
				if tt.firewall {
					proxy := config.Defaults["model_providers"].(map[string]any)[codexOpenAIProxyProviderID].(map[string]any)
					assert.Equal(t, "CODEX_API_KEY", proxy["env_key"])
				}
			}
		})
	}
	assert.False(t, isCodexAzureOIDC(nil))
	assert.False(t, isCodexAzureOIDC(&WorkflowData{}))
}

func TestCodexAzureOIDCWorkflow(t *testing.T) {
	dir := testutil.TempDir(t, "codex-azure-oidc-*")
	workflows := filepath.Join(dir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	file := filepath.Join(workflows, "entra.md")
	require.NoError(t, os.WriteFile(file, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
  id-token: write
engine:
  id: codex
  model: gpt-5.4
  auth:
    type: github-oidc
    provider: azure
    azure-tenant-id: 00000000-0000-0000-0000-000000000001
    azure-client-id: 00000000-0000-0000-0000-000000000002
    audience: api://AzureADTokenExchange
    azure-scope: api://00000000-0000-0000-0000-000000000003/.default
  env:
    OPENAI_BASE_URL: https://gateway.example.com/v1
network:
  allowed: [defaults, gateway.example.com, login.microsoftonline.com]
safe-outputs:
  add-comment:
---
Inspect the repository and report findings.
`), 0o644))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	content, err := os.ReadFile(filepath.Join(workflows, "entra.lock.yml"))
	require.NoError(t, err)
	lock := string(content)
	assert.NotContains(t, lock, "Validate Codex secret")
	assert.NotContains(t, lock, "secrets.CODEX_API_KEY")
	assert.NotContains(t, lock, "secrets.OPENAI_API_KEY")
	assert.Contains(t, lock, "GH_AW_CODEX_OIDC_AUTH: azure")
	assert.Contains(t, lock, "AWF_AUTH_TYPE: github-oidc")
	assert.Contains(t, lock, "AWF_AUTH_AZURE_SCOPE: api://00000000-0000-0000-0000-000000000003/.default")
	assert.Contains(t, lock, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	assert.Contains(t, lock, "--exclude-env ACTIONS_ID_TOKEN_REQUEST_URL")
	detectionSection := extractJobSection(lock, "detection")
	assert.NotEmpty(t, detectionSection)
	assert.Contains(t, detectionSection, "GH_AW_CODEX_OIDC_AUTH: azure")
	assert.Contains(t, detectionSection, "http://api-proxy:10000/reflect")
	assert.Contains(t, detectionSection, "github-oidc/azure")
	assert.NotContains(t, detectionSection, `env_key = "CODEX_API_KEY"`)
	assert.NotContains(t, detectionSection, `CODEX_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}`)
}

func TestCodexAzureOIDCFallbackKeepsKeylessProvider(t *testing.T) {
	data := &WorkflowData{
		Model: "gpt-5.4",
		EngineConfig: &EngineConfig{
			ID: "codex", FallbackModels: []string{"openai/gpt-5.3-codex"},
			Auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureTenantID: "tenant", AzureClientID: "client"},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	assert.Empty(t, fallbackProviderSecretNames(data))
	env := NewCodexEngine().buildCodexExecutionEnv(data, true, true, "GH_AW_MODEL_AGENT_CODEX")
	assert.NotContains(t, env, "CODEX_API_KEY")
	assert.NotContains(t, env, "OPENAI_API_KEY")
	assert.Equal(t, "azure", env["GH_AW_CODEX_OIDC_AUTH"])

	data.EngineConfig.FallbackModels = append(data.EngineConfig.FallbackModels, "anthropic/claude-sonnet-4-6")
	assert.Equal(t, []string{"ANTHROPIC_API_KEY"}, fallbackProviderSecretNames(data))
	env = NewCodexEngine().buildCodexExecutionEnv(data, true, true, "GH_AW_MODEL_AGENT_CODEX")
	assert.Contains(t, env, "ANTHROPIC_API_KEY")
	assert.NotContains(t, env, "CODEX_API_KEY")
	assert.NotContains(t, env, "OPENAI_API_KEY")
	assert.Len(t, buildFallbackProviderValidationSteps(data), 1)
}

func TestValidateAzureWIFEngineAuth(t *testing.T) {
	tests := []struct {
		name        string
		auth        *EngineAuthConfig
		wantErr     bool
		errContains string
	}{
		{name: "nil auth", wantErr: false},
		{name: "other auth type", auth: &EngineAuthConfig{Type: "static", Provider: "azure"}, wantErr: false},
		{name: "other provider", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "gcp"}, wantErr: false},
		{name: "complete Azure auth", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureTenantID: "tenant", AzureClientID: "client"}, wantErr: false},
		{name: "missing tenant ID", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureClientID: "client"}, wantErr: true, errContains: "azure-tenant-id"},
		{name: "missing client ID", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure", AzureTenantID: "tenant"}, wantErr: true, errContains: "azure-client-id"},
		{name: "missing both IDs", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "azure"}, wantErr: true, errContains: "azure-tenant-id, azure-client-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := &WorkflowData{}
			if tt.auth != nil {
				workflow.EngineConfig = &EngineConfig{Auth: tt.auth}
			}
			err := validateAzureWIFEngineAuth(workflow)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Contains(t, err.Error(), "engine.auth with provider=azure requires the following fields")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateOpenAIWIFEngineAuth(t *testing.T) {
	tests := []struct {
		name        string
		auth        *EngineAuthConfig
		wantErr     bool
		errContains string
	}{
		{name: "other provider", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "anthropic"}},
		{name: "complete OpenAI auth", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}},
		{name: "missing identity provider ID", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, wantErr: true, errContains: "identity-provider-id"},
		{name: "missing service account ID", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", Audience: "https://api.openai.com/v1"}, wantErr: true, errContains: "service-account-id"},
		{name: "missing audience", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account"}, wantErr: true, errContains: "audience"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := &WorkflowData{EngineConfig: &EngineConfig{Auth: tt.auth}}
			err := validateOpenAIWIFEngineAuth(workflow)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCodexOpenAIWIFWorkflow(t *testing.T) {
	dir := testutil.TempDir(t, "codex-openai-wif-*")
	workflows := filepath.Join(dir, constants.GetWorkflowDir())
	require.NoError(t, os.MkdirAll(workflows, 0o755))
	file := filepath.Join(workflows, "openai-wif.md")
	require.NoError(t, os.WriteFile(file, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
  id-token: write
engine:
  id: codex
  auth:
    type: github-oidc
    provider: openai
    identity-provider-id: ${{ vars.OPENAI_IDENTITY_PROVIDER_ID }}
    service-account-id: ${{ vars.OPENAI_SERVICE_ACCOUNT_ID }}
    audience: https://api.openai.com/v1
network:
  allowed: [defaults]
safe-outputs:
  add-comment:
---
Inspect the repository and report findings.
`), 0o644))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	content, err := os.ReadFile(filepath.Join(workflows, "openai-wif.lock.yml"))
	require.NoError(t, err)
	lock := string(content)
	assert.NotContains(t, lock, "secrets.CODEX_API_KEY")
	assert.NotContains(t, lock, "secrets.OPENAI_API_KEY")
	assert.Contains(t, lock, "AWF_AUTH_PROVIDER: openai")
	assert.Contains(t, lock, "AWF_AUTH_OPENAI_IDENTITY_PROVIDER_ID: ${{ vars.OPENAI_IDENTITY_PROVIDER_ID }}")
	assert.Contains(t, lock, "AWF_AUTH_OPENAI_SERVICE_ACCOUNT_ID: ${{ vars.OPENAI_SERVICE_ACCOUNT_ID }}")
	assert.Contains(t, lock, "AWF_AUTH_OIDC_AUDIENCE: https://api.openai.com/v1")
	assert.Contains(t, lock, "GH_AW_CODEX_OIDC_AUTH: openai")
}

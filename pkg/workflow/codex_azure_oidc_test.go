//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
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
		name         string
		auth         *EngineAuthConfig
		env          map[string]string
		version      string
		apiProxy     string
		modelRouting bool
		wantErr      bool
		errContains  string
	}{
		{name: "other provider", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "anthropic"}},
		{name: "complete OpenAI auth", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}},
		{name: "missing identity provider ID", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, wantErr: true, errContains: "identity-provider-id"},
		{name: "missing service account ID", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", Audience: "https://api.openai.com/v1"}, wantErr: true, errContains: "service-account-id"},
		{name: "missing audience", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account"}, wantErr: true, errContains: "audience"},
		{name: "older AWF release", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: "v0.29.10", wantErr: true, errContains: "requires AWF v0.29.11 or newer"},
		{name: "latest AWF release", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: "latest", wantErr: true, errContains: "requires AWF v0.29.11 or newer"},
		{name: "short AWF version", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: "v1", wantErr: true, errContains: "requires AWF v0.29.11 or newer"},
		{name: "older API proxy image", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion), apiProxy: "registry.example.com/approved/api-proxy:v0.29.10@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", wantErr: true, errContains: "requires an API proxy image tagged v0.29.11 or newer"},
		{name: "unversioned API proxy image", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion), apiProxy: "registry.example.com/approved/api-proxy:stable@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", wantErr: true, errContains: "does not establish support for OpenAI WIF"},
		{name: "short API proxy version", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion), apiProxy: "registry.example.com/approved/api-proxy:v1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", wantErr: true, errContains: "does not establish support for OpenAI WIF"},
		{name: "two-part API proxy version", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion), apiProxy: "registry.example.com/approved/api-proxy:1.2@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", wantErr: true, errContains: "does not establish support for OpenAI WIF"},
		{name: "supported API proxy image", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion), apiProxy: "registry.example.com/approved/api-proxy:0.29.11@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{name: "model routing generated image is not an explicit override", auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion), modelRouting: true},
		{name: "environment auth with older AWF release", env: map[string]string{"AWF_AUTH_TYPE": "github-oidc", "AWF_AUTH_PROVIDER": "openai", "AWF_AUTH_OPENAI_IDENTITY_PROVIDER_ID": "idp", "AWF_AUTH_OPENAI_SERVICE_ACCOUNT_ID": "service-account", "AWF_AUTH_OIDC_AUDIENCE": "https://api.openai.com/v1"}, version: "v0.29.10", wantErr: true, errContains: "requires AWF v0.29.11 or newer"},
		{name: "environment auth with supported AWF release", env: map[string]string{"AWF_AUTH_TYPE": "github-oidc", "AWF_AUTH_PROVIDER": "openai", "AWF_AUTH_OPENAI_IDENTITY_PROVIDER_ID": "idp", "AWF_AUTH_OPENAI_SERVICE_ACCOUNT_ID": "service-account", "AWF_AUTH_OIDC_AUDIENCE": "https://api.openai.com/v1"}, version: string(constants.AWFOpenAIWIFMinVersion)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflow := &WorkflowData{
				EngineConfig:       &EngineConfig{ID: "codex", Auth: tt.auth, Env: tt.env},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: tt.version}},
			}
			if tt.modelRouting {
				workflow.EngineConfig.ModelRouting = &CopilotModelRoutingConfig{}
			}
			if tt.apiProxy != "" {
				workflow.SandboxConfig = &SandboxConfig{Agent: &AgentSandboxConfig{Images: map[string]string{
					awfImageRoleAPIProxy: tt.apiProxy,
				}}}
			}
			err := validateOpenAIWIFEngineAuthAtPath(workflow, "engine")
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
	agentSection := extractJobSection(lock, "agent")
	assert.NotContains(t, agentSection, "auth.openai.com")
	detectionSection := extractJobSection(lock, "detection")
	assert.NotEmpty(t, detectionSection)
	assert.Contains(t, detectionSection, "http://api-proxy:10000/reflect")
	assert.NotContains(t, detectionSection, "auth.openai.com")
	assert.Contains(t, detectionSection, "github-oidc/openai")
	assert.Contains(t, detectionSection, "No configured OpenAI endpoint uses github-oidc/openai")
	assert.NotContains(t, detectionSection, `env_key = "CODEX_API_KEY"`)
}

func TestCodexOpenAIWIFDetectionValidationAndPermissions(t *testing.T) {
	tests := []struct {
		name        string
		authFields  string
		wantErr     string
		wantIdToken bool
	}{
		{
			name:       "missing identity provider ID",
			authFields: "service-account-id: service-account\naudience: https://api.openai.com/v1",
			wantErr:    "safe-outputs.threat-detection.engine.auth with provider=openai requires the following fields: identity-provider-id",
		},
		{
			name:       "missing service account ID",
			authFields: "identity-provider-id: idp\naudience: https://api.openai.com/v1",
			wantErr:    "service-account-id",
		},
		{
			name:       "missing audience",
			authFields: "identity-provider-id: idp\nservice-account-id: service-account",
			wantErr:    "audience",
		},
		{
			name:        "valid detection-only OpenAI WIF grants token permission",
			authFields:  "identity-provider-id: idp\nservice-account-id: service-account\naudience: https://api.openai.com/v1",
			wantIdToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "codex-openai-wif-detection-*")
			file := filepath.Join(dir, "workflow.md")
			source := `---
on: workflow_dispatch
engine:
  id: codex
  model: gpt-5.4
network:
  allowed: [defaults]
safe-outputs:
  add-comment:
  threat-detection:
    engine:
      id: codex
      auth:
        type: github-oidc
        provider: openai
        AUTH_FIELDS
---
Inspect the repository.
`
			authFields := strings.ReplaceAll(tt.authFields, "\n", "\n        ")
			source = strings.ReplaceAll(source, "AUTH_FIELDS", authFields)
			require.NoError(t, os.WriteFile(file, []byte(source), 0o644))
			err := NewCompiler().CompileWorkflow(file)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			content, err := os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
			require.NoError(t, err)
			detectionSection := extractJobSection(string(content), "detection")
			assert.NotEmpty(t, detectionSection)
			assert.NotContains(t, detectionSection, "auth.openai.com")
			if tt.wantIdToken {
				assert.Contains(t, detectionSection, "id-token: write")
			}
		})
	}
}

func TestCodexOpenAIWIFUnsupportedConfigurations(t *testing.T) {
	for _, tt := range []struct {
		name    string
		engine  string
		network string
		wantErr string
	}{
		{name: "Codex", engine: "codex"},
		{name: "Claude", engine: "claude", wantErr: "engine.id: auth.provider=openai requires codex"},
		{name: "firewall disabled", engine: "codex", network: "strict: false\nfeatures:\n  dangerously-disable-sandbox-agent: true\nsandbox:\n  agent: false\n", wantErr: "requires the firewall to be enabled"},
		{name: "GitHub inference", engine: "codex\n  model-provider: github", wantErr: "requires the OpenAI inference provider"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "openai-wif-validation-*")
			file := filepath.Join(dir, "workflow.md")
			source := `---
on: workflow_dispatch
permissions:
  contents: read
  id-token: write
engine:
  id: ENGINE
  auth:
    type: github-oidc
    provider: openai
    identity-provider-id: idp
    service-account-id: service-account
    audience: https://api.openai.com/v1
NETWORK---
Inspect the repository.
`
			source = strings.ReplaceAll(source, "ENGINE", tt.engine)
			source = strings.ReplaceAll(source, "NETWORK", tt.network)
			require.NoError(t, os.WriteFile(file, []byte(source), 0o644))
			err := NewCompiler().CompileWorkflow(file)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestCodexOpenAIWIFDetectionUsesEffectiveDetectorConfig(t *testing.T) {
	tests := []struct {
		name        string
		mainConfig  string
		detectModel string
	}{
		{
			name: "detector-only configuration without main engine",
		},
		{
			name:       "cross-engine detection inherits main model",
			mainConfig: "engine:\n  id: copilot\n  model: copilot/gpt-5\n",
		},
		{
			name:        "explicit detector model overrides main model",
			mainConfig:  "engine:\n  id: copilot\n  model: copilot/gpt-5\n",
			detectModel: "    model: gpt-5.4\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := testutil.TempDir(t, "openai-wif-effective-detection-*")
			file := filepath.Join(dir, "workflow.md")
			source := `---
on: workflow_dispatch
permissions:
  contents: read
MAIN_CONFIGnetwork:
  allowed: [defaults]
safe-outputs:
  add-comment:
  threat-detection:
DETECTION_MODEL    engine:
      id: codex
      auth:
        type: github-oidc
        provider: openai
        identity-provider-id: idp
        service-account-id: service-account
        audience: https://api.openai.com/v1
---
Inspect the repository.
`
			source = strings.ReplaceAll(source, "MAIN_CONFIG", tt.mainConfig)
			source = strings.ReplaceAll(source, "DETECTION_MODEL", tt.detectModel)
			require.NoError(t, os.WriteFile(file, []byte(source), 0o644))
			require.NoError(t, NewCompiler().CompileWorkflow(file))

			content, err := os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
			require.NoError(t, err)
			detectionSection := extractJobSection(string(content), "detection")
			assert.NotContains(t, detectionSection, "auth.openai.com")
			assert.Contains(t, detectionSection, "id-token: write")
		})
	}
}

func TestCodexOpenAIWIFSameEngineDetectorOverrideGetsIDTokenPermission(t *testing.T) {
	dir := testutil.TempDir(t, "openai-wif-same-engine-detector-*")
	file := filepath.Join(dir, "workflow.md")
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
    identity-provider-id: idp
    service-account-id: service-account
    audience: https://api.openai.com/v1
network:
  allowed: [defaults]
safe-outputs:
  add-comment:
  threat-detection:
    engine:
      id: codex
---
Inspect the repository.
`), 0o644))
	require.NoError(t, NewCompiler().CompileWorkflow(file))

	content, err := os.ReadFile(filepath.Join(dir, "workflow.lock.yml"))
	require.NoError(t, err)
	detectionSection := extractJobSection(string(content), "detection")
	assert.Contains(t, detectionSection, "id-token: write")
	assert.Contains(t, detectionSection, "AWF_AUTH_TYPE: github-oidc")
	assert.Contains(t, detectionSection, "AWF_AUTH_PROVIDER: openai")
	assert.Contains(t, detectionSection, "GH_AW_CODEX_OIDC_AUTH: openai")
	assert.NotContains(t, detectionSection, `env_key = "CODEX_API_KEY"`)
}

func TestCodexOpenAIWIFDetectionValidationReportsDetectorEnginePath(t *testing.T) {
	dir := testutil.TempDir(t, "openai-wif-invalid-detector-engine-*")
	file := filepath.Join(dir, "workflow.md")
	require.NoError(t, os.WriteFile(file, []byte(`---
on: workflow_dispatch
engine:
  id: codex
network:
  allowed: [defaults]
safe-outputs:
  threat-detection:
    engine:
      id: claude
      auth:
        type: github-oidc
        provider: openai
        identity-provider-id: idp
        service-account-id: service-account
        audience: https://api.openai.com/v1
---
Inspect the repository.
`), 0o644))

	err := NewCompiler().CompileWorkflow(file)
	require.ErrorContains(t, err, "safe-outputs.threat-detection.engine.id: auth.provider=openai requires codex")
}

func TestCodexOpenAIWIFFallbackKeepsKeylessProvider(t *testing.T) {
	data := &WorkflowData{
		Model: "gpt-5.4",
		EngineConfig: &EngineConfig{
			ID: "codex", FallbackModels: []string{"openai/gpt-5.3-codex"},
			Auth: &EngineAuthConfig{Type: "github-oidc", Provider: "openai", OpenAIIdentityProviderID: "idp", ServiceAccountID: "service-account", Audience: "https://api.openai.com/v1"},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	assert.Empty(t, fallbackProviderSecretNames(data))
	env := map[string]string{}
	applyFallbackProviderEnv(env, data)
	assert.NotContains(t, env, "CODEX_API_KEY")
	assert.NotContains(t, env, "OPENAI_API_KEY")
	env = NewCodexEngine().buildCodexExecutionEnv(data, true, true, "GH_AW_MODEL_AGENT_CODEX")
	assert.Equal(t, "openai", env["GH_AW_CODEX_OIDC_AUTH"])
	assert.NotContains(t, env, "CODEX_API_KEY")
	assert.NotContains(t, env, "OPENAI_API_KEY")
	data.EngineConfig.FallbackModels = append(data.EngineConfig.FallbackModels, "anthropic/claude-sonnet-4-6")
	assert.Equal(t, []string{"ANTHROPIC_API_KEY"}, fallbackProviderSecretNames(data))
	applyFallbackProviderEnv(env, data)
	assert.Contains(t, env, "ANTHROPIC_API_KEY")
	assert.NotContains(t, env, "OPENAI_API_KEY")
}

func TestCodexOIDCReflectionGuardEscapesProvider(t *testing.T) {
	guard := codexOIDCReflectionGuard("openai\"; process.exit(0); //")
	assert.Contains(t, guard, `github-oidc/openai\"; process.exit(0); //`)
}

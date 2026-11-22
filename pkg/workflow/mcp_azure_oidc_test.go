//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestMCPAzureOIDCCompilation(t *testing.T) {
	for _, engine := range []string{"copilot", "codex", "pi"} {
		t.Run(engine, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "azure-mcp.md")
			require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf(`---
on: workflow_dispatch
permissions:
  contents: read
  id-token: write
engine: %s
tools:
  github: false
mcp-servers:
  azure:
    type: http
    url: https://example.ai.gateway.azure.com/default/toolservers/search/mcp
    allowed: [search]
    auth:
      type: github-oidc
      provider: azure
      audience: "${{ vars.AZURE_OIDC_AUDIENCE || 'api://AzureADTokenExchange' }}"
      azure-tenant-id: "${{ vars.AZURE_TENANT_ID }}"
      azure-client-id: "${{ vars.AZURE_CLIENT_ID }}"
      azure-scope: "${{ vars.AZURE_AI_GATEWAY_SCOPE }}"
      azure-cloud: public
network:
  allowed:
    - defaults
    - example.ai.gateway.azure.com
    - login.microsoftonline.com
---
Search the configured gateway.
`, engine)), 0600))
			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(file))
			compiled, err := os.ReadFile(strings.TrimSuffix(file, ".md") + ".lock.yml")
			require.NoError(t, err)
			var workflow map[string]any
			require.NoError(t, yaml.Unmarshal(compiled, &workflow))
			jobs := workflow["jobs"].(map[string]any)
			steps := jobs["agent"].(map[string]any)["steps"].([]any)
			var gateway map[string]any
			for _, step := range steps {
				if candidate := step.(map[string]any); candidate["id"] == "start-mcp-gateway" {
					gateway = candidate
				}
			}
			require.NotNil(t, gateway)
			run := gateway["run"].(string)
			require.NotContains(t, run, "${{", "auth expressions must be data, never shell source")
			require.Contains(t, run, " -e ACTIONS_ID_TOKEN_REQUEST_URL")
			require.Contains(t, run, " -e ACTIONS_ID_TOKEN_REQUEST_TOKEN")
			require.Contains(t, string(compiled), "--exclude-env ACTIONS_ID_TOKEN_REQUEST_TOKEN")
			require.Contains(t, string(compiled), "--exclude-env ACTIONS_ID_TOKEN_REQUEST_URL")
			bindings := gateway["env"].(map[string]any)
			for _, expression := range []string{
				"${{ vars.AZURE_OIDC_AUDIENCE || 'api://AzureADTokenExchange' }}",
				"${{ vars.AZURE_TENANT_ID }}",
				"${{ vars.AZURE_CLIENT_ID }}",
				"${{ vars.AZURE_AI_GATEWAY_SCOPE }}",
			} {
				placeholder, env := replaceMCPExpressions(expression, false)
				require.Contains(t, run, placeholder)
				for name, value := range env {
					require.Equal(t, value, bindings[name])
					require.Contains(t, run, " -e "+name)
				}
			}
		})
	}
}

func TestMCPAzureOIDCAuthRendering(t *testing.T) {
	auth := map[string]any{
		"type": "github-oidc", "provider": "azure",
		"azure-tenant-id": "tenant", "azure-client-id": "client",
		"azure-scope": "https://ai.gateway.azure.com/.default",
	}
	tool := map[string]any{"type": "http", "url": "https://example.com/mcp", "auth": auth}
	typed, err := ParseToolsConfig(map[string]any{"azure": tool})
	require.NoError(t, err)
	roundTrip := mcpServerConfigToMap(typed.Custom["azure"])
	config, err := getMCPConfig(roundTrip, "azure")
	require.NoError(t, err)
	require.Equal(t, "tenant", config.Auth.AzureTenantID)

	var output strings.Builder
	require.NoError(t, renderSharedMCPConfig(&output, "azure", roundTrip, MCPConfigRenderer{Format: "json"}))
	command := exec.Command("bash", "-c", "cat <<EOF\n{\n"+output.String()+"}\nEOF")
	rendered, err := command.Output()
	require.NoError(t, err)
	var server map[string]any
	require.NoError(t, json.Unmarshal(rendered, &server))
	require.Equal(t, map[string]any{
		"type": "github-oidc", "provider": "azure",
		"azureTenantId": "tenant", "azureClientId": "client",
		"azureScope": "https://ai.gateway.azure.com/.default",
	}, server["auth"])
}

func TestMCPAzureOIDCExpressionRoundTrip(t *testing.T) {
	tool := map[string]any{
		"type": "http", "url": "https://example.com/mcp",
		"auth": map[string]any{
			"type": "github-oidc", "provider": "azure",
			"azure-tenant-id": "${{ vars.TENANT }}", "azure-client-id": "client",
			"azure-scope": "https://example.com/\"$(printf unsafe)`printf unsafe`",
		},
	}
	config, err := getMCPConfig(tool, "azure")
	require.NoError(t, err)
	var output strings.Builder
	require.NoError(t, renderSharedMCPConfig(&output, "azure", tool, MCPConfigRenderer{Format: "json"}))
	command := exec.Command("bash", "-c", "cat <<EOF\n{\n"+output.String()+"}\nEOF")
	rendered, err := command.Output()
	require.NoError(t, err)
	command = exec.Command("node", "-e", `
const fs = require("fs");
const {resolveMCPConfigExpressions} = require("../../actions/setup/js/start_mcp_gateway.cjs");
process.stdout.write(JSON.stringify(resolveMCPConfigExpressions(JSON.parse(fs.readFileSync(0, "utf8")))));
`)
	command.Stdin = strings.NewReader(string(rendered))
	command.Env = os.Environ()
	tenant := "tenant\"\\\n$(printf unsafe)`printf unsafe`"
	for name := range collectMCPConfigExpressionBindings(config) {
		command.Env = append(command.Env, name+"="+tenant)
	}
	resolved, err := command.Output()
	require.NoError(t, err)
	var server map[string]any
	require.NoError(t, json.Unmarshal(resolved, &server))
	auth := server["auth"].(map[string]any)
	require.Equal(t, tenant, auth["azureTenantId"])
	require.Equal(t, tool["auth"].(map[string]any)["azure-scope"], auth["azureScope"])
}

func TestMCPAzureOIDCUnsupportedCompilation(t *testing.T) {
	for _, sandbox := range []string{"sandbox:\n  mcp:\n    version: v0.5.11\n", "sandbox:\n  mcp:\n    container: ghcr.io/example/mcpg\n"} {
		file := filepath.Join(t.TempDir(), "azure-mcp.md")
		require.NoError(t, os.WriteFile(file, []byte(`---
on: workflow_dispatch
strict: false
engine: copilot
permissions:
  contents: read
  id-token: write
`+sandbox+`mcp-servers:
  azure:
    type: http
    url: https://example.com/mcp
    auth:
      type: github-oidc
      provider: azure
      azure-tenant-id: tenant
      azure-client-id: client
---
Use the gateway.
`), 0600))
		require.ErrorContains(t, NewCompiler().CompileWorkflow(file), "mcp-servers.azure.auth.provider: azure requires")
	}
}

func TestMCPAzureOIDCGatewayGate(t *testing.T) {
	for _, test := range []struct {
		name    string
		gateway *MCPGatewayRuntimeConfig
		wantErr bool
	}{
		{name: "default gateway"},
		{name: "stock release", gateway: &MCPGatewayRuntimeConfig{Container: constants.DefaultMCPGatewayContainer, Version: string(constants.MCPGAzureOIDCMinVersion)}},
		{name: "newer stock release", gateway: &MCPGatewayRuntimeConfig{Version: "v999.0.0"}},
		{name: "older stock release", gateway: &MCPGatewayRuntimeConfig{Version: "v0.5.11"}, wantErr: true},
		{name: "unverified stock tag", gateway: &MCPGatewayRuntimeConfig{Version: "latest"}, wantErr: true},
		{name: "custom without version", gateway: &MCPGatewayRuntimeConfig{Container: "ghcr.io/example/mcpg"}, wantErr: true},
		{name: "custom mutable tag", gateway: &MCPGatewayRuntimeConfig{Container: "ghcr.io/example/mcpg", Version: "latest"}, wantErr: true},
		{name: "custom reviewed build", gateway: &MCPGatewayRuntimeConfig{Container: "ghcr.io/example/mcpg", Version: "v0.5.12"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{
				SandboxConfig: &SandboxConfig{MCP: test.gateway},
				Tools: map[string]any{"azure": map[string]any{
					"type": "http", "url": "https://example.com/mcp",
					"auth": map[string]any{
						"type": "github-oidc", "provider": "azure",
						"azure-tenant-id": "tenant", "azure-client-id": "client",
					},
				}},
			}
			err := validateHTTPMCPAzureOIDCGateway(data)
			if test.wantErr {
				require.ErrorContains(t, err, "mcp-servers.azure.auth.provider: azure requires")
			} else {
				require.NoError(t, err)
			}
			require.ErrorContains(t, validateOIDCPermissions(data, nil), "permissions.id-token: write")
			data.NetworkPermissions = &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: "v0.25.2"}}
			require.ErrorContains(t, validateHTTPMCPOIDCAwfVersion(data), "requires AWF")
			data.Tools["azure"].(map[string]any)["auth"] = map[string]any{"type": "github-oidc"}
			require.NoError(t, validateHTTPMCPAzureOIDCGateway(data), "direct GitHub OIDC must remain supported")
		})
	}
}

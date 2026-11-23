//go:build !integration

package parser

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMCPAzureAuthConfig(t *testing.T) {
	raw := map[string]any{
		"type":            "github-oidc",
		"provider":        "azure",
		"azure-tenant-id": "${{ vars.AZURE_TENANT_ID }}",
		"azure-client-id": "${{ vars.AZURE_CLIENT_ID }}",
		"azure-scope":     "https://ai.gateway.azure.com/.default",
		"azure-cloud":     "public",
	}
	auth, err := ParseMCPAuthConfig(raw, "gateway")
	require.NoError(t, err)
	require.Equal(t, "azure", auth.Provider)
	require.Equal(t, raw["azure-tenant-id"], auth.AzureTenantID)
	require.Equal(t, raw["azure-client-id"], auth.AzureClientID)
	require.Equal(t, raw["azure-scope"], auth.AzureScope)
	require.Empty(t, auth.Audience, "Azure assertion audience is defaulted by the gateway, not to the MCP URL")

	for _, test := range []struct {
		name   string
		key    string
		value  any
		remove string
		want   string
	}{
		{name: "missing tenant", remove: "azure-tenant-id", want: "azure-tenant-id is required"},
		{name: "missing client", remove: "azure-client-id", want: "azure-client-id is required"},
		{name: "blank client", key: "azure-client-id", value: " ", want: "must be a non-empty string"},
		{name: "nonstring tenant", key: "azure-tenant-id", value: 42, want: "must be a non-empty string"},
		{name: "blank scope", key: "azure-scope", value: " ", want: "must be a non-empty string"},
		{name: "unknown provider", key: "provider", value: "aws", want: "provider must be 'azure'"},
		{name: "missing provider", remove: "provider", want: "requires auth.provider: azure"},
		{name: "unknown cloud", key: "azure-cloud", value: "moon", want: "azure-cloud must be"},
		{name: "misspelled key", key: "azureTenantId", value: "tenant", want: "azureTenantId is not supported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := maps.Clone(raw)
			delete(config, test.remove)
			if test.key != "" {
				config[test.key] = test.value
			}
			_, err := ParseMCPAuthConfig(config, "gateway")
			require.ErrorContains(t, err, test.want)
			frontmatter := map[string]any{
				"on": "workflow_dispatch",
				"mcp-servers": map[string]any{"gateway": map[string]any{
					"type": "http", "url": "https://example.com/mcp", "auth": config,
				}},
			}
			require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "azure.md"))
		})
	}
}

package parser

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/types"
)

// ParseMCPAuthConfig parses workflow-facing HTTP MCP authentication fields.
func ParseMCPAuthConfig(value any, toolName string) (*types.MCPAuthConfig, error) {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp-servers.%s.auth must be an object", toolName)
	}
	auth := &types.MCPAuthConfig{}
	fields := map[string]*string{
		"type":            &auth.Type,
		"audience":        &auth.Audience,
		"provider":        &auth.Provider,
		"azure-tenant-id": &auth.AzureTenantID,
		"azure-client-id": &auth.AzureClientID,
		"azure-scope":     &auth.AzureScope,
		"azure-cloud":     &auth.AzureCloud,
	}
	for _, key := range sliceutil.SortedKeys(raw) {
		target, known := fields[key]
		if !known {
			return nil, fmt.Errorf("mcp-servers.%s.auth.%s is not supported", toolName, key)
		}
		text, ok := raw[key].(string)
		if !ok || strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("mcp-servers.%s.auth.%s must be a non-empty string", toolName, key)
		}
		*target = text
	}
	if auth.Type != "github-oidc" {
		return nil, fmt.Errorf("mcp-servers.%s.auth.type must be 'github-oidc'", toolName)
	}
	if auth.Provider != "" && auth.Provider != "azure" {
		return nil, fmt.Errorf("mcp-servers.%s.auth.provider must be 'azure' or omitted for direct GitHub OIDC", toolName)
	}
	for _, key := range []string{"azure-tenant-id", "azure-client-id", "azure-scope", "azure-cloud"} {
		if _, exists := raw[key]; exists && auth.Provider != "azure" {
			return nil, fmt.Errorf("mcp-servers.%s.auth.%s requires auth.provider: azure", toolName, key)
		}
	}
	if auth.Provider == "azure" {
		for _, key := range []string{"azure-tenant-id", "azure-client-id"} {
			if strings.TrimSpace(*fields[key]) == "" {
				return nil, fmt.Errorf("mcp-servers.%s.auth.%s is required when auth.provider is azure", toolName, key)
			}
		}
	}
	switch auth.AzureCloud {
	case "", "public", "usgovernment", "china":
	default:
		return nil, fmt.Errorf("mcp-servers.%s.auth.azure-cloud must be public, usgovernment, or china", toolName)
	}
	return auth, nil
}

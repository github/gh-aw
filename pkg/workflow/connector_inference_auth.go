package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type ConnectorAzureOIDC struct {
	TenantID string `json:"azure-tenant-id"`
	ClientID string `json:"azure-client-id"`
	Scope    string `json:"azure-scope"`
	Audience string `json:"audience,omitempty"`
	Cloud    string `json:"azure-cloud,omitempty"`
}

var (
	connectorAzureID    = regexp.MustCompile(`(?i)^(?:[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}|\$\{\{\s*vars\.[a-z_][a-z0-9_]*\s*\}\})$`)
	connectorAzureScope = regexp.MustCompile(`^(?:(?:https|api)://[A-Za-z0-9.-]+(?:/[A-Za-z0-9._~-]+)*|[0-9a-fA-F-]{36})/\.default$`)
)

func (azure *ConnectorAzureOIDC) engineAuth() *EngineAuthConfig {
	return &EngineAuthConfig{
		Type: "github-oidc", Provider: "azure",
		AzureTenantID: azure.TenantID, AzureClientID: azure.ClientID, AzureScope: azure.Scope,
		Audience: azure.Audience, AzureCloud: azure.Cloud,
	}
}

// loginHost returns the Entra authority host for the configured Azure cloud.
func (azure *ConnectorAzureOIDC) loginHost() string {
	switch azure.Cloud {
	case "usgovernment":
		return "login.microsoftonline.us"
	case "china":
		return "login.chinacloudapi.cn"
	default:
		return "login.microsoftonline.com"
	}
}

func validateConnectorAzureOIDC(alias string, registration ConnectorRegistration) error {
	for name, azure := range registration.AzureOIDC {
		if registration.Spec == nil || registration.Connection != "" {
			return errors.New("azure-oidc requires an inline connector declaration")
		}
		credential, declared := registration.Spec.Credentials[name]
		if azure == nil || !declared || (credential.Phase != "inference" && credential.Phase != "agent" && credential.Phase != "safe-output") ||
			credential.Header != "Authorization" || credential.Scheme != "bearer" ||
			registration.GitHubApps[name] != nil || registration.OIDCTokens[name] != nil ||
			registration.Credentials[name].Secret != connectorAppSecret(alias, name) {
			return errors.New("azure-oidc requires its own agent, safe-output or inference phase Authorization/bearer credential")
		}
		for field, value := range map[string]string{
			"azure-tenant-id": azure.TenantID, "azure-client-id": azure.ClientID, "azure-scope": azure.Scope,
		} {
			if strings.TrimSpace(value) == "" || len(value) > 2048 || strings.ContainsAny(value, "\r\n\t") {
				return fmt.Errorf("azure-oidc.%s requires a nonempty bounded value", field)
			}
		}
		if azure.Cloud != "" && azure.Cloud != "public" && azure.Cloud != "usgovernment" && azure.Cloud != "china" {
			return errors.New("azure-oidc.azure-cloud must be public, usgovernment or china")
		}
		if len(azure.Audience) > 2048 || strings.ContainsAny(azure.Audience, "\r\n\t") {
			return errors.New("azure-oidc.audience must be a bounded audience without control characters")
		}
		if credential.Phase != "inference" {
			if !connectorAzureID.MatchString(azure.TenantID) || !connectorAzureID.MatchString(azure.ClientID) {
				return errors.New("azure-oidc agent and safe-output tenant/client IDs must be UUIDs or standalone vars references")
			}
			if !connectorAzureScope.MatchString(azure.Scope) {
				return errors.New("azure-oidc agent and safe-output scope must be a single literal resource /.default scope")
			}
			if strings.ContainsAny(azure.Audience, " ${}\x7f") || strings.ContainsFunc(azure.Audience, func(r rune) bool { return r < 32 }) {
				return errors.New("azure-oidc audience must be a bounded literal URI")
			}
		}
	}
	return nil
}

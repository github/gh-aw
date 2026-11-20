package workflow

import (
	"errors"
	"fmt"
	"strings"
)

type ConnectorAzureOIDC struct {
	TenantID string `json:"azure-tenant-id"`
	ClientID string `json:"azure-client-id"`
	Scope    string `json:"azure-scope"`
	Audience string `json:"audience,omitempty"`
	Cloud    string `json:"azure-cloud,omitempty"`
}

func (azure *ConnectorAzureOIDC) engineAuth() *EngineAuthConfig {
	return &EngineAuthConfig{
		Type: "github-oidc", Provider: "azure",
		AzureTenantID: azure.TenantID, AzureClientID: azure.ClientID, AzureScope: azure.Scope,
		Audience: azure.Audience, AzureCloud: azure.Cloud,
	}
}

func validateConnectorAzureOIDC(alias string, registration ConnectorRegistration) error {
	for name, azure := range registration.AzureOIDC {
		if registration.Spec == nil {
			return errors.New("azure-oidc requires an inline connector declaration")
		}
		credential, declared := registration.Spec.Credentials[name]
		if azure == nil || !declared || credential.Phase != "inference" ||
			credential.Header != "Authorization" || credential.Scheme != "bearer" ||
			registration.GitHubApps[name] != nil || registration.OIDCTokens[name] != nil ||
			registration.Credentials[name].Secret != connectorAppSecret(alias, name) {
			return errors.New("azure-oidc requires its own inference-phase Authorization/bearer credential")
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
	}
	return nil
}

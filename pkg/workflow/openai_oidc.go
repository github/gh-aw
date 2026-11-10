package workflow

func isOpenAIWIFWithAWF(workflowData *WorkflowData) bool {
	if workflowData == nil {
		return false
	}
	auth := resolveOpenAIWIFAuth(workflowData.EngineConfig)
	return auth != nil && auth.OpenAIIdentityProviderID != "" && auth.ServiceAccountID != "" &&
		auth.Audience != "" && isFirewallEnabled(workflowData)
}

func resolveOpenAIWIFAuth(config *EngineConfig) *EngineAuthConfig {
	if config == nil {
		return nil
	}
	if config.Auth != nil {
		if config.Auth.Type == "github-oidc" && config.Auth.Provider == "openai" {
			return config.Auth
		}
		return nil
	}
	if config.Env["AWF_AUTH_TYPE"] != "github-oidc" || config.Env["AWF_AUTH_PROVIDER"] != "openai" {
		return nil
	}
	return &EngineAuthConfig{
		Type:                     "github-oidc",
		Provider:                 "openai",
		OpenAIIdentityProviderID: config.Env["AWF_AUTH_OPENAI_IDENTITY_PROVIDER_ID"],
		ServiceAccountID:         config.Env["AWF_AUTH_OPENAI_SERVICE_ACCOUNT_ID"],
		Audience:                 config.Env["AWF_AUTH_OIDC_AUDIENCE"],
	}
}

package workflow

func isOpenAIWIFWithAWF(workflowData *WorkflowData) bool {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.Auth == nil {
		return false
	}
	auth := workflowData.EngineConfig.Auth
	return auth.Type == "github-oidc" && auth.Provider == "openai" &&
		auth.OpenAIIdentityProviderID != "" && auth.ServiceAccountID != "" &&
		auth.Audience != "" && isFirewallEnabled(workflowData)
}

package workflow

import (
	"net"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var genAIProviderLog = logger.New("workflow:genai_provider")

// OTel GenAI semantic-convention gen_ai.provider.name values used for BYOK inference.
const (
	genAIProviderAzureOpenAI = "azure.ai.openai"
	genAIProviderOpenAI      = "openai"
	genAIProviderAnthropic   = "anthropic"
	genAIProviderGCP         = "gcp.gen_ai"
)

// azureInferenceHostSuffixes identify Azure-hosted OpenAI-compatible endpoints,
// including Azure OpenAI, AI Foundry, APIM and Azure AI Gateway hosts.
var azureInferenceHostSuffixes = []string{".azure.com", ".azure-api.net", ".azure.us", ".azure.cn"}

// resolveBYOKGenAIProviderName returns the OTel gen_ai.provider.name for Copilot
// BYOK inference. It returns "" when the run is not Copilot BYOK, so telemetry
// keeps the engine-derived provider for GitHub-hosted inference.
func resolveBYOKGenAIProviderName(workflowData *WorkflowData) string {
	if ResolveEngineID(workflowData) != "copilot" || !isCopilotBYOKMode(workflowData, isFirewallEnabled(workflowData)) {
		return ""
	}
	authProvider := resolveEngineAuthProvider(workflowData)
	if !engineEnvHasNonEmptyValue(workflowData, constants.CopilotProviderBaseURL) {
		// Provider override routed through the AWF gateway (engine.provider).
		provider := genAIProviderForName(string(resolveEngineLLMProvider(workflowData, LLMProviderGitHub)))
		if authProvider == "azure" {
			provider = genAIProviderAzureOpenAI
		}
		genAIProviderLog.Printf("Resolved BYOK GenAI provider from engine provider override: %s (auth=%q)", provider, authProvider)
		return provider
	}
	providerType := literalLower(getEngineEnvOverrides(workflowData)["COPILOT_PROVIDER_TYPE"])
	// The auth provider only selects how gh-aw authenticates to the endpoint
	// (e.g. GCP OIDC to an OpenAI-compatible gateway), so only Azure auth changes
	// the endpoint type; every other auth provider keeps the openai default.
	provider := genAIProviderOpenAI
	switch {
	case providerType == "anthropic":
		provider = genAIProviderAnthropic
	case providerType == "azure" || authProvider == "azure" || isAzureInferenceHost(extractLiteralEngineEnvHost(workflowData, constants.CopilotProviderBaseURL)):
		provider = genAIProviderAzureOpenAI
	}
	genAIProviderLog.Printf("Resolved BYOK GenAI provider: %s (type=%q, auth=%q)", provider, providerType, authProvider)
	return provider
}

// resolveEngineAuthProvider mirrors applyEngineAuthEnv precedence: an
// AWF_AUTH_PROVIDER entry in engine.env wins over engine.auth.provider.
// Expression values cannot be inspected and resolve to "".
func resolveEngineAuthProvider(workflowData *WorkflowData) string {
	if value, ok := getEngineEnvOverrides(workflowData)["AWF_AUTH_PROVIDER"]; ok {
		return literalLower(value)
	}
	if workflowData.EngineConfig != nil && workflowData.EngineConfig.Auth != nil {
		return literalLower(workflowData.EngineConfig.Auth.Provider)
	}
	return ""
}

func genAIProviderForName(name string) string {
	switch name {
	case "azure":
		return genAIProviderAzureOpenAI
	case "anthropic":
		return genAIProviderAnthropic
	case "gcp", "gemini", "google":
		return genAIProviderGCP
	default:
		return genAIProviderOpenAI
	}
}

func literalLower(value string) string {
	if strings.Contains(value, "${{") {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func isAzureInferenceHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(host)
	for _, suffix := range azureInferenceHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const piBackendGoogle UniversalLLMBackend = "google"

var piNativeProviderKeys = map[string][]string{
	"google":                 {"GEMINI_API_KEY"},
	"azure-openai-responses": {"AZURE_OPENAI_API_KEY"},
	"azure":                  {"AZURE_OPENAI_API_KEY"},
	"openrouter":             {"OPENROUTER_API_KEY"},
	"deepseek":               {"DEEPSEEK_API_KEY"},
	"mistral":                {"MISTRAL_API_KEY"},
	"groq":                   {"GROQ_API_KEY"},
	"cerebras":               {"CEREBRAS_API_KEY"},
	"xai":                    {"XAI_API_KEY"},
	"nvidia":                 {"NVIDIA_API_KEY"},
	"huggingface":            {"HF_TOKEN"},
	"fireworks":              {"FIREWORKS_API_KEY"},
	"together":               {"TOGETHER_API_KEY"},
	"baseten":                {"BASETEN_API_KEY"},
	"vercel-ai-gateway":      {"AI_GATEWAY_API_KEY"},
	"opencode":               {"OPENCODE_API_KEY"},
	"radius":                 {"RADIUS_API_KEY"},
	"typesafe":               {"TYPESAFE_API_KEY"},
	"kimi-coding":            {"KIMI_API_KEY"},
	"minimax":                {"MINIMAX_API_KEY"},
	"google-vertex":          {},
	"amazon-bedrock":         {},
}

func piConfiguredProvider(data *WorkflowData) string {
	if data != nil {
		if provider, _, ok := strings.Cut(data.Model, "/"); ok {
			switch strings.ToLower(provider) {
			case "copilot", "github":
				return "github-copilot"
			case "codex":
				return "openai"
			case "gemini":
				return "google"
			default:
				return strings.ToLower(provider)
			}
		}
	}
	return "github-copilot"
}

func piExecutionProvider(data *WorkflowData) string {
	provider := piConfiguredProvider(data)
	if data == nil || data.EngineConfig == nil || data.EngineConfig.LLMProvider == "" {
		return provider
	}
	switch provider {
	case "github-copilot", "anthropic", "openai", "google":
		switch resolvePiBackend(data) {
		case UniversalLLMBackendAnthropic:
			return "anthropic"
		case UniversalLLMBackendCodex:
			return "openai"
		case piBackendGoogle:
			return "google"
		default:
			return "github-copilot"
		}
	default:
		return provider
	}
}

func piProviderProfile(data *WorkflowData) universalLLMBackendProfile {
	profile := piStaticProviderProfile(data)
	if data != nil && data.EngineConfig != nil && data.EngineConfig.Auth != nil &&
		data.EngineConfig.Auth.Type == "github-oidc" && isFirewallEnabled(data) {
		for _, name := range profile.coreSecretNames {
			delete(profile.env, name)
		}
		profile.coreSecretNames = nil
	}
	return profile
}

func piStaticProviderProfile(data *WorkflowData) universalLLMBackendProfile {
	provider := piConfiguredProvider(data)
	backend := resolvePiBackend(data)
	if backend == piBackendGoogle {
		return universalLLMBackendProfile{
			coreSecretNames: []string{"GEMINI_API_KEY"},
			env:             map[string]string{"GEMINI_API_KEY": "${{ secrets.GEMINI_API_KEY }}"},
			baseURLEnvName:  "GEMINI_API_BASE_URL",
			gatewayPort:     constants.GeminiLLMGatewayPort,
		}
	}
	if !isFirewallEnabled(data) && data != nil && data.EngineConfig != nil && data.EngineConfig.LLMProvider == "" &&
		provider != "github-copilot" && provider != "openai" && provider != "anthropic" {
		keys := piNativeProviderKeys[provider]
		env := make(map[string]string, len(keys))
		for _, key := range keys {
			env[key] = "${{ secrets." + key + " }}"
		}
		return universalLLMBackendProfile{coreSecretNames: keys, env: env}
	}
	return getUniversalLLMBackendProfile(backend, hasCopilotRequestsWritePermission(data))
}

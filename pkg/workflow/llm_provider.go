package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var llmProviderLog = logger.New("workflow:llm_provider")

// LLMProvider identifies the inference provider used by an engine (e.g. "github", "anthropic", "openai").
type LLMProvider string

// String returns the string representation of the provider, satisfying the fmt.Stringer interface.
func (p LLMProvider) String() string {
	return string(p)
}

const (
	LLMProviderGitHub    LLMProvider = "github"
	LLMProviderAnthropic LLMProvider = "anthropic"
	LLMProviderOpenAI    LLMProvider = "openai"
	LLMProviderGoogle    LLMProvider = "google"
)

var llmProviderAliases = map[string]LLMProvider{
	"copilot":        LLMProviderGitHub,
	"github":         LLMProviderGitHub,
	"github-copilot": LLMProviderGitHub,
	"github_models":  LLMProviderGitHub,
	"anthropic":      LLMProviderAnthropic,
	"openai":         LLMProviderOpenAI,
	"google":         LLMProviderGoogle,
	"gemini":         LLMProviderGoogle,
}

type llmProviderProfile struct {
	id          LLMProvider
	gatewayPort int
}

func normalizeLLMProvider(provider string) LLMProvider {
	normalized := strings.TrimSpace(provider)
	if normalized == "" {
		return LLMProviderAnthropic
	}
	normalized = strings.ToLower(normalized)
	if alias, ok := llmProviderAliases[normalized]; ok {
		return alias
	}
	return LLMProvider(normalized)
}

func resolveEngineLLMProvider(workflowData *WorkflowData, defaultProvider LLMProvider) LLMProvider {
	if workflowData == nil || workflowData.EngineConfig == nil || workflowData.EngineConfig.LLMProvider == "" {
		provider := normalizeLLMProvider(string(defaultProvider))
		llmProviderLog.Printf("Resolved LLM provider from default: %s", provider)
		return provider
	}
	provider := normalizeLLMProvider(string(workflowData.EngineConfig.LLMProvider))
	llmProviderLog.Printf("Resolved LLM provider from engine config: %s", provider)
	return provider
}

func resolveEngineLLMProviderFromModel(workflowData *WorkflowData, defaultProvider LLMProvider) LLMProvider {
	if workflowData != nil && (workflowData.EngineConfig == nil || workflowData.EngineConfig.LLMProvider == "") &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(workflowData.Model)), "copilot/") {
		return LLMProviderGitHub
	}
	return resolveEngineLLMProvider(workflowData, defaultProvider)
}

func llmProviderProfileFor(provider LLMProvider) llmProviderProfile {
	switch provider {
	case LLMProviderGitHub:
		return llmProviderProfile{
			id:          LLMProviderGitHub,
			gatewayPort: constants.CopilotLLMGatewayPort,
		}
	case LLMProviderOpenAI:
		return llmProviderProfile{
			id:          LLMProviderOpenAI,
			gatewayPort: constants.CodexLLMGatewayPort,
		}
	case LLMProviderGoogle:
		return llmProviderProfile{id: provider, gatewayPort: constants.GeminiLLMGatewayPort}
	default:
		return llmProviderProfile{
			id:          LLMProviderAnthropic,
			gatewayPort: constants.ClaudeLLMGatewayPort,
		}
	}
}

func llmProviderSecretNames(provider LLMProvider) []string {
	switch provider {
	case LLMProviderGitHub:
		return []string{"COPILOT_GITHUB_TOKEN"}
	case LLMProviderOpenAI:
		return []string{"CODEX_API_KEY", "OPENAI_API_KEY"}
	case LLMProviderGoogle:
		return []string{"GEMINI_API_KEY"}
	default:
		return []string{"ANTHROPIC_API_KEY"}
	}
}

func llmProviderSecretExpression(provider LLMProvider, workflowData *WorkflowData) string {
	switch provider {
	case LLMProviderGitHub:
		if hasCopilotRequestsWritePermission(workflowData) {
			llmProviderLog.Print("Using github.token for GitHub Copilot (copilot-requests write permission present)")
			return "${{ github.token }}"
		}
		llmProviderLog.Print("Using COPILOT_GITHUB_TOKEN secret for GitHub Copilot")
		return "${{ secrets.COPILOT_GITHUB_TOKEN }}"
	case LLMProviderOpenAI:
		return "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}"
	case LLMProviderGoogle:
		return "${{ secrets.GEMINI_API_KEY }}"
	default:
		return "${{ secrets.ANTHROPIC_API_KEY }}"
	}
}

func llmProviderGatewayBaseURL(provider LLMProvider) string {
	profile := llmProviderProfileFor(provider)
	return fmt.Sprintf("http://host.docker.internal:%d", profile.gatewayPort)
}

func llmProviderDocsURL(provider LLMProvider) string {
	switch provider {
	case LLMProviderGitHub:
		return "https://github.github.com/gh-aw/reference/engines/#github-copilot-default"
	case LLMProviderOpenAI:
		return "https://github.github.com/gh-aw/reference/engines/#openai-codex"
	case LLMProviderGoogle:
		return "https://geminicli.com/docs/get-started/authentication/"
	default:
		return "https://github.github.com/gh-aw/reference/engines/#anthropic-claude-code"
	}
}

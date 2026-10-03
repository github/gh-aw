package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) warnCodexCopilotModelCompatibility(data *WorkflowData, markdownPath string) {
	if data == nil || data.EngineConfig == nil ||
		data.EngineConfig.ID != string(constants.CodexEngine) {
		return
	}

	model := strings.TrimSpace(data.Model)
	if model == "" || strings.Contains(model, "${{") {
		return
	}
	model = strings.ToLower(model)
	baseModel, _, _ := strings.Cut(model, "?")
	usesGitHubInference := strings.HasPrefix(baseModel, "copilot/") ||
		NewCodexEngine().ResolveLLMProvider(data) == LLMProviderGitHub
	if !usesGitHubInference || isCodexCompatibleModel(baseModel) {
		return
	}

	message := fmt.Sprintf(
		"Codex with model %q may fail because Codex relies on capabilities that general-purpose Copilot models do not provide. Select a supported Codex model such as copilot/gpt-6.1-sol",
		data.Model,
	)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		formatCompilerMessage(markdownPath, "warning", message)))
	c.IncrementWarningCount()
}

func isCodexCompatibleModel(model string) bool {
	if strings.Contains(model, "codex") {
		return true
	}
	switch codexModelID(model) {
	case "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-6-astra",
		"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna":
		return true
	default:
		return false
	}
}

func (c *Compiler) warnUnknownConfiguredModels(data *WorkflowData, markdownPath string) {
	if c.configuredModelValidator == nil {
		return
	}
	for _, warning := range c.configuredModelValidator(data) {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
			formatCompilerMessage(markdownPath, "warning", warning)))
		c.IncrementWarningCount()
	}
}

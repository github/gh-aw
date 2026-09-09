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
	baseModel := strings.SplitN(model, "?", 2)[0]
	usesGitHubInference := strings.HasPrefix(baseModel, "copilot/") ||
		NewCodexEngine().ResolveLLMProvider(data) == LLMProviderGitHub
	if !usesGitHubInference || strings.Contains(baseModel, "codex") {
		return
	}

	message := fmt.Sprintf(
		"Codex with model %q may fail because Codex relies on capabilities that general-purpose Copilot models do not provide. Select a Codex model such as copilot/gpt-5.3-codex",
		data.Model,
	)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		formatCompilerMessage(markdownPath, "warning", message)))
	c.IncrementWarningCount()
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

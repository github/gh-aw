package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

func (c *Compiler) warnCopilotIntegrationModelAvailability(data *WorkflowData, markdownPath string) {
	if data == nil || data.EngineConfig == nil || data.EngineConfig.ID != "copilot" ||
		NewCopilotEngine().ResolveLLMProvider(data) != LLMProviderGitHub ||
		data.EngineConfig.Env[constants.CopilotProviderBaseURL] != "" {
		return
	}
	model := strings.ToLower(strings.TrimSpace(data.Model))
	if strings.Contains(model, "${{") {
		return
	}
	model, _, _ = strings.Cut(model, "?")
	model = strings.TrimPrefix(strings.TrimPrefix(model, "github-copilot/"), "copilot/")
	if !strings.HasPrefix(model, "gemini-") {
		return
	}
	message := fmt.Sprintf("Copilot model %q may not be available to the agentic-workflows integration, even when enabled by organization policy and available in the standalone Copilot CLI. The bundled model catalog does not guarantee integration access. Select a model in the workflow's live catalog; enabling missing Gemini models for agentic-workflows requires Copilot API support", data.Model)
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		formatCompilerMessage(markdownPath, "warning", message)))
	c.IncrementWarningCount()
}

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
	if !usesGitHubInference || codexModelID(baseModel) == "auto" || isCodexCompatibleModel(baseModel) {
		return
	}
	// An alias is resolved at runtime by the Codex harness; its result cannot be judged here.
	if configuredModelAliasKey(data.Model, data.ModelMappings) != "" {
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

// configuredModelAliasKey returns the gh-aw alias key used by a configured model (after
// removing an optional provider/ prefix and ?parameters), or "" when the model is an
// exact id or a GitHub Actions expression.
func configuredModelAliasKey(model string, aliases map[string][]string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.Contains(model, "${{") {
		return ""
	}
	base, _, _ := strings.Cut(model, "?")
	if _, after, found := strings.Cut(base, "/"); found {
		base = after
	}
	if _, ok := aliases[base]; ok && base != "" {
		return base
	}
	return ""
}

// warnEngineCannotResolveModelAlias warns when an engine that does not run the shared
// "resolve configured model" step (Gemini and declarative engines) is given a gh-aw
// alias: the alias would reach the engine's CLI unchanged.
func (c *Compiler) warnEngineCannotResolveModelAlias(data *WorkflowData, markdownPath string) {
	if data == nil || data.EngineConfig == nil {
		return
	}
	engineID := ResolveEngineID(data)
	if engineID == "" || engineRunsSharedModelResolution(engineID) {
		return
	}
	alias := configuredModelAliasKey(data.Model, data.ModelMappings)
	if alias == "" {
		return
	}
	message := fmt.Sprintf("engine %q does not resolve gh-aw model aliases yet: model %q uses alias %q, which is passed to the engine unchanged. Use an exact model id with this engine", engineID, data.Model, alias)
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

// warnClaudeCopilotSettingsOverride warns when a Claude-on-Copilot workflow passes its
// own --settings in engine.args, because gh-aw then keeps that value and does not add
// the Copilot model-id map (modelOverrides) for Claude Code.
func (c *Compiler) warnClaudeCopilotSettingsOverride(data *WorkflowData, markdownPath string) {
	if data == nil {
		return
	}
	if ResolveEngineID(data) == string(constants.ClaudeEngine) {
		c.warnClaudeCopilotSettingsArgs("engine.args", data, markdownPath)
	}
}

// warnClaudeCopilotDetectionSettingsOverride is the safe-outputs.threat-detection.engine
// counterpart of warnClaudeCopilotSettingsOverride. It runs once safe-outputs are parsed
// and resolves the detection model as the detection job does, so an inherited copilot/
// model selects Copilot inference for the detection engine too.
func (c *Compiler) warnClaudeCopilotDetectionSettingsOverride(data *WorkflowData, markdownPath string) {
	if data == nil || !IsDetectionJobEnabled(data.SafeOutputs) {
		return
	}
	td := data.SafeOutputs.ThreatDetection.EngineConfig
	if td == nil || td.ID != string(constants.ClaudeEngine) {
		return
	}
	detectionData := &WorkflowData{EngineConfig: td, Model: resolveThreatDetectionModel(data, td.ID, NewClaudeEngine())}
	c.warnClaudeCopilotSettingsArgs("safe-outputs.threat-detection.engine.args", detectionData, markdownPath)
}

func (c *Compiler) warnClaudeCopilotSettingsArgs(field string, data *WorkflowData, markdownPath string) {
	if data.EngineConfig == nil || !claudeArgsHaveSettings(data.EngineConfig.Args) || !claudeUsesCopilotInference(data) {
		return
	}
	message := field + " passes --settings, so the Copilot model-id map (modelOverrides) for Claude Code was not added. " +
		"Add a modelOverrides entry that maps each Anthropic model id to its Copilot id (for example \"claude-haiku-4-5\": \"claude-haiku-4.5\") to your --settings value"
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
		formatCompilerMessage(markdownPath, "warning", message)))
	c.IncrementWarningCount()
}

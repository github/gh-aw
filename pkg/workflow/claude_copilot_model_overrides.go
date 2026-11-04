package workflow

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var claudeCopilotModelOverridesLog = logger.New("workflow:claude_copilot_model_overrides")

// claudeSettingsEnvVar carries Claude Code --settings JSON to claude_harness.cjs.
// The external threat-detect binary builds its own Claude Code arguments and
// launches Claude Code through the harness, so the harness appends --settings
// from this variable when the arguments do not already contain one.
const claudeSettingsEnvVar = "GH_AW_CLAUDE_SETTINGS"

// claudeCopilotModelOverrides maps each Anthropic model id to the Copilot model id for
// every Copilot Claude id whose version uses a dot (claude-haiku-4.5) instead of
// Anthropic's hyphen (claude-haiku-4-5), using the same bundled catalog as the harness.
// Ids that already match get no entry.
func claudeCopilotModelOverrides() map[string]string {
	overrides := make(map[string]string)
	for id := range bundledProviderModelIDs()[copilotRoutingNamespace] {
		if !strings.HasPrefix(id, "claude-") || !strings.Contains(id, ".") {
			continue
		}
		overrides[strings.ReplaceAll(id, ".", "-")] = id
	}
	claudeCopilotModelOverridesLog.Printf("Built %d Claude model override(s) from the bundled Copilot catalog", len(overrides))
	return overrides
}

// claudeCopilotModelOverridesSettings returns the Claude Code --settings JSON with the
// modelOverrides map. Map keys are marshalled in sorted order, so output is stable.
var claudeCopilotModelOverridesSettings = sync.OnceValue(func() string {
	settings := struct {
		ModelOverrides map[string]string `json:"modelOverrides"`
	}{ModelOverrides: claudeCopilotModelOverrides()}
	encoded, err := json.Marshal(settings)
	if err != nil {
		panic("BUG: failed to marshal Claude modelOverrides settings: " + err.Error())
	}
	return string(encoded)
})

// claudeArgsHaveSettings reports whether args pass --settings in either the
// "--settings <value>" or "--settings=<value>" form.
func claudeArgsHaveSettings(args []string) bool {
	for _, arg := range args {
		if arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			return true
		}
	}
	return false
}

// claudeArgsSettingsValue returns the last --settings value in args, in either the
// "--settings <value>" or "--settings=<value>" form.
func claudeArgsSettingsValue(args []string) (string, bool) {
	value, found := "", false
	for i, arg := range args {
		next := i + 1
		if arg == "--settings" && next >= 0 && next < len(args) {
			value, found = args[next], true
		} else if after, ok := strings.CutPrefix(arg, "--settings="); ok {
			value, found = after, true
		}
	}
	return value, found
}

// claudeDetectionSettings returns the --settings value for claude_harness.cjs in a
// Claude-on-Copilot detection run: the modelOverrides map, or the workflow's own
// --settings from engine.args on the external detector path, where threat-detect
// builds its own Claude Code arguments and does not receive engine.args.
func claudeDetectionSettings(workflowData *WorkflowData) string {
	if shouldApplyClaudeCopilotModelOverrides(workflowData) {
		claudeCopilotModelOverridesLog.Print("Applying Claude Copilot modelOverrides settings for detection run")
		return claudeCopilotModelOverridesSettings()
	}
	if !claudeUsesCopilotInference(workflowData) || workflowData.EngineConfig == nil ||
		!isFeatureEnabled(constants.GHAWDetectionFeatureFlag, workflowData) {
		return ""
	}
	settings, found := claudeArgsSettingsValue(workflowData.EngineConfig.Args)
	claudeCopilotModelOverridesLog.Printf("Using workflow-provided --settings for detection run: found=%t", found)
	return settings
}

// claudeUsesCopilotInference reports whether Claude Code runs on GitHub Copilot inference.
func claudeUsesCopilotInference(workflowData *WorkflowData) bool {
	return NewClaudeEngine().ResolveLLMProvider(workflowData) == LLMProviderGitHub
}

// shouldApplyClaudeCopilotModelOverrides reports whether gh-aw passes Claude Code the
// Copilot modelOverrides map. A workflow-provided --settings in engine.args wins.
func shouldApplyClaudeCopilotModelOverrides(workflowData *WorkflowData) bool {
	if !claudeUsesCopilotInference(workflowData) {
		return false
	}
	apply := workflowData.EngineConfig == nil || !claudeArgsHaveSettings(workflowData.EngineConfig.Args)
	claudeCopilotModelOverridesLog.Printf("shouldApplyClaudeCopilotModelOverrides: copilot_inference=true apply=%t", apply)
	return apply
}

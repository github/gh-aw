package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var claudeSettingsLog = logger.New("workflow:claude_settings")

// claudeSettingsEnvVar carries Claude Code --settings JSON to claude_harness.cjs.
// The external threat-detect binary builds its own Claude Code arguments and
// launches Claude Code through the harness, so the harness appends --settings
// from this variable when the arguments do not already contain one.
const claudeSettingsEnvVar = "GH_AW_CLAUDE_SETTINGS"

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
// Claude-on-Copilot detection run: the workflow's own --settings from engine.args
// on the external detector path, where threat-detect
// builds its own Claude Code arguments and does not receive engine.args.
func claudeDetectionSettings(workflowData *WorkflowData) string {
	if !claudeUsesCopilotInference(workflowData) || workflowData.EngineConfig == nil ||
		!isFeatureEnabled(constants.GHAWDetectionFeatureFlag, workflowData) {
		return ""
	}
	settings, found := claudeArgsSettingsValue(workflowData.EngineConfig.Args)
	claudeSettingsLog.Printf("Using workflow-provided --settings for detection run: found=%t", found)
	return settings
}

// claudeUsesCopilotInference reports whether Claude Code runs on GitHub Copilot inference.
func claudeUsesCopilotInference(workflowData *WorkflowData) bool {
	return NewClaudeEngine().ResolveLLMProvider(workflowData) == LLMProviderGitHub
}

package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var consolidatedSafeOutputsEnvvarsLog = logger.New("workflow:compiler_safe_outputs_envvars")

// buildJobLevelSafeOutputEnvVars builds environment variables that should be set at the job level
// for the consolidated safe_outputs job. These are variables that are common to all safe output steps.
func (c *Compiler) buildJobLevelSafeOutputEnvVars(data *WorkflowData, workflowID string) map[string]string {
	envVars := make(map[string]string)

	// Set GH_AW_WORKFLOW_ID to the workflow ID (filename without extension)
	// This is used for branch naming in create_pull_request and other operations
	envVars["GH_AW_WORKFLOW_ID"] = fmt.Sprintf("%q", workflowID)

	// Set GH_AW_CALLER_WORKFLOW_ID to uniquely identify the calling workflow at runtime.
	// When a reusable workflow is called via workflow_call, multiple callers share the
	// same GH_AW_WORKFLOW_ID (derived from the reusable file). This separate value
	// combines the runtime repository (to identify the caller repo) with the compile-time
	// workflow ID (filename without extension), producing a stable "owner/repo/workflow-id"
	// form used for close-older-issues disambiguation.
	envVars["GH_AW_CALLER_WORKFLOW_ID"] = fmt.Sprintf(`"${{ github.repository }}/%s"`, workflowID)

	// Add workflow metadata that's common to all steps
	envVars["GH_AW_WORKFLOW_NAME"] = fmt.Sprintf("%q", data.Name)

	if data.FrontmatterEmoji != "" {
		envVars["GH_AW_WORKFLOW_EMOJI"] = fmt.Sprintf("%q", data.FrontmatterEmoji)
	}

	if data.Source != "" {
		envVars["GH_AW_WORKFLOW_SOURCE"] = fmt.Sprintf("%q", data.Source)
		sourceURL := buildSourceURL(data.Source)
		if sourceURL != "" {
			envVars["GH_AW_WORKFLOW_SOURCE_URL"] = fmt.Sprintf("%q", sourceURL)
		}
	} else if localURL := buildLocalWorkflowSourceURL(c.markdownPath); localURL != "" {
		// For local workflows (no external source), point to the markdown file in the repo
		// so that failure issue links resolve to the workflow source rather than "#".
		envVars["GH_AW_WORKFLOW_SOURCE_URL"] = fmt.Sprintf("%q", localURL)
	}

	if data.TrackerID != "" {
		envVars["GH_AW_TRACKER_ID"] = fmt.Sprintf("%q", data.TrackerID)
	}

	// Bake the repository project UTC offset (from aw.json) into safe-outputs job env
	// so runtime JavaScript helpers do not need to read aw.json on the runner.
	if utcOffset := c.getCompiledProjectUTCOffset(); utcOffset != "" {
		envVars["GH_AW_PROJECT_UTC"] = fmt.Sprintf("%q", utcOffset)
	}

	// Add engine metadata that's common to all steps
	if data.EngineConfig != nil {
		if data.EngineConfig.ID != "" {
			envVars["GH_AW_ENGINE_ID"] = fmt.Sprintf("%q", data.EngineConfig.ID)
		}
		if data.EngineConfig.Version != "" {
			envVars["GH_AW_ENGINE_VERSION"] = fmt.Sprintf("%q", data.EngineConfig.Version)
		}
		// Prefer explicit compile-time model; fall back to the runtime model captured by the
		// activation job so footers always show the actual model used for auditability.
		if data.Model != "" {
			envVars["GH_AW_ENGINE_MODEL"] = fmt.Sprintf("%q", data.Model)
		} else {
			envVars["GH_AW_ENGINE_MODEL"] = fmt.Sprintf("${{ needs.%s.outputs.model }}", constants.AgentJobName)
		}
	}

	envVars["GH_AW_AIC"] = fmt.Sprintf("${{ needs.%s.outputs.aic }}", constants.AgentJobName)
	envVars["GH_AW_AMBIENT_CONTEXT"] = fmt.Sprintf("${{ needs.%s.outputs.ambient_context }}", constants.AgentJobName)
	envVars["GH_AW_AGENT_AIC"] = fmt.Sprintf("${{ needs.%s.outputs.aic }}", constants.AgentJobName)

	// Add slash command metadata so safe output handlers can render run-again footer hints.
	if len(data.Command) > 0 {
		if commandsJSON, err := json.Marshal(data.Command); err == nil {
			envVars["GH_AW_COMMANDS"] = fmt.Sprintf("%q", string(commandsJSON))
		}
		if data.CommandPlaceholder != "" {
			envVars["GH_AW_COMMAND_PLACEHOLDER"] = fmt.Sprintf("%q", data.CommandPlaceholder)
		}
	}
	// Add label command metadata so safe output handlers can render run-again footer hints.
	if len(data.LabelCommand) > 0 {
		if labelCommandsJSON, err := json.Marshal(data.LabelCommand); err == nil {
			envVars["GH_AW_LABEL_COMMANDS"] = fmt.Sprintf("%q", string(labelCommandsJSON))
		}
	}

	// Add safe output job environment variables (staged/target repo)
	if data.SafeOutputs != nil {
		if value := resolveSafeOutputsStagedValue(c.trialMode, data.SafeOutputs.Staged); value != nil {
			if isExpression(*value) {
				envVars["GH_AW_SAFE_OUTPUTS_STAGED"] = *value
			} else {
				envVars["GH_AW_SAFE_OUTPUTS_STAGED"] = "\"true\""
			}
		}
	}

	// Set GH_AW_TARGET_REPO_SLUG - prefer trial target repo (applies to all steps)
	// Note: Individual steps with target-repo config will override this in their step-level env
	if c.trialMode && c.trialLogicalRepoSlug != "" {
		envVars["GH_AW_TARGET_REPO_SLUG"] = fmt.Sprintf("%q", c.trialLogicalRepoSlug)
	}

	// Add messages config if present (applies to all steps)
	if data.SafeOutputs != nil && data.SafeOutputs.Messages != nil {
		messagesJSON, err := serializeMessagesConfig(data.SafeOutputs.Messages)
		if err != nil {
			consolidatedSafeOutputsEnvvarsLog.Printf("Warning: failed to serialize messages config: %v", err)
		} else if messagesJSON != "" {
			envVars["GH_AW_SAFE_OUTPUT_MESSAGES"] = fmt.Sprintf("%q", messagesJSON)
		}
	}

	// Note: GH_AW_CI_TRIGGER_TOKEN is added at the step level (in buildHandlerManagerStep)
	// rather than job level, since only the Process Safe Outputs step needs it,
	// and only when create-pull-request or push-to-pull-request-branch is configured.

	// Note: Asset upload configuration is not needed here because upload_assets
	// is now handled as a separate job (see buildUploadAssetsJob)

	// Pass detection conclusion and reason to safe outputs when threat detection is enabled.
	// This allows handlers (e.g., push-to-pull-request-branch) to adjust behavior on warnings.
	if IsDetectionJobEnabled(data.SafeOutputs) {
		envVars["GH_AW_DETECTION_CONCLUSION"] = fmt.Sprintf("${{ needs.%s.outputs.detection_conclusion }}", constants.DetectionJobName)
		envVars["GH_AW_DETECTION_REASON"] = fmt.Sprintf("${{ needs.%s.outputs.detection_reason }}", constants.DetectionJobName)
		envVars["GH_AW_THREAT_DETECTION_AIC"] = fmt.Sprintf("${{ needs.%s.outputs.aic }}", constants.DetectionJobName)
	}

	// Automatically inject the PR head SHA captured at trigger time so that PR-review
	// handlers (submit_pull_request_review, create_pull_request_review_comment) pin their
	// reviews to the reviewed commit without requiring any user YAML configuration.
	// For workflow_run triggers this prevents attribution drift when a new commit lands
	// on the PR while the agent is running (the safe_outputs job runs after the agent job
	// and pulls.get() would otherwise return the new HEAD sha).
	if headSHAExpr := headSHAExpressionForTrigger(data.RawFrontmatter["on"]); headSHAExpr != "" {
		envVars["GH_AW_HEAD_SHA"] = headSHAExpr
	}

	return envVars
}

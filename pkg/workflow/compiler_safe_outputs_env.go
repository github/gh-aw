package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var compilerSafeOutputsEnvLog = logger.New("workflow:compiler_safe_outputs_env")

func (c *Compiler) addAllSafeOutputConfigEnvVars(steps *[]string, data *WorkflowData) {
	compilerSafeOutputsEnvLog.Print("Adding safe output config environment variables")
	if data.SafeOutputs == nil {
		compilerSafeOutputsEnvLog.Print("No safe outputs configured, skipping env var addition")
		return
	}

	// Track if we've already added staged flag to avoid duplicates
	stagedFlagAdded := false

	// Create Issue env vars - target-repo, allowed_labels and allowed_repos now in config object
	if data.SafeOutputs.CreateIssues != nil {
		cfg := data.SafeOutputs.CreateIssues
		compilerSafeOutputsEnvLog.Print("Processing create-issue env vars")
		// Add staged flag if needed (but not if target-repo is specified or we're in trial mode)
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded && cfg.TargetRepoSlug == "" {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
			compilerSafeOutputsEnvLog.Print("Added staged flag for create-issue")
		}
		// Check if copilot is in assignees - if so, we'll output issues for assign_to_agent job
		if hasCopilotAssignee(cfg.Assignees) {
			*steps = append(*steps, "          GH_AW_ASSIGN_COPILOT: \"true\"\n")
			compilerSafeOutputsEnvLog.Print("Copilot assignment requested - will output issues_to_assign_copilot")
		}
	}

	// Add Comment - all config now in handler config JSON
	if data.SafeOutputs.AddComments != nil {
		cfg := data.SafeOutputs.AddComments
		// Add staged flag if needed (but not if target-repo is specified or we're in trial mode)
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded && cfg.TargetRepoSlug == "" {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
		}
		// All add_comment configuration (target, target-repo, hide_older_comments, max) is now in handler config JSON
	}

	// Add Labels - all config now in handler config JSON
	if data.SafeOutputs.AddLabels != nil {
		cfg := data.SafeOutputs.AddLabels
		// Add staged flag if needed (but not if target-repo is specified or we're in trial mode)
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded && cfg.TargetRepoSlug == "" {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
		}
		// All add_labels configuration (allowed, max, target) is now in handler config JSON
	}

	// Remove Labels - all config now in handler config JSON
	if data.SafeOutputs.RemoveLabels != nil {
		cfg := data.SafeOutputs.RemoveLabels
		// Add staged flag if needed (but not if target-repo is specified or we're in trial mode)
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded && cfg.TargetRepoSlug == "" {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
		}
		// All remove_labels configuration (allowed, max, target) is now in handler config JSON
	}

	// Update Issue env vars
	if data.SafeOutputs.UpdateIssues != nil {
		cfg := data.SafeOutputs.UpdateIssues
		// Add staged flag if needed (but not if target-repo is specified or we're in trial mode)
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded && cfg.TargetRepoSlug == "" {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
		}
	}

	// Update Discussion env vars
	if data.SafeOutputs.UpdateDiscussions != nil {
		cfg := data.SafeOutputs.UpdateDiscussions
		// Add staged flag if needed (but not if target-repo is specified or we're in trial mode)
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded && cfg.TargetRepoSlug == "" {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
		}
		// All update configuration (target, allow_title, allow_body, allow_labels) is now in handler config JSON
	}

	// Create Pull Request env vars
	if data.SafeOutputs.CreatePullRequests != nil {
		// Add staged flag if needed
		if !c.trialMode && data.SafeOutputs.Staged && !stagedFlagAdded {
			*steps = append(*steps, "          GH_AW_SAFE_OUTPUTS_STAGED: \"true\"\n")
			stagedFlagAdded = true
		}
		// Note: base_branch and max_patch_size are now in handler config JSON
	}

	if stagedFlagAdded {
		_ = stagedFlagAdded // Mark as used for linter
	}

	// Note: Most handlers read from the config.json file, so we may not need all env vars here
}

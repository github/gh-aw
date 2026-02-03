package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsJobsLog = logger.New("workflow:safe_outputs_jobs")

// ========================================
// Safe Output Job Configuration and Builder
// ========================================

// SafeOutputJobConfig holds configuration for building a safe output job
// This config struct extracts the common parameters across all safe output job builders
type SafeOutputJobConfig struct {
	// Job metadata
	JobName     string // e.g., "create_issue"
	StepName    string // e.g., "Create Output Issue"
	StepID      string // e.g., "create_issue"
	MainJobName string // Main workflow job name for dependencies

	// Custom environment variables specific to this safe output type
	CustomEnvVars []string

	// JavaScript script constant to include in the GitHub Script step
	Script string

	// Script name for looking up custom action path (optional)
	// If provided and action mode is custom, the compiler will use a custom action
	// instead of inline JavaScript. Example: "create_issue"
	ScriptName string

	// Job configuration
	Permissions     *Permissions      // Job permissions
	Outputs         map[string]string // Job outputs
	Condition       ConditionNode     // Job condition (if clause)
	Needs           []string          // Job dependencies
	PreSteps        []string          // Optional steps to run before the GitHub Script step
	PostSteps       []string          // Optional steps to run after the GitHub Script step
	Token           string            // GitHub token for this output type
	UseCopilotToken bool              // Whether to use Copilot token preference chain
	UseAgentToken   bool              // Whether to use agent token preference chain (config token > GH_AW_AGENT_TOKEN)
	TargetRepoSlug  string            // Target repository for cross-repo operations
}

// buildSafeOutputJob creates a safe output job with common scaffolding
// This extracts the repeated pattern found across safe output job builders:
// 1. Validate configuration
// 2. Build custom environment variables
// 3. Invoke buildGitHubScriptStep
// 4. Create Job with standard metadata
func (c *Compiler) buildSafeOutputJob(data *WorkflowData, config SafeOutputJobConfig) (*Job, error) {
	safeOutputsJobsLog.Printf("Building safe output job: %s (actionMode=%s)", config.JobName, c.actionMode)
	var steps []string

	// Add GitHub App token minting step if app is configured
	if data.SafeOutputs != nil && data.SafeOutputs.App != nil {
		safeOutputsJobsLog.Print("Adding GitHub App token minting step with auto-computed permissions")
		steps = append(steps, c.buildGitHubAppTokenMintStep(data.SafeOutputs.App, config.Permissions)...)
	}

	// Add pre-steps if provided (e.g., checkout, git config for create-pull-request)
	if len(config.PreSteps) > 0 {
		safeOutputsJobsLog.Printf("Adding %d pre-steps to job", len(config.PreSteps))
		steps = append(steps, config.PreSteps...)
	}

	// Build the step based on action mode
	var scriptSteps []string
	if c.actionMode.UsesExternalActions() && config.ScriptName != "" {
		// Use custom action mode (dev or release) if enabled and script name is provided
		safeOutputsJobsLog.Printf("Using custom action mode (%s) for script: %s", c.actionMode, config.ScriptName)
		scriptSteps = c.buildCustomActionStep(data, GitHubScriptStepConfig{
			StepName:        config.StepName,
			StepID:          config.StepID,
			MainJobName:     config.MainJobName,
			CustomEnvVars:   config.CustomEnvVars,
			Script:          config.Script,
			Token:           config.Token,
			UseCopilotToken: config.UseCopilotToken,
			UseAgentToken:   config.UseAgentToken,
		}, config.ScriptName)
	} else {
		// Use inline mode (default behavior)
		// If ScriptName is provided, convert it to ScriptFile (.cjs extension)
		scriptFile := ""
		if config.ScriptName != "" {
			scriptFile = config.ScriptName + ".cjs"
			safeOutputsJobsLog.Printf("Using inline mode with external script: %s", scriptFile)
		} else {
			safeOutputsJobsLog.Printf("Using inline mode (actions/github-script)")
		}
		scriptSteps = c.buildGitHubScriptStep(data, GitHubScriptStepConfig{
			StepName:        config.StepName,
			StepID:          config.StepID,
			MainJobName:     config.MainJobName,
			CustomEnvVars:   config.CustomEnvVars,
			Script:          config.Script,
			ScriptFile:      scriptFile,
			Token:           config.Token,
			UseCopilotToken: config.UseCopilotToken,
			UseAgentToken:   config.UseAgentToken,
		})
	}
	steps = append(steps, scriptSteps...)

	// Add post-steps if provided (e.g., assignees, reviewers)
	if len(config.PostSteps) > 0 {
		steps = append(steps, config.PostSteps...)
	}

	// Add GitHub App token invalidation step if app is configured
	if data.SafeOutputs != nil && data.SafeOutputs.App != nil {
		safeOutputsJobsLog.Print("Adding GitHub App token invalidation step")
		steps = append(steps, c.buildGitHubAppTokenInvalidationStep()...)
	}

	// Determine job condition
	jobCondition := config.Condition
	if jobCondition == nil {
		safeOutputsJobsLog.Printf("No custom condition provided, using default for job: %s", config.JobName)
		jobCondition = BuildSafeOutputType(config.JobName)
	}

	// Determine job needs
	needs := config.Needs
	if len(needs) == 0 {
		needs = []string{config.MainJobName}
	}
	safeOutputsJobsLog.Printf("Job %s needs: %v", config.JobName, needs)

	// Create the job with standard configuration
	job := &Job{
		Name:           config.JobName,
		If:             jobCondition.Render(),
		RunsOn:         c.formatSafeOutputsRunsOn(data.SafeOutputs),
		Permissions:    config.Permissions.RenderToYAML(),
		TimeoutMinutes: 10, // 10-minute timeout as required for all safe output jobs
		Steps:          steps,
		Outputs:        config.Outputs,
		Needs:          needs,
	}

	return job, nil
}

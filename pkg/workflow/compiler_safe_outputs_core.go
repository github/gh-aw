package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

var consolidatedSafeOutputsLog = logger.New("workflow:compiler_safe_outputs_consolidated")

// hasProjectRelatedSafeOutputs checks if any project-related safe outputs are configured
// Project-related safe outputs require the @actions/github package for Octokit instantiation
func (c *Compiler) hasProjectRelatedSafeOutputs(safeOutputs *SafeOutputsConfig) bool {
	if safeOutputs == nil {
		return false
	}

	return safeOutputs.UpdateProjects != nil ||
		safeOutputs.CreateProjects != nil ||
		safeOutputs.CreateProjectStatusUpdates != nil
}

// SafeOutputStepConfig holds configuration for building a single safe output step
// within the consolidated safe-outputs job
type SafeOutputStepConfig struct {
	StepName        string            // Human-readable step name (e.g., "Create Issue")
	StepID          string            // Step ID for referencing outputs (e.g., "create_issue")
	Script          string            // JavaScript script to execute (for inline mode)
	ScriptName      string            // Name of the script in the registry (for file mode)
	CustomEnvVars   []string          // Environment variables specific to this step
	Condition       ConditionNode     // Step-level condition (if clause)
	Token           string            // GitHub token for this step
	UseCopilotToken bool              // Whether to use Copilot token preference chain
	UseAgentToken   bool              // Whether to use agent token preference chain
	PreSteps        []string          // Optional steps to run before the script step
	PostSteps       []string          // Optional steps to run after the script step
	Outputs         map[string]string // Outputs from this step
}

// Note: The implementation functions have been moved to focused module files:
// - buildConsolidatedSafeOutputsJob, buildJobLevelSafeOutputEnvVars, buildDetectionSuccessCondition
//   are in compiler_safe_outputs_job.go
// - buildConsolidatedSafeOutputStep, buildSharedPRCheckoutSteps, buildHandlerManagerStep
//   are in compiler_safe_outputs_steps.go
// - addHandlerManagerConfigEnvVar is in compiler_safe_outputs_config.go
// - addAllSafeOutputConfigEnvVars is in compiler_safe_outputs_env.go

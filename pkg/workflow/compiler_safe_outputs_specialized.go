package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var specializedOutputsLog = logger.New("workflow:compiler_safe_outputs_specialized")

// buildAssignToAgentStepConfig builds the configuration for assigning to an agent
func (c *Compiler) buildAssignToAgentStepConfig(data *WorkflowData, mainJobName string, threatDetectionEnabled bool) SafeOutputStepConfig {
	cfg := data.SafeOutputs.AssignToAgent
	specializedOutputsLog.Printf("Building assign-to-agent step config: max=%d, default_agent=%s", cfg.Max, cfg.DefaultAgent)

	var customEnvVars []string
	customEnvVars = append(customEnvVars, c.buildStepLevelSafeOutputEnvVars(data, "")...)

	// Add max count environment variable for JavaScript to validate against
	if cfg.Max > 0 {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_AGENT_MAX_COUNT: %d\n", cfg.Max))
	}

	// Add default agent environment variable
	if cfg.DefaultAgent != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_AGENT_DEFAULT: %q\n", cfg.DefaultAgent))
	}

	// Add target configuration environment variable
	if cfg.Target != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_AGENT_TARGET: %q\n", cfg.Target))
	}

	// Add allowed agents list environment variable (comma-separated)
	if len(cfg.Allowed) > 0 {
		allowedStr := ""
		for i, agent := range cfg.Allowed {
			if i > 0 {
				allowedStr += ","
			}
			allowedStr += agent
		}
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_AGENT_ALLOWED: %q\n", allowedStr))
	}

	// Add ignore-if-error flag if set
	if cfg.IgnoreIfError {
		customEnvVars = append(customEnvVars, "          GH_AW_AGENT_IGNORE_IF_ERROR: \"true\"\n")
	}

	// Allow assign_to_agent to reference issues created earlier in the same run via temporary IDs (aw_...)
	// The handler manager (process_safe_outputs) produces a temporary_id_map output when create_issue is enabled.
	if data.SafeOutputs != nil && data.SafeOutputs.CreateIssues != nil {
		customEnvVars = append(customEnvVars, "          GH_AW_TEMPORARY_ID_MAP: ${{ steps.process_safe_outputs.outputs.temporary_id_map }}\n")
	}

	condition := BuildSafeOutputType("assign_to_agent")

	return SafeOutputStepConfig{
		StepName:      "Assign To Agent",
		StepID:        "assign_to_agent",
		ScriptName:    "assign_to_agent",
		Script:        getAssignToAgentScript(),
		CustomEnvVars: customEnvVars,
		Condition:     condition,
		Token:         cfg.GitHubToken,
		UseAgentToken: true,
	}
}

// buildCreateAgentTaskStepConfig builds the configuration for creating an agent session
func (c *Compiler) buildCreateAgentSessionStepConfig(data *WorkflowData, mainJobName string, threatDetectionEnabled bool) SafeOutputStepConfig {
	cfg := data.SafeOutputs.CreateAgentSessions
	specializedOutputsLog.Print("Building create-agent-session step config")

	var customEnvVars []string
	customEnvVars = append(customEnvVars, c.buildStepLevelSafeOutputEnvVars(data, "")...)

	condition := BuildSafeOutputType("create_agent_session")

	return SafeOutputStepConfig{
		StepName:        "Create Agent Session",
		StepID:          "create_agent_session",
		Script:          "const { main } = require('/opt/gh-aw/actions/create_agent_session.cjs'); await main();",
		CustomEnvVars:   customEnvVars,
		Condition:       condition,
		Token:           cfg.GitHubToken,
		UseCopilotToken: true,
	}
}

// buildCreateProjectStepConfig builds the configuration for creating a project
func (c *Compiler) buildCreateProjectStepConfig(data *WorkflowData, mainJobName string, threatDetectionEnabled bool) SafeOutputStepConfig {
	cfg := data.SafeOutputs.CreateProjects
	specializedOutputsLog.Printf("Building create-project step config: target_owner=%s, title_prefix=%s", cfg.TargetOwner, cfg.TitlePrefix)

	var customEnvVars []string
	customEnvVars = append(customEnvVars, c.buildStepLevelSafeOutputEnvVars(data, "")...)

	// Add target-owner default if configured
	if cfg.TargetOwner != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATE_PROJECT_TARGET_OWNER: %q\n", cfg.TargetOwner))
	}

	// Add title-prefix default if configured
	if cfg.TitlePrefix != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_CREATE_PROJECT_TITLE_PREFIX: %q\n", cfg.TitlePrefix))
	}

	// Get the effective token using the Projects-specific precedence chain
	// This includes fallback to GH_AW_PROJECT_GITHUB_TOKEN if no custom token is configured
	// Note: Projects v2 requires a PAT or GitHub App - the default GITHUB_TOKEN cannot work
	effectiveToken := getEffectiveProjectGitHubToken(cfg.GitHubToken, data.GitHubToken)

	// Always expose the effective token as GH_AW_PROJECT_GITHUB_TOKEN environment variable
	// The JavaScript code checks process.env.GH_AW_PROJECT_GITHUB_TOKEN to provide helpful error messages
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_PROJECT_GITHUB_TOKEN: %s\n", effectiveToken))

	condition := BuildSafeOutputType("create_project")

	return SafeOutputStepConfig{
		StepName:      "Create Project",
		StepID:        "create_project",
		ScriptName:    "create_project",
		Script:        getCreateProjectScript(),
		CustomEnvVars: customEnvVars,
		Condition:     condition,
		Token:         effectiveToken,
	}
}

package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/logger"
)

var createAgentSessionLog = logger.New("workflow:create_agent_session")

// CreateAgentSessionConfig holds configuration for creating GitHub Copilot agent sessions from agent output
type CreateAgentSessionConfig struct {
	BaseSafeOutputConfig `yaml:",inline"`
	Base                 string   `yaml:"base,omitempty"`          // Base branch for the pull request
	TargetRepoSlug       string   `yaml:"target-repo,omitempty"`   // Target repository in format "owner/repo" for cross-repository agent sessions
	AllowedRepos         []string `yaml:"allowed-repos,omitempty"` // List of additional repositories that agent sessions can be created in (additionally to the target-repo)
}

// parseAgentSessionConfig handles create-agent-session configuration
func (c *Compiler) parseAgentSessionConfig(outputMap map[string]any) *CreateAgentSessionConfig {
	// Try new key first
	if configData, exists := outputMap["create-agent-session"]; exists {
		createAgentSessionLog.Print("Parsing create-agent-session configuration")
		agentSessionConfig := &CreateAgentSessionConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			// Parse base branch
			if base, exists := configMap["base"]; exists {
				if baseStr, ok := base.(string); ok {
					agentSessionConfig.Base = baseStr
				}
			}

			// Parse target-repo using shared helper with validation
			targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
			if isInvalid {
				return nil // Invalid configuration, return nil to cause validation error
			}
			agentSessionConfig.TargetRepoSlug = targetRepoSlug

			// Parse common base fields with default max of 1
			c.parseBaseSafeOutputConfig(configMap, &agentSessionConfig.BaseSafeOutputConfig, 1)
		} else {
			// If configData is nil or not a map (e.g., "create-agent-session:" with no value),
			// still set the default max
			agentSessionConfig.Max = 1
		}

		return agentSessionConfig
	}

	// Fall back to deprecated key for backward compatibility
	if configData, exists := outputMap["create-agent-task"]; exists {
		createAgentSessionLog.Print("WARNING: Using deprecated 'create-agent-task' configuration. Please migrate to 'create-agent-session' using 'gh aw fix'")
		agentSessionConfig := &CreateAgentSessionConfig{}

		if configMap, ok := configData.(map[string]any); ok {
			// Parse base branch
			if base, exists := configMap["base"]; exists {
				if baseStr, ok := base.(string); ok {
					agentSessionConfig.Base = baseStr
				}
			}

			// Parse target-repo using shared helper with validation
			targetRepoSlug, isInvalid := parseTargetRepoWithValidation(configMap)
			if isInvalid {
				return nil // Invalid configuration, return nil to cause validation error
			}
			agentSessionConfig.TargetRepoSlug = targetRepoSlug

			// Parse common base fields with default max of 1
			c.parseBaseSafeOutputConfig(configMap, &agentSessionConfig.BaseSafeOutputConfig, 1)
		} else {
			// If configData is nil or not a map (e.g., "create-agent-task:" with no value),
			// still set the default max
			agentSessionConfig.Max = 1
		}

		return agentSessionConfig
	}

	return nil
}

// buildCreateOutputAgentSessionJob creates the create_agent_session job
func (c *Compiler) buildCreateOutputAgentSessionJob(data *WorkflowData, mainJobName string) (*Job, error) {
	if data.SafeOutputs == nil || data.SafeOutputs.CreateAgentSessions == nil {
		return nil, fmt.Errorf("safe-outputs.create-agent-session configuration is required")
	}

	createAgentSessionLog.Printf("Building create-agent-session job: workflow=%s, main_job=%s, base=%s",
		data.Name, mainJobName, data.SafeOutputs.CreateAgentSessions.Base)

	var preSteps []string

	// Step 1: Checkout repository for gh CLI to work
	preSteps = append(preSteps, "      - name: Checkout repository for gh CLI\n")
	preSteps = append(preSteps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/checkout")))
	preSteps = append(preSteps, "        with:\n")
	preSteps = append(preSteps, "          persist-credentials: false\n")

	// Build custom environment variables specific to create-agent-session
	customEnvVars := []string{
		fmt.Sprintf("          GITHUB_AW_WORKFLOW_NAME: %q\n", data.Name),
	}

	// Pass the base branch configuration
	if data.SafeOutputs.CreateAgentSessions.Base != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GITHUB_AW_AGENT_SESSION_BASE: %q\n", data.SafeOutputs.CreateAgentSessions.Base))
	} else {
		// Default to the current branch or default branch
		customEnvVars = append(customEnvVars, "          GITHUB_AW_AGENT_SESSION_BASE: ${{ github.ref_name }}\n")
	}

	// Add standard environment variables (metadata + staged/target repo)
	customEnvVars = append(customEnvVars, c.buildStandardSafeOutputEnvVars(data, data.SafeOutputs.CreateAgentSessions.TargetRepoSlug)...)

	// Create outputs for the job
	outputs := map[string]string{
		"session_number": "${{ steps.create_agent_session.outputs.session_number }}",
		"session_url":    "${{ steps.create_agent_session.outputs.session_url }}",
	}

	jobCondition := BuildSafeOutputType("create_agent_session")

	// Use the shared builder function to create the job
	return c.buildSafeOutputJob(data, SafeOutputJobConfig{
		JobName:         "create_agent_session",
		StepName:        "Create Agent Session",
		StepID:          "create_agent_session",
		MainJobName:     mainJobName,
		CustomEnvVars:   customEnvVars,
		Script:          "const { main } = require('/opt/gh-aw/actions/create_agent_session.cjs'); await main();",
		Permissions:     NewPermissionsContentsWriteIssuesWritePRWrite(),
		Outputs:         outputs,
		Condition:       jobCondition,
		PreSteps:        preSteps,
		Token:           data.SafeOutputs.CreateAgentSessions.GitHubToken,
		UseCopilotToken: true, // Use Copilot token preference for agent session creation
		TargetRepoSlug:  data.SafeOutputs.CreateAgentSessions.TargetRepoSlug,
	})
}

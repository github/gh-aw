package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputsStepsLog = logger.New("workflow:safe_outputs_steps")

// ========================================
// Safe Output Step Builders
// ========================================

// buildCustomActionStep creates a step that uses a custom action reference
// instead of inline JavaScript via actions/github-script
func (c *Compiler) buildCustomActionStep(data *WorkflowData, config GitHubScriptStepConfig, scriptName string) []string {
	safeOutputsStepsLog.Printf("Building custom action step: %s (scriptName=%s, actionMode=%s)", config.StepName, scriptName, c.actionMode)

	var steps []string

	// Get the action path from the script registry
	actionPath := DefaultScriptRegistry.GetActionPath(scriptName)
	if actionPath == "" {
		safeOutputsStepsLog.Printf("WARNING: No action path found for script %s, falling back to inline mode", scriptName)
		// Set ScriptFile for inline mode fallback
		config.ScriptFile = scriptName + ".cjs"
		return c.buildGitHubScriptStep(data, config)
	}

	// Resolve the action reference based on mode
	actionRef := c.resolveActionReference(actionPath, data)
	if actionRef == "" {
		safeOutputsStepsLog.Printf("WARNING: Could not resolve action reference for %s, falling back to inline mode", actionPath)
		// Set ScriptFile for inline mode fallback
		config.ScriptFile = scriptName + ".cjs"
		return c.buildGitHubScriptStep(data, config)
	}

	// Add artifact download steps before the custom action step
	steps = append(steps, buildAgentOutputDownloadSteps()...)

	// Step name and metadata
	steps = append(steps, fmt.Sprintf("      - name: %s\n", config.StepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", actionRef))

	// Environment variables section
	steps = append(steps, "        env:\n")
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}\n")
	steps = append(steps, config.CustomEnvVars...)
	c.addCustomSafeOutputEnvVars(&steps, data)

	// With section for inputs (replaces github-token in actions/github-script)
	steps = append(steps, "        with:\n")

	// Map github-token to token input for custom actions
	c.addCustomActionGitHubToken(&steps, data, config.Token, config.UseAgentToken, config.UseCopilotToken)

	return steps
}

// addCustomActionGitHubToken adds a GitHub token as action input.
// The token precedence depends on the tokenType flags:
// - UseAgentToken: customToken > SafeOutputs.GitHubToken > data.GitHubToken > GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
// - UseCopilotToken: customToken > SafeOutputs.GitHubToken > data.GitHubToken > COPILOT_GITHUB_TOKEN || GH_AW_GITHUB_TOKEN
// - Default: customToken > SafeOutputs.GitHubToken > data.GitHubToken > GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
func (c *Compiler) addCustomActionGitHubToken(steps *[]string, data *WorkflowData, customToken string, useAgentToken, useCopilotToken bool) {
	var token string

	// Agent token mode: use full precedence chain for agent assignment
	if useAgentToken {
		var safeOutputsToken string
		if data.SafeOutputs != nil {
			safeOutputsToken = data.SafeOutputs.GitHubToken
		}
		// Precedence: customToken > safeOutputsToken > data.GitHubToken > default Agent fallback chain
		token = getEffectiveAgentGitHubToken(customToken, getEffectiveAgentGitHubToken(safeOutputsToken, data.GitHubToken))
	} else if useCopilotToken {
		// Copilot mode: use getEffectiveCopilotGitHubToken with safe-outputs token precedence
		var safeOutputsToken string
		if data.SafeOutputs != nil {
			safeOutputsToken = data.SafeOutputs.GitHubToken
		}
		// Precedence: customToken > safeOutputsToken > data.GitHubToken > default Copilot fallback
		token = getEffectiveCopilotGitHubToken(customToken, getEffectiveCopilotGitHubToken(safeOutputsToken, data.GitHubToken))
	} else {
		// Standard mode: use safe output token chain (data.GitHubToken, then GITHUB_TOKEN)
		var safeOutputsToken string
		if data.SafeOutputs != nil {
			safeOutputsToken = data.SafeOutputs.GitHubToken
		}
		token = getEffectiveSafeOutputGitHubToken(customToken, getEffectiveSafeOutputGitHubToken(safeOutputsToken, data.GitHubToken))
	}

	*steps = append(*steps, fmt.Sprintf("          token: %s\n", token))
}

// GitHubScriptStepConfig holds configuration for building a GitHub Script step
type GitHubScriptStepConfig struct {
	// Step metadata
	StepName string // e.g., "Create Output Issue"
	StepID   string // e.g., "create_issue"

	// Main job reference for agent output
	MainJobName string

	// Environment variables specific to this safe output type
	// These are added after GH_AW_AGENT_OUTPUT
	CustomEnvVars []string

	// JavaScript script constant to format and include (for inline mode)
	Script string

	// ScriptFile is the .cjs filename to require (e.g., "noop.cjs")
	// If empty, Script will be inlined instead
	ScriptFile string

	// Token configuration (passed to addSafeOutputGitHubTokenForConfig or addSafeOutputCopilotGitHubTokenForConfig)
	Token string

	// UseCopilotToken indicates whether to use the Copilot token preference chain
	// (COPILOT_GITHUB_TOKEN > GH_AW_GITHUB_TOKEN (legacy))
	// This should be true for Copilot-related operations like creating agent tasks,
	// assigning copilot to issues, or adding copilot as PR reviewer
	UseCopilotToken bool

	// UseAgentToken indicates whether to use the agent token preference chain
	// (config token > GH_AW_AGENT_TOKEN)
	// This should be true for agent assignment operations (assign-to-agent)
	UseAgentToken bool
}

// buildGitHubScriptStep creates a GitHub Script step with common scaffolding
// This extracts the repeated pattern found across safe output job builders
func (c *Compiler) buildGitHubScriptStep(data *WorkflowData, config GitHubScriptStepConfig) []string {
	safeOutputsStepsLog.Printf("Building GitHub Script step: %s (useCopilotToken=%v, useAgentToken=%v)", config.StepName, config.UseCopilotToken, config.UseAgentToken)

	var steps []string

	// Add artifact download steps before the GitHub Script step
	steps = append(steps, buildAgentOutputDownloadSteps()...)

	// Step name and metadata
	steps = append(steps, fmt.Sprintf("      - name: %s\n", config.StepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Environment variables section
	steps = append(steps, "        env:\n")

	// Read GH_AW_AGENT_OUTPUT from environment (set by artifact download step)
	// instead of directly from job outputs which may be masked by GitHub Actions
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}\n")

	// Add custom environment variables specific to this safe output type
	steps = append(steps, config.CustomEnvVars...)

	// Add custom environment variables from safe-outputs.env
	c.addCustomSafeOutputEnvVars(&steps, data)

	// With section for github-token
	steps = append(steps, "        with:\n")
	if config.UseAgentToken {
		c.addSafeOutputAgentGitHubTokenForConfig(&steps, data, config.Token)
	} else if config.UseCopilotToken {
		c.addSafeOutputCopilotGitHubTokenForConfig(&steps, data, config.Token)
	} else {
		c.addSafeOutputGitHubTokenForConfig(&steps, data, config.Token)
	}

	steps = append(steps, "          script: |\n")

	// Use require() if ScriptFile is specified, otherwise inline the script
	if config.ScriptFile != "" {
		steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
		steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
		steps = append(steps, fmt.Sprintf("            const { main } = require('"+SetupActionDestination+"/%s');\n", config.ScriptFile))
		steps = append(steps, "            await main();\n")
	} else {
		// Add the formatted JavaScript script (inline)
		formattedScript := FormatJavaScriptForYAML(config.Script)
		steps = append(steps, formattedScript...)
	}

	return steps
}

// buildGitHubScriptStepWithoutDownload creates a GitHub Script step without artifact download steps
// This is useful when multiple script steps are needed in the same job and artifact downloads
// should only happen once at the beginning
func (c *Compiler) buildGitHubScriptStepWithoutDownload(data *WorkflowData, config GitHubScriptStepConfig) []string {
	safeOutputsStepsLog.Printf("Building GitHub Script step without download: %s", config.StepName)

	var steps []string

	// Step name and metadata (no artifact download steps)
	steps = append(steps, fmt.Sprintf("      - name: %s\n", config.StepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

	// Environment variables section
	steps = append(steps, "        env:\n")

	// Read GH_AW_AGENT_OUTPUT from environment (set by artifact download step)
	// instead of directly from job outputs which may be masked by GitHub Actions
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}\n")

	// Add custom environment variables specific to this safe output type
	steps = append(steps, config.CustomEnvVars...)

	// Add custom environment variables from safe-outputs.env
	c.addCustomSafeOutputEnvVars(&steps, data)

	// With section for github-token
	steps = append(steps, "        with:\n")
	if config.UseAgentToken {
		c.addSafeOutputAgentGitHubTokenForConfig(&steps, data, config.Token)
	} else if config.UseCopilotToken {
		c.addSafeOutputCopilotGitHubTokenForConfig(&steps, data, config.Token)
	} else {
		c.addSafeOutputGitHubTokenForConfig(&steps, data, config.Token)
	}

	steps = append(steps, "          script: |\n")

	// Use require() if ScriptFile is specified, otherwise inline the script
	if config.ScriptFile != "" {
		steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
		steps = append(steps, "            setupGlobals(core, github, context, exec, io);\n")
		steps = append(steps, fmt.Sprintf("            const { main } = require('"+SetupActionDestination+"/%s');\n", config.ScriptFile))
		steps = append(steps, "            await main();\n")
	} else {
		// Add the formatted JavaScript script (inline)
		formattedScript := FormatJavaScriptForYAML(config.Script)
		steps = append(steps, formattedScript...)
	}

	return steps
}

// buildAgentOutputDownloadSteps creates steps to download the agent output artifact
// and set the GH_AW_AGENT_OUTPUT environment variable for safe-output jobs
func buildAgentOutputDownloadSteps() []string {
	return buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:     "agent-output",                // Use hyphenated name without extension
		ArtifactFilename: constants.AgentOutputFilename, // Filename inside the artifact directory
		DownloadPath:     "/tmp/gh-aw/safeoutputs/",
		SetupEnvStep:     true,
		EnvVarName:       "GH_AW_AGENT_OUTPUT",
		StepName:         "Download agent output artifact",
	})
}

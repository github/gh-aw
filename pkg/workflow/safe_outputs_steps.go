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

	// Add artifact download steps before the custom action step.
	// In workflow_call context, use the per-invocation prefix to avoid artifact name clashes.
	// These steps are used in jobs that depend on the agent job (not activation), so use
	// the agent-downstream prefix expression.
	steps = append(steps, buildAgentOutputDownloadSteps(artifactPrefixExprForAgentDownstreamJob(data), c.getActionPin)...)

	// Step name and metadata
	steps = append(steps, fmt.Sprintf("      - name: %s\n", config.StepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
	steps = append(steps, fmt.Sprintf("        uses: %s\n", actionRef))

	// Environment variables section
	steps = append(steps, "        env:\n")
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}\n")
	steps = append(steps, config.CustomEnvVars...)
	c.addCustomSafeOutputEnvVars(&steps, data)

	// With section for inputs (replaces github-token in actions/github-script)
	steps = append(steps, "        with:\n")

	// Map github-token to token input for custom actions
	c.addCustomActionGitHubToken(&steps, data, config)

	return steps
}

// addCustomActionGitHubToken adds a GitHub token as action input.
// The token precedence depends on the tokenType flags:
// - UseCopilotCodingAgentToken: customToken > SafeOutputs.GitHubToken > GH_AW_AGENT_TOKEN || GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
// - UseCopilotRequestsToken: customToken > SafeOutputs.GitHubToken > COPILOT_GITHUB_TOKEN
// - Default: customToken > SafeOutputs.GitHubToken > GH_AW_GITHUB_TOKEN || GITHUB_TOKEN
func (c *Compiler) addCustomActionGitHubToken(steps *[]string, data *WorkflowData, config GitHubScriptStepConfig) {
	safeOutputsStepsLog.Printf("Selecting GitHub token for step: %s (copilotCodingAgent=%t, copilotRequests=%t)", config.StepName, config.UseCopilotCodingAgentToken, config.UseCopilotRequestsToken)
	var token string

	// Get safe-outputs level token
	var safeOutputsToken string
	if data.SafeOutputs != nil {
		safeOutputsToken = data.SafeOutputs.GitHubToken
	}

	// Choose the first non-empty custom token for precedence
	resolvedCustomToken := config.CustomToken
	if resolvedCustomToken == "" {
		resolvedCustomToken = safeOutputsToken
	}

	// Agent token mode: use full precedence chain for agent assignment
	if config.UseCopilotCodingAgentToken {
		token = getEffectiveCopilotCodingAgentGitHubToken(resolvedCustomToken)
	} else if config.UseCopilotRequestsToken {
		// Copilot mode: use getEffectiveCopilotRequestsToken with safe-outputs token precedence
		token = getEffectiveCopilotRequestsToken(resolvedCustomToken)
	} else {
		// Standard mode: use safe output token chain
		token = resolveSafeOutputGitHubToken(resolvedCustomToken)
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

	// CustomToken configuration (passed to addSafeOutputGitHubTokenForConfig or addSafeOutputCopilotGitHubTokenForConfig)
	CustomToken string

	// UseCopilotRequestsToken indicates whether to use the Copilot token preference chain
	// custom token > COPILOT_GITHUB_TOKEN
	// This should be true for Copilot-related operations like creating agent tasks,
	// assigning copilot to issues, or adding copilot as PR reviewer
	UseCopilotRequestsToken bool

	// UseCopilotCodingAgentToken indicates whether to use the agent token preference chain
	// (config token > GH_AW_AGENT_TOKEN)
	// This should be true for agent assignment operations (assign-to-agent)
	UseCopilotCodingAgentToken bool

	// StepCondition is an optional `if:` expression for the step.
	// When non-empty, `if: {StepCondition}` is inserted after the step ID so the
	// step runs only when the condition is true. Use "always()" to run even after
	// earlier steps in the same job have failed.
	StepCondition string
}

// buildGitHubScriptStep creates a GitHub Script step with common scaffolding
// This extracts the repeated pattern found across safe output job builders
func (c *Compiler) buildGitHubScriptStep(data *WorkflowData, config GitHubScriptStepConfig) []string {
	safeOutputsStepsLog.Printf("Building GitHub Script step: %s (useCopilotRequestsToken=%v, useCopilotCodingAgentToken=%v)", config.StepName, config.UseCopilotRequestsToken, config.UseCopilotCodingAgentToken)

	return c.buildGitHubScriptStepCommon(data, config, true)
}

// buildGitHubScriptStepWithoutDownload creates a GitHub Script step without artifact download steps
// This is useful when multiple script steps are needed in the same job and artifact downloads
// should only happen once at the beginning
func (c *Compiler) buildGitHubScriptStepWithoutDownload(data *WorkflowData, config GitHubScriptStepConfig) []string {
	safeOutputsStepsLog.Printf("Building GitHub Script step without download: %s", config.StepName)

	return c.buildGitHubScriptStepCommon(data, config, false)
}

// buildGitHubScriptStepCommon renders the shared GitHub Script step scaffold.
// When includeDownload is true, artifact download steps are emitted before the step.
func (c *Compiler) buildGitHubScriptStepCommon(data *WorkflowData, config GitHubScriptStepConfig, includeDownload bool) []string {
	var steps []string

	if includeDownload {
		// Add artifact download steps before the GitHub Script step.
		// In workflow_call context, use the per-invocation prefix to avoid artifact name clashes.
		// These steps are used in jobs that depend on the agent job (not activation), so use
		// the agent-downstream prefix expression.
		steps = append(steps, buildAgentOutputDownloadSteps(artifactPrefixExprForAgentDownstreamJob(data), c.getActionPin)...)
	}

	// Step name and metadata
	steps = append(steps, fmt.Sprintf("      - name: %s\n", config.StepName))
	steps = append(steps, fmt.Sprintf("        id: %s\n", config.StepID))
	// Add optional step-level condition (e.g. "always()" to run even after prior step failures)
	if config.StepCondition != "" {
		steps = append(steps, fmt.Sprintf("        if: %s\n", config.StepCondition))
	}
	steps = append(steps, fmt.Sprintf("        uses: %s\n", getCachedActionPin("actions/github-script", data)))

	// Environment variables section
	steps = append(steps, "        env:\n")

	// Read GH_AW_AGENT_OUTPUT from environment (set by artifact download step)
	// instead of directly from job outputs which may be masked by GitHub Actions
	steps = append(steps, "          GH_AW_AGENT_OUTPUT: ${{ steps.setup-agent-output-env.outputs.GH_AW_AGENT_OUTPUT }}\n")

	// Add custom environment variables specific to this safe output type
	steps = append(steps, config.CustomEnvVars...)

	// Add custom environment variables from safe-outputs.env
	c.addCustomSafeOutputEnvVars(&steps, data)

	// With section for github-token
	steps = append(steps, "        with:\n")
	if config.UseCopilotCodingAgentToken {
		c.addSafeOutputAgentGitHubTokenForConfig(&steps, data, config.CustomToken)
	} else if config.UseCopilotRequestsToken {
		c.addSafeOutputCopilotGitHubTokenForConfig(&steps, data, config.CustomToken)
	} else {
		c.addSafeOutputGitHubTokenForConfig(&steps, data, config.CustomToken)
	}

	steps = append(steps, "          script: |\n")

	// Use require() if ScriptFile is specified, otherwise inline the script
	if config.ScriptFile != "" {
		steps = append(steps, "            const { setupGlobals } = require('"+SetupActionDestination+"/setup_globals.cjs');\n")
		steps = append(steps, "            setupGlobals(core, github, context, exec, io, getOctokit);\n")
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
// and set the GH_AW_AGENT_OUTPUT environment variable for safe-output jobs.
// GH_AW_AGENT_OUTPUT is only set when the artifact was actually downloaded successfully.
// prefix is prepended to the artifact name; use empty string for non-workflow_call workflows.
// pinAction resolves the download-artifact action reference; pass c.getActionPin from Compiler methods.
func buildAgentOutputDownloadSteps(prefix string, pinAction func(string) string) []string {
	safeOutputsStepsLog.Printf("Building agent output download steps with prefix: %q", prefix)
	return buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName:     prefix + constants.AgentArtifactName.String(),               // Unified agent artifact (prefixed in workflow_call)
		FallbackArtifact: prefix + constants.AgentOutputFallbackArtifactName.String(), // Tiny fallback copy of the agent output
		ArtifactFilename: constants.AgentOutputFilename.String(),                      // Filename inside the artifact directory
		DownloadPath:     constants.TmpGhAwDirSlash,
		SetupEnvStep:     true,
		EnvVarName:       "GH_AW_AGENT_OUTPUT",
		StepName:         "Download agent output artifact",
		StepID:           "download-agent-output",
	}, pinAction)
}

// buildDetectionArtifactDownloadSteps creates a step to download the detection artifact
// into the threat-detection working directory so its firewall proxy/audit logs are
// available for collect_usage_artifact_files.sh in the conclusion job (see gh-aw#54047).
// prefix is prepended to the artifact name; use empty string for non-workflow_call workflows.
// pinAction resolves the download-artifact action reference; pass c.getActionPin from Compiler methods.
func buildDetectionArtifactDownloadSteps(prefix string, pinAction func(string) string) []string {
	safeOutputsStepsLog.Printf("Building detection artifact download steps with prefix: %q", prefix)
	return buildArtifactDownloadSteps(ArtifactDownloadConfig{
		ArtifactName: prefix + constants.DetectionArtifactName.String(),
		DownloadPath: constants.ThreatDetectionDir + "/",
		StepName:     "Download detection artifact",
		StepID:       "download-detection-artifact",
	}, pinAction)
}

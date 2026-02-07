package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var compilerMainJobLog = logger.New("workflow:compiler_main_job")

// buildMainJob creates the main agent job that runs the AI agent with the configured engine and tools.
// This job depends on the activation job if it exists, and handles the main workflow logic.
func (c *Compiler) buildMainJob(data *WorkflowData, activationJobCreated bool) (*Job, error) {
	log.Printf("Building main job for workflow: %s", data.Name)
	var steps []string

	// Add setup action steps at the beginning of the job
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Main job doesn't need project support (no safe outputs processed here)
		steps = append(steps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)
	}

	// Checkout .github folder for agent job to access workflow configurations and runtime imports
	// This works in all modes including release mode where actions aren't checked out
	steps = append(steps, c.generateCheckoutGitHubFolder(data)...)

	// Find custom jobs that depend on pre_activation - these are handled by the activation job
	customJobsBeforeActivation := c.getCustomJobsDependingOnPreActivation(data.Jobs)

	var jobCondition = data.If
	if activationJobCreated {
		// If the if condition references custom jobs that run before activation,
		// the activation job handles the condition, so clear it here
		if c.referencesCustomJobOutputs(data.If, data.Jobs) && len(customJobsBeforeActivation) > 0 {
			jobCondition = "" // Activation job handles this condition
		} else if !c.referencesCustomJobOutputs(data.If, data.Jobs) {
			jobCondition = "" // Main job depends on activation job, so no need for inline condition
		}
		// Note: If data.If references custom jobs that DON'T depend on pre_activation,
		// we keep the condition on the agent job
	}

	// Note: workflow_run repository safety check is applied exclusively to activation job

	// Permission checks are now handled by the separate check_membership job
	// No role checks needed in the main job

	// Build step content using the generateMainJobSteps helper method
	// but capture it into a string instead of writing directly
	var stepBuilder strings.Builder
	if err := c.generateMainJobSteps(&stepBuilder, data); err != nil {
		return nil, fmt.Errorf("failed to generate main job steps: %w", err)
	}

	// Split the steps content into individual step entries
	stepsContent := stepBuilder.String()
	if stepsContent != "" {
		steps = append(steps, stepsContent)
	}

	var depends []string
	if activationJobCreated {
		depends = []string{string(constants.ActivationJobName)} // Depend on the activation job only if it exists
	}

	// Add custom jobs as dependencies only if they don't depend on pre_activation or agent
	// Custom jobs that depend on pre_activation are now dependencies of activation,
	// so the agent job gets them transitively through activation
	// Custom jobs that depend on agent should run AFTER the agent job, not before it
	if data.Jobs != nil {
		for jobName := range data.Jobs {
			// Skip jobs.pre-activation (or pre_activation) as it's handled specially
			if jobName == string(constants.PreActivationJobName) || jobName == "pre-activation" {
				continue
			}

			// Only add as direct dependency if it doesn't depend on pre_activation or agent
			// (jobs that depend on pre_activation are handled through activation)
			// (jobs that depend on agent are post-execution jobs like failure handlers)
			if configMap, ok := data.Jobs[jobName].(map[string]any); ok {
				if !jobDependsOnPreActivation(configMap) && !jobDependsOnAgent(configMap) {
					depends = append(depends, jobName)
				}
			}
		}
	}

	// IMPORTANT: Even though jobs that depend on pre_activation are transitively accessible
	// through the activation job, if the workflow content directly references their outputs
	// (e.g., ${{ needs.search_issues.outputs.* }}), we MUST add them as direct dependencies.
	// This is required for GitHub Actions expression evaluation and actionlint validation.
	referencedJobs := c.getReferencedCustomJobs(data.MarkdownContent, data.Jobs)
	for _, jobName := range referencedJobs {
		// Skip jobs.pre-activation (or pre_activation) as it's handled specially
		if jobName == string(constants.PreActivationJobName) || jobName == "pre-activation" {
			continue
		}

		// Check if this job is already in depends
		alreadyDepends := false
		for _, dep := range depends {
			if dep == jobName {
				alreadyDepends = true
				break
			}
		}
		// Add it if not already present
		if !alreadyDepends {
			depends = append(depends, jobName)
			compilerMainJobLog.Printf("Added direct dependency on custom job '%s' because it's referenced in workflow content", jobName)
		}
	}

	// Build outputs for all engines (GH_AW_SAFE_OUTPUTS functionality)
	// Build job outputs
	// Always include model output for reuse in other jobs
	outputs := map[string]string{
		"model": "${{ steps.generate_aw_info.outputs.model }}",
	}

	// Only add secret_verification_result output if the engine adds the validate-secret step
	// The validate-secret step is only added by engines that include it in GetInstallationSteps()
	engine, err := c.getAgenticEngine(data.AI)
	if err != nil {
		return nil, fmt.Errorf("failed to get agentic engine: %w", err)
	}
	if EngineHasValidateSecretStep(engine, data) {
		outputs["secret_verification_result"] = "${{ steps.validate-secret.outputs.verification_result }}"
		compilerMainJobLog.Printf("Added secret_verification_result output (engine includes validate-secret step)")
	} else {
		compilerMainJobLog.Printf("Skipped secret_verification_result output (engine does not include validate-secret step)")
	}

	// Add safe-output specific outputs if the workflow uses the safe-outputs feature
	if data.SafeOutputs != nil {
		outputs["output"] = "${{ steps.collect_output.outputs.output }}"
		outputs["output_types"] = "${{ steps.collect_output.outputs.output_types }}"
		outputs["has_patch"] = "${{ steps.collect_output.outputs.has_patch }}"
	}

	// Add checkout_pr_success output to track PR checkout status only if the checkout-pr step will be generated
	// This is used by the conclusion job to skip failure handling when checkout fails
	// (e.g., when PR is merged and branch is deleted)
	// The checkout-pr step is only generated when the workflow has contents read permission
	if ShouldGeneratePRCheckoutStep(data) {
		outputs["checkout_pr_success"] = "${{ steps.checkout-pr.outputs.checkout_pr_success || 'true' }}"
		compilerMainJobLog.Print("Added checkout_pr_success output (workflow has contents read access)")
	} else {
		compilerMainJobLog.Print("Skipped checkout_pr_success output (workflow lacks contents read access)")
	}

	// Build job-level environment variables for safe outputs
	var env map[string]string
	if data.SafeOutputs != nil {
		env = make(map[string]string)

		// Set GH_AW_SAFE_OUTPUTS to path in /opt (read-only mount for agent container)
		// The MCP server writes agent outputs to this file during execution
		// This file is in /opt to prevent the agent container from having write access
		env["GH_AW_SAFE_OUTPUTS"] = "/opt/gh-aw/safeoutputs/outputs.jsonl"

		// Set GH_AW_MCP_LOG_DIR for safe outputs MCP server logging
		// Store in mcp-logs directory so it's included in mcp-logs artifact
		env["GH_AW_MCP_LOG_DIR"] = "/tmp/gh-aw/mcp-logs/safeoutputs"

		// Set config and tools paths (readonly files in /opt/gh-aw)
		env["GH_AW_SAFE_OUTPUTS_CONFIG_PATH"] = "/opt/gh-aw/safeoutputs/config.json"
		env["GH_AW_SAFE_OUTPUTS_TOOLS_PATH"] = "/opt/gh-aw/safeoutputs/tools.json"

		// Add asset-related environment variables
		// These must always be set (even to empty) because awmg v0.0.12+ validates ${VAR} references
		if data.SafeOutputs.UploadAssets != nil {
			env["GH_AW_ASSETS_BRANCH"] = fmt.Sprintf("%q", data.SafeOutputs.UploadAssets.BranchName)
			env["GH_AW_ASSETS_MAX_SIZE_KB"] = fmt.Sprintf("%d", data.SafeOutputs.UploadAssets.MaxSizeKB)
			env["GH_AW_ASSETS_ALLOWED_EXTS"] = fmt.Sprintf("%q", strings.Join(data.SafeOutputs.UploadAssets.AllowedExts, ","))
		} else {
			// Set empty defaults when upload-assets is not configured
			env["GH_AW_ASSETS_BRANCH"] = `""`
			env["GH_AW_ASSETS_MAX_SIZE_KB"] = "0"
			env["GH_AW_ASSETS_ALLOWED_EXTS"] = `""`
		}

		// DEFAULT_BRANCH is used by safeoutputs MCP server
		// Use repository default branch from GitHub context
		env["DEFAULT_BRANCH"] = "${{ github.event.repository.default_branch }}"
	}

	// Generate agent concurrency configuration
	agentConcurrency := GenerateJobConcurrencyConfig(data)

	// Set up permissions for the agent job
	// Agent job ALWAYS needs contents: read to access .github and .actions folders
	permissions := data.Permissions
	if permissions == "" {
		// No permissions specified, just add contents: read
		perms := NewPermissionsContentsRead()
		permissions = perms.RenderToYAML()
	} else {
		// Parse existing permissions and add contents: read
		parser := NewPermissionsParser(permissions)
		perms := parser.ToPermissions()

		// Only add contents: read if not already present
		if level, exists := perms.Get(PermissionContents); !exists || level == PermissionNone {
			perms.Set(PermissionContents, PermissionRead)
			permissions = perms.RenderToYAML()
		}
	}

	job := &Job{
		Name:        string(constants.AgentJobName),
		If:          jobCondition,
		RunsOn:      c.indentYAMLLines(data.RunsOn, "    "),
		Environment: c.indentYAMLLines(data.Environment, "    "),
		Container:   c.indentYAMLLines(data.Container, "    "),
		Services:    c.indentYAMLLines(data.Services, "    "),
		Permissions: c.indentYAMLLines(permissions, "    "),
		Concurrency: c.indentYAMLLines(agentConcurrency, "    "),
		Env:         env,
		Steps:       steps,
		Needs:       depends,
		Outputs:     outputs,
	}

	return job, nil
}

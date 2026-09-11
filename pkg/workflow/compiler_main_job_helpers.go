package workflow

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// buildMainJobCondition computes the if-condition string for the main agent job.
// When the activation job exists it clears or rewrites the condition appropriately,
// and appends the daily-AIC-guardrail guard expression when configured.
func (c *Compiler) buildMainJobCondition(data *WorkflowData, activationJobCreated bool) string {
	customJobsBeforeActivation := c.getCustomJobsDependingOnPreActivation(data.Jobs)

	jobCondition := data.If
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
	if activationJobCreated && hasMaxDailyAICGuardrail(data) {
		compilerMainJobLog.Print("Applying daily-AIC guardrail to main job condition")
		guard := &ExpressionNode{Expression: fmt.Sprintf("needs.%s.outputs.daily_ai_credits_exceeded != 'true'", constants.ActivationJobName)}
		if jobCondition == "" {
			jobCondition = RenderCondition(guard)
		} else {
			jobCondition = RenderCondition(BuildAnd(&ExpressionNode{Expression: stripExpressionWrapper(jobCondition)}, guard))
		}
	}
	if activationJobCreated && isDockerSbxRuntime(data) {
		compilerMainJobLog.Print("Applying docker-sbx secret guardrail to main job condition")
		guard := &ExpressionNode{Expression: fmt.Sprintf("needs.%s.outputs.docker_sbx_secrets_result != 'failed'", constants.ActivationJobName)}
		if jobCondition == "" {
			jobCondition = RenderCondition(guard)
		} else {
			jobCondition = RenderCondition(BuildAnd(&ExpressionNode{Expression: stripExpressionWrapper(jobCondition)}, guard))
		}
	}
	compilerMainJobLog.Printf("Built main job condition: activationJobCreated=%v, hasCondition=%v", activationJobCreated, jobCondition != "")
	return jobCondition
}

// buildMainJobDependencies builds the list of jobs that the main agent job depends on and
// returns the engine.env content string so that callers can warn about built-in job references.
func (c *Compiler) buildMainJobDependencies(data *WorkflowData, activationJobCreated bool) (depends []string, engineEnvContent string) {
	if activationJobCreated {
		depends = []string{string(constants.ActivationJobName)} // Depend on the activation job only if it exists
	}

	// Add custom jobs as dependencies only if they don't depend on pre_activation or agent
	// Custom jobs that depend on pre_activation are now dependencies of activation,
	// so the agent job gets them transitively through activation
	// Custom jobs that depend on agent should run AFTER the agent job, not before it
	if data.Jobs != nil {
		for _, jobName := range sliceutil.SortedKeys(data.Jobs) {
			// Skip built-in jobs as they are handled separately and should not become custom dependencies.
			if isBuiltinJobName(jobName) {
				continue
			}
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
	// Also check custom steps from the frontmatter, which are also added to the agent job.
	// Also check engine.env values, which may contain needs.<job>.outputs.* expressions.
	var contentBuilder strings.Builder
	contentBuilder.WriteString(data.MarkdownContent)
	if data.CustomSteps != "" {
		contentBuilder.WriteByte('\n')
		contentBuilder.WriteString(data.CustomSteps)
	}
	// Compute engine.env content once; returned for use in the built-in job reference warning.
	if data.EngineConfig != nil && len(data.EngineConfig.Env) > 0 {
		var engineEnvBuilder strings.Builder
		for _, envValue := range data.EngineConfig.Env {
			engineEnvBuilder.WriteByte('\n')
			engineEnvBuilder.WriteString(envValue)
		}
		engineEnvContent = engineEnvBuilder.String()
		contentBuilder.WriteString(engineEnvContent)
		compilerMainJobLog.Printf("Including %d engine.env values in agent job dependency scan", len(data.EngineConfig.Env))
	}

	referencedJobs := c.getReferencedCustomJobs(contentBuilder.String(), data.Jobs)
	for _, jobName := range referencedJobs {
		// Skip built-in jobs as they are handled separately and should not become custom dependencies.
		if isBuiltinJobName(jobName) {
			continue
		}
		if !slices.Contains(depends, jobName) {
			depends = append(depends, jobName)
			compilerMainJobLog.Printf("Added direct dependency on custom job '%s' because it's referenced in workflow content or engine.env", jobName)
		}
	}
	return depends, engineEnvContent
}

// warnBuiltinJobEnvReferences emits a warning when engine.env values reference built-in
// job names in needs expressions.  Built-in jobs are managed by the compiler and cannot be
// added as direct agent dependencies, so such expressions silently evaluate to empty at runtime.
func (c *Compiler) warnBuiltinJobEnvReferences(depends []string, engineEnvContent string) {
	if engineEnvContent == "" {
		return
	}
	builtinNames := sliceutil.SortedKeys(constants.KnownBuiltInJobNames)
	builtinsWarned := make(map[string]struct{})
	for _, builtinJobName := range builtinNames {
		// Skip built-ins that are already direct dependencies (e.g., activation) —
		// their outputs are accessible and the expression is valid.
		if slices.Contains(depends, builtinJobName) {
			continue
		}
		if setutil.Contains(builtinsWarned, builtinJobName) {
			continue
		}
		if strings.Contains(engineEnvContent, "needs."+builtinJobName+".") {
			builtinsWarned[builtinJobName] = struct{}{}
			warningMsg := fmt.Sprintf(
				"engine.env references built-in job '%s' in a needs expression. "+
					"Built-in jobs are managed by the compiler and cannot be added as direct agent dependencies; "+
					"this expression will silently evaluate to an empty string at runtime.",
				builtinJobName,
			)
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(warningMsg))
			c.IncrementWarningCount()
		}
	}
}

// buildMainJobCoreOutputs returns the fixed output declarations that every main agent job exposes.
func buildMainJobCoreOutputs(data *WorkflowData) map[string]string {
	usageStepID := constants.ParseMCPGatewayStepID
	if isFirewallEnabled(data) || data.AI == string(constants.CopilotEngine) {
		usageStepID = constants.ParseTokenUsageStepID
	}
	return map[string]string{
		"model": fmt.Sprintf("${{ needs.%s.outputs.model }}", constants.ActivationJobName),
		// effective_tokens is the total ET for the run, captured by the MCP gateway log parser step.
		// It is exposed here so that the safe_outputs job can set GH_AW_EFFECTIVE_TOKENS and render
		// the {effective_tokens_suffix} template expression in footer templates.
		"effective_tokens": fmt.Sprintf("${{ steps.%s.outputs.effective_tokens }}", constants.ParseMCPGatewayStepID),
		// aic is the total AI Credits cost for the run (1 AIC == 0.01 USD), captured by the
		// MCP gateway log parser step and passed to downstream jobs for footer rendering.
		"aic": fmt.Sprintf("${{ steps.%s.outputs.aic }}", usageStepID),
		// ambient_context is the first-request context size metric:
		// input_tokens + (cache_tokens / 10), where cache tokens are normalized as 10x cheaper.
		"ambient_context": fmt.Sprintf("${{ steps.%s.outputs.ambient_context }}", usageStepID),
		// ai_credits_rate_limit_error is true when MCP gateway logs indicate AI credits
		// budget exhaustion or API rate limiting attributable to credit constraints.
		"ai_credits_rate_limit_error": fmt.Sprintf("${{ steps.%s.outputs.ai_credits_rate_limit_error || 'false' }}", constants.ParseMCPGatewayStepID),
		// unknown_model_ai_credits is true when the AWF API proxy rejects a request because the
		// model is not in the built-in pricing table and maxAiCredits is active.
		"unknown_model_ai_credits": fmt.Sprintf("${{ steps.%s.outputs.unknown_model_ai_credits || 'false' }}", constants.ParseMCPGatewayStepID),
		// setup-trace-id propagates the shared OTLP trace ID to downstream jobs (detection, safe_outputs, cache, etc.)
		"setup-trace-id": "${{ steps.setup.outputs.trace-id }}",
		// setup-span-id propagates the setup span parent so downstream setup spans form one tree.
		"setup-span-id": "${{ steps.setup.outputs.span-id }}",
		// setup-parent-span-id propagates the global setup parent span ID across jobs.
		"setup-parent-span-id": "${{ steps.setup.outputs.parent-span-id || steps.setup.outputs.span-id }}",
	}
}

// addMainJobEngineErrorOutputs adds engine error detection output declarations to outputs when the
// configured engine provides an error-detection script ID.
func (c *Compiler) addMainJobEngineErrorOutputs(outputs map[string]string, data *WorkflowData) {
	// Add inference_access_error, mcp_policy_error, agentic_engine_timeout,
	// model_not_supported_error, http_400_response_error, invocation_cap_exceeded,
	// and shell_expansion_guard_rejected outputs for engines
	// that provide an error detection step.
	// These outputs are written by the host-runner detect-agent-errors step (via the
	// engine's GetErrorDetectionScriptId script) rather than from inside the AWF container,
	// because GITHUB_OUTPUT is not accessible inside the sandbox.
	engine, engineErr := c.getAgenticEngine(data.AI)
	if engineErr != nil {
		return
	}
	if engine.GetErrorDetectionScriptId() == "" {
		return
	}
	stepRef := fmt.Sprintf("steps.%s.outputs", constants.DetectAgentErrorsStepID)
	outputs["inference_access_error"] = fmt.Sprintf("${{ %s.inference_access_error || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added inference_access_error output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["mcp_policy_error"] = fmt.Sprintf("${{ %s.mcp_policy_error || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added mcp_policy_error output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["agentic_engine_timeout"] = fmt.Sprintf("${{ %s.agentic_engine_timeout || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added agentic_engine_timeout output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["model_not_supported_error"] = fmt.Sprintf("${{ %s.model_not_supported_error || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added model_not_supported_error output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["http_400_response_error"] = fmt.Sprintf("${{ %s.http_400_response_error || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added http_400_response_error output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["invocation_cap_exceeded"] = fmt.Sprintf("${{ %s.invocation_cap_exceeded || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added invocation_cap_exceeded output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["max_cache_misses_exceeded"] = fmt.Sprintf("${{ %s.max_cache_misses_exceeded || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added max_cache_misses_exceeded output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["missing_model_pricing_error"] = fmt.Sprintf("${{ %s.missing_model_pricing_error || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added missing_model_pricing_error output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["missing_model_pricing_model_name"] = fmt.Sprintf("${{ %s.missing_model_pricing_model_name || '' }}", stepRef)
	compilerMainJobLog.Printf("Added missing_model_pricing_model_name output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
	outputs["shell_expansion_guard_rejected"] = fmt.Sprintf("${{ %s.shell_expansion_guard_rejected || 'false' }}", stepRef)
	compilerMainJobLog.Printf("Added shell_expansion_guard_rejected output (engine=%s, step=%s)", engine.GetID(), constants.DetectAgentErrorsStepID)
}

// buildMainJobOutputs builds the complete outputs map for the main agent job.
func (c *Compiler) buildMainJobOutputs(data *WorkflowData) map[string]string {
	outputs := buildMainJobCoreOutputs(data)

	// Note: secret_verification_result is now an output of the activation job (not the agent job).
	// The validate-secret step runs in the activation job, before context variable validation.

	// Propagate the artifact prefix from the activation job so that downstream jobs depending
	// only on the agent job (e.g. update_cache_memory, safe-jobs) can still access the prefix
	// without needing a direct dependency on the activation job.
	if hasWorkflowCallTrigger(data.On) {
		outputs[constants.ArtifactPrefixOutputName] = "${{ needs.activation.outputs.artifact_prefix }}"
		compilerMainJobLog.Print("Added artifact_prefix output to agent job (workflow_call context)")
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

	// Expose restore step outputs so downstream failure handling can compute whether
	// any cache-memory restore matched an existing cache entry.
	if data.CacheMemoryConfig != nil && len(data.CacheMemoryConfig.Caches) > 0 {
		for i := range data.CacheMemoryConfig.Caches {
			stepID := fmt.Sprintf("restore_cache_memory_%d", i)
			outputs[fmt.Sprintf("cache_memory_restore_%d_matched_key", i)] = fmt.Sprintf("${{ steps.%s.outputs.cache-matched-key || '' }}", stepID)
			outputs[fmt.Sprintf("cache_memory_restore_%d_cache_hit", i)] = fmt.Sprintf("${{ steps.%s.outputs.cache-hit || 'false' }}", stepID)
		}
	}

	c.addMainJobEngineErrorOutputs(outputs, data)
	return outputs
}

// buildMainJobEnv builds the job-level environment variable map for the main agent job.
func (c *Compiler) buildMainJobEnv(data *WorkflowData) map[string]string { //nolint:largefunc // Existing environment assembly remains explicit.
	var env map[string]string
	if data != nil && data.EngineConfig != nil && data.EngineConfig.Version != "" {
		env = make(map[string]string)
		env["GH_AW_ENGINE_VERSION"] = fmt.Sprintf("%q", data.EngineConfig.Version)
	}

	// Disable the Chromium process sandbox for playwright CLI mode.
	// GitHub Actions runners are containerised environments where kernel namespace
	// sandboxing is unavailable, which causes playwright-cli to abort with
	// "Playwright can't run in this sandbox environment".
	if isPlaywrightCLIMode(data.Tools) {
		if env == nil {
			env = make(map[string]string)
		}
		env["PLAYWRIGHT_MCP_SANDBOX"] = "false"
	}

	if data.SafeOutputs != nil {
		compilerMainJobLog.Printf("Configuring safe-outputs job env for main job (uploadAssets=%v)", data.SafeOutputs.UploadAssets != nil)
		if env == nil {
			env = make(map[string]string)
		}

		// Set GH_AW_MCP_LOG_DIR for safe outputs MCP server logging
		// Store in mcp-logs directory so it's included in mcp-logs artifact
		env["GH_AW_MCP_LOG_DIR"] = constants.TmpMcpLogsSafeOutputsDir

		// Note: GH_AW_SAFE_OUTPUTS, GH_AW_SAFE_OUTPUTS_CONFIG_PATH, and
		// GH_AW_SAFE_OUTPUTS_TOOLS_PATH are set via a run step (see generateSetRuntimePathsStep)
		// because the runner context is not available in job-level env: blocks.

		// Add asset-related environment variables
		// These must always be set (even to empty) because awmg v0.0.12+ validates ${VAR} references
		if data.SafeOutputs.UploadAssets != nil {
			env["GH_AW_ASSETS_BRANCH"] = fmt.Sprintf("%q", data.SafeOutputs.UploadAssets.BranchName)
			env["GH_AW_ASSETS_MAX_SIZE_KB"] = strconv.Itoa(data.SafeOutputs.UploadAssets.MaxSizeKB)
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

		// Add PR head baseline environment variables used for incremental patches.
		// These are only populated (via GITHUB_ENV) by the "Checkout PR branch" step,
		// which is skipped for non-PR events (e.g. schedule, workflow_dispatch without
		// a PR context). They must still be declared here (even empty) because the
		// MCP gateway validates every ${VAR} reference in its config and fails with
		// "undefined environment variable referenced" if the variable is entirely unset.
		env["GH_AW_PR_HEAD_BASE_BRANCH"] = `""`
		env["GH_AW_PR_HEAD_BASE_SHA"] = `""`
		env["GH_AW_PR_HEAD_BASE_REPO"] = `""`
		env["GH_AW_PR_HEAD_BASE_PR_NUMBER"] = `""`
		env["GH_AW_PR_HEAD_BASE_REF"] = `""`
		env["GH_AW_PR_HEAD_REPO"] = `""`
	}

	// Set GH_AW_WORKFLOW_ID_SANITIZED for cache-memory keys
	// This contains the workflow ID with all hyphens removed and lowercased
	// Used in cache keys to avoid spaces and special characters
	if data.WorkflowID != "" {
		if env == nil {
			env = make(map[string]string)
		}
		env["GH_AW_WORKFLOW_ID_SANITIZED"] = SanitizeWorkflowIDForCacheKey(data.WorkflowID)
	}

	// Bake the repository project UTC offset (from aw.json) into job env so runtime
	// JavaScript helpers do not need to read aw.json on the runner.
	if utcOffset := c.getCompiledProjectUTCOffset(); utcOffset != "" {
		if env == nil {
			env = make(map[string]string)
		}
		env["GH_AW_PROJECT_UTC"] = fmt.Sprintf("%q", utcOffset)
	}

	return env
}

// buildMainJobPermissions builds the final permissions string for the main agent job.
// It optionally adds contents: read for dev/script mode, then infers additional read
// permissions from gh CLI commands found in all agent job step sections.
func (c *Compiler) buildMainJobPermissions(data *WorkflowData) (string, error) {
	permissions := augmentPermissionsForDevMode(c, data, filterJobLevelPermissions(data.Permissions, data.CachedPermissions))

	agentAllScripts := collectAgentJobScripts(data)
	if len(agentAllScripts) == 0 {
		return permissions, nil
	}
	compilerMainJobLog.Printf("Analyzing %d agent job script(s) for gh command permissions", len(agentAllScripts))

	writeCmds, err := detectWriteCommandsInShellScripts(agentAllScripts)
	if err != nil {
		return "", err
	}
	if len(writeCmds) > 0 {
		compilerMainJobLog.Printf("Rejecting main job: %d write gh command(s) found in agent steps", len(writeCmds))
		return "", fmt.Errorf(
			"agent job uses write gh command(s) [%s]; write operations are not permitted in agent job steps because the agent job runs with read-only permissions. Use safe-outputs for write operations. See: https://github.github.com/gh-aw/reference/safe-outputs/",
			strings.Join(writeCmds, ", "),
		)
	}

	// Infer read permissions unless the user explicitly zeroed out all permissions.
	// Check data.Permissions (the original value) since augmentPermissionsForDevMode above may have
	// already expanded "permissions: {}" into an explicit block.
	// Uses the same exact-string check as tools.go (the YAML parser always normalizes
	// "permissions: {}" to this canonical form when parsing the frontmatter).
	if data.Permissions != "permissions: {}" && permissions != "" {
		inferred, err := inferPermissionsFromShellScripts(agentAllScripts)
		if err != nil {
			return "", err
		}
		if len(inferred) > 0 {
			compilerMainJobLog.Printf("Inferred %d read permission(s) from agent shell scripts", len(inferred))
			permissions = mergeInferredIntoPermissionsYAML(permissions, inferred)
		}
	}

	return permissions, nil
}

func operationalValueGraderEnabled(data *WorkflowData) bool {
	if data == nil || data.Graders == nil {
		return false
	}
	grader, ok := data.Graders.Graders["operational-value"]
	return ok && (grader.Enabled == nil || *grader.Enabled)
}

// augmentPermissionsForDevMode adds contents: read to permissions when the compiler is in
// dev or script mode and the actions folder checkout is needed.
//
// GitHub App-only permissions (e.g., members, administration) are filtered out before
// rendering to the job-level permissions block because they are not valid GitHub Actions
// workflow permissions and cause a parse error when queued.
func augmentPermissionsForDevMode(c *Compiler, data *WorkflowData, permissions string) string {
	needsContentsRead := (c.actionMode.IsDev() || c.actionMode.IsScript()) && len(c.generateCheckoutActionsFolder(data)) > 0
	if !needsContentsRead {
		return permissions
	}
	compilerMainJobLog.Print("Augmenting main job permissions with contents: read for dev/script mode")
	if permissions == "" {
		perms := NewPermissionsContentsRead()
		return filterJobLevelPermissions(perms.RenderToYAML())
	}
	// Parse the already-filtered permissions string (not the raw data.Permissions)
	// since filterJobLevelPermissions may have adjusted the indentation/format.
	parser := NewPermissionsParser(permissions)
	perms := parser.ToPermissions()
	if level, exists := perms.Get(PermissionContents); !exists || level == PermissionNone {
		perms.Set(PermissionContents, PermissionRead)
		return filterJobLevelPermissions(perms.RenderToYAML())
	}
	return permissions
}

// collectAgentJobScripts gathers the shell `run` scripts from all step sections that are
// injected into the agent job. Only top-level frontmatter sections and the agent job's
// setup-steps / pre-steps sub-sections are included; other jobs.agent.* sections are
// ignored because applyBuiltinJobPreSteps does not inject them.
func collectAgentJobScripts(data *WorkflowData) []string {
	agentJobName := string(constants.AgentJobName)
	scripts := extractRunScriptsFromSectionYAML(data.PreSteps, "pre-steps")
	scripts = append(scripts, extractRunScriptsFromSectionYAML(data.CustomSteps, "steps")...)
	scripts = append(scripts, extractRunScriptsFromSectionYAML(data.PreAgentSteps, "pre-agent-steps")...)
	scripts = append(scripts, extractRunScriptsFromSectionYAML(data.PostSteps, "post-steps")...)
	if data.Jobs != nil {
		scripts = append(scripts, extractRunScriptsFromJobSection(data.Jobs, agentJobName, "setup-steps")...)
		scripts = append(scripts, extractRunScriptsFromJobSection(data.Jobs, agentJobName, "pre-steps")...)
	}
	return scripts
}

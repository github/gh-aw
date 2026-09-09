package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

func needsDailyAICCachePermission(data *WorkflowData) bool {
	return hasMaxDailyAICGuardrail(data) && data.WorkflowID != ""
}

// buildConclusionSetupSteps extracts the common setup, token minting, and artifact steps.
func (c *Compiler) buildConclusionSetupSteps(data *WorkflowData) []string {
	var steps []string

	// Add setup step to copy scripts
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Notify comment job doesn't need project support
		// Conclusion/notify job depends on activation, reuse its trace ID
		notifyTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		notifyParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStep(data, setupActionRef, SetupActionDestination, false, notifyTraceID, notifyParentSpanID)...)
	}

	// Add GitHub App token minting step if app is configured
	if data.SafeOutputs.GitHubApp != nil {
		permissions := ComputePermissionsForSafeOutputs(data.SafeOutputs)
		var appTokenFallbackRepo string
		if hasWorkflowCallTrigger(data.On) {
			appTokenFallbackRepo = "${{ needs.activation.outputs.target_repo_name }}"
		}
		steps = append(steps, c.buildGitHubAppTokenMintStepForRepository(
			data.SafeOutputs.GitHubApp,
			permissions,
			appTokenFallbackRepo,
			inferSingleCheckoutRepositoryForGitHubAppOwner(data),
		)...)
	}

	// Add artifact download steps once (shared by noop and conclusion steps).
	// In workflow_call context, use the per-invocation prefix to avoid artifact name clashes.
	steps = append(steps, buildAgentOutputDownloadSteps(artifactPrefixExprForDownstreamJob(data), c.getActionPin)...)
	// Download the detection artifact (when threat detection is enabled) so its firewall
	// proxy/audit logs (collected under threat-detection/sandbox/firewall/ by
	// buildCopyDetectionFirewallLogsStep) land at the paths collect_usage_artifact_files.sh
	// expects, letting detection-phase usage surface in the usage artifact and count toward
	// the AI-credits budget cap (see gh-aw#54047).
	if IsDetectionJobEnabled(data.SafeOutputs) {
		steps = append(steps, buildDetectionArtifactDownloadSteps(artifactPrefixExprForDownstreamJob(data), c.getActionPin)...)
	}
	steps = append(steps, buildUsageArtifactUploadSteps(artifactPrefixExprForDownstreamJob(data), data.Evals != nil && data.Evals.HasEvals(), c.getActionPin)...)
	if needsDailyAICCachePermission(data) {
		steps = append(steps, buildDailyAICUsageCacheSteps(data, c.getActionPin)...)
	}

	return steps
}

// buildConclusionNoOpStep builds the merged no-op handler step.
func (c *Compiler) buildConclusionNoOpStep(data *WorkflowData, mainJobName string) []string {
	// Add noop processing step if noop is configured.
	// This single step replaces the former two-step "Process No-Op Messages" + "Handle No-Op Message"
	// sequence: handle_noop_message.cjs now loads agent output directly (no cross-step dep).
	if data.SafeOutputs.NoOp == nil {
		return nil
	}
	var envVars []string
	envVars = append(envVars, buildTemplatableIntEnvVar("GH_AW_NOOP_MAX", data.SafeOutputs.NoOp.Max)...)
	envVars = append(envVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID, buildLocalWorkflowSourceURL(c.markdownPath))...)
	envVars = append(envVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AGENT_CONCLUSION: ${{ needs.%s.result }}\n", mainJobName))
	envVars = append(envVars, buildTemplatableBoolEnvVar("GH_AW_NOOP_REPORT_AS_ISSUE", data.SafeOutputs.NoOp.ReportAsIssue)...)
	if data.SafeOutputs.NoOp.ReportAsIssue == nil {
		envVars = append(envVars, "          GH_AW_NOOP_REPORT_AS_ISSUE: \"true\"\n")
	}
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AIC: ${{ needs.%s.outputs.aic }}\n", mainJobName))
	if IsDetectionJobEnabled(data.SafeOutputs) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_THREAT_DETECTION_AIC: ${{ needs.%s.outputs.aic }}\n", constants.DetectionJobName))
	}
	if data.Evals != nil && data.Evals.HasEvals() {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_EVALS_AIC: ${{ needs.%s.outputs.aic }}\n", constants.EvalsJobName))
	}
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AMBIENT_CONTEXT: ${{ needs.%s.outputs.ambient_context }}\n", mainJobName))
	if data.WorkflowID != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID))
		if isSteeringIssueEnabled(data) {
			envVars = append(envVars,
				"          GH_AW_STEERING_ISSUE_NUMBER: ${{ needs.activation.outputs.steering_issue_number }}\n",
				"          GH_AW_STEERING_ISSUE_URL: ${{ needs.activation.outputs.steering_issue_url }}\n",
			)
		}
	}
	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Process no-op messages",
		StepID:        "noop",
		MainJobName:   mainJobName,
		CustomEnvVars: envVars,
		ScriptFile:    "handle_noop_message.cjs",
		CustomToken:   data.SafeOutputs.NoOp.GitHubToken,
	})
}

// buildConclusionDetectionRunsStep builds the detection-runs logging step.
func (c *Compiler) buildConclusionDetectionRunsStep(data *WorkflowData, mainJobName string) []string {
	// Add detection runs logging step if threat detection is enabled.
	// This posts a comment to the "[aw] Detection Runs" tracking issue whenever
	// the detection job produces a warning or failure conclusion.
	if !IsDetectionJobEnabled(data.SafeOutputs) {
		return nil
	}
	envVars := buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID, buildLocalWorkflowSourceURL(c.markdownPath))
	envVars = append(envVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	envVars = append(envVars, fmt.Sprintf("          GH_AW_DETECTION_CONCLUSION: ${{ needs.%s.outputs.detection_conclusion }}\n", constants.DetectionJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_DETECTION_REASON: ${{ needs.%s.outputs.detection_reason }}\n", constants.DetectionJobName))
	steps := c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Log detection run",
		StepID:        "detection_runs",
		MainJobName:   mainJobName,
		CustomEnvVars: envVars,
		ScriptFile:    "handle_detection_runs.cjs",
	})
	notifyCommentLog.Print("Added detection runs logging step to conclusion job")
	return steps
}

// buildConclusionMissingToolStep builds the missing-tool handler step.
func (c *Compiler) buildConclusionMissingToolStep(data *WorkflowData, mainJobName string) []string {
	// Add missing_tool processing step if missing-tool is configured
	if data.SafeOutputs.MissingTool == nil {
		return nil
	}
	var envVars []string
	envVars = append(envVars, buildTemplatableIntEnvVar("GH_AW_MISSING_TOOL_MAX", data.SafeOutputs.MissingTool.Max)...)
	envVars = append(envVars, buildTemplatableBoolEnvVar("GH_AW_MISSING_TOOL_CREATE_ISSUE", data.SafeOutputs.MissingTool.CreateIssue)...)
	if data.SafeOutputs.MissingTool.TitlePrefix != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_TITLE_PREFIX: %q\n", data.SafeOutputs.MissingTool.TitlePrefix))
	}
	if len(data.SafeOutputs.MissingTool.Labels) > 0 {
		if labelsJSON, err := json.Marshal(data.SafeOutputs.MissingTool.Labels); err == nil {
			envVars = append(envVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_LABELS: %q\n", string(labelsJSON)))
		}
	}
	envVars = append(envVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID, buildLocalWorkflowSourceURL(c.markdownPath))...)
	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Record missing tool",
		StepID:        "missing_tool",
		MainJobName:   mainJobName,
		CustomEnvVars: envVars,
		Script:        "const { main } = require('${{ runner.temp }}/gh-aw/actions/missing_tool.cjs'); await main();",
		ScriptFile:    "missing_tool.cjs",
		CustomToken:   data.SafeOutputs.MissingTool.GitHubToken,
	})
}

// buildConclusionReportIncompleteStep builds the report-incomplete handler step.
func (c *Compiler) buildConclusionReportIncompleteStep(data *WorkflowData, mainJobName string) []string {
	// Add report_incomplete processing step if report-incomplete is configured
	if data.SafeOutputs.ReportIncomplete == nil {
		return nil
	}
	var envVars []string
	envVars = append(envVars, buildTemplatableIntEnvVar("GH_AW_REPORT_INCOMPLETE_MAX", data.SafeOutputs.ReportIncomplete.Max)...)
	envVars = append(envVars, buildTemplatableBoolEnvVar("GH_AW_REPORT_INCOMPLETE_CREATE_ISSUE", data.SafeOutputs.ReportIncomplete.CreateIssue)...)
	if data.SafeOutputs.ReportIncomplete.TitlePrefix != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_REPORT_INCOMPLETE_TITLE_PREFIX: %q\n", data.SafeOutputs.ReportIncomplete.TitlePrefix))
	}
	if len(data.SafeOutputs.ReportIncomplete.Labels) > 0 {
		if labelsJSON, err := json.Marshal(data.SafeOutputs.ReportIncomplete.Labels); err == nil {
			envVars = append(envVars, fmt.Sprintf("          GH_AW_REPORT_INCOMPLETE_LABELS: %q\n", string(labelsJSON)))
		}
	}
	envVars = append(envVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID, buildLocalWorkflowSourceURL(c.markdownPath))...)
	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Record incomplete",
		StepID:        "report_incomplete",
		MainJobName:   mainJobName,
		CustomEnvVars: envVars,
		Script:        "const { main } = require('${{ runner.temp }}/gh-aw/actions/report_incomplete_handler.cjs'); await main();",
		ScriptFile:    "report_incomplete_handler.cjs",
		CustomToken:   data.SafeOutputs.ReportIncomplete.GitHubToken,
	})
}

// serializeConclusionMessagesJSON serializes safe-output messages once for reuse.
func serializeConclusionMessagesJSON(data *WorkflowData) string {
	// Serialize messages config once for reuse in both handler steps below.
	if data.SafeOutputs == nil || data.SafeOutputs.Messages == nil {
		return ""
	}
	jsonText, err := serializeMessagesConfig(data.SafeOutputs.Messages)
	if err != nil {
		notifyCommentLog.Printf("Warning: failed to serialize messages config: %v", err)
		return ""
	}
	return jsonText
}

// buildAgentFailureCoreVars builds the core environment for the agent failure handler.
func (c *Compiler) buildAgentFailureCoreVars(data *WorkflowData, mainJobName string) ([]string, CodingAgentEngine, error) {
	var envVars []string
	envVars = append(envVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID, buildLocalWorkflowSourceURL(c.markdownPath))...)
	envVars = append(envVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AGENT_CONCLUSION: ${{ needs.%s.result }}\n", mainJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_WORKFLOW_ID: %q\n", data.WorkflowID))

	expiresHours := DefaultActionFailureIssueExpiresHours
	if data.SafeOutputs != nil && data.SafeOutputs.ReportFailureAsIssue != nil && data.SafeOutputs.ReportFailureAsIssue.String() == "false" {
		expiresHours = 0
	} else {
		repoConfig, err := c.loadRepoConfig()
		if err != nil {
			notifyCommentLog.Printf("Warning: failed to load repo config for action failure issue expiration (using default %d hours): %v. Check that %s exists and matches schema requirements", DefaultActionFailureIssueExpiresHours, err, RepoConfigFileName)
		} else {
			expiresHours = repoConfig.ActionFailureIssueExpiresHours()
		}
	}
	envVars = append(envVars, fmt.Sprintf("          GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: %q\n", strconv.Itoa(expiresHours)))
	if data.EngineConfig != nil && data.EngineConfig.ID != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_ENGINE_ID: %q\n", data.EngineConfig.ID))
	}
	engine, err := c.getAgenticEngine(data.AI)
	if err != nil {
		return nil, nil, fmt.Errorf("could not resolve agentic engine from workflow AI configuration; ensure a supported engine or model is configured: %w", err)
	}
	if EngineHasValidateSecretStep(engine, data) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SECRET_VERIFICATION_RESULT: ${{ needs.%s.outputs.secret_verification_result }}\n", constants.ActivationJobName))
		if msg := engine.GetSecretFailureMessage(data); msg != "" {
			envVars = append(envVars, fmt.Sprintf("          GH_AW_ENGINE_SECRET_FAILURE_MESSAGE: %q\n", msg))
		}
	}
	if isDockerSbxRuntime(data) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_DOCKER_SBX_SECRETS_RESULT: ${{ needs.%s.outputs.docker_sbx_secrets_result }}\n", constants.ActivationJobName))
	}
	if ShouldGeneratePRCheckoutStep(data) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_CHECKOUT_PR_SUCCESS: ${{ needs.%s.outputs.checkout_pr_success }}\n", mainJobName))
	}
	envVars = append(envVars, fmt.Sprintf("          GH_AW_EFFECTIVE_TOKENS: ${{ needs.%s.outputs.effective_tokens || '' }}\n", mainJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AI_CREDITS_RATE_LIMIT_ERROR: ${{ needs.%s.outputs.ai_credits_rate_limit_error || 'false' }}\n", mainJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_UNKNOWN_MODEL_AI_CREDITS: ${{ needs.%s.outputs.unknown_model_ai_credits || 'false' }}\n", mainJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AIC: ${{ needs.%s.outputs.aic }}\n", mainJobName))
	if IsDetectionJobEnabled(data.SafeOutputs) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_THREAT_DETECTION_AIC: ${{ needs.%s.outputs.aic }}\n", constants.DetectionJobName))
	}
	if data.Evals != nil && data.Evals.HasEvals() {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_EVALS_AIC: ${{ needs.%s.outputs.aic }}\n", constants.EvalsJobName))
	}
	if data.EngineConfig != nil && data.EngineConfig.MaxAICredits != 0 {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MAX_AI_CREDITS: %q\n", strconv.FormatInt(data.EngineConfig.MaxAICredits, 10)))
	} else {
		expr := compilerenv.BuildDefaultMaxAICreditsExpression(strconv.FormatInt(constants.DefaultMaxAICredits, 10))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MAX_AI_CREDITS: %s\n", expr))
	}
	return envVars, engine, nil
}

// buildAgentFailureEngineDetectionVars appends engine-specific detection outputs.
func buildAgentFailureEngineDetectionVars(engine CodingAgentEngine, data *WorkflowData, mainJobName string) []string {
	// Pass engine error-detection outputs to the conclusion job when the selected engine
	// provides a host-runner detect-agent-errors step.
	// Contract: engines returning a non-empty GetErrorDetectionScriptId() must run
	// actions/setup/js/detect_agent_errors.cjs, which emits all outputs below.
	// These outputs cover:
	//   - inference_access_error: token lacks inference access
	//   - mcp_policy_error: MCP servers blocked by enterprise/organization policy
	//   - agentic_engine_timeout: engine process killed by signal (step timeout)
	//   - model_not_supported_error: configured model name is invalid or unavailable
	//   - http_400_response_error: engine returned a generic HTTP 400 Bad Request response
	//   - capi_quota_exceeded_error: Copilot/CAPI quota exhaustion/rate-limit response
	//   - max_cache_misses_exceeded: AWF API proxy consecutive cache miss guardrail fired
	//   - shell_expansion_guard_rejected: sandbox command-injection guard rejected shell expansion patterns
	var envVars []string
	if engine.GetErrorDetectionScriptId() != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_INFERENCE_ACCESS_ERROR: ${{ needs.%s.outputs.inference_access_error }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MCP_POLICY_ERROR: ${{ needs.%s.outputs.mcp_policy_error }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_AGENTIC_ENGINE_TIMEOUT: ${{ needs.%s.outputs.agentic_engine_timeout }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MODEL_NOT_SUPPORTED_ERROR: ${{ needs.%s.outputs.model_not_supported_error }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_HTTP_400_RESPONSE_ERROR: ${{ needs.%s.outputs.http_400_response_error }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MAX_CACHE_MISSES_EXCEEDED: ${{ needs.%s.outputs.max_cache_misses_exceeded }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MISSING_MODEL_PRICING_ERROR: ${{ needs.%s.outputs.missing_model_pricing_error }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_MISSING_MODEL_PRICING_MODEL_NAME: ${{ needs.%s.outputs.missing_model_pricing_model_name }}\n", mainJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SHELL_EXPANSION_GUARD_REJECTED: ${{ needs.%s.outputs.shell_expansion_guard_rejected }}\n", mainJobName))
	}
	if apiHosts := getEngineAPIHosts(data, engine); len(apiHosts) > 0 {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_ENGINE_API_HOSTS: %q\n", strings.Join(apiHosts, ",")))
	}
	return envVars
}

// buildAgentFailureActivationStatusVars appends status outputs from safe outputs and activation jobs.
func buildAgentFailureActivationStatusVars(data *WorkflowData) []string {
	var envVars []string
	if isSteeringIssueEnabled(data) {
		envVars = append(envVars, "          GH_AW_FAILURE_ISSUE_NUMBER: ${{ needs.activation.outputs.steering_issue_number }}\n")
	}
	if data.SafeOutputs.AssignToAgent != nil {
		envVars = append(envVars, "          GH_AW_ASSIGNMENT_ERRORS: ${{ needs.safe_outputs.outputs.assign_to_agent_assignment_errors }}\n")
		envVars = append(envVars, "          GH_AW_ASSIGNMENT_ERROR_COUNT: ${{ needs.safe_outputs.outputs.assign_to_agent_assignment_error_count }}\n")
	}
	if data.SafeOutputs.CreateDiscussions != nil {
		envVars = append(envVars, "          GH_AW_CREATE_DISCUSSION_ERRORS: ${{ needs.safe_outputs.outputs.create_discussion_errors }}\n")
		envVars = append(envVars, "          GH_AW_CREATE_DISCUSSION_ERROR_COUNT: ${{ needs.safe_outputs.outputs.create_discussion_error_count }}\n")
	}
	if data.SafeOutputs.PushToPullRequestBranch != nil || data.SafeOutputs.CreatePullRequests != nil {
		envVars = append(envVars, "          GH_AW_CODE_PUSH_FAILURE_ERRORS: ${{ needs.safe_outputs.outputs.code_push_failure_errors }}\n")
		envVars = append(envVars, "          GH_AW_CODE_PUSH_FAILURE_COUNT: ${{ needs.safe_outputs.outputs.code_push_failure_count }}\n")
	}
	if data.SafeOutputs.GitHubApp != nil {
		envVars = append(envVars, "          GH_AW_SAFE_OUTPUTS_APP_TOKEN_MINTING_FAILED: ${{ needs.safe_outputs.outputs.app_token_minting_failed }}\n")
		envVars = append(envVars, "          GH_AW_CONCLUSION_APP_TOKEN_MINTING_FAILED: ${{ steps.safe-outputs-app-token.outcome == 'failure' }}\n")
	}
	if data.ActivationGitHubApp != nil {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_ACTIVATION_APP_TOKEN_MINTING_FAILED: ${{ needs.%s.outputs.activation_app_token_minting_failed }}\n", constants.ActivationJobName))
	}
	envVars = append(envVars, fmt.Sprintf("          GH_AW_LOCKDOWN_CHECK_FAILED: ${{ needs.%s.outputs.lockdown_check_failed }}\n", constants.ActivationJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_OAUTH_TOKEN_CHECK_FAILED: ${{ needs.%s.outputs.oauth_token_check_failed }}\n", constants.ActivationJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_STALE_LOCK_FILE_FAILED: ${{ needs.%s.outputs.stale_lock_file_failed }}\n", constants.ActivationJobName))
	// SkillReferences holds structured skill refs (with github-token/github-app); Skills holds raw
	// skill specs from simple frontmatter. Both result in activation-job skill install steps.
	if len(data.SkillReferences) > 0 || len(data.Skills) > 0 {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SKILL_INSTALL_FAILURE_COUNT: ${{ needs.%s.outputs.skill_install_failure_count || '0' }}\n", constants.ActivationJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SKILL_INSTALL_ERRORS: ${{ needs.%s.outputs.skill_install_errors || '' }}\n", constants.ActivationJobName))
	}
	if hasMaxDailyAICGuardrail(data) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_DAILY_AI_CREDITS_EXCEEDED: ${{ needs.%s.outputs.daily_ai_credits_exceeded }}\n", constants.ActivationJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_DAILY_AI_CREDITS_TOTAL_EFFECTIVE_TOKENS: ${{ needs.%s.outputs.daily_ai_credits_total_effective_tokens }}\n", constants.ActivationJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_DAILY_AI_CREDITS_THRESHOLD: ${{ needs.%s.outputs.daily_ai_credits_threshold }}\n", constants.ActivationJobName))
	}
	return envVars
}

// buildAgentFailureRepoMemoryVars appends repo-memory validation outputs.
func buildAgentFailureRepoMemoryVars(data *WorkflowData) []string {
	// Pass repo-memory failure outputs if repo-memory is configured
	// This allows the agent failure handler to report both job-level failures and validation issues
	if data.RepoMemoryConfig == nil || len(data.RepoMemoryConfig.Memories) == 0 {
		return nil
	}
	envVars := []string{"          GH_AW_PUSH_REPO_MEMORY_RESULT: ${{ needs.push_repo_memory.result }}\n"}
	for _, memory := range data.RepoMemoryConfig.Memories {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_REPO_MEMORY_VALIDATION_FAILED_%s: ${{ needs.push_repo_memory.outputs.validation_failed_%s }}\n", memory.ID, memory.ID))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_REPO_MEMORY_VALIDATION_ERROR_%s: ${{ needs.push_repo_memory.outputs.validation_error_%s }}\n", memory.ID, memory.ID))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_REPO_MEMORY_PATCH_SIZE_EXCEEDED_%s: ${{ needs.push_repo_memory.outputs.patch_size_exceeded_%s }}\n", memory.ID, memory.ID))
	}
	return envVars
}

// buildAgentFailureReportingPolicyVars appends reporting policy configuration.
func buildAgentFailureReportingPolicyVars(data *WorkflowData) []string {
	var envVars []string
	if data.SafeOutputs.GroupReports {
		envVars = append(envVars, "          GH_AW_GROUP_REPORTS: \"true\"\n")
	} else {
		envVars = append(envVars, "          GH_AW_GROUP_REPORTS: \"false\"\n")
	}
	if data.SafeOutputs.ReportFailureAsIssue == nil {
		envVars = append(envVars, "          GH_AW_FAILURE_REPORT_AS_ISSUE: \"true\"\n")
	} else {
		appendReportFailureEnvVar := func(enabled bool) {
			envVars = append(envVars, fmt.Sprintf("          GH_AW_FAILURE_REPORT_AS_ISSUE: %q\n", strconv.FormatBool(enabled)))
		}
		shouldIncludeCategoryFilters := true
		reportSetting := data.SafeOutputs.ReportFailureAsIssue.String()
		switch reportSetting {
		case "true":
			appendReportFailureEnvVar(true)
		case "false":
			appendReportFailureEnvVar(false)
			shouldIncludeCategoryFilters = false
		default:
			envVars = append(envVars, buildTemplatableBoolEnvVar("GH_AW_FAILURE_REPORT_AS_ISSUE", templatableBoolPtrToStringPtr(data.SafeOutputs.ReportFailureAsIssue))...)
			shouldIncludeCategoryFilters = false
		}
		if shouldIncludeCategoryFilters {
			if len(data.SafeOutputs.ReportFailureAsIssueCategories) > 0 {
				if categoriesJSON, err := json.Marshal(data.SafeOutputs.ReportFailureAsIssueCategories); err == nil {
					envVars = append(envVars, fmt.Sprintf("          GH_AW_FAILURE_CATEGORIES_FILTER: %q\n", string(categoriesJSON)))
				}
			}
			if len(data.SafeOutputs.ReportFailureAsIssueExcludedCategories) > 0 {
				if excludedJSON, err := json.Marshal(data.SafeOutputs.ReportFailureAsIssueExcludedCategories); err == nil {
					envVars = append(envVars, fmt.Sprintf("          GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER: %q\n", string(excludedJSON)))
				}
			}
		}
	}
	if data.SafeOutputs.MissingTool != nil && data.SafeOutputs.MissingTool.ReportAsFailure != nil {
		envVars = append(envVars, buildTemplatableBoolEnvVar("GH_AW_MISSING_TOOL_REPORT_AS_FAILURE", data.SafeOutputs.MissingTool.ReportAsFailure)...)
	} else {
		envVars = append(envVars, "          GH_AW_MISSING_TOOL_REPORT_AS_FAILURE: \"true\"\n")
	}
	if data.SafeOutputs.MissingData != nil && data.SafeOutputs.MissingData.ReportAsFailure != nil {
		envVars = append(envVars, buildTemplatableBoolEnvVar("GH_AW_MISSING_DATA_REPORT_AS_FAILURE", data.SafeOutputs.MissingData.ReportAsFailure)...)
	} else {
		envVars = append(envVars, "          GH_AW_MISSING_DATA_REPORT_AS_FAILURE: \"true\"\n")
	}
	return envVars
}

// buildAgentFailureCacheMemoryVars appends optional failure-reporting extras.
func buildAgentFailureCacheMemoryVars(data *WorkflowData, mainJobName string) []string {
	var envVars []string
	if data.SafeOutputs.FailureIssueRepo != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_FAILURE_ISSUE_REPO: %q\n", data.SafeOutputs.FailureIssueRepo))
	}
	if timeoutValue := strings.TrimPrefix(data.TimeoutMinutes, "timeout-minutes: "); timeoutValue != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_TIMEOUT_MINUTES: %q\n", timeoutValue))
	}
	if data.CacheMemoryConfig == nil || len(data.CacheMemoryConfig.Caches) == 0 {
		return envVars
	}
	envVars = append(envVars, "          GH_AW_CACHE_MEMORY_ENABLED: \"true\"\n")
	for i := range data.CacheMemoryConfig.Caches {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_CACHE_MEMORY_RESTORE_%d_MATCHED_KEY: ${{ needs.%s.outputs.cache_memory_restore_%d_matched_key || '' }}\n", i, mainJobName, i))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_CACHE_MEMORY_RESTORE_%d_CACHE_HIT: ${{ needs.%s.outputs.cache_memory_restore_%d_cache_hit || 'false' }}\n", i, mainJobName, i))
	}
	return envVars
}

// buildAgentFailureStep builds the agent failure handler step.
func (c *Compiler) buildAgentFailureStep(data *WorkflowData, mainJobName, messagesJSON, steeringToken string) ([]string, error) {
	// Add agent failure handling step - creates/updates an issue when agent job fails
	// This step always runs and checks if the agent job failed
	// Build environment variables for the agent failure handler
	envVars, engine, err := c.buildAgentFailureCoreVars(data, mainJobName)
	if err != nil {
		return nil, err
	}
	envVars = append(envVars, buildAgentFailureEngineDetectionVars(engine, data, mainJobName)...)
	envVars = append(envVars, buildAgentFailureActivationStatusVars(data)...)
	if messagesJSON != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
	}
	envVars = append(envVars, buildAgentFailureRepoMemoryVars(data)...)
	envVars = append(envVars, buildAgentFailureReportingPolicyVars(data)...)
	envVars = append(envVars, buildAgentFailureCacheMemoryVars(data, mainJobName)...)
	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Handle agent failure",
		StepID:        "handle_agent_failure",
		MainJobName:   mainJobName,
		CustomEnvVars: envVars,
		Script:        "const { main } = require('${{ runner.temp }}/gh-aw/actions/handle_agent_failure.cjs'); await main();",
		ScriptFile:    "handle_agent_failure.cjs",
		CustomToken:   steeringToken,
		StepCondition: "always()",
	}), nil
}

// buildConclusionScriptEnvVars builds environment variables for the completion status script.
func (c *Compiler) buildConclusionScriptEnvVars(data *WorkflowData, mainJobName string, safeOutputJobNames []string, messagesJSON string) []string {
	// Build environment variables for the conclusion script
	var envVars []string
	envVars = append(envVars, fmt.Sprintf("          GH_AW_COMMENT_ID: ${{ needs.%s.outputs.comment_id }}\n", constants.ActivationJobName))
	envVars = append(envVars, fmt.Sprintf("          GH_AW_COMMENT_REPO: ${{ needs.%s.outputs.comment_repo }}\n", constants.ActivationJobName))
	envVars = append(envVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	envVars = append(envVars, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	if data.TrackerID != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_TRACKER_ID: %q\n", data.TrackerID))
	}
	envVars = append(envVars, fmt.Sprintf("          GH_AW_AGENT_CONCLUSION: ${{ needs.%s.result }}\n", mainJobName))
	if slices.Contains(safeOutputJobNames, string(constants.SafeOutputsJobName)) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUTS_RESULT: ${{ needs.%s.result }}\n", constants.SafeOutputsJobName))
		notifyCommentLog.Print("Added safe_outputs job result environment variable to conclusion job")
	}
	if IsDetectionJobEnabled(data.SafeOutputs) {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_DETECTION_CONCLUSION: ${{ needs.%s.outputs.detection_conclusion }}\n", constants.DetectionJobName))
		envVars = append(envVars, fmt.Sprintf("          GH_AW_DETECTION_REASON: ${{ needs.%s.outputs.detection_reason }}\n", constants.DetectionJobName))
		notifyCommentLog.Print("Added detection conclusion and reason environment variables to conclusion job")
	}
	if data.SafeOutputs.AssignToAgent != nil {
		envVars = append(envVars, "          GH_AW_ASSIGNMENT_ERROR_COUNT: ${{ needs.safe_outputs.outputs.assign_to_agent_assignment_error_count }}\n")
	}
	if messagesJSON != "" {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
	}
	if len(safeOutputJobNames) > 0 {
		if safeOutputJobsJSON, jobURLEnvVars := buildSafeOutputJobsEnvVars(safeOutputJobNames); safeOutputJobsJSON != "" {
			envVars = append(envVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_JOBS: %q\n", safeOutputJobsJSON))
			envVars = append(envVars, jobURLEnvVars...)
			notifyCommentLog.Printf("Added safe output jobs info for %d job(s)", len(safeOutputJobNames))
		}
	}
	return envVars
}

// buildConclusionJobCondition builds the condition guarding the conclusion job.
func (c *Compiler) buildConclusionJobCondition(data *WorkflowData, mainJobName string, safeOutputJobNames []string) ConditionNode {
	// Build the condition for this job:
	// 1. always() - run even if agent fails
	// 2. agent was activated (not skipped) OR an activation guardrail failed
	// 3. IF comment_id exists: add_comment job either doesn't exist OR hasn't created a comment yet
	alwaysFunc := BuildFunctionCall("always")
	agentNotSkipped := BuildNotEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.result", mainJobName)), BuildStringLiteral("skipped"))
	lockdownCheckFailed := BuildEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.lockdown_check_failed", constants.ActivationJobName)), BuildStringLiteral("true"))
	oauthTokenCheckFailed := BuildEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.oauth_token_check_failed", constants.ActivationJobName)), BuildStringLiteral("true"))
	staleLockFileFailed := BuildEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.stale_lock_file_failed", constants.ActivationJobName)), BuildStringLiteral("true"))
	activationGuardrailsFailed := BuildOr(lockdownCheckFailed, BuildOr(oauthTokenCheckFailed, staleLockFileFailed))
	// Only reference the secret_verification_result output when the activation job
	// actually declares it. The output is emitted only when the engine provides a
	// validate-secret step (see addActivationSecretValidationStep); referencing it
	// unconditionally produces a dangling needs.*.outputs.* expression that fails
	// actionlint in generated lock files.
	if engine, err := c.getAgenticEngine(data.AI); err == nil && EngineHasValidateSecretStep(engine, data) {
		secretVerificationFailed := BuildEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.secret_verification_result", constants.ActivationJobName)), BuildStringLiteral("failed"))
		activationGuardrailsFailed = BuildOr(activationGuardrailsFailed, secretVerificationFailed)
	}
	if isDockerSbxRuntime(data) {
		dockerSbxSecretsFailed := BuildEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.docker_sbx_secrets_result", constants.ActivationJobName)), BuildStringLiteral("failed"))
		activationGuardrailsFailed = BuildOr(activationGuardrailsFailed, dockerSbxSecretsFailed)
	}
	if hasMaxDailyAICGuardrail(data) {
		dailyAICExceeded := BuildEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.daily_ai_credits_exceeded", constants.ActivationJobName)), BuildStringLiteral("true"))
		activationGuardrailsFailed = BuildOr(activationGuardrailsFailed, dailyAICExceeded)
	}
	if len(data.SkillReferences) > 0 || len(data.Skills) > 0 {
		skillInstallFailureCount := BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.skill_install_failure_count", constants.ActivationJobName))
		skillInstallFailed := BuildAnd(
			BuildNotEquals(skillInstallFailureCount, BuildStringLiteral("")),
			BuildNotEquals(skillInstallFailureCount, BuildStringLiteral("0")),
		)
		activationGuardrailsFailed = BuildOr(activationGuardrailsFailed, skillInstallFailed)
	}
	if isSteeringIssueEnabled(data) {
		steeringIssueExists := BuildNotEquals(BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.steering_issue_number", constants.ActivationJobName)), BuildStringLiteral(""))
		activationGuardrailsFailed = BuildOr(activationGuardrailsFailed, steeringIssueExists)
	}
	condition := BuildAnd(alwaysFunc, BuildOr(agentNotSkipped, activationGuardrailsFailed))

	if slices.Contains(safeOutputJobNames, "add_comment") {
		return BuildAnd(condition, &NotNode{Child: BuildPropertyAccess("needs.add_comment.outputs.comment_id")})
	}
	return condition
}

// buildConclusionJobNeeds builds dependencies for the conclusion job.
func buildConclusionJobNeeds(data *WorkflowData, mainJobName string, safeOutputJobNames []string) []string {
	// Build dependencies - this job depends on all safe output jobs to ensure it runs last
	needs := append([]string{mainJobName, string(constants.ActivationJobName)}, safeOutputJobNames...)
	if IsDetectionJobEnabled(data.SafeOutputs) {
		needs = append(needs, string(constants.DetectionJobName))
		notifyCommentLog.Print("Added detection job dependency to conclusion job")
	}
	return needs
}

// buildConclusionJobOutputs builds outputs for the conclusion job.
func buildConclusionJobOutputs(data *WorkflowData) map[string]string {
	// Create outputs for the job (include noop and missing_tool outputs if configured)
	outputs := map[string]string{}
	if data.SafeOutputs.NoOp != nil {
		outputs["noop_message"] = "${{ steps.noop.outputs.noop_message }}"
	}
	if data.SafeOutputs.MissingTool != nil {
		outputs["tools_reported"] = "${{ steps.missing_tool.outputs.tools_reported }}"
		outputs["total_count"] = "${{ steps.missing_tool.outputs.total_count }}"
	}
	if data.SafeOutputs.ReportIncomplete != nil {
		outputs["incomplete_count"] = "${{ steps.report_incomplete.outputs.incomplete_count }}"
	}
	return outputs
}

// buildConclusionJobConcurrency builds workflow-specific concurrency config.
func (c *Compiler) buildConclusionJobConcurrency(data *WorkflowData) string {
	// Build concurrency config for the conclusion job using the workflow ID.
	// This prevents concurrent agents on the same workflow from interfering with each other.
	if data.WorkflowID == "" {
		return ""
	}
	group := "gh-aw-conclusion-" + data.WorkflowID
	if data.ConcurrencyJobDiscriminator != "" {
		notifyCommentLog.Printf("Appending job discriminator to conclusion job concurrency group: %s", data.ConcurrencyJobDiscriminator)
		group = fmt.Sprintf("%s-%s", group, data.ConcurrencyJobDiscriminator)
	}
	concurrencyValue := fmt.Sprintf("concurrency:\n  group: %q\n  cancel-in-progress: false", group)
	if isGroupConcurrencyQueueEnabled(data) {
		concurrencyValue += "\n  queue: max"
	}
	notifyCommentLog.Printf("Configuring conclusion job concurrency group: %s", group)
	return c.indentYAMLLines(concurrencyValue, "    ")
}

// buildConclusionReportFailedJobsStep builds the step that queries the workflow run's jobs,
// identifies failed non-builtin jobs, and creates a failure issue for them.
// Returns nil when report-failed-jobs is explicitly set to false.
func (c *Compiler) buildConclusionReportFailedJobsStep(data *WorkflowData, mainJobName string) []string {
	// Skip when explicitly disabled via frontmatter
	if data.SafeOutputs != nil && data.SafeOutputs.ReportFailedJobs != nil && !*data.SafeOutputs.ReportFailedJobs {
		notifyCommentLog.Print("Skipping report-failed-jobs step: disabled in frontmatter")
		return nil
	}
	var envVars []string
	envVars = append(envVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID, buildLocalWorkflowSourceURL(c.markdownPath))...)
	envVars = append(envVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	if data.SafeOutputs != nil && data.SafeOutputs.ReportFailedJobs != nil {
		envVars = append(envVars, fmt.Sprintf("          GH_AW_REPORT_FAILED_JOBS: %q\n", strconv.FormatBool(*data.SafeOutputs.ReportFailedJobs)))
	} else {
		envVars = append(envVars, "          GH_AW_REPORT_FAILED_JOBS: \"true\"\n")
	}
	return c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Report failed jobs",
		StepID:        "report_failed_jobs",
		MainJobName:   mainJobName,
		CustomEnvVars: envVars,
		ScriptFile:    "report_failed_jobs.cjs",
		StepCondition: "always()",
	})
}

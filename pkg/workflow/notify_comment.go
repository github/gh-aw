package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var notifyCommentLog = logger.New("workflow:notify_comment")

// buildConclusionJob creates a job that updates the activation comment with workflow completion status
// This job is only generated when both add-comment and ai-reaction are configured.
// This job runs when:
// 1. always() - runs even if agent fails
// 2. A comment was created in activation job (comment_id exists)
// 3. NO add_comment output was produced by the agent
// 4. NO create_pull_request output was produced by the agent
// This job depends on all safe output jobs to ensure it runs last
func (c *Compiler) buildConclusionJob(data *WorkflowData, mainJobName string, safeOutputJobNames []string) (*Job, error) {
	notifyCommentLog.Printf("Building conclusion job: main_job=%s, safe_output_jobs_count=%d", mainJobName, len(safeOutputJobNames))

	// Create this job when:
	// 1. Safe outputs are configured (because noop is always enabled as a fallback)
	// The job will:
	// - Update activation comment with noop messages (if comment exists)
	// - Write noop messages to step summary (if no comment)

	hasAddComment := data.SafeOutputs != nil && data.SafeOutputs.AddComments != nil
	hasCommand := len(data.Command) > 0
	hasNoOp := data.SafeOutputs != nil && data.SafeOutputs.NoOp != nil
	hasReaction := data.AIReaction != "" && data.AIReaction != "none"
	hasSafeOutputs := data.SafeOutputs != nil

	notifyCommentLog.Printf("Configuration checks: has_add_comment=%t, has_command=%t, has_noop=%t, has_reaction=%t, has_safe_outputs=%t", hasAddComment, hasCommand, hasNoOp, hasReaction, hasSafeOutputs)

	// Always create this job when safe-outputs exist (because noop is always enabled)
	// This ensures noop messages can be handled even without reactions
	if !hasSafeOutputs {
		notifyCommentLog.Printf("Skipping job: no safe-outputs configured")
		return nil, nil // No safe-outputs configured, no need for conclusion job
	}

	// Build the job steps
	var steps []string

	// Add setup step to copy scripts
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Notify comment job doesn't need project support
		steps = append(steps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)
	}

	// Add GitHub App token minting step if app is configured
	if data.SafeOutputs.App != nil {
		// Use permissions for the conclusion job
		permissions := NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite()
		steps = append(steps, c.buildGitHubAppTokenMintStep(data.SafeOutputs.App, permissions)...)
	}

	// Add debug step
	steps = append(steps, "      - name: Debug job inputs\n")
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          COMMENT_ID: ${{ needs.%s.outputs.comment_id }}\n", constants.ActivationJobName))
	steps = append(steps, fmt.Sprintf("          COMMENT_REPO: ${{ needs.%s.outputs.comment_repo }}\n", constants.ActivationJobName))
	steps = append(steps, fmt.Sprintf("          AGENT_OUTPUT_TYPES: ${{ needs.%s.outputs.output_types }}\n", mainJobName))
	steps = append(steps, fmt.Sprintf("          AGENT_CONCLUSION: ${{ needs.%s.result }}\n", mainJobName))
	steps = append(steps, "        run: |\n")
	steps = append(steps, "          echo \"Comment ID: $COMMENT_ID\"\n")
	steps = append(steps, "          echo \"Comment Repo: $COMMENT_REPO\"\n")
	steps = append(steps, "          echo \"Agent Output Types: $AGENT_OUTPUT_TYPES\"\n")
	steps = append(steps, "          echo \"Agent Conclusion: $AGENT_CONCLUSION\"\n")

	// Add artifact download steps once (shared by noop and conclusion steps)
	steps = append(steps, buildAgentOutputDownloadSteps()...)

	// Add noop processing step if noop is configured
	if data.SafeOutputs.NoOp != nil {
		// Build custom environment variables specific to noop
		var noopEnvVars []string
		if data.SafeOutputs.NoOp.Max > 0 {
			noopEnvVars = append(noopEnvVars, fmt.Sprintf("          GH_AW_NOOP_MAX: %d\n", data.SafeOutputs.NoOp.Max))
		}

		// Add workflow metadata for consistency
		noopEnvVars = append(noopEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)

		// Build the noop processing step (without artifact downloads - already added above)
		noopSteps := c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
			StepName:      "Process No-Op Messages",
			StepID:        "noop",
			MainJobName:   mainJobName,
			CustomEnvVars: noopEnvVars,
			Script:        getNoOpScript(),
			ScriptFile:    "noop.cjs",
			Token:         data.SafeOutputs.NoOp.GitHubToken,
		})
		steps = append(steps, noopSteps...)
	}

	// Add missing_tool processing step if missing-tool is configured
	if data.SafeOutputs.MissingTool != nil {
		// Build custom environment variables specific to missing-tool
		var missingToolEnvVars []string
		if data.SafeOutputs.MissingTool.Max > 0 {
			missingToolEnvVars = append(missingToolEnvVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_MAX: %d\n", data.SafeOutputs.MissingTool.Max))
		}

		// Add create-issue configuration
		if data.SafeOutputs.MissingTool.CreateIssue {
			missingToolEnvVars = append(missingToolEnvVars, "          GH_AW_MISSING_TOOL_CREATE_ISSUE: \"true\"\n")
		}

		// Add title-prefix configuration
		if data.SafeOutputs.MissingTool.TitlePrefix != "" {
			missingToolEnvVars = append(missingToolEnvVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_TITLE_PREFIX: %q\n", data.SafeOutputs.MissingTool.TitlePrefix))
		}

		// Add labels configuration
		if len(data.SafeOutputs.MissingTool.Labels) > 0 {
			labelsJSON, err := json.Marshal(data.SafeOutputs.MissingTool.Labels)
			if err == nil {
				missingToolEnvVars = append(missingToolEnvVars, fmt.Sprintf("          GH_AW_MISSING_TOOL_LABELS: %q\n", string(labelsJSON)))
			}
		}

		// Add workflow metadata for consistency
		missingToolEnvVars = append(missingToolEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)

		// Build the missing_tool processing step (without artifact downloads - already added above)
		missingToolSteps := c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
			StepName:      "Record Missing Tool",
			StepID:        "missing_tool",
			MainJobName:   mainJobName,
			CustomEnvVars: missingToolEnvVars,
			Script:        "const { main } = require('/opt/gh-aw/actions/missing_tool.cjs'); await main();",
			ScriptFile:    "missing_tool.cjs",
			Token:         data.SafeOutputs.MissingTool.GitHubToken,
		})
		steps = append(steps, missingToolSteps...)
	}

	// Add agent failure handling step - creates/updates an issue when agent job fails
	// This step always runs and checks if the agent job failed
	// Build environment variables for the agent failure handler
	var agentFailureEnvVars []string
	agentFailureEnvVars = append(agentFailureEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)
	agentFailureEnvVars = append(agentFailureEnvVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	agentFailureEnvVars = append(agentFailureEnvVars, fmt.Sprintf("          GH_AW_AGENT_CONCLUSION: ${{ needs.%s.result }}\n", mainJobName))

	// Only add secret_verification_result if the engine adds the validate-secret step
	// The validate-secret step is only added by engines that include it in GetInstallationSteps()
	engine, err := c.getAgenticEngine(data.AI)
	if err != nil {
		return nil, fmt.Errorf("failed to get agentic engine: %w", err)
	}
	if EngineHasValidateSecretStep(engine, data) {
		agentFailureEnvVars = append(agentFailureEnvVars, fmt.Sprintf("          GH_AW_SECRET_VERIFICATION_RESULT: ${{ needs.%s.outputs.secret_verification_result }}\n", mainJobName))
	}

	// Add checkout_pr_success to detect PR checkout failures (e.g., PR merged and branch deleted)
	agentFailureEnvVars = append(agentFailureEnvVars, fmt.Sprintf("          GH_AW_CHECKOUT_PR_SUCCESS: ${{ needs.%s.outputs.checkout_pr_success }}\n", mainJobName))

	// Pass assignment error outputs from safe_outputs job if assign-to-agent is configured
	if data.SafeOutputs != nil && data.SafeOutputs.AssignToAgent != nil {
		agentFailureEnvVars = append(agentFailureEnvVars, "          GH_AW_ASSIGNMENT_ERRORS: ${{ needs.safe_outputs.outputs.assign_to_agent_assignment_errors }}\n")
		agentFailureEnvVars = append(agentFailureEnvVars, "          GH_AW_ASSIGNMENT_ERROR_COUNT: ${{ needs.safe_outputs.outputs.assign_to_agent_assignment_error_count }}\n")
	}

	// Pass custom messages config if present
	if data.SafeOutputs != nil && data.SafeOutputs.Messages != nil {
		messagesJSON, err := serializeMessagesConfig(data.SafeOutputs.Messages)
		if err != nil {
			notifyCommentLog.Printf("Warning: failed to serialize messages config for agent failure handler: %v", err)
		} else if messagesJSON != "" {
			agentFailureEnvVars = append(agentFailureEnvVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
		}
	}

	// Build the agent failure handling step
	agentFailureSteps := c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Handle Agent Failure",
		StepID:        "handle_agent_failure",
		MainJobName:   mainJobName,
		CustomEnvVars: agentFailureEnvVars,
		Script:        "const { main } = require('/opt/gh-aw/actions/handle_agent_failure.cjs'); await main();",
		ScriptFile:    "handle_agent_failure.cjs",
		Token:         "", // Will use default GITHUB_TOKEN
	})
	steps = append(steps, agentFailureSteps...)

	// Add create_pull_request error handling step if create-pull-request is configured
	if data.SafeOutputs != nil && data.SafeOutputs.CreatePullRequests != nil {
		// Build environment variables for the create PR error handler
		var createPRErrorEnvVars []string
		// Note: With consolidated safe outputs, individual handler errors are not exposed as outputs.
		// The error handler script will skip gracefully if CREATE_PR_ERROR_MESSAGE is not set.
		createPRErrorEnvVars = append(createPRErrorEnvVars, buildWorkflowMetadataEnvVarsWithTrackerID(data.Name, data.Source, data.TrackerID)...)
		createPRErrorEnvVars = append(createPRErrorEnvVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")

		// Build the create PR error handling step
		createPRErrorSteps := c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
			StepName:      "Handle Create Pull Request Error",
			StepID:        "handle_create_pr_error",
			MainJobName:   mainJobName,
			CustomEnvVars: createPRErrorEnvVars,
			Script:        "const { main } = require('/opt/gh-aw/actions/handle_create_pr_error.cjs'); await main();",
			ScriptFile:    "handle_create_pr_error.cjs",
			Token:         "", // Will use default GITHUB_TOKEN
		})
		steps = append(steps, createPRErrorSteps...)
	}

	// Build environment variables for the conclusion script
	var customEnvVars []string
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_COMMENT_ID: ${{ needs.%s.outputs.comment_id }}\n", constants.ActivationJobName))
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_COMMENT_REPO: ${{ needs.%s.outputs.comment_repo }}\n", constants.ActivationJobName))
	customEnvVars = append(customEnvVars, "          GH_AW_RUN_URL: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}\n")
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))
	// Pass the tracker-id if present
	if data.TrackerID != "" {
		customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_TRACKER_ID: %q\n", data.TrackerID))
	}
	customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_AGENT_CONCLUSION: ${{ needs.%s.result }}\n", mainJobName))

	// Pass detection conclusion if threat detection is enabled
	if data.SafeOutputs.ThreatDetection != nil {
		customEnvVars = append(customEnvVars, "          GH_AW_DETECTION_CONCLUSION: ${{ needs.detection.result }}\n")
		notifyCommentLog.Print("Added detection conclusion environment variable to conclusion job")
	}

	// Pass custom messages config if present
	if data.SafeOutputs != nil && data.SafeOutputs.Messages != nil {
		messagesJSON, err := serializeMessagesConfig(data.SafeOutputs.Messages)
		if err != nil {
			notifyCommentLog.Printf("Warning: failed to serialize messages config: %v", err)
		} else if messagesJSON != "" {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
		}
	}

	// Pass safe output job information for link generation
	if len(safeOutputJobNames) > 0 {
		safeOutputJobsJSON, jobURLEnvVars := buildSafeOutputJobsEnvVars(safeOutputJobNames)
		if safeOutputJobsJSON != "" {
			customEnvVars = append(customEnvVars, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_JOBS: %q\n", safeOutputJobsJSON))
			customEnvVars = append(customEnvVars, jobURLEnvVars...)
			notifyCommentLog.Printf("Added safe output jobs info for %d job(s)", len(safeOutputJobNames))
		}
	}

	// Get token from config
	var token string
	if data.SafeOutputs != nil && data.SafeOutputs.AddComments != nil {
		token = data.SafeOutputs.AddComments.GitHubToken
	}

	// Build the conclusion GitHub Script step (without artifact downloads - already added above)
	scriptSteps := c.buildGitHubScriptStepWithoutDownload(data, GitHubScriptStepConfig{
		StepName:      "Update reaction comment with completion status",
		StepID:        "conclusion",
		MainJobName:   mainJobName,
		CustomEnvVars: customEnvVars,
		Script:        getNotifyCommentErrorScript(),
		ScriptFile:    "notify_comment_error.cjs",
		Token:         token,
	})
	steps = append(steps, scriptSteps...)

	// Add unlock step if lock-for-agent is enabled
	if data.LockForAgent {
		// Build condition: only unlock if issue was locked by activation job
		// Must match lock condition: event type is 'issues' or 'issue_comment'
		// Use the issue_locked output from activation job to determine if unlock is needed
		eventTypeCheck := BuildOr(
			BuildEventTypeEquals("issues"),
			BuildEventTypeEquals("issue_comment"),
		)
		lockedOutputCheck := BuildEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.issue_locked", constants.ActivationJobName)),
			BuildStringLiteral("true"),
		)

		unlockCondition := BuildAnd(
			BuildFunctionCall("always"), // Always run, even on failure
			BuildAnd(eventTypeCheck, lockedOutputCheck),
		)

		steps = append(steps, "      - name: Unlock issue after agent workflow\n")
		steps = append(steps, "        id: unlock-issue\n")
		steps = append(steps, fmt.Sprintf("        if: %s\n", unlockCondition.Render()))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("unlock-issue.cjs"))

		notifyCommentLog.Print("Added unlock issue step to conclusion job")
	}

	// Add GitHub App token invalidation step if app is configured
	if data.SafeOutputs.App != nil {
		notifyCommentLog.Print("Adding GitHub App token invalidation step to conclusion job")
		steps = append(steps, c.buildGitHubAppTokenInvalidationStep()...)
	}

	// Build the condition for this job:
	// 1. always() - run even if agent fails
	// 2. agent was activated (not skipped)
	// 3. IF comment_id exists: add_comment job either doesn't exist OR hasn't created a comment yet
	//
	// Note: The job should always run to handle noop messages (either update comment or write to summary)
	// The script (notify_comment_error.cjs) handles the case where there's no comment gracefully

	alwaysFunc := BuildFunctionCall("always")

	// Check that agent job was activated (not skipped)
	agentNotSkipped := BuildNotEquals(
		BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
		BuildStringLiteral("skipped"),
	)

	// Check if add_comment job exists in the safe output jobs
	hasAddCommentJob := false
	for _, jobName := range safeOutputJobNames {
		if jobName == "add_comment" {
			hasAddCommentJob = true
			break
		}
	}

	// Build the condition based on whether add_comment job exists
	var condition ConditionNode
	if hasAddCommentJob {
		// If add_comment job exists, also check that it hasn't already created a comment
		// This prevents duplicate updates when add_comment has already updated the activation comment
		noAddCommentOutput := &NotNode{
			Child: BuildPropertyAccess("needs.add_comment.outputs.comment_id"),
		}
		condition = BuildAnd(
			BuildAnd(alwaysFunc, agentNotSkipped),
			noAddCommentOutput,
		)
	} else {
		// If add_comment job doesn't exist, just check the basic conditions
		condition = BuildAnd(alwaysFunc, agentNotSkipped)
	}

	// Build dependencies - this job depends on all safe output jobs to ensure it runs last
	needs := []string{mainJobName, string(constants.ActivationJobName)}
	needs = append(needs, safeOutputJobNames...)

	// Add detection job to dependencies if threat detection is enabled
	if data.SafeOutputs.ThreatDetection != nil {
		needs = append(needs, "detection")
		notifyCommentLog.Print("Added detection job to conclusion dependencies")
	}

	notifyCommentLog.Printf("Job built successfully: dependencies_count=%d", len(needs))

	// Create outputs for the job (include noop and missing_tool outputs if configured)
	outputs := map[string]string{}
	if data.SafeOutputs.NoOp != nil {
		outputs["noop_message"] = "${{ steps.noop.outputs.noop_message }}"
	}
	if data.SafeOutputs.MissingTool != nil {
		outputs["tools_reported"] = "${{ steps.missing_tool.outputs.tools_reported }}"
		outputs["total_count"] = "${{ steps.missing_tool.outputs.total_count }}"
	}

	job := &Job{
		Name:        "conclusion",
		If:          condition.Render(),
		RunsOn:      c.formatSafeOutputsRunsOn(data.SafeOutputs),
		Permissions: NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite().RenderToYAML(),
		Steps:       steps,
		Needs:       needs,
		Outputs:     outputs,
	}

	return job, nil
}

// buildSafeOutputJobsEnvVars creates environment variables for safe output job URLs
// Returns both a JSON mapping and the actual environment variable declarations
func buildSafeOutputJobsEnvVars(jobNames []string) (string, []string) {
	// Map job names to their expected URL output keys
	jobOutputMapping := make(map[string]string)
	var envVars []string

	for _, jobName := range jobNames {
		var urlKey string
		switch jobName {
		case "create_issue":
			urlKey = "issue_url"
		case "add_comment":
			urlKey = "comment_url"
		case "create_pull_request":
			urlKey = "pull_request_url"
		case "create_discussion":
			urlKey = "discussion_url"
		case "create_pr_review_comment":
			urlKey = "review_comment_url"
		case "close_issue":
			urlKey = "issue_url"
		case "close_pull_request":
			urlKey = "pull_request_url"
		case "close_discussion":
			urlKey = "discussion_url"
		case "create_agent_session":
			urlKey = "task_url"
		case "push_to_pull_request_branch":
			urlKey = "commit_url"
		default:
			// Skip jobs that don't have URL outputs
			continue
		}

		jobOutputMapping[jobName] = urlKey

		// Add environment variable for this job's URL output
		envVarName := fmt.Sprintf("GH_AW_OUTPUT_%s_%s",
			toEnvVarCase(jobName),
			toEnvVarCase(urlKey))
		envVars = append(envVars,
			fmt.Sprintf("          %s: ${{ needs.%s.outputs.%s }}\n",
				envVarName, jobName, urlKey))
	}

	if len(jobOutputMapping) == 0 {
		return "", nil
	}

	jsonBytes, err := json.Marshal(jobOutputMapping)
	if err != nil {
		notifyCommentLog.Printf("Warning: failed to marshal safe output jobs info: %v", err)
		return "", nil
	}

	return string(jsonBytes), envVars
}

// toEnvVarCase converts a string to uppercase environment variable case
func toEnvVarCase(s string) string {
	// Convert to uppercase and keep underscores
	result := ""
	for _, ch := range s {
		if ch >= 'a' && ch <= 'z' {
			result += string(ch - 32) // Convert to uppercase
		} else if ch >= 'A' && ch <= 'Z' {
			result += string(ch)
		} else if ch == '_' {
			result += "_"
		}
	}
	return result
}

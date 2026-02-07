package workflow

import (
	"fmt"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compilerActivationJobLog = logger.New("workflow:compiler_activation_job")

// buildActivationJob creates the activation job that handles timestamp checking, reactions, and locking.
// This job depends on the pre-activation job if it exists, and runs before the main agent job.
func (c *Compiler) buildActivationJob(data *WorkflowData, preActivationJobCreated bool, workflowRunRepoSafety string, lockFilename string) (*Job, error) {
	outputs := map[string]string{}
	var steps []string

	// Team member check is now handled by the separate check_membership job
	// No inline role checks needed in the task job anymore

	// Add setup step to copy activation scripts (required - no inline fallback)
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef == "" {
		return nil, fmt.Errorf("setup action reference is required but could not be resolved")
	}

	// For dev mode (local action path), checkout the actions folder first
	steps = append(steps, c.generateCheckoutActionsFolder(data)...)

	// Activation job doesn't need project support (no safe outputs processed here)
	steps = append(steps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)

	// Add timestamp check for lock file vs source file using GitHub API
	// No checkout step needed - uses GitHub API to check commit times
	steps = append(steps, "      - name: Check workflow file timestamps\n")
	steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
	steps = append(steps, "        env:\n")
	steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_FILE: \"%s\"\n", lockFilename))
	steps = append(steps, "        with:\n")
	steps = append(steps, "          script: |\n")
	steps = append(steps, generateGitHubScriptWithRequire("check_workflow_timestamp_api.cjs"))

	// Use inlined compute-text script only if needed (no shared action)
	if data.NeedsTextOutput {
		steps = append(steps, "      - name: Compute current body text\n")
		steps = append(steps, "        id: compute-text\n")
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("compute_text.cjs"))

		// Set up outputs
		outputs["text"] = "${{ steps.compute-text.outputs.text }}"
	}

	// Add comment with workflow run link if ai-reaction is configured and not "none"
	// Note: The reaction was already added in the pre-activation job for immediate feedback
	if data.AIReaction != "" && data.AIReaction != "none" {
		reactionCondition := BuildReactionCondition()

		steps = append(steps, "      - name: Add comment with workflow run link\n")
		steps = append(steps, "        id: add-comment\n")
		steps = append(steps, fmt.Sprintf("        if: %s\n", reactionCondition.Render()))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

		// Add environment variables
		steps = append(steps, "        env:\n")
		steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", data.Name))

		// Add tracker-id if present
		if data.TrackerID != "" {
			steps = append(steps, fmt.Sprintf("          GH_AW_TRACKER_ID: %q\n", data.TrackerID))
		}

		// Add lock-for-agent status if enabled
		if data.LockForAgent {
			steps = append(steps, "          GH_AW_LOCK_FOR_AGENT: \"true\"\n")
		}

		// Pass custom messages config if present (for custom run-started messages)
		if data.SafeOutputs != nil && data.SafeOutputs.Messages != nil {
			messagesJSON, err := serializeMessagesConfig(data.SafeOutputs.Messages)
			if err != nil {
				compilerActivationJobLog.Printf("Warning: failed to serialize messages config for activation job: %v", err)
			} else if messagesJSON != "" {
				steps = append(steps, fmt.Sprintf("          GH_AW_SAFE_OUTPUT_MESSAGES: %q\n", messagesJSON))
			}
		}

		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("add_workflow_run_comment.cjs"))

		// Add comment outputs (no reaction_id since reaction was added in pre-activation)
		outputs["comment_id"] = "${{ steps.add-comment.outputs.comment-id }}"
		outputs["comment_url"] = "${{ steps.add-comment.outputs.comment-url }}"
		outputs["comment_repo"] = "${{ steps.add-comment.outputs.comment-repo }}"
	}

	// Add lock step if lock-for-agent is enabled
	if data.LockForAgent {
		// Build condition: only lock if event type is 'issues' or 'issue_comment'
		// lock-for-agent can be configured under on.issues or on.issue_comment
		// For issue_comment events, context.issue.number automatically resolves to the parent issue
		lockCondition := BuildOr(
			BuildEventTypeEquals("issues"),
			BuildEventTypeEquals("issue_comment"),
		)

		steps = append(steps, "      - name: Lock issue for agent workflow\n")
		steps = append(steps, "        id: lock-issue\n")
		steps = append(steps, fmt.Sprintf("        if: %s\n", lockCondition.Render()))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("lock-issue.cjs"))

		// Add output for tracking if issue was locked
		outputs["issue_locked"] = "${{ steps.lock-issue.outputs.locked }}"

		// Add lock message to reaction comment if reaction is enabled
		if data.AIReaction != "" && data.AIReaction != "none" {
			compilerActivationJobLog.Print("Adding lock notification to reaction message")
		}
	}

	// Always declare comment_id and comment_repo outputs to avoid actionlint errors
	// These will be empty if no reaction is configured, and the scripts handle empty values gracefully
	// Use plain empty strings (quoted) to avoid triggering security scanners like zizmor
	if _, exists := outputs["comment_id"]; !exists {
		outputs["comment_id"] = `""`
	}
	if _, exists := outputs["comment_repo"]; !exists {
		outputs["comment_repo"] = `""`
	}

	// Add slash_command output if this is a command workflow
	// This output contains the matched command name from check_command_position step
	if len(data.Command) > 0 {
		if preActivationJobCreated {
			// Reference the matched_command output from pre_activation job
			outputs["slash_command"] = fmt.Sprintf("${{ needs.%s.outputs.%s }}", string(constants.PreActivationJobName), constants.MatchedCommandOutput)
		} else {
			// Fallback to steps reference if pre_activation doesn't exist (shouldn't happen for command workflows)
			outputs["slash_command"] = fmt.Sprintf("${{ steps.%s.outputs.%s }}", constants.CheckCommandPositionStepID, constants.MatchedCommandOutput)
		}
	}

	// If no steps have been added, add a placeholder step to make the job valid
	// This can happen when the activation job is created only for an if condition
	if len(steps) == 0 {
		steps = append(steps, "      - run: echo \"Activation success\"\n")
	}

	// Build the conditional expression that validates activation status and other conditions
	var activationNeeds []string
	var activationCondition string

	// Find custom jobs that depend on pre_activation - these run before activation
	customJobsBeforeActivation := c.getCustomJobsDependingOnPreActivation(data.Jobs)

	if preActivationJobCreated {
		// Activation job depends on pre-activation job and checks the "activated" output
		activationNeeds = []string{string(constants.PreActivationJobName)}

		// Also depend on custom jobs that run after pre_activation but before activation
		activationNeeds = append(activationNeeds, customJobsBeforeActivation...)

		activatedExpr := BuildEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.outputs.%s", string(constants.PreActivationJobName), constants.ActivatedOutput)),
			BuildStringLiteral("true"),
		)

		// If there are custom jobs before activation and the if condition references them,
		// include that condition in the activation job's if clause
		if data.If != "" && c.referencesCustomJobOutputs(data.If, data.Jobs) && len(customJobsBeforeActivation) > 0 {
			// Include the custom job output condition in the activation job
			unwrappedIf := stripExpressionWrapper(data.If)
			ifExpr := &ExpressionNode{Expression: unwrappedIf}
			combinedExpr := BuildAnd(activatedExpr, ifExpr)
			activationCondition = combinedExpr.Render()
		} else if data.If != "" && !c.referencesCustomJobOutputs(data.If, data.Jobs) {
			// Include user's if condition that doesn't reference custom jobs
			unwrappedIf := stripExpressionWrapper(data.If)
			ifExpr := &ExpressionNode{Expression: unwrappedIf}
			combinedExpr := BuildAnd(activatedExpr, ifExpr)
			activationCondition = combinedExpr.Render()
		} else {
			activationCondition = activatedExpr.Render()
		}
	} else {
		// No pre-activation check needed
		// Add custom jobs that would run before activation as dependencies
		activationNeeds = append(activationNeeds, customJobsBeforeActivation...)

		if data.If != "" && c.referencesCustomJobOutputs(data.If, data.Jobs) && len(customJobsBeforeActivation) > 0 {
			// Include the custom job output condition
			activationCondition = data.If
		} else if !c.referencesCustomJobOutputs(data.If, data.Jobs) {
			activationCondition = data.If
		}
	}

	// Apply workflow_run repository safety check exclusively to activation job
	// This check is combined with any existing activation condition
	if workflowRunRepoSafety != "" {
		activationCondition = c.combineJobIfConditions(activationCondition, workflowRunRepoSafety)
	}

	// Set permissions - activation job always needs contents:read for GitHub API access
	// Also add reaction permissions if reaction is configured and not "none"
	// Also add issues:write permission if lock-for-agent is enabled (for locking issues)
	permsMap := map[PermissionScope]PermissionLevel{
		PermissionContents: PermissionRead, // Always needed for GitHub API access to check file commits
	}

	if data.AIReaction != "" && data.AIReaction != "none" {
		permsMap[PermissionDiscussions] = PermissionWrite
		permsMap[PermissionIssues] = PermissionWrite
		permsMap[PermissionPullRequests] = PermissionWrite
	}

	// Add issues:write permission if lock-for-agent is enabled (even without reaction)
	if data.LockForAgent {
		permsMap[PermissionIssues] = PermissionWrite
	}

	perms := NewPermissionsFromMap(permsMap)
	permissions := perms.RenderToYAML()

	// Set environment if manual-approval is configured
	var environment string
	if data.ManualApproval != "" {
		// Strip ANSI escape codes from manual-approval environment name
		cleanManualApproval := stringutil.StripANSIEscapeCodes(data.ManualApproval)
		environment = fmt.Sprintf("environment: %s", cleanManualApproval)
	}

	job := &Job{
		Name:                       string(constants.ActivationJobName),
		If:                         activationCondition,
		HasWorkflowRunSafetyChecks: workflowRunRepoSafety != "", // Mark job as having workflow_run safety checks
		RunsOn:                     c.formatSafeOutputsRunsOn(data.SafeOutputs),
		Permissions:                permissions,
		Environment:                environment,
		Steps:                      steps,
		Outputs:                    outputs,
		Needs:                      activationNeeds, // Depend on pre-activation job if it exists
	}

	return job, nil
}

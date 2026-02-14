package workflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var compilerActivationJobsLog = logger.New("workflow:compiler_activation_jobs")

// buildPreActivationJob creates a unified pre-activation job that combines membership checks and stop-time validation.
// This job exposes a single "activated" output that indicates whether the workflow should proceed.
func (c *Compiler) buildPreActivationJob(data *WorkflowData, needsPermissionCheck bool) (*Job, error) {
	compilerActivationJobsLog.Printf("Building pre-activation job: needsPermissionCheck=%v, hasStopTime=%v", needsPermissionCheck, data.StopTime != "")
	var steps []string
	var permissions string

	// Extract custom steps and outputs from jobs.pre-activation if present
	customSteps, customOutputs, err := c.extractPreActivationCustomFields(data.Jobs)
	if err != nil {
		return nil, fmt.Errorf("failed to extract pre-activation custom fields: %w", err)
	}

	// Add setup step to copy activation scripts (required - no inline fallback)
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef == "" {
		return nil, fmt.Errorf("setup action reference is required but could not be resolved")
	}

	// For dev mode (local action path), checkout the actions folder first
	// This requires contents: read permission
	steps = append(steps, c.generateCheckoutActionsFolder(data)...)
	needsContentsRead := (c.actionMode.IsDev() || c.actionMode.IsScript()) && len(c.generateCheckoutActionsFolder(data)) > 0

	// Pre-activation job doesn't need project support (no safe outputs processed here)
	steps = append(steps, c.generateSetupStep(setupActionRef, SetupActionDestination, false)...)

	// Determine permissions for pre-activation job
	var perms *Permissions
	if needsContentsRead {
		perms = NewPermissionsContentsRead()
	}

	// Add reaction permissions if reaction is configured (reactions added in pre-activation for immediate feedback)
	if data.AIReaction != "" && data.AIReaction != "none" {
		if perms == nil {
			perms = NewPermissions()
		}
		// Add write permissions for reactions
		perms.Set(PermissionIssues, PermissionWrite)
		perms.Set(PermissionPullRequests, PermissionWrite)
		perms.Set(PermissionDiscussions, PermissionWrite)
	}

	// Add actions: read permission if rate limiting is configured (needed to query workflow runs)
	if data.RateLimit != nil {
		if perms == nil {
			perms = NewPermissions()
		}
		perms.Set(PermissionActions, PermissionRead)
	}

	// Set permissions if any were configured
	if perms != nil {
		permissions = perms.RenderToYAML()
	}

	// Add reaction step immediately after setup for instant user feedback
	// This happens BEFORE any checks, so users see progress immediately
	if data.AIReaction != "" && data.AIReaction != "none" {
		reactionCondition := BuildReactionCondition()

		steps = append(steps, fmt.Sprintf("      - name: Add %s reaction for immediate feedback\n", data.AIReaction))
		steps = append(steps, "        id: react\n")
		steps = append(steps, fmt.Sprintf("        if: %s\n", reactionCondition.Render()))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))

		// Add environment variables
		steps = append(steps, "        env:\n")
		// Quote the reaction value to prevent YAML interpreting +1/-1 as integers
		steps = append(steps, fmt.Sprintf("          GH_AW_REACTION: %q\n", data.AIReaction))

		steps = append(steps, "        with:\n")
		// Explicitly use the GitHub Actions token (GITHUB_TOKEN) for reactions
		// This ensures proper authentication for adding reactions
		steps = append(steps, "          github-token: ${{ secrets.GITHUB_TOKEN }}\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("add_reaction.cjs"))
	}

	// Add team member check if permission checks are needed
	if needsPermissionCheck {
		steps = c.generateMembershipCheck(data, steps)
	}

	// Add rate limit check if configured
	if data.RateLimit != nil {
		steps = c.generateRateLimitCheck(data, steps)
	}

	// Add stop-time check if configured
	if data.StopTime != "" {
		// Extract workflow name for the stop-time check
		workflowName := data.Name

		steps = append(steps, "      - name: Check stop-time limit\n")
		steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckStopTimeStepID))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        env:\n")
		// Strip ANSI escape codes from stop-time value
		cleanStopTime := stringutil.StripANSIEscapeCodes(data.StopTime)
		steps = append(steps, fmt.Sprintf("          GH_AW_STOP_TIME: %s\n", cleanStopTime))
		steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", workflowName))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("check_stop_time.cjs"))
	}

	// Add skip-if-match check if configured
	if data.SkipIfMatch != nil {
		// Extract workflow name for the skip-if-match check
		workflowName := data.Name

		steps = append(steps, "      - name: Check skip-if-match query\n")
		steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipIfMatchStepID))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        env:\n")
		steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_QUERY: %q\n", data.SkipIfMatch.Query))
		steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", workflowName))
		steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_MAX_MATCHES: \"%d\"\n", data.SkipIfMatch.Max))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("check_skip_if_match.cjs"))
	}

	// Add skip-if-no-match check if configured
	if data.SkipIfNoMatch != nil {
		// Extract workflow name for the skip-if-no-match check
		workflowName := data.Name

		steps = append(steps, "      - name: Check skip-if-no-match query\n")
		steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckSkipIfNoMatchStepID))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        env:\n")
		steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_QUERY: %q\n", data.SkipIfNoMatch.Query))
		steps = append(steps, fmt.Sprintf("          GH_AW_WORKFLOW_NAME: %q\n", workflowName))
		steps = append(steps, fmt.Sprintf("          GH_AW_SKIP_MIN_MATCHES: \"%d\"\n", data.SkipIfNoMatch.Min))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("check_skip_if_no_match.cjs"))
	}

	// Add command position check if this is a command workflow
	if len(data.Command) > 0 {
		steps = append(steps, "      - name: Check command position\n")
		steps = append(steps, fmt.Sprintf("        id: %s\n", constants.CheckCommandPositionStepID))
		steps = append(steps, fmt.Sprintf("        uses: %s\n", GetActionPin("actions/github-script")))
		steps = append(steps, "        env:\n")
		// Pass commands as JSON array
		commandsJSON, _ := json.Marshal(data.Command)
		steps = append(steps, fmt.Sprintf("          GH_AW_COMMANDS: %q\n", string(commandsJSON)))
		steps = append(steps, "        with:\n")
		steps = append(steps, "          script: |\n")
		steps = append(steps, generateGitHubScriptWithRequire("check_command_position.cjs"))
	}

	// Append custom steps from jobs.pre-activation if present
	if len(customSteps) > 0 {
		compilerActivationJobsLog.Printf("Adding %d custom steps to pre-activation job", len(customSteps))
		steps = append(steps, customSteps...)
	}

	// Generate the activated output expression using expression builders
	var activatedNode ConditionNode

	// Build condition nodes for each check
	var conditions []ConditionNode

	if needsPermissionCheck {
		// Add membership check condition
		membershipCheck := BuildComparison(
			BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", constants.CheckMembershipStepID, constants.IsTeamMemberOutput)),
			"==",
			BuildStringLiteral("true"),
		)
		conditions = append(conditions, membershipCheck)
	}

	if data.StopTime != "" {
		// Add stop-time check condition
		stopTimeCheck := BuildComparison(
			BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", constants.CheckStopTimeStepID, constants.StopTimeOkOutput)),
			"==",
			BuildStringLiteral("true"),
		)
		conditions = append(conditions, stopTimeCheck)
	}

	if data.SkipIfMatch != nil {
		// Add skip-if-match check condition
		skipCheckOk := BuildComparison(
			BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", constants.CheckSkipIfMatchStepID, constants.SkipCheckOkOutput)),
			"==",
			BuildStringLiteral("true"),
		)
		conditions = append(conditions, skipCheckOk)
	}

	if data.SkipIfNoMatch != nil {
		// Add skip-if-no-match check condition
		skipNoMatchCheckOk := BuildComparison(
			BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", constants.CheckSkipIfNoMatchStepID, constants.SkipNoMatchCheckOkOutput)),
			"==",
			BuildStringLiteral("true"),
		)
		conditions = append(conditions, skipNoMatchCheckOk)
	}

	if data.RateLimit != nil {
		// Add rate limit check condition
		rateLimitCheck := BuildComparison(
			BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", constants.CheckRateLimitStepID, constants.RateLimitOkOutput)),
			"==",
			BuildStringLiteral("true"),
		)
		conditions = append(conditions, rateLimitCheck)
	}

	if len(data.Command) > 0 {
		// Add command position check condition
		commandPositionCheck := BuildComparison(
			BuildPropertyAccess(fmt.Sprintf("steps.%s.outputs.%s", constants.CheckCommandPositionStepID, constants.CommandPositionOkOutput)),
			"==",
			BuildStringLiteral("true"),
		)
		conditions = append(conditions, commandPositionCheck)
	}

	// Build the final expression
	if len(conditions) == 0 {
		// This should never happen - it means pre-activation job was created without any checks
		// If we reach this point, it's a developer error in the compiler logic
		return nil, fmt.Errorf("developer error: pre-activation job created without permission check or stop-time configuration")
	} else if len(conditions) == 1 {
		// Single condition
		activatedNode = conditions[0]
	} else {
		// Multiple conditions - combine with AND
		activatedNode = conditions[0]
		for i := 1; i < len(conditions); i++ {
			activatedNode = BuildAnd(activatedNode, conditions[i])
		}
	}

	// Render the expression with ${{ }} wrapper
	activatedExpression := fmt.Sprintf("${{ %s }}", activatedNode.Render())

	outputs := map[string]string{
		"activated": activatedExpression,
	}

	// Add matched_command output if this is a command workflow
	// This allows the activation job to access the matched command via needs.pre_activation.outputs.matched_command
	if len(data.Command) > 0 {
		outputs[constants.MatchedCommandOutput] = fmt.Sprintf("${{ steps.%s.outputs.%s }}", constants.CheckCommandPositionStepID, constants.MatchedCommandOutput)
	}

	// Merge custom outputs from jobs.pre-activation if present
	if len(customOutputs) > 0 {
		compilerActivationJobsLog.Printf("Adding %d custom outputs to pre-activation job", len(customOutputs))
		for key, value := range customOutputs {
			outputs[key] = value
		}
	}

	// Pre-activation job uses the user's original if condition (data.If)
	// The workflow_run safety check is NOT applied here - it's only on the activation job
	// Don't include conditions that reference custom job outputs (those belong on the agent job)
	var jobIfCondition string
	if !c.referencesCustomJobOutputs(data.If, data.Jobs) {
		jobIfCondition = data.If
	}

	job := &Job{
		Name:        string(constants.PreActivationJobName),
		If:          jobIfCondition,
		RunsOn:      c.formatSafeOutputsRunsOn(data.SafeOutputs),
		Permissions: permissions,
		Steps:       steps,
		Outputs:     outputs,
	}

	return job, nil
}

// extractPreActivationCustomFields extracts custom steps and outputs from jobs.pre-activation field in frontmatter.
// It validates that only steps and outputs fields are present, and errors on any other fields.
// If both jobs.pre-activation and jobs.pre_activation are defined, imports from both.
// Returns (customSteps, customOutputs, error).
func (c *Compiler) extractPreActivationCustomFields(jobs map[string]any) ([]string, map[string]string, error) {
	if jobs == nil {
		return nil, nil, nil
	}

	var customSteps []string
	var customOutputs map[string]string

	// Check both jobs.pre-activation and jobs.pre_activation (users might define both by mistake)
	// Import from both if both are defined
	jobVariants := []string{"pre-activation", string(constants.PreActivationJobName)}

	for _, jobName := range jobVariants {
		preActivationJob, exists := jobs[jobName]
		if !exists {
			continue
		}

		// jobs.pre-activation must be a map
		configMap, ok := preActivationJob.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("jobs.%s must be an object, got %T", jobName, preActivationJob)
		}

		// Validate that only steps and outputs fields are present
		allowedFields := map[string]bool{
			"steps":   true,
			"outputs": true,
		}

		for field := range configMap {
			if !allowedFields[field] {
				return nil, nil, fmt.Errorf("jobs.%s: unsupported field '%s' - only 'steps' and 'outputs' are allowed", jobName, field)
			}
		}

		// Extract steps
		if stepsValue, hasSteps := configMap["steps"]; hasSteps {
			stepsList, ok := stepsValue.([]any)
			if !ok {
				return nil, nil, fmt.Errorf("jobs.%s.steps must be an array, got %T", jobName, stepsValue)
			}

			for i, step := range stepsList {
				stepMap, ok := step.(map[string]any)
				if !ok {
					return nil, nil, fmt.Errorf("jobs.%s.steps[%d] must be an object, got %T", jobName, i, step)
				}

				// Convert step to YAML
				stepYAML, err := c.convertStepToYAML(stepMap)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to convert jobs.%s.steps[%d] to YAML: %w", jobName, i, err)
				}
				customSteps = append(customSteps, stepYAML)
			}
			compilerActivationJobsLog.Printf("Extracted %d custom steps from jobs.%s", len(stepsList), jobName)
		}

		// Extract outputs
		if outputsValue, hasOutputs := configMap["outputs"]; hasOutputs {
			outputsMap, ok := outputsValue.(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("jobs.%s.outputs must be an object, got %T", jobName, outputsValue)
			}

			if customOutputs == nil {
				customOutputs = make(map[string]string)
			}
			for key, val := range outputsMap {
				valStr, ok := val.(string)
				if !ok {
					return nil, nil, fmt.Errorf("jobs.%s.outputs.%s must be a string, got %T", jobName, key, val)
				}
				// If the same output key is defined in both variants, the second one wins (pre_activation)
				customOutputs[key] = valStr
			}
			compilerActivationJobsLog.Printf("Extracted %d custom outputs from jobs.%s", len(outputsMap), jobName)
		}
	}

	return customSteps, customOutputs, nil
}

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

		// Set up outputs - includes text, title, and body
		outputs["text"] = "${{ steps.compute-text.outputs.text }}"
		outputs["title"] = "${{ steps.compute-text.outputs.title }}"
		outputs["body"] = "${{ steps.compute-text.outputs.body }}"
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
				return nil, fmt.Errorf("failed to serialize messages config for activation job: %w", err)
			}
			if messagesJSON != "" {
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
			compilerActivationJobsLog.Print("Adding lock notification to reaction message")
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
			compilerActivationJobsLog.Printf("Added direct dependency on custom job '%s' because it's referenced in workflow content", jobName)
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
		compilerActivationJobsLog.Printf("Added secret_verification_result output (engine includes validate-secret step)")
	} else {
		compilerActivationJobsLog.Printf("Skipped secret_verification_result output (engine does not include validate-secret step)")
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
		compilerActivationJobsLog.Print("Added checkout_pr_success output (workflow has contents read access)")
	} else {
		compilerActivationJobsLog.Print("Skipped checkout_pr_success output (workflow lacks contents read access)")
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

	// Set GH_AW_WORKFLOW_ID_SANITIZED for cache-memory keys
	// This contains the workflow ID with all hyphens removed and lowercased
	// Used in cache keys to avoid spaces and special characters
	if data.WorkflowID != "" {
		if env == nil {
			env = make(map[string]string)
		}
		sanitizedID := SanitizeWorkflowIDForCacheKey(data.WorkflowID)
		env["GH_AW_WORKFLOW_ID_SANITIZED"] = sanitizedID
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

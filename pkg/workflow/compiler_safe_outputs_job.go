package workflow

import (
	"fmt"
	"maps"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

var consolidatedSafeOutputsJobLog = logger.New("workflow:compiler_safe_outputs_job")

// stepNameLinePrefix matches the canonical YAML line emitted by this compiler for
// step starts in job.Steps (6-space indent + "- name: ").
const stepNameLinePrefix = "      - name: "

// uploadArtifactStagingDownloadStepCount is the number of YAML string entries emitted by the
// upload-artifact staging download step block (name, continue-on-error, uses, with, name, path).
// It must match the literal slice appended in buildPreambleTokenSteps.
const uploadArtifactStagingDownloadStepCount = 6

// getSafeOutputsHeadApp returns the first non-nil HeadGitHubApp config from
// create-pull-request or push-to-pull-request-branch handlers, used to generate
// the safe-outputs-head-app-token step.
func getSafeOutputsHeadApp(safeOutputs *SafeOutputsConfig) *GitHubAppConfig {
	if safeOutputs == nil {
		return nil
	}
	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.HeadGitHubApp != nil {
		return safeOutputs.CreatePullRequests.HeadGitHubApp
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.HeadGitHubApp != nil {
		return safeOutputs.PushToPullRequestBranch.HeadGitHubApp
	}
	return nil
}

// getSafeOutputsHeadRepoSlug returns the HeadRepoSlug associated with the configured
// head-github-app, used to scope the minted token to the correct fork repository.
func getSafeOutputsHeadRepoSlug(safeOutputs *SafeOutputsConfig) string {
	if safeOutputs == nil {
		return ""
	}
	if safeOutputs.CreatePullRequests != nil && safeOutputs.CreatePullRequests.HeadGitHubApp != nil {
		return safeOutputs.CreatePullRequests.HeadRepoSlug
	}
	if safeOutputs.PushToPullRequestBranch != nil && safeOutputs.PushToPullRequestBranch.HeadGitHubApp != nil {
		return safeOutputs.PushToPullRequestBranch.HeadRepoSlug
	}
	return ""
}

// headRepoNameFromSlug extracts the repository name (without owner) from an "owner/repo"
// slug for use as the fallback repositories value in app token minting.
// Returns an empty string when the slug is absent, cannot be parsed, or contains an expression.
// The expression guard uses "${{" as a conservative prefix; standard GitHub Actions expressions
// always begin with exactly this prefix so this check is sufficient in practice.
func headRepoNameFromSlug(slug string) string {
	if slug == "" {
		return ""
	}
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) == 2 && !strings.Contains(parts[1], "${{") {
		return parts[1]
	}
	return ""
}

// messagesContainPreActivationRef reports whether any message template in cfg
// contains a reference to a needs.pre_activation.outputs.* expression.
// When true, the safe_outputs and conclusion jobs must declare pre_activation
// in their needs so that GitHub Actions can resolve the expression at runtime.
func messagesContainPreActivationRef(cfg *SafeOutputMessagesConfig) bool {
	if cfg == nil {
		return false
	}
	preActivationOutputsRef := "needs." + string(constants.PreActivationJobName) + ".outputs."
	for _, field := range []string{
		cfg.Footer,
		cfg.FooterInstall,
		cfg.FooterWorkflowRecompile,
		cfg.FooterWorkflowRecompileComment,
		cfg.StagedTitle,
		cfg.StagedDescription,
		cfg.ActivationComments,
		cfg.RunStarted,
		cfg.RunSuccess,
		cfg.RunFailure,
		cfg.DetectionFailure,
		cfg.PullRequestCreated,
		cfg.IssueCreated,
		cfg.CommitPushed,
		cfg.AgentFailureIssue,
		cfg.AgentFailureComment,
		cfg.BodyHeader,
	} {
		if strings.Contains(field, preActivationOutputsRef) {
			return true
		}
	}
	return false
}

// buildConsolidatedSafeOutputsJob builds a single job containing all safe output operations
// as separate steps within that job. This reduces the number of jobs in the workflow
// while maintaining observability through distinct step names, IDs, and outputs.
//
// File mode: Instead of inlining bundled JavaScript in YAML, this function:
// 1. Collects all JavaScript files needed by enabled safe outputs
// 2. Generates a "Setup JavaScript files" step to write them to /tmp/gh-aw/scripts/
// 3. Each safe output step requires from the local filesystem
func (c *Compiler) buildConsolidatedSafeOutputsJob(data *WorkflowData, mainJobName, markdownPath string) (*Job, []string, error) {
	if data.SafeOutputs == nil {
		consolidatedSafeOutputsJobLog.Print("No safe outputs configured, skipping consolidated job")
		return nil, nil, nil
	}

	consolidatedSafeOutputsJobLog.Print("Building consolidated safe outputs job with file mode")

	// Compute permissions and threat detection flag up front; both are used across phases.
	permissions := ComputePermissionsForSafeOutputs(data.SafeOutputs)
	// When observability.otlp.github-app is configured without app-id/private-key
	// credentials, id-token: write is needed so the safe_outputs job can mint the OTLP
	// OIDC token via core.getIDToken(audience) (mirrors threat_detection_job.go).
	if hasOTLPGitHubOIDCAuth(data.ParsedFrontmatter, data.RawFrontmatter) {
		permissions.Set(PermissionIdToken, PermissionWrite)
	}
	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)

	// Compute artifact prefix once; it is referenced in all three phases.
	agentArtifactPrefix := artifactPrefixExprForDownstreamJob(data)

	// Phase 1: Setup action, artifact downloads, and user-provided steps
	setupSteps, err := c.buildSafeOutputsSetupAndDownloadSteps(data, agentArtifactPrefix)
	if err != nil {
		return nil, nil, err
	}

	// Phase 2: Handler manager, SARIF, custom actions, and named outputs
	handlerSteps, outputs, safeOutputStepNames, err := c.buildSafeOutputsHandlerOutputsAndActionSteps(data, agentArtifactPrefix, markdownPath)
	if err != nil {
		return nil, nil, err
	}

	// Early return when no safe output handler steps were emitted
	if len(safeOutputStepNames) == 0 {
		consolidatedSafeOutputsJobLog.Print("No safe output steps were added")
		return nil, nil, nil
	}

	// Combine the setup steps with the handler steps
	steps := append(setupSteps, handlerSteps...)

	// Phase 3: App-token insertion, finalization, job condition/deps, and job construction
	return c.buildSafeOutputsJobFromParts(buildSafeOutputsJobFromPartsOptions{
		data:                   data,
		mainJobName:            mainJobName,
		markdownPath:           markdownPath,
		agentArtifactPrefix:    agentArtifactPrefix,
		steps:                  steps,
		outputs:                outputs,
		safeOutputStepNames:    safeOutputStepNames,
		permissions:            permissions,
		threatDetectionEnabled: threatDetectionEnabled,
	})
}

// buildSafeOutputsSetupAndDownloadSteps builds the initial steps for the consolidated safe
// outputs job: setup action (with optional actions-folder checkout), OTLP header masking,
// agent artifact downloads, patch artifact download (when PR operations are configured),
// shared PR checkout, GH Enterprise host configuration, and user-provided steps.
func (c *Compiler) buildSafeOutputsSetupAndDownloadSteps(data *WorkflowData, agentArtifactPrefix string) ([]string, error) {
	var steps []string

	steps = append(steps, c.buildSafeOutputsSetupSteps(data)...)
	steps = append(steps, c.buildSafeOutputsDownloadSteps(data, agentArtifactPrefix)...)

	// Configure GH_HOST for GHES/GHEC compatibility.
	// The safe-outputs job runs as an independent GitHub Actions job and does not
	// inherit GITHUB_ENV from the agent job. User-provided steps (below) and future
	// safe-output handlers that invoke the gh CLI need GH_HOST to target the
	// correct enterprise instance.
	steps = append(steps, generateGHESHostConfigurationStep())

	userSteps, err := c.buildSafeOutputsUserProvidedSteps(data)
	if err != nil {
		return nil, err
	}
	steps = append(steps, userSteps...)

	return steps, nil
}

// buildSafeOutputsSetupSteps returns the setup action and OTLP header/attribute
// masking steps that must run first in the consolidated safe-outputs job.
func (c *Compiler) buildSafeOutputsSetupSteps(data *WorkflowData) []string {
	var steps []string

	// Add setup action to copy JavaScript files
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" || c.actionMode.IsScript() {
		// For dev mode (local action path), checkout the actions folder first
		steps = append(steps, c.generateCheckoutActionsFolder(data)...)

		// Enable artifact client flag if upload-artifact safe output is configured
		enableArtifactClient := data.SafeOutputs != nil && data.SafeOutputs.UploadArtifact != nil

		// Safe outputs job depends on agent job; reuse the agent's trace ID so all jobs share one OTLP trace
		safeOutputsTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		safeOutputsParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		steps = append(steps, c.generateSetupStep(data, setupActionRef, SetupActionDestination, enableArtifactClient, safeOutputsTraceID, safeOutputsParentSpanID)...)
	}

	// Mask OTLP telemetry headers immediately after setup so authentication tokens cannot
	// leak into runner debug logs for any subsequent step in the safe outputs job.
	if isOTLPHeadersPresent(data) {
		steps = append(steps, generateOTLPHeadersMaskStep())
	}
	// Mask custom OTLP attribute values so user-supplied values cannot leak into runner logs.
	if isOTLPAttributesPresent(data) {
		steps = append(steps, generateOTLPAttributesMaskStep())
	}

	return steps
}

// buildSafeOutputsDownloadSteps returns the agent output artifact download steps,
// plus (when applicable) the patch artifact download and shared PR checkout steps.
func (c *Compiler) buildSafeOutputsDownloadSteps(data *WorkflowData, agentArtifactPrefix string) []string {
	var steps []string

	// Add artifact download steps after setup.
	// In workflow_call context, use the per-invocation prefix to avoid artifact name clashes.
	steps = append(steps, buildAgentOutputDownloadSteps(agentArtifactPrefix, c.getActionPin)...)

	// Add patch artifact download if create-pull-request or push-to-pull-request-branch is enabled
	// Both of these safe outputs require the patch file to apply changes
	// Download from unified agent artifact (prefixed in workflow_call context)
	if usesPatchesAndCheckouts(data.SafeOutputs) {
		consolidatedSafeOutputsJobLog.Print("Adding patch artifact download for create-pull-request or push-to-pull-request-branch")
		patchDownloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName: agentArtifactPrefix + constants.AgentArtifactName.String(),
			DownloadPath: constants.TmpGhAwDirSlash,
			SetupEnvStep: false, // No environment variable needed, the script checks the file directly
			StepName:     "Download patch artifact",
		}, c.getActionPin)
		steps = append(steps, patchDownloadSteps...)

		// Add checkout and git config steps for PR operations. These mirror the agent job's
		// checkout layout exactly (same CheckoutManager generators); the base branch is
		// resolved by the JS handler at apply time, so no checkout-time base ref is needed.
		consolidatedSafeOutputsJobLog.Print("Adding shared checkout step for PR operations")
		checkoutSteps := c.buildSharedPRCheckoutSteps(data)
		steps = append(steps, checkoutSteps...)
	}

	return steps
}

// buildSafeOutputsUserProvidedSteps converts the user-provided safe-outputs.steps
// frontmatter entries into pinned, YAML-rendered workflow steps.
func (c *Compiler) buildSafeOutputsUserProvidedSteps(data *WorkflowData) ([]string, error) {
	var steps []string

	if len(data.SafeOutputs.Steps) == 0 {
		return steps, nil
	}

	consolidatedSafeOutputsJobLog.Printf("Adding %d user-provided steps to safe-outputs job", len(data.SafeOutputs.Steps))
	for i, step := range data.SafeOutputs.Steps {
		stepMap, ok := step.(map[string]any)
		if !ok {
			consolidatedSafeOutputsJobLog.Printf("Warning: safe-outputs step at index %d is not a valid step object (must be a map with properties like name, run, uses). Skipping this step.", i)
			continue
		}
		typedStep, err := MapToStep(stepMap)
		if err != nil {
			return nil, fmt.Errorf("failed to convert safe-outputs step at index %d to typed step: %w", i, err)
		}
		pinnedStep, err := applyActionPinToTypedStep(typedStep, data)
		if err != nil {
			return nil, fmt.Errorf("failed to pin action for safe-outputs step at index %d: %w", i, err)
		}
		stepYAML, err := ConvertStepToYAML(pinnedStep.ToMap())
		if err != nil {
			return nil, fmt.Errorf("failed to convert safe-outputs step at index %d to YAML: %w", i, err)
		}
		steps = append(steps, stepYAML)
	}

	return steps, nil
}

// buildSafeOutputsHandlerOutputsAndActionSteps builds the handler-manager step (if needed),
// all job-level outputs derived from the handler, SARIF artifact upload, custom action steps,
// and the named convenience outputs for first-created items.
// It returns the collected steps, outputs map, and the list of safe-output step names registered.
func (c *Compiler) buildSafeOutputsHandlerOutputsAndActionSteps(data *WorkflowData, agentArtifactPrefix, markdownPath string) ([]string, map[string]string, []string, error) {
	state := safeOutputsHandlerOutputsAndActionState{outputs: make(map[string]string)}
	if err := c.appendCustomScriptFilesStep(data, &state); err != nil {
		return nil, nil, nil, err
	}
	c.appendUploadArtifactStagingDownloadStep(data, agentArtifactPrefix, &state)
	if err := c.appendHandlerManagerStep(data, &state); err != nil {
		return nil, nil, nil, err
	}
	c.appendSarifArtifactUploadStep(data, agentArtifactPrefix, &state)
	c.appendCustomActionSteps(data, markdownPath, &state)
	addNamedSafeOutputHandlerOutputs(data, state.outputs)

	return state.steps, state.outputs, state.safeOutputStepNames, nil
}

type safeOutputsHandlerOutputsAndActionState struct {
	steps               []string
	outputs             map[string]string
	safeOutputStepNames []string
}

// hasHandlerManagerTypes reports whether the workflow configures any safe-output type that is
// processed by the consolidated handler manager step (as opposed to a dedicated job/step).
func hasHandlerManagerTypes(data *WorkflowData) bool {
	return data.SafeOutputs.CreateIssues != nil ||
		data.SafeOutputs.CreateWorkItems != nil ||
		data.SafeOutputs.UpdateWorkItems != nil ||
		data.SafeOutputs.CommentOnWorkItems != nil ||
		data.SafeOutputs.AssignWorkItems != nil ||
		data.SafeOutputs.LinkWorkItems != nil ||
		data.SafeOutputs.UploadWorkItemAttachments != nil ||
		data.SafeOutputs.LinearCreateIssue != nil ||
		data.SafeOutputs.LinearAddComment != nil ||
		data.SafeOutputs.LinearUpdateIssue != nil ||
		data.SafeOutputs.AddComments != nil ||
		data.SafeOutputs.CreateDiscussions != nil ||
		data.SafeOutputs.CloseIssues != nil ||
		data.SafeOutputs.CloseDiscussions != nil ||
		data.SafeOutputs.AddLabels != nil ||
		data.SafeOutputs.RemoveLabels != nil ||
		data.SafeOutputs.UpdateIssues != nil ||
		data.SafeOutputs.UpdateDiscussions != nil ||
		data.SafeOutputs.LinkSubIssue != nil ||
		data.SafeOutputs.UpdateRelease != nil ||
		data.SafeOutputs.CreatePullRequestReviewComments != nil ||
		data.SafeOutputs.SubmitPullRequestReview != nil ||
		data.SafeOutputs.ReplyToPullRequestReviewComment != nil ||
		data.SafeOutputs.ResolvePullRequestReviewThread != nil ||
		data.SafeOutputs.CreatePullRequests != nil ||
		data.SafeOutputs.PushToPullRequestBranch != nil ||
		data.SafeOutputs.UpdatePullRequests != nil ||
		data.SafeOutputs.ClosePullRequests != nil ||
		data.SafeOutputs.MarkPullRequestAsReadyForReview != nil ||
		data.SafeOutputs.ApproveWorkflowRun != nil ||
		data.SafeOutputs.HideComment != nil ||
		data.SafeOutputs.SetIssueType != nil ||
		data.SafeOutputs.SetIssueField != nil ||
		data.SafeOutputs.DispatchWorkflow != nil ||
		data.SafeOutputs.CallWorkflow != nil ||
		data.SafeOutputs.CreateCodeScanningAlerts != nil ||
		data.SafeOutputs.AutofixCodeScanningAlert != nil ||
		data.SafeOutputs.CreateCheckRun != nil ||
		data.SafeOutputs.MissingTool != nil ||
		data.SafeOutputs.MissingData != nil ||
		data.SafeOutputs.AssignToAgent != nil || // assign_to_agent is now handled by the handler manager
		data.SafeOutputs.CreateAgentSessions != nil || // create_agent_session is now handled by the handler manager
		data.SafeOutputs.UploadArtifact != nil || // upload_artifact is handled inline in the handler loop
		data.SafeOutputs.UploadCodeCoverage != nil || // upload_code_coverage is handled inline in the handler loop
		len(data.SafeOutputs.Scripts) > 0 || // Custom scripts run in the handler loop
		len(data.SafeOutputs.Actions) > 0 // Custom actions need handler to export their payloads
}

// appendCustomScriptFilesStep appends the setup step(s) for writing custom safe-output scripts to
// disk, when the workflow declares any, to the accumulated job state.
func (c *Compiler) appendCustomScriptFilesStep(data *WorkflowData, state *safeOutputsHandlerOutputsAndActionState) error {
	if len(data.SafeOutputs.Scripts) > 0 {
		consolidatedSafeOutputsJobLog.Printf("Adding setup step for %d custom safe-output script(s)", len(data.SafeOutputs.Scripts))
		scriptSetupSteps, err := buildCustomScriptFilesStep(data.SafeOutputs.Scripts)
		if err != nil {
			return fmt.Errorf("failed to build custom script files step: %w", err)
		}
		state.steps = append(state.steps, scriptSetupSteps...)
	}
	return nil
}

// appendUploadArtifactStagingDownloadStep appends a step that downloads the upload-artifact
// staging artifact produced by the agent job, when the workflow uses the upload_artifact safe
// output, so the handler manager can process staged files.
func (c *Compiler) appendUploadArtifactStagingDownloadStep(data *WorkflowData, agentArtifactPrefix string, state *safeOutputsHandlerOutputsAndActionState) {
	if usesSafeOutputsArtifactStaging(data.SafeOutputs) {
		consolidatedSafeOutputsJobLog.Print("Adding upload-artifact staging download step")
		stagingArtifactName := agentArtifactPrefix + SafeOutputsUploadArtifactStagingArtifactName
		state.steps = append(state.steps,
			"      - name: Download upload-artifact staging\n",
			"        continue-on-error: true\n",
			fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/download-artifact")),
			"        with:\n",
			fmt.Sprintf("          name: %s\n", stagingArtifactName),
			fmt.Sprintf("          path: %s\n", artifactStagingDirExpr),
		)
	}
}

// appendHandlerManagerStep appends the consolidated handler manager step, when the workflow
// declares any safe-output type it handles, and records its outputs and step name.
func (c *Compiler) appendHandlerManagerStep(data *WorkflowData, state *safeOutputsHandlerOutputsAndActionState) error {
	if hasHandlerManagerTypes(data) {
		consolidatedSafeOutputsJobLog.Print("Using handler manager for safe outputs")
		handlerManagerSteps, err := c.buildHandlerManagerStep(data)
		if err != nil {
			return err
		}
		handlerManagerSteps = injectLinearCredentialsIntoProcessorStep(handlerManagerSteps, data.SafeOutputs)
		handlerManagerSteps = injectJiraCredentialsIntoProcessorStep(handlerManagerSteps, data.SafeOutputs)
		state.steps = append(state.steps, handlerManagerSteps...)
		state.safeOutputStepNames = append(state.safeOutputStepNames, "process_safe_outputs")
		addHandlerManagerOutputs(data, state.outputs)
	}
	return nil
}

// appendSarifArtifactUploadStep appends the step that uploads the SARIF artifact produced by the
// handler manager, when create_code_scanning_alert is configured and not staged, and exposes its
// sarif_file output for the downstream upload_code_scanning_sarif job.
func (c *Compiler) appendSarifArtifactUploadStep(data *WorkflowData, agentArtifactPrefix string, state *safeOutputsHandlerOutputsAndActionState) {
	if data.SafeOutputs.CreateCodeScanningAlerts != nil && !isHandlerStaged(c.trialMode || templatableBoolIsTrue(data.SafeOutputs.Staged), data.SafeOutputs.CreateCodeScanningAlerts.Staged) {
		consolidatedSafeOutputsJobLog.Print("Exposing sarif_file output for upload_code_scanning_sarif job")
		state.outputs["sarif_file"] = "${{ steps.process_safe_outputs.outputs.sarif_file }}"
		state.steps = append(state.steps, buildSarifArtifactUploadStep(agentArtifactPrefix, c.getActionPin)...)
	}
}

// appendCustomActionSteps resolves and appends the steps for any custom safe-output actions
// declared by the workflow, recording a step name for each so later steps can depend on it.
func (c *Compiler) appendCustomActionSteps(data *WorkflowData, markdownPath string, state *safeOutputsHandlerOutputsAndActionState) {
	if len(data.SafeOutputs.Actions) > 0 {
		c.resolveAllActions(data, markdownPath)
		actionStepYAML := c.buildActionSteps(data)
		state.steps = append(state.steps, actionStepYAML...)
		for actionName := range data.SafeOutputs.Actions {
			normalizedName := stringutil.NormalizeSafeOutputIdentifier(actionName)
			state.safeOutputStepNames = append(state.safeOutputStepNames, "action_"+normalizedName)
		}
	}
}

// addHandlerManagerOutputs populates the job outputs map with the common outputs exposed by the
// process_safe_outputs handler manager step, plus any conditional per-type outputs.
func addHandlerManagerOutputs(data *WorkflowData, outputs map[string]string) {
	maps.Copy(outputs, map[string]string{
		"process_safe_outputs_temporary_id_map": "${{ steps.process_safe_outputs.outputs.temporary_id_map }}",
		"process_safe_outputs_processed_count":  "${{ steps.process_safe_outputs.outputs.processed_count }}",
		"process_safe_outputs_items_succeeded":  "${{ steps.process_safe_outputs.outputs.items_succeeded }}",
		"process_safe_outputs_items_applied":    "${{ steps.process_safe_outputs.outputs.items_applied }}",
		"process_safe_outputs_items_skipped":    "${{ steps.process_safe_outputs.outputs.items_skipped }}",
		"process_safe_outputs_items_warnings":   "${{ steps.process_safe_outputs.outputs.items_warnings }}",
		"process_safe_outputs_items_cancelled":  "${{ steps.process_safe_outputs.outputs.items_cancelled }}",
		"process_safe_outputs_items_deferred":   "${{ steps.process_safe_outputs.outputs.items_deferred }}",
		"process_safe_outputs_items_failed":     "${{ steps.process_safe_outputs.outputs.items_failed }}",
		"process_safe_outputs_status":           "${{ steps.process_safe_outputs.outputs.status }}",
		"create_discussion_errors":              "${{ steps.process_safe_outputs.outputs.create_discussion_errors }}",
		"create_discussion_error_count":         "${{ steps.process_safe_outputs.outputs.create_discussion_error_count }}",
		"code_push_failure_errors":              "${{ steps.process_safe_outputs.outputs.code_push_failure_errors }}",
		"code_push_failure_count":               "${{ steps.process_safe_outputs.outputs.code_push_failure_count }}",
	})
	addConditionalHandlerManagerOutputs(data, outputs)
}

// addConditionalHandlerManagerOutputs adds outputs from the handler manager step that are only
// exposed when the corresponding safe-output type (assign_to_agent, create_agent_session, or
// upload_artifact) is configured.
func addConditionalHandlerManagerOutputs(data *WorkflowData, outputs map[string]string) {
	if data.SafeOutputs.AssignToAgent != nil {
		consolidatedSafeOutputsJobLog.Print("Exposing assign_to_agent outputs from handler manager")
		outputs["assign_to_agent_assigned"] = "${{ steps.process_safe_outputs.outputs.assign_to_agent_assigned }}"
		outputs["assign_to_agent_assignment_errors"] = "${{ steps.process_safe_outputs.outputs.assign_to_agent_assignment_errors }}"
		outputs["assign_to_agent_assignment_error_count"] = "${{ steps.process_safe_outputs.outputs.assign_to_agent_assignment_error_count }}"
	}
	if data.SafeOutputs.CreateAgentSessions != nil {
		consolidatedSafeOutputsJobLog.Print("Exposing create_agent_session outputs from handler manager")
		outputs["create_agent_session_session_number"] = "${{ steps.process_safe_outputs.outputs.session_number }}"
		outputs["create_agent_session_session_url"] = "${{ steps.process_safe_outputs.outputs.session_url }}"
	}
	if data.SafeOutputs.UploadArtifact != nil {
		consolidatedSafeOutputsJobLog.Print("Exposing upload_artifact outputs from handler manager")
		outputs["upload_artifact_count"] = "${{ steps.process_safe_outputs.outputs.upload_artifact_count }}"
		for i := range data.SafeOutputs.UploadArtifact.MaxUploads {
			outputs[fmt.Sprintf("upload_artifact_slot_%d_tmp_id", i)] = fmt.Sprintf("${{ steps.process_safe_outputs.outputs.slot_%d_tmp_id }}", i)
		}
	}
	if data.SafeOutputs.UploadCodeCoverage != nil {
		consolidatedSafeOutputsJobLog.Print("Exposing upload_code_coverage outputs from handler manager")
		outputs["upload_code_coverage_file"] = "${{ steps.process_safe_outputs.outputs.upload_code_coverage_file }}"
		outputs["upload_code_coverage_language"] = "${{ steps.process_safe_outputs.outputs.upload_code_coverage_language }}"
		outputs["upload_code_coverage_label"] = "${{ steps.process_safe_outputs.outputs.upload_code_coverage_label }}"
	}
}

func addNamedSafeOutputHandlerOutputs(data *WorkflowData, outputs map[string]string) {
	if data.SafeOutputs.AddReviewer != nil {
		outputs["add_reviewer_reviewers_added"] = "${{ steps.process_safe_outputs.outputs.reviewers_added }}"
	}
	if data.SafeOutputs.AssignMilestone != nil {
		outputs["assign_milestone_milestone_assigned"] = "${{ steps.process_safe_outputs.outputs.milestone_assigned }}"
	}
	if data.SafeOutputs.AssignToUser != nil {
		outputs["assign_to_user_assigned"] = "${{ steps.process_safe_outputs.outputs.assigned }}"
	}
	if data.SafeOutputs.CreateIssues != nil {
		outputs["created_issue_number"] = "${{ steps.process_safe_outputs.outputs.created_issue_number }}"
		outputs["created_issue_url"] = "${{ steps.process_safe_outputs.outputs.created_issue_url }}"
	}

	if data.SafeOutputs.CreatePullRequests != nil {
		outputs["created_pr_number"] = "${{ steps.process_safe_outputs.outputs.created_pr_number }}"
		outputs["created_pr_url"] = "${{ steps.process_safe_outputs.outputs.created_pr_url }}"
	}

	if data.SafeOutputs.AddComments != nil {
		outputs["comment_id"] = "${{ steps.process_safe_outputs.outputs.comment_id }}"
		outputs["comment_url"] = "${{ steps.process_safe_outputs.outputs.comment_url }}"
	}

	if data.SafeOutputs.PushToPullRequestBranch != nil {
		outputs["push_commit_sha"] = "${{ steps.process_safe_outputs.outputs.push_commit_sha }}"
		outputs["push_commit_url"] = "${{ steps.process_safe_outputs.outputs.push_commit_url }}"
	}

	if data.SafeOutputs.CallWorkflow != nil {
		outputs["call_workflow_name"] = "${{ steps.process_safe_outputs.outputs.call_workflow_name }}"
		outputs["call_workflow_payload"] = "${{ steps.process_safe_outputs.outputs.call_workflow_payload }}"
	}
}

// buildSafeOutputsJobFromPartsOptions bundles the inputs required by buildSafeOutputsJobFromParts
// to assemble the final safe_outputs job.
type buildSafeOutputsJobFromPartsOptions struct {
	data                   *WorkflowData
	mainJobName            string
	markdownPath           string
	agentArtifactPrefix    string
	steps                  []string
	outputs                map[string]string
	safeOutputStepNames    []string
	permissions            *Permissions
	threatDetectionEnabled bool
}

// buildSafeOutputsJobFromParts finalizes the step list (app-token insertion, token invalidation,
// items-manifest upload, dev-mode restore, script-mode cleanup), builds the job condition and
// dependency list, and assembles the Job struct for the safe_outputs job.
func (c *Compiler) buildSafeOutputsJobFromParts(
	opts buildSafeOutputsJobFromPartsOptions,
) (*Job, []string, error) {
	data := opts.data
	mainJobName := opts.mainJobName
	markdownPath := opts.markdownPath
	agentArtifactPrefix := opts.agentArtifactPrefix
	steps := opts.steps
	outputs := opts.outputs
	safeOutputStepNames := opts.safeOutputStepNames
	permissions := opts.permissions
	threatDetectionEnabled := opts.threatDetectionEnabled

	// Build and insert preamble token minting steps (GitHub App tokens) before checkout/safe-output steps.
	preambleTokenSteps := c.buildPreambleTokenSteps(data, outputs)
	if len(preambleTokenSteps) > 0 {
		steps = c.insertPreambleTokenStepsIntoSteps(steps, preambleTokenSteps, data, agentArtifactPrefix)
	}

	steps = c.appendFinalSafeOutputSteps(data, steps, agentArtifactPrefix)

	jobCondition := buildSafeOutputsJobCondition(data, threatDetectionEnabled)
	needs := c.buildSafeOutputsJobNeeds(data, mainJobName, threatDetectionEnabled)
	workflowID := GetWorkflowIDFromPath(markdownPath)
	jobEnv := c.buildJobLevelSafeOutputEnvVars(data, workflowID)

	var concurrency string
	if data.SafeOutputs.ConcurrencyGroup != "" {
		concurrency = c.indentYAMLLines(fmt.Sprintf("concurrency:\n  group: %q\n  cancel-in-progress: false", data.SafeOutputs.ConcurrencyGroup), "    ")
		consolidatedSafeOutputsJobLog.Printf("Configuring safe_outputs job concurrency group: %s", data.SafeOutputs.ConcurrencyGroup)
	}

	const defaultSafeOutputsTimeoutMinutes = 45
	timeoutMinutes := defaultSafeOutputsTimeoutMinutes
	if data.SafeOutputs.TimeoutMinutes > 0 {
		timeoutMinutes = data.SafeOutputs.TimeoutMinutes
	}

	job := &Job{
		Name:           "safe_outputs",
		If:             RenderCondition(jobCondition),
		RunsOn:         c.formatFrameworkJobRunsOn(data),
		Environment:    c.indentYAMLLines(resolveSafeOutputsEnvironment(data), "    "),
		Permissions:    permissions.RenderToYAML(),
		TimeoutMinutes: timeoutMinutes,
		Concurrency:    concurrency,
		Env:            jobEnv,
		Steps:          steps,
		Outputs:        outputs,
		Needs:          needs,
	}

	consolidatedSafeOutputsJobLog.Printf("Built consolidated safe outputs job with %d steps", len(safeOutputStepNames))

	return job, safeOutputStepNames, nil
}

// buildPreambleTokenSteps builds GitHub App token minting steps that must be inserted before
// checkout and safe-output handler steps. It also mutates outputs to track minting failures.
func (c *Compiler) buildPreambleTokenSteps(data *WorkflowData, outputs map[string]string) []string {
	var preambleTokenSteps []string
	if data.SafeOutputs.GitHubApp != nil {
		appPermissions := computePermissionsForSafeOutputs(data.SafeOutputs, true)
		if appPermissions != nil && len(appPermissions.permissions) == 0 {
			// No enabled handler (e.g. a Linear-only configuration) consumes GitHub
			// permissions from this global app, so skip minting an unrelated
			// installation token purely because a top-level github-app was
			// configured or auto-copied via applyTopLevelGitHubAppFallbacks.
			safeOutputsPermissionsLog.Print("No GitHub-backed safe output handler enabled; skipping global GitHub App token minting")
		} else {
			outputs["app_token_minting_failed"] = "${{ steps.safe-outputs-app-token.outcome == 'failure' }}"
			var appTokenFallbackRepo string
			if hasWorkflowCallTrigger(data.On) {
				appTokenFallbackRepo = "${{ needs.activation.outputs.target_repo_name }}"
			}
			preambleTokenSteps = append(preambleTokenSteps, c.buildGitHubAppTokenMintStepForRepository(
				data.SafeOutputs.GitHubApp,
				appPermissions,
				appTokenFallbackRepo,
				inferSingleCheckoutRepositoryForGitHubAppOwner(data),
			)...)
		}
	}
	if headApp := getSafeOutputsHeadApp(data.SafeOutputs); headApp != nil {
		headRepoSlug := getSafeOutputsHeadRepoSlug(data.SafeOutputs)
		preambleTokenSteps = append(preambleTokenSteps, c.buildGitHubAppTokenMintStepWithMeta(
			headApp,
			nil,
			headRepoNameFromSlug(headRepoSlug),
			headRepoSlug,
			"Generate GitHub App head token",
			"safe-outputs-head-app-token",
		)...)
	}
	return preambleTokenSteps
}

// insertPreambleTokenStepsIntoSteps inserts preambleTokenSteps at the correct position
// within steps: after setup/download steps but before checkout and safe-output handler steps.
func (c *Compiler) insertPreambleTokenStepsIntoSteps(steps []string, preambleTokenSteps []string, data *WorkflowData, agentArtifactPrefix string) []string {
	insertIndex := c.calculatePreambleInsertIndex(steps, data, agentArtifactPrefix)

	// The insertion index is line-oriented; if it lands in the middle of a
	// multi-line run/with block, move it to the next step boundary.
	for insertIndex < len(steps) && !strings.HasPrefix(steps[insertIndex], stepNameLinePrefix) {
		insertIndex++
	}
	if insertIndex == len(steps) {
		consolidatedSafeOutputsJobLog.Printf(
			"WARN: preamble-token insertion reached end of steps slice (len=%d); step ordering may be incorrect",
			len(steps),
		)
	}

	var newSteps []string
	newSteps = append(newSteps, steps[:insertIndex]...)
	newSteps = append(newSteps, preambleTokenSteps...)
	newSteps = append(newSteps, steps[insertIndex:]...)
	return newSteps
}

// calculatePreambleInsertIndex computes the line-offset index at which preamble token steps
// should be inserted: after setup, OTLP mask, artifact download, and patch download steps.
func (c *Compiler) calculatePreambleInsertIndex(steps []string, data *WorkflowData, agentArtifactPrefix string) int {
	insertIndex := 0
	setupActionRef := c.resolveActionReference("./actions/setup", data)
	if setupActionRef != "" {
		insertIndex += len(c.generateCheckoutActionsFolder(data))
		countTraceID := fmt.Sprintf("${{ needs.%s.outputs.setup-trace-id }}", constants.ActivationJobName)
		countParentSpanID := setupParentSpanNeedsExpr(constants.ActivationJobName)
		insertIndex += len(c.generateSetupStep(data, setupActionRef, SetupActionDestination, data.SafeOutputs != nil && data.SafeOutputs.UploadArtifact != nil, countTraceID, countParentSpanID))
	}
	if isOTLPHeadersPresent(data) {
		insertIndex += strings.Count(generateOTLPHeadersMaskStep(), stepNameLinePrefix)
	}
	if isOTLPAttributesPresent(data) {
		insertIndex += strings.Count(generateOTLPAttributesMaskStep(), stepNameLinePrefix)
	}
	insertIndex += len(buildAgentOutputDownloadSteps(agentArtifactPrefix, c.getActionPin))
	if usesSafeOutputsArtifactStaging(data.SafeOutputs) {
		// The staging download step has uploadArtifactStagingDownloadStepCount YAML string entries.
		insertIndex += uploadArtifactStagingDownloadStepCount
	}
	if usesPatchesAndCheckouts(data.SafeOutputs) {
		patchDownloadSteps := buildArtifactDownloadSteps(ArtifactDownloadConfig{
			ArtifactName: agentArtifactPrefix + constants.AgentArtifactName.String(),
			DownloadPath: constants.TmpGhAwDirSlash,
			SetupEnvStep: false,
			StepName:     "Download patch artifact",
		}, c.getActionPin)
		insertIndex += len(patchDownloadSteps)
	}
	return insertIndex
}

// appendFinalSafeOutputSteps appends the manifest upload, dev-mode restore, and script-mode
// cleanup steps that must come after all safe-output handler steps.
func (c *Compiler) appendFinalSafeOutputSteps(data *WorkflowData, steps []string, agentArtifactPrefix string) []string {
	isStaged := c.trialMode || templatableBoolIsTrue(data.SafeOutputs.Staged)
	if !isStaged {
		steps = append(steps, buildSafeOutputItemsManifestUploadStep(agentArtifactPrefix, c.getActionPin)...)
	}
	if c.actionMode.IsDev() && usesPatchesAndCheckouts(data.SafeOutputs) {
		steps = append(steps, c.generateRestoreActionsSetupStep())
		consolidatedSafeOutputsJobLog.Print("Added restore actions folder step to safe_outputs job (dev mode with checkout)")
	}
	if c.actionMode.IsScript() {
		steps = append(steps, c.generateScriptModeCleanupStep())
	}
	return steps
}

// buildSafeOutputsJobCondition constructs the "if" expression for the safe_outputs job.
// The job runs when the agent job completed (not skipped) and, when threat detection is enabled,
// when the detection job passed.
func buildSafeOutputsJobCondition(data *WorkflowData, threatDetectionEnabled bool) ConditionNode {
	agentNotSkipped := BuildAnd(
		&NotNode{Child: BuildFunctionCall("cancelled")},
		BuildNotEquals(
			BuildPropertyAccess(fmt.Sprintf("needs.%s.result", constants.AgentJobName)),
			BuildStringLiteral("skipped"),
		),
	)
	if IsConditionalDetection(data.SafeOutputs) {
		// When detection is expression-controlled, the detection job may be skipped at runtime
		// (expression evaluated to false). Use always() to prevent safe_outputs from being
		// skipped due to a skipped dependency, and accept both success and skipped results.
		return BuildAnd(
			BuildAnd(BuildFunctionCall("always"), agentNotSkipped),
			buildDetectionPassedCondition(),
		)
	}
	if threatDetectionEnabled {
		return BuildAnd(agentNotSkipped, buildDetectionSuccessCondition())
	}
	return agentNotSkipped
}

// buildSafeOutputsJobNeeds returns the ordered list of job names that safe_outputs depends on.
func (c *Compiler) buildSafeOutputsJobNeeds(data *WorkflowData, mainJobName string, threatDetectionEnabled bool) []string {
	// safe_outputs depends on agent; when threat detection is enabled it also
	// depends on the detection job (so that detection_success is available).
	needs := []string{mainJobName}
	if threatDetectionEnabled {
		needs = append(needs, string(constants.DetectionJobName))
		consolidatedSafeOutputsJobLog.Print("Added detection job dependency to safe_outputs job")
	}
	// Always add activation job dependency to get the trace-id for OTLP correlation,
	// and also when needed for other reasons:
	// - create_pull_request or push_to_pull_request_branch (need the activation artifact)
	// - lock-for-agent (need the activation lock)
	// - workflow_call trigger (need needs.activation.outputs.target_repo for cross-repo token/dispatch)
	needs = append(needs, string(constants.ActivationJobName))
	// Add unlock job dependency if lock-for-agent is enabled
	// This ensures the issue is unlocked before safe outputs run
	if data.LockForAgent {
		needs = append(needs, "unlock")
		consolidatedSafeOutputsJobLog.Print("Added unlock job dependency to safe_outputs job")
	}
	seenNeeds := make(map[string]struct{}, len(needs))
	for _, need := range needs {
		seenNeeds[need] = struct{}{}
	}
	if data.SafeOutputs != nil {
		for _, need := range data.SafeOutputs.Needs {
			if setutil.Contains(seenNeeds, need) {
				continue
			}
			needs = append(needs, need)
			seenNeeds[need] = struct{}{}
			consolidatedSafeOutputsJobLog.Printf("Added explicit safe-outputs needs dependency to safe_outputs job: %s", need)
		}
	}
	// If any message template references needs.pre_activation.outputs.*, add pre_activation
	// as a dependency so that GitHub Actions can resolve the expression at runtime.
	if data.SafeOutputs != nil && messagesContainPreActivationRef(data.SafeOutputs.Messages) {
		if _, exists := c.jobManager.GetJob(string(constants.PreActivationJobName)); exists {
			preActName := string(constants.PreActivationJobName)
			if !setutil.Contains(seenNeeds, preActName) {
				needs = append(needs, preActName)
				seenNeeds[preActName] = struct{}{} // keep map consistent with all other appends
				consolidatedSafeOutputsJobLog.Print("Added pre_activation dependency to safe_outputs job (messages reference pre_activation outputs)")
			}
		}
	}
	return needs
}

// headSHAExpressionForTrigger returns the GitHub Actions expression for the PR head SHA
// based on the workflow's `on:` field. Returns an empty string if the trigger type does
// not carry a directly accessible PR head SHA (e.g. push, schedule, issues).
//
// The returned expression is injected as GH_AW_HEAD_SHA in the safe_outputs job so that
// PR-review handlers automatically pin their reviews to the commit that was in place when
// the workflow triggered, preventing attribution drift if new commits land during the run.
func headSHAExpressionForTrigger(onField any) string {
	switch v := onField.(type) {
	case map[string]any:
		if _, ok := v["workflow_run"]; ok {
			return "${{ github.event.workflow_run.head_sha }}"
		}
		if _, ok := v["pull_request"]; ok {
			return "${{ github.event.pull_request.head.sha }}"
		}
		if _, ok := v["pull_request_target"]; ok {
			return "${{ github.event.pull_request.head.sha }}"
		}
	case string:
		switch v {
		case "workflow_run":
			return "${{ github.event.workflow_run.head_sha }}"
		case "pull_request", "pull_request_target":
			return "${{ github.event.pull_request.head.sha }}"
		}
	}
	return ""
}

// resolveSafeOutputsEnvironment resolves the effective GitHub deployment environment for
// safe-output jobs. If safe-outputs.environment is explicitly set, it takes precedence.
// Otherwise the top-level environment: field is propagated so that environment-scoped
// secrets are accessible in all safe-output jobs.
func resolveSafeOutputsEnvironment(data *WorkflowData) string {
	if data.SafeOutputs != nil && data.SafeOutputs.Environment != "" {
		return data.SafeOutputs.Environment
	}
	return data.Environment
}

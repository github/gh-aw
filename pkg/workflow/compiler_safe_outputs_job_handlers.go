package workflow

import (
	"fmt"
	"maps"

	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
)

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
	state.steps = append(state.steps, c.buildWorkQueueFinishStep(data)...)
	c.appendLedgerTransactionsArtifactUpload(data, &state)
	c.appendSarifArtifactUploadStep(data, agentArtifactPrefix, &state)
	if err := c.appendCustomActionSteps(data, markdownPath, &state); err != nil {
		return nil, nil, nil, err
	}
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
	return isWorkQueueParticipant(data) ||
		data.SafeOutputs.CreateSecurityAdvisory != nil ||
		data.SafeOutputs.AddSecurityAdvisoryComment != nil ||
		data.SafeOutputs.CreateIssues != nil ||
		data.SafeOutputs.AddComments != nil ||
		data.SafeOutputs.CreateDiscussions != nil ||
		data.SafeOutputs.CreateProjectStatusUpdates != nil ||
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
		data.SafeOutputs.MergePullRequest != nil ||
		data.SafeOutputs.MarkPullRequestAsReadyForReview != nil ||
		data.SafeOutputs.ApproveWorkflowRun != nil ||
		data.SafeOutputs.HideComment != nil ||
		data.SafeOutputs.SetIssueStatus != nil ||
		data.SafeOutputs.SetIssueType != nil ||
		data.SafeOutputs.SetIssueField != nil ||
		data.SafeOutputs.DispatchWorkflow != nil ||
		data.SafeOutputs.CallWorkflow != nil ||
		data.SafeOutputs.CreateCodeScanningAlerts != nil ||
		data.SafeOutputs.AutofixCodeScanningAlert != nil ||
		data.SafeOutputs.CreateCheckRun != nil ||
		data.SafeOutputs.MissingTool != nil ||
		data.SafeOutputs.MissingData != nil ||
		isRoutedRequestContinuation(data.SafeOutputs) || // Process Safe Outputs is the only route consumer
		data.SafeOutputs.AssignToAgent != nil || // assign_to_agent is now handled by the handler manager
		data.SafeOutputs.CreateAgentSessions != nil || // create_agent_session is now handled by the handler manager
		data.SafeOutputs.UploadArtifact != nil || // upload_artifact is handled inline in the handler loop
		data.SafeOutputs.UploadCodeCoverage != nil || // upload_code_coverage is handled inline in the handler loop
		len(data.SafeOutputs.Scripts) > 0 || // Custom scripts run in the handler loop
		len(data.SafeOutputs.Actions) > 0 || // Custom actions need handler to export their payloads
		(data.LedgerConfig != nil && data.LedgerConfig.Enabled())
}

func (c *Compiler) appendLedgerTransactionsArtifactUpload(data *WorkflowData, state *safeOutputsHandlerOutputsAndActionState) {
	if data.LedgerConfig == nil || !data.LedgerConfig.Enabled() {
		return
	}
	state.steps = append(state.steps,
		"      - name: Upload validated ledger transactions\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: %s\n", ledgerTransactionsArtifactName),
		fmt.Sprintf("          path: %s\n", ledgerTransactionsArtifactPath),
		"          if-no-files-found: error\n",
		"          retention-days: 1\n",
	)
}

// appendCustomScriptFilesStep appends the setup step(s) for writing custom safe-output scripts to
// disk, when the workflow declares any, to the accumulated job state.
func (c *Compiler) appendCustomScriptFilesStep(data *WorkflowData, state *safeOutputsHandlerOutputsAndActionState) error {
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		if data.Connectors[alias].SafeOutputs != nil {
			if isWorkQueueParticipant(data) {
				return fmt.Errorf("connectors.%s.safe-outputs is not supported for work-queue participants; independent connector effect verification is required", alias)
			}
		}
	}
	state.steps = append(state.steps, c.connectorGitHubAppSteps(data, "safe-output")...)
	if !isWorkQueueParticipant(data) && len(data.SafeOutputs.Scripts) > 0 {
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
	types := safeOutputsArtifactStagingTypes(data.SafeOutputs)
	if len(types) != 0 {
		consolidatedSafeOutputsJobLog.Print("Adding upload-artifact staging download step")
		condition := BuildSafeOutputType(types[0])
		for _, outputType := range types[1:] {
			condition = BuildOr(condition, BuildSafeOutputType(outputType))
		}
		stagingArtifactName := agentArtifactPrefix + SafeOutputsUploadArtifactStagingArtifactName
		state.steps = append(state.steps,
			"      - name: Download upload-artifact staging\n",
			fmt.Sprintf("        if: %s\n", RenderCondition(condition)),
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
	if !isWorkQueueParticipant(data) && data.SafeOutputs.CreateCodeScanningAlerts != nil && !isHandlerStaged(c.trialMode || templatableBoolIsTrue(data.SafeOutputs.Staged), data.SafeOutputs.CreateCodeScanningAlerts.Staged) {
		consolidatedSafeOutputsJobLog.Print("Exposing sarif_file output for upload_code_scanning_sarif job")
		state.outputs["sarif_file"] = "${{ steps.process_safe_outputs.outputs.sarif_file }}"
		state.steps = append(state.steps, buildSarifArtifactUploadStep(agentArtifactPrefix, c.getActionPin)...)
	}
}

// appendCustomActionSteps resolves and appends the steps for any custom safe-output actions
// declared by the workflow, recording a step name for each so later steps can depend on it.
func (c *Compiler) appendCustomActionSteps(data *WorkflowData, markdownPath string, state *safeOutputsHandlerOutputsAndActionState) error {
	if isWorkQueueParticipant(data) {
		return nil
	}
	if len(data.SafeOutputs.Actions) > 0 {
		if err := c.resolveAllActions(data, markdownPath); err != nil {
			return err
		}
		actionStepYAML, err := c.buildActionSteps(data)
		if err != nil {
			return err
		}
		state.steps = append(state.steps, actionStepYAML...)
		for actionName := range data.SafeOutputs.Actions {
			normalizedName := stringutil.NormalizeSafeOutputIdentifier(actionName)
			state.safeOutputStepNames = append(state.safeOutputStepNames, "action_"+normalizedName)
		}
	}
	return nil
}

// addHandlerManagerOutputs populates the job outputs map with the common outputs exposed by the
// process_safe_outputs handler manager step, plus any conditional per-type outputs.
func addHandlerManagerOutputs(data *WorkflowData, outputs map[string]string) {
	maps.Copy(outputs, map[string]string{
		"process_safe_outputs_processed_count": "${{ steps.process_safe_outputs.outputs.processed_count }}",
		"process_safe_outputs_items_succeeded": "${{ steps.process_safe_outputs.outputs.items_succeeded }}",
		"process_safe_outputs_items_applied":   "${{ steps.process_safe_outputs.outputs.items_applied }}",
		"process_safe_outputs_items_skipped":   "${{ steps.process_safe_outputs.outputs.items_skipped }}",
		"process_safe_outputs_items_warnings":  "${{ steps.process_safe_outputs.outputs.items_warnings }}",
		"process_safe_outputs_items_cancelled": "${{ steps.process_safe_outputs.outputs.items_cancelled }}",
		"process_safe_outputs_items_deferred":  "${{ steps.process_safe_outputs.outputs.items_deferred }}",
		"process_safe_outputs_items_failed":    "${{ steps.process_safe_outputs.outputs.items_failed }}",
		"process_safe_outputs_status":          "${{ steps.process_safe_outputs.outputs.status }}",
		"create_discussion_errors":             "${{ steps.process_safe_outputs.outputs.create_discussion_errors }}",
		"create_discussion_error_count":        "${{ steps.process_safe_outputs.outputs.create_discussion_error_count }}",
		"code_push_failure_errors":             "${{ steps.process_safe_outputs.outputs.code_push_failure_errors }}",
		"code_push_failure_count":              "${{ steps.process_safe_outputs.outputs.code_push_failure_count }}",
	})
	addConditionalHandlerManagerOutputs(data, outputs)
}

// addConditionalHandlerManagerOutputs exposes trusted queue-control and handler-manager
// outputs only when the corresponding queue role or safe-output type is configured.
func addConditionalHandlerManagerOutputs(data *WorkflowData, outputs map[string]string) {
	if isWorkQueueParticipant(data) && data.WorkQueuePolicy != nil {
		outputs["work_queue_requests"] = "${{ steps.work_queue_controls.outputs.work_queue_requests }}"
	}
	if workQueueRequiresAssignment(data) {
		outputs["work_queue_outcome"] = "${{ steps.work_queue_finish.outputs.work_queue_outcome }}"
	}
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

	if exportsActivationCommentLinks(data) {
		outputs["activation_comment_links"] = "${{ steps.process_safe_outputs.outputs.activation_comment_links }}"
	}

	if data.SafeOutputs.PushToPullRequestBranch != nil {
		outputs["push_commit_sha"] = "${{ steps.process_safe_outputs.outputs.push_commit_sha }}"
		outputs["push_commit_url"] = "${{ steps.process_safe_outputs.outputs.push_commit_url }}"
	}

	if data.SafeOutputs.CallWorkflow != nil {
		outputs["call_workflow_name"] = "${{ steps.process_safe_outputs.outputs.call_workflow_name }}"
		outputs["call_workflow_payload"] = "${{ steps.process_safe_outputs.outputs.call_workflow_payload }}"
		if data.SafeOutputs.AuthorizationRequirements["call_workflow"] != nil {
			outputs["call_workflow_record_index"] = "${{ steps.process_safe_outputs.outputs.call_workflow_record_index }}"
		}
	}
}

// exportsActivationCommentLinks reports whether the safe_outputs job may append item links
// (created pull requests, fallback issues, pushed commits) to the activation comment.
// Those links are exported so the conclusion job can preserve them when it re-renders
// the activation comment with the final run status.
func exportsActivationCommentLinks(data *WorkflowData) bool {
	return data.SafeOutputs != nil && (data.SafeOutputs.CreatePullRequests != nil || data.SafeOutputs.PushToPullRequestBranch != nil)
}

package workflow

import (
	"fmt"
)

// ========================================
// Safe Output State Inspection
// ========================================
//
// This file contains functions for querying, inspecting, and validating the
// state of a SafeOutputsConfig. hasAnySafeOutputEnabled and
// hasNonBuiltinSafeOutputsEnabled use direct nil-checks instead of reflection
// for performance (these functions are called on every compilation).
//
// NOTE: Most safe output dispatch behavior is driven by safeOutputHandlers in
// safe_output_handlers.go. The two functions below intentionally remain direct
// nil-check cascades because they are hot-path checks executed on every compile.

// safeOutputFieldMapping maps SafeOutputsConfig struct field names to their tool names.
// This map is used by imports, prompt generation, and other metadata operations.
// It is NOT used for existence checks — see hasAnySafeOutputEnabled and
// hasNonBuiltinSafeOutputsEnabled for the performance-critical direct-field versions.
var safeOutputFieldMapping = buildSafeOutputFieldMapping()

// hasAnySafeOutputEnabled reports whether any safe output field is non-nil.
// It uses direct struct-field nil checks instead of reflection for performance;
// this function is called on every compilation and is on the hot path.
//
// NOTE: keep this function in sync with safeOutputFieldMapping above and
// hasNonBuiltinSafeOutputsEnabled below when adding new safe output types.
func hasAnySafeOutputEnabled(safeOutputs *SafeOutputsConfig) bool { //nolint:largefunc // Existing explicit checks avoid reflection on the compilation hot path.
	if safeOutputs == nil {
		return false
	}

	// Check map fields separately
	if len(safeOutputs.Jobs) > 0 || len(safeOutputs.Scripts) > 0 || len(safeOutputs.Actions) > 0 {
		return true
	}

	// Direct nil checks — no reflection, no heap allocation.
	return safeOutputs.CreateIssues != nil ||
		safeOutputs.CreateWorkItems != nil ||
		safeOutputs.UpdateWorkItems != nil ||
		safeOutputs.CommentOnWorkItems != nil ||
		safeOutputs.AssignWorkItems != nil ||
		safeOutputs.LinkWorkItems != nil ||
		safeOutputs.UploadWorkItemAttachments != nil ||
		hasAnyJiraSafeOutputEnabled(safeOutputs) ||
		safeOutputs.CreateAgentSessions != nil ||
		safeOutputs.CreateDiscussions != nil ||
		safeOutputs.UpdateDiscussions != nil ||
		safeOutputs.CloseDiscussions != nil ||
		safeOutputs.CloseIssues != nil ||
		safeOutputs.ClosePullRequests != nil ||
		safeOutputs.MarkPullRequestAsReadyForReview != nil ||
		safeOutputs.ApproveWorkflowRun != nil ||
		safeOutputs.DismissPullRequestReview != nil ||
		safeOutputs.AddComments != nil ||
		safeOutputs.CreatePullRequests != nil ||
		safeOutputs.CreatePullRequestReviewComments != nil ||
		safeOutputs.SubmitPullRequestReview != nil ||
		safeOutputs.ReplyToPullRequestReviewComment != nil ||
		safeOutputs.ResolvePullRequestReviewThread != nil ||
		safeOutputs.CreateCodeScanningAlerts != nil ||
		safeOutputs.AutofixCodeScanningAlert != nil ||
		safeOutputs.CreateCheckRun != nil ||
		safeOutputs.AddLabels != nil ||
		safeOutputs.RemoveLabels != nil ||
		safeOutputs.ReplaceLabel != nil ||
		safeOutputs.AddReviewer != nil ||
		safeOutputs.AssignMilestone != nil ||
		safeOutputs.AssignToAgent != nil ||
		safeOutputs.AssignToUser != nil ||
		safeOutputs.UnassignFromUser != nil ||
		safeOutputs.UpdateIssues != nil ||
		safeOutputs.UpdatePullRequests != nil ||
		safeOutputs.MergePullRequest != nil ||
		safeOutputs.PushToPullRequestBranch != nil ||
		safeOutputs.UploadAssets != nil ||
		safeOutputs.UploadArtifact != nil ||
		safeOutputs.UploadCodeCoverage != nil ||
		safeOutputs.UpdateRelease != nil ||
		safeOutputs.UpdateProjects != nil ||
		safeOutputs.CreateProjects != nil ||
		safeOutputs.CreateProjectStatusUpdates != nil ||
		safeOutputs.LinkSubIssue != nil ||
		safeOutputs.HideComment != nil ||
		safeOutputs.DispatchWorkflow != nil ||
		safeOutputs.DispatchRepository != nil ||
		safeOutputs.CallWorkflow != nil ||
		safeOutputs.MissingTool != nil ||
		safeOutputs.MissingData != nil ||
		safeOutputs.SetIssueType != nil ||
		safeOutputs.SetIssueField != nil ||
		safeOutputs.NoOp != nil ||
		hasLinearSafeOutputs(safeOutputs)
}

// The builtin types (noop, missing-data, missing-tool) are excluded from this check
// because they are always auto-enabled and do not represent a meaningful output action.
//
// NOTE: keep this function in sync with safeOutputFieldMapping above and
// hasAnySafeOutputEnabled above when adding new safe output types.
func hasNonBuiltinSafeOutputsEnabled(safeOutputs *SafeOutputsConfig) bool { //nolint:largefunc // Existing explicit checks avoid reflection on the compilation hot path.
	if safeOutputs == nil {
		return false
	}

	// Custom safe-jobs, scripts, and actions are always non-builtin
	if len(safeOutputs.Jobs) > 0 || len(safeOutputs.Scripts) > 0 || len(safeOutputs.Actions) > 0 {
		return true
	}

	// Direct nil checks for non-builtin pointer fields.
	return safeOutputs.CreateIssues != nil ||
		safeOutputs.CreateWorkItems != nil ||
		safeOutputs.UpdateWorkItems != nil ||
		safeOutputs.CommentOnWorkItems != nil ||
		safeOutputs.AssignWorkItems != nil ||
		safeOutputs.LinkWorkItems != nil ||
		safeOutputs.UploadWorkItemAttachments != nil ||
		hasAnyJiraSafeOutputEnabled(safeOutputs) ||
		safeOutputs.CreateAgentSessions != nil ||
		safeOutputs.CreateDiscussions != nil ||
		safeOutputs.UpdateDiscussions != nil ||
		safeOutputs.CloseDiscussions != nil ||
		safeOutputs.CloseIssues != nil ||
		safeOutputs.ClosePullRequests != nil ||
		safeOutputs.MarkPullRequestAsReadyForReview != nil ||
		safeOutputs.ApproveWorkflowRun != nil ||
		safeOutputs.DismissPullRequestReview != nil ||
		safeOutputs.AddComments != nil ||
		safeOutputs.CreatePullRequests != nil ||
		safeOutputs.CreatePullRequestReviewComments != nil ||
		safeOutputs.SubmitPullRequestReview != nil ||
		safeOutputs.ReplyToPullRequestReviewComment != nil ||
		safeOutputs.ResolvePullRequestReviewThread != nil ||
		safeOutputs.CreateCodeScanningAlerts != nil ||
		safeOutputs.AutofixCodeScanningAlert != nil ||
		safeOutputs.CreateCheckRun != nil ||
		safeOutputs.AddLabels != nil ||
		safeOutputs.RemoveLabels != nil ||
		safeOutputs.ReplaceLabel != nil ||
		safeOutputs.AddReviewer != nil ||
		safeOutputs.AssignMilestone != nil ||
		safeOutputs.AssignToAgent != nil ||
		safeOutputs.AssignToUser != nil ||
		safeOutputs.UnassignFromUser != nil ||
		safeOutputs.UpdateIssues != nil ||
		safeOutputs.UpdatePullRequests != nil ||
		safeOutputs.MergePullRequest != nil ||
		safeOutputs.PushToPullRequestBranch != nil ||
		safeOutputs.UploadAssets != nil ||
		safeOutputs.UploadArtifact != nil ||
		safeOutputs.UploadCodeCoverage != nil ||
		safeOutputs.UpdateRelease != nil ||
		safeOutputs.UpdateProjects != nil ||
		safeOutputs.CreateProjects != nil ||
		safeOutputs.CreateProjectStatusUpdates != nil ||
		safeOutputs.LinkSubIssue != nil ||
		safeOutputs.HideComment != nil ||
		safeOutputs.DispatchWorkflow != nil ||
		safeOutputs.DispatchRepository != nil ||
		safeOutputs.CallWorkflow != nil ||
		safeOutputs.SetIssueType != nil ||
		safeOutputs.SetIssueField != nil || // non-builtin safe output field
		hasLinearSafeOutputs(safeOutputs)
}

func hasLinearSafeOutputs(safeOutputs *SafeOutputsConfig) bool {
	return safeOutputs != nil &&
		(safeOutputs.LinearCreateIssue != nil ||
			safeOutputs.LinearAddComment != nil ||
			safeOutputs.LinearUpdateIssue != nil)
}

// HasSafeOutputsEnabled checks if any safe-outputs are enabled
func HasSafeOutputsEnabled(safeOutputs *SafeOutputsConfig) bool {
	enabled := hasAnySafeOutputEnabled(safeOutputs)

	if safeOutputsConfigLog.Enabled() {
		safeOutputsConfigLog.Printf("Safe outputs enabled check: %v", enabled)
	}

	return enabled
}

// applyDefaultCreateIssue injects a default create-issues safe output when no non-builtin
// safe outputs are configured (including when safe-outputs is nil / not configured at all).
// The injected config uses the workflow ID as the label and [workflowID] as the title prefix.
// The AutoInjectedCreateIssue flag is set so the prompt generator can add a specific
// instruction for the agent. This aligns create-issue with the other builtin safe outputs
// (noop, missing-tool, missing-data) that are always available.
func applyDefaultCreateIssue(workflowData *WorkflowData) {
	if workflowData.CommentMemoryConfig != nil || hasNonBuiltinSafeOutputsEnabled(workflowData.SafeOutputs) {
		return
	}
	if workflowData.SafeOutputs == nil {
		workflowData.SafeOutputs = &SafeOutputsConfig{}
	}

	workflowID := workflowData.WorkflowID
	safeOutputsConfigLog.Printf("Auto-injecting create-issues for workflow %q (no non-builtin safe outputs configured)", workflowID)
	workflowData.SafeOutputs.CreateIssues = &CreateIssuesConfig{
		BaseSafeOutputConfig: BaseSafeOutputConfig{Max: defaultIntStr(1)},
		Labels:               []string{workflowID},
		TitlePrefix:          fmt.Sprintf("[%s]", workflowID),
	}
	workflowData.SafeOutputs.AutoInjectedCreateIssue = true
}

package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
)

var safeOutputsMaxValidationLog = logger.New("workflow:safe_outputs_max_validation")

// isInvalidMaxValue returns true if n is not a valid max field value.
// Valid values are positive integers (n > 0) or -1 (unlimited).
// Invalid values are 0 and negative integers except -1.
func isInvalidMaxValue(n int) bool {
	if n == -1 {
		return false // -1 = unlimited, explicitly allowed by spec
	}
	return n <= 0
}

// maxInvalidErrSuffix is the common suffix of max validation error messages.
const maxInvalidErrSuffix = "\n\nThe max field controls how many times this safe output can be triggered.\nProvide a positive integer (e.g., max: 1 or max: 5) or -1 for unlimited"

// checkMaxField validates a single safe-output max field value.
// Returns an error if the max value is invalid (0 or negative, except -1).
// Returns nil if the max pointer is nil, the value is an expression, or is valid.
func checkMaxField(toolName string, maxPtr *string) error {
	if maxPtr == nil || isExpression(*maxPtr) {
		return nil
	}
	n, err := strconv.Atoi(*maxPtr)
	if err != nil {
		return nil
	}
	if isInvalidMaxValue(n) {
		toolDisplayName := strings.ReplaceAll(toolName, "_", "-")
		safeOutputsMaxValidationLog.Printf("Invalid max value %d for %s", n, toolDisplayName)
		return fmt.Errorf(
			"safe-outputs.%s: max must be a positive integer or -1 (unlimited), got %d%s",
			toolDisplayName, n, maxInvalidErrSuffix,
		)
	}
	return nil
}

// validateSafeOutputsMax validates that all max fields in safe-outputs configs hold valid values.
// Valid values are positive integers (n > 0) or -1 (unlimited per spec).
// 0 and other negative values are rejected.
// GitHub Actions expressions (e.g. "${{ inputs.max }}") are not evaluable at compile time
// and are therefore skipped.
//
// This function uses direct struct field access instead of reflection for performance;
// it is on the hot path and called on every compilation. The field ordering matches
// the sorted safeOutputFieldMapping keys for deterministic error reporting.
//
//nolint:largefunc // Direct field access intentionally keeps all safe-output max checks together.
func validateSafeOutputsMax(config *SafeOutputsConfig) error {
	if config == nil {
		return nil
	}

	safeOutputsMaxValidationLog.Print("Validating safe-outputs max fields")

	// Direct field access — no reflection, no heap allocation.
	// Fields are checked in the alphabetical order of their struct field names,
	// matching the sort order of safeOutputFieldMapping keys for deterministic
	// error reporting.
	if config.AssignWorkItems != nil {
		if err := checkMaxField("ado_assign_work_item", config.AssignWorkItems.Max); err != nil {
			return err
		}
	}
	if config.CommentOnWorkItems != nil {
		if err := checkMaxField("ado_comment_on_work_item", config.CommentOnWorkItems.Max); err != nil {
			return err
		}
	}
	if config.CreateWorkItems != nil {
		if err := checkMaxField("ado_create_work_item", config.CreateWorkItems.Max); err != nil {
			return err
		}
	}
	if config.LinkWorkItems != nil {
		if err := checkMaxField("ado_link_work_items", config.LinkWorkItems.Max); err != nil {
			return err
		}
	}
	if config.UpdateWorkItems != nil {
		if err := checkMaxField("ado_update_work_item", config.UpdateWorkItems.Max); err != nil {
			return err
		}
	}
	if config.UploadWorkItemAttachments != nil {
		if err := checkMaxField("ado_upload_workitem_attachment", config.UploadWorkItemAttachments.Max); err != nil {
			return err
		}
	}
	if config.AddComments != nil {
		if err := checkMaxField("add_comment", config.AddComments.Max); err != nil {
			return err
		}
	}
	if config.AddLabels != nil {
		if err := checkMaxField("add_labels", config.AddLabels.Max); err != nil {
			return err
		}
	}
	if config.AddReviewer != nil {
		if err := checkMaxField("add_reviewer", config.AddReviewer.Max); err != nil {
			return err
		}
	}
	if config.AssignMilestone != nil {
		if err := checkMaxField("assign_milestone", config.AssignMilestone.Max); err != nil {
			return err
		}
	}
	if config.AssignToAgent != nil {
		if err := checkMaxField("assign_to_agent", config.AssignToAgent.Max); err != nil {
			return err
		}
	}
	if config.AssignToUser != nil {
		if err := checkMaxField("assign_to_user", config.AssignToUser.Max); err != nil {
			return err
		}
	}
	if config.AutofixCodeScanningAlert != nil {
		if err := checkMaxField("autofix_code_scanning_alert", config.AutofixCodeScanningAlert.Max); err != nil {
			return err
		}
	}
	if config.CallWorkflow != nil {
		if err := checkMaxField("call_workflow", config.CallWorkflow.Max); err != nil {
			return err
		}
	}
	if config.CloseDiscussions != nil {
		if err := checkMaxField("close_discussion", config.CloseDiscussions.Max); err != nil {
			return err
		}
	}
	if config.CloseIssues != nil {
		if err := checkMaxField("close_issue", config.CloseIssues.Max); err != nil {
			return err
		}
	}
	if config.ClosePullRequests != nil {
		if err := checkMaxField("close_pull_request", config.ClosePullRequests.Max); err != nil {
			return err
		}
	}
	if config.CreateAgentSessions != nil {
		if err := checkMaxField("create_agent_session", config.CreateAgentSessions.Max); err != nil {
			return err
		}
	}
	if config.CreateCheckRun != nil {
		if err := checkMaxField("create_check_run", config.CreateCheckRun.Max); err != nil {
			return err
		}
	}
	if config.CreateCodeScanningAlerts != nil {
		if err := checkMaxField("create_code_scanning_alert", config.CreateCodeScanningAlerts.Max); err != nil {
			return err
		}
	}
	if config.CreateDiscussions != nil {
		if err := checkMaxField("create_discussion", config.CreateDiscussions.Max); err != nil {
			return err
		}
	}
	if config.CreateIssues != nil {
		if err := checkMaxField("create_issue", config.CreateIssues.Max); err != nil {
			return err
		}
	}
	if config.CreateProjectStatusUpdates != nil {
		if err := checkMaxField("create_project_status_update", config.CreateProjectStatusUpdates.Max); err != nil {
			return err
		}
	}
	if config.CreateProjects != nil {
		if err := checkMaxField("create_project", config.CreateProjects.Max); err != nil {
			return err
		}
	}
	if config.CreatePullRequestReviewComments != nil {
		if err := checkMaxField("create_pull_request_review_comment", config.CreatePullRequestReviewComments.Max); err != nil {
			return err
		}
	}
	if config.CreatePullRequests != nil {
		if err := checkMaxField("create_pull_request", config.CreatePullRequests.Max); err != nil {
			return err
		}
	}
	if config.DispatchWorkflow != nil {
		if err := checkMaxField("dispatch_workflow", config.DispatchWorkflow.Max); err != nil {
			return err
		}
	}
	if config.DismissPullRequestReview != nil {
		if err := checkMaxField("dismiss_pull_request_review", config.DismissPullRequestReview.Max); err != nil {
			return err
		}
	}
	if config.HideComment != nil {
		if err := checkMaxField("hide_comment", config.HideComment.Max); err != nil {
			return err
		}
	}
	if config.JiraAddComment != nil {
		if err := checkMaxField("jira_add_comment", config.JiraAddComment.Max); err != nil {
			return err
		}
	}
	if config.JiraAddLabel != nil {
		if err := checkMaxField("jira_add_label", config.JiraAddLabel.Max); err != nil {
			return err
		}
	}
	if config.JiraCreateIssue != nil {
		if err := checkMaxField("jira_create_issue", config.JiraCreateIssue.Max); err != nil {
			return err
		}
	}
	if config.JiraUpdateIssue != nil {
		if err := checkMaxField("jira_update_issue", config.JiraUpdateIssue.Max); err != nil {
			return err
		}
	}
	if config.LinkSubIssue != nil {
		if err := checkMaxField("link_sub_issue", config.LinkSubIssue.Max); err != nil {
			return err
		}
	}
	if err := validateLinearSafeOutputsMax(config); err != nil {
		return err
	}
	if config.MarkPullRequestAsReadyForReview != nil {
		if err := checkMaxField("mark_pull_request_as_ready_for_review", config.MarkPullRequestAsReadyForReview.Max); err != nil {
			return err
		}

	}
	if config.ApproveWorkflowRun != nil {
		if err := checkMaxField("approve_workflow_run", config.ApproveWorkflowRun.Max); err != nil {
			return err
		}
	}
	if config.MergePullRequest != nil {
		if err := checkMaxField("merge_pull_request", config.MergePullRequest.Max); err != nil {
			return err
		}
	}
	if config.MissingData != nil {
		if err := checkMaxField("missing_data", config.MissingData.Max); err != nil {
			return err
		}
	}
	if config.MissingTool != nil {
		if err := checkMaxField("missing_tool", config.MissingTool.Max); err != nil {
			return err
		}
	}
	if config.NoOp != nil {
		if err := checkMaxField("noop", config.NoOp.Max); err != nil {
			return err
		}
	}
	if config.PushToPullRequestBranch != nil {
		if err := checkMaxField("push_to_pull_request_branch", config.PushToPullRequestBranch.Max); err != nil {
			return err
		}
	}
	if config.RemoveLabels != nil {
		if err := checkMaxField("remove_labels", config.RemoveLabels.Max); err != nil {
			return err
		}
	}
	if config.ReplaceLabel != nil {
		if err := checkMaxField("replace_label", config.ReplaceLabel.Max); err != nil {
			return err
		}
	}
	if config.ReplyToPullRequestReviewComment != nil {
		if err := checkMaxField("reply_to_pull_request_review_comment", config.ReplyToPullRequestReviewComment.Max); err != nil {
			return err
		}
	}
	if config.ResolvePullRequestReviewThread != nil {
		if err := checkMaxField("resolve_pull_request_review_thread", config.ResolvePullRequestReviewThread.Max); err != nil {
			return err
		}
	}
	if config.SetIssueType != nil {
		if err := checkMaxField("set_issue_type", config.SetIssueType.Max); err != nil {
			return err
		}
	}
	if config.SetIssueField != nil {
		if err := checkMaxField("set_issue_field", config.SetIssueField.Max); err != nil {
			return err
		}
	}
	if config.SubmitPullRequestReview != nil {
		if err := checkMaxField("submit_pull_request_review", config.SubmitPullRequestReview.Max); err != nil {
			return err
		}
	}
	if config.UnassignFromUser != nil {
		if err := checkMaxField("unassign_from_user", config.UnassignFromUser.Max); err != nil {
			return err
		}
	}
	if config.UpdateDiscussions != nil {
		if err := checkMaxField("update_discussion", config.UpdateDiscussions.Max); err != nil {
			return err
		}
	}
	if config.UpdateIssues != nil {
		if err := checkMaxField("update_issue", config.UpdateIssues.Max); err != nil {
			return err
		}
	}
	if config.UpdateProjects != nil {
		if err := checkMaxField("update_project", config.UpdateProjects.Max); err != nil {
			return err
		}
	}
	if config.UpdatePullRequests != nil {
		if err := checkMaxField("update_pull_request", config.UpdatePullRequests.Max); err != nil {
			return err
		}
	}
	if config.UpdateRelease != nil {
		if err := checkMaxField("update_release", config.UpdateRelease.Max); err != nil {
			return err
		}
	}
	if config.UploadArtifact != nil {
		if err := checkMaxField("upload_artifact", config.UploadArtifact.Max); err != nil {
			return err
		}
	}
	if config.UploadAssets != nil {
		if err := checkMaxField("upload_asset", config.UploadAssets.Max); err != nil {
			return err
		}
	}
	if config.UploadCodeCoverage != nil {
		if err := checkMaxField("upload_code_coverage", config.UploadCodeCoverage.Max); err != nil {
			return err
		}
	}

	// Validate max on dispatch_repository tools (different structure: map of tools).
	// Use sorted tool names for deterministic error reporting.
	if config.DispatchRepository != nil {
		sortedToolNames := sliceutil.SortedKeys(config.DispatchRepository.Tools)

		for _, toolName := range sortedToolNames {
			tool := config.DispatchRepository.Tools[toolName]
			if tool == nil || tool.Max == nil || isExpression(*tool.Max) {
				continue
			}

			n, err := strconv.Atoi(*tool.Max)
			if err != nil {
				continue
			}

			if isInvalidMaxValue(n) {
				safeOutputsMaxValidationLog.Printf("Invalid max value %d for dispatch_repository tool %s", n, toolName)
				return fmt.Errorf(
					"safe-outputs.dispatch_repository.%s: max must be a positive integer or -1 (unlimited), got %d%s",
					toolName, n, maxInvalidErrSuffix,
				)
			}
		}
	}

	safeOutputsMaxValidationLog.Print("Safe-outputs max fields validation passed")
	return nil
}

func validateLinearSafeOutputsMax(config *SafeOutputsConfig) error {
	handlers := []struct {
		name string
		max  *string
	}{
		{name: "linear_add_comment", max: config.linearAddCommentMax()},
		{name: "linear_create_issue", max: config.linearCreateIssueMax()},
		{name: "linear_update_issue", max: config.linearUpdateIssueMax()},
	}
	for _, handler := range handlers {
		if err := checkMaxField(handler.name, handler.max); err != nil {
			return err
		}
	}
	return nil
}

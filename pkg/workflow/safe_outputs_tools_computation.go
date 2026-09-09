package workflow

import "github.com/github/gh-aw/pkg/logger"

var safeOutputsToolsComputationLog = logger.New("workflow:safe_outputs_tools_computation")

// computeEnabledToolNames returns the set of predefined tool names that are enabled
// by the workflow's SafeOutputsConfig. Dynamic tools (dispatch-workflow, custom jobs,
// call-workflow) are excluded because they are generated separately.
//
//nolint:largefunc // Built-in tool enablement is kept as one exhaustive mapping.
func computeEnabledToolNames(data *WorkflowData) map[string]struct {
} {
	enabledTools := make(map[string]struct {
	})
	if data.SafeOutputs == nil {
		safeOutputsToolsComputationLog.Print("No safe outputs configuration, returning empty tool set")
		return enabledTools
	}

	if data.SafeOutputs.CreateIssues != nil {
		enabledTools["create_issue"] = struct {
		}{}
	}
	if data.SafeOutputs.CreateWorkItems != nil {
		enabledTools["ado_create_work_item"] = struct {
		}{}
	}
	if data.SafeOutputs.UpdateWorkItems != nil {
		enabledTools["ado_update_work_item"] = struct {
		}{}
	}
	if data.SafeOutputs.CommentOnWorkItems != nil {
		enabledTools["ado_comment_on_work_item"] = struct {
		}{}
	}
	if data.SafeOutputs.AssignWorkItems != nil {
		enabledTools["ado_assign_work_item"] = struct {
		}{}
	}
	if data.SafeOutputs.LinkWorkItems != nil {
		enabledTools["ado_link_work_items"] = struct {
		}{}
	}
	if data.SafeOutputs.UploadWorkItemAttachments != nil {
		enabledTools["ado_upload_workitem_attachment"] = struct {
		}{}
	}
	if data.SafeOutputs.LinearCreateIssue != nil {
		enabledTools["linear_create_issue"] = struct{}{}
	}
	if data.SafeOutputs.LinearAddComment != nil {
		enabledTools["linear_add_comment"] = struct{}{}
	}
	if data.SafeOutputs.LinearUpdateIssue != nil {
		enabledTools["linear_update_issue"] = struct{}{}
	}
	if data.SafeOutputs.CreateAgentSessions != nil {
		enabledTools["create_agent_session"] = struct {
		}{}
	}
	if data.SafeOutputs.CreateDiscussions != nil {
		enabledTools["create_discussion"] = struct {
		}{}
	}
	if data.SafeOutputs.UpdateDiscussions != nil {
		enabledTools["update_discussion"] = struct {
		}{}
	}
	if data.SafeOutputs.CloseDiscussions != nil {
		enabledTools["close_discussion"] = struct {
		}{}
	}
	if data.SafeOutputs.CloseIssues != nil {
		enabledTools["close_issue"] = struct {
		}{}
	}
	if data.SafeOutputs.ClosePullRequests != nil {
		enabledTools["close_pull_request"] = struct {
		}{}
	}
	if data.SafeOutputs.MarkPullRequestAsReadyForReview != nil {
		enabledTools["mark_pull_request_as_ready_for_review"] = struct {
		}{}
	}
	if data.SafeOutputs.ApproveWorkflowRun != nil {
		enabledTools["approve_workflow_run"] = struct {
		}{}
	}
	if data.SafeOutputs.DismissPullRequestReview != nil {
		enabledTools["dismiss_pull_request_review"] = struct {
		}{}
	}
	if data.SafeOutputs.AddComments != nil {
		enabledTools["add_comment"] = struct {
		}{}
	}
	if data.SafeOutputs.CreatePullRequests != nil {
		enabledTools["create_pull_request"] = struct {
		}{}
	}
	if data.SafeOutputs.CreatePullRequestReviewComments != nil {
		enabledTools["create_pull_request_review_comment"] = struct {
		}{}
	}
	if data.SafeOutputs.SubmitPullRequestReview != nil {
		enabledTools["submit_pull_request_review"] = struct {
		}{}
	}
	if data.SafeOutputs.ReplyToPullRequestReviewComment != nil {
		enabledTools["reply_to_pull_request_review_comment"] = struct {
		}{}
	}
	if data.SafeOutputs.ResolvePullRequestReviewThread != nil {
		enabledTools["resolve_pull_request_review_thread"] = struct {
		}{}
	}
	if data.SafeOutputs.CreateCodeScanningAlerts != nil {
		enabledTools["create_code_scanning_alert"] = struct {
		}{}
	}
	if data.SafeOutputs.AutofixCodeScanningAlert != nil {
		enabledTools["autofix_code_scanning_alert"] = struct {
		}{}
	}
	if data.SafeOutputs.CreateCheckRun != nil {
		enabledTools["create_check_run"] = struct {
		}{}
	}
	if data.SafeOutputs.AddLabels != nil {
		enabledTools["add_labels"] = struct {
		}{}
	}
	if data.SafeOutputs.RemoveLabels != nil {
		enabledTools["remove_labels"] = struct {
		}{}
	}
	if data.SafeOutputs.ReplaceLabel != nil {
		enabledTools["replace_label"] = struct {
		}{}
	}
	if data.SafeOutputs.AddReviewer != nil {
		enabledTools["add_reviewer"] = struct {
		}{}
	}
	if data.SafeOutputs.AssignMilestone != nil {
		enabledTools["assign_milestone"] = struct {
		}{}
	}
	if data.SafeOutputs.AssignToAgent != nil {
		enabledTools["assign_to_agent"] = struct {
		}{}
	}
	if data.SafeOutputs.AssignToUser != nil {
		enabledTools["assign_to_user"] = struct {
		}{}
	}
	if data.SafeOutputs.UnassignFromUser != nil {
		enabledTools["unassign_from_user"] = struct {
		}{}
	}
	if data.SafeOutputs.UpdateIssues != nil {
		enabledTools["update_issue"] = struct {
		}{}
	}
	if data.SafeOutputs.UpdatePullRequests != nil {
		enabledTools["update_pull_request"] = struct {
		}{}
	}
	if data.SafeOutputs.PushToPullRequestBranch != nil {
		enabledTools["push_to_pull_request_branch"] = struct {
		}{}
	}
	if data.SafeOutputs.UploadAssets != nil {
		enabledTools["upload_asset"] = struct {
		}{}
	}
	if data.SafeOutputs.UploadArtifact != nil {
		enabledTools["upload_artifact"] = struct {
		}{}
	}
	if data.SafeOutputs.UploadCodeCoverage != nil {
		enabledTools["upload_code_coverage"] = struct {
		}{}
	}
	if data.SafeOutputs.MissingTool != nil {
		enabledTools["missing_tool"] = struct {
		}{}
	}
	if data.SafeOutputs.MissingData != nil {
		enabledTools["missing_data"] = struct {
		}{}
	}
	if data.SafeOutputs.UpdateRelease != nil {
		enabledTools["update_release"] = struct {
		}{}
	}
	if data.SafeOutputs.NoOp != nil {
		enabledTools["noop"] = struct {
		}{}
	}
	if data.SafeOutputs.LinkSubIssue != nil {
		enabledTools["link_sub_issue"] = struct {
		}{}
	}
	if data.SafeOutputs.HideComment != nil {
		enabledTools["hide_comment"] = struct {
		}{}
	}
	if data.SafeOutputs.SetIssueType != nil {
		enabledTools["set_issue_type"] = struct {
		}{}
	}
	if data.SafeOutputs.SetIssueField != nil {
		enabledTools["set_issue_field"] = struct {
		}{}
	}
	if data.SafeOutputs.UpdateProjects != nil {
		enabledTools["update_project"] = struct {
		}{}
	}
	if data.SafeOutputs.CreateProjectStatusUpdates != nil {
		enabledTools["create_project_status_update"] = struct {
		}{}
	}
	if data.SafeOutputs.CreateProjects != nil {
		enabledTools["create_project"] = struct {
		}{}
	}

	// Add push_repo_memory tool if repo-memory is configured
	if data.RepoMemoryConfig != nil && len(data.RepoMemoryConfig.Memories) > 0 {
		enabledTools["push_repo_memory"] = struct {
		}{}
	}

	safeOutputsToolsComputationLog.Printf("Computed %d enabled safe output tool names", len(enabledTools))
	return enabledTools
}

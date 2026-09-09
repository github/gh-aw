package workflow

import (
	"reflect"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/typeutil"
)

var safeOutputHandlerLog = logger.New("workflow:safe_output_handlers")

type safeOutputHandlerDescriptor struct {
	Key               string
	Aliases           []string
	StructField       string
	ToolName          string
	Builtin           bool
	NewConfig         func() any
	PermissionBuilder func(*SafeOutputsConfig) *Permissions
}

var safeOutputHandlers = []safeOutputHandlerDescriptor{
	{
		Key:         "ado-create-work-item",
		StructField: "CreateWorkItems",
		ToolName:    "ado_create_work_item",
		NewConfig:   func() any { return &CreateWorkItemConfig{} },
	},
	{
		Key:         "ado-update-work-item",
		StructField: "UpdateWorkItems",
		ToolName:    "ado_update_work_item",
		NewConfig:   func() any { return &UpdateWorkItemConfig{} },
	},
	{
		Key:         "ado-comment-on-work-item",
		StructField: "CommentOnWorkItems",
		ToolName:    "ado_comment_on_work_item",
		NewConfig:   func() any { return &CommentOnWorkItemConfig{} },
	},
	{
		Key:         "ado-assign-work-item",
		StructField: "AssignWorkItems",
		ToolName:    "ado_assign_work_item",
		NewConfig:   func() any { return &AssignWorkItemConfig{} },
	},
	{
		Key:         "ado-link-work-items",
		StructField: "LinkWorkItems",
		ToolName:    "ado_link_work_items",
		NewConfig:   func() any { return &LinkWorkItemsConfig{} },
	},
	{
		Key:         "ado-upload-workitem-attachment",
		StructField: "UploadWorkItemAttachments",
		ToolName:    "ado_upload_workitem_attachment",
		NewConfig:   func() any { return &UploadWorkItemAttachmentConfig{} },
	},
	{
		Key:         "linear-create-issue",
		StructField: "LinearCreateIssue",
		ToolName:    "linear_create_issue",
		NewConfig:   func() any { return &LinearCreateIssueConfig{} },
	},
	{
		Key:         "linear-add-comment",
		StructField: "LinearAddComment",
		ToolName:    "linear_add_comment",
		NewConfig:   func() any { return &LinearTargetConfig{} },
	},
	{
		Key:         "linear-update-issue",
		StructField: "LinearUpdateIssue",
		ToolName:    "linear_update_issue",
		NewConfig:   func() any { return &LinearUpdateIssueConfig{} },
	},
	{
		Key:         "create-issue",
		StructField: "CreateIssues",
		ToolName:    "create_issue",
		NewConfig:   func() any { return &CreateIssuesConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateIssues") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "jira-create-issue",
		StructField: "JiraCreateIssue",
		ToolName:    "jira_create_issue",
		NewConfig:   func() any { return &JiraSafeOutputConfig{} },
	},
	{
		Key:         "jira-update-issue",
		StructField: "JiraUpdateIssue",
		ToolName:    "jira_update_issue",
		NewConfig:   func() any { return &JiraSafeOutputConfig{} },
	},
	{
		Key:         "jira-add-comment",
		StructField: "JiraAddComment",
		ToolName:    "jira_add_comment",
		NewConfig:   func() any { return &JiraSafeOutputConfig{} },
	},
	{
		Key:         "jira-add-label",
		StructField: "JiraAddLabel",
		ToolName:    "jira_add_label",
		NewConfig:   func() any { return &JiraSafeOutputConfig{} },
	},
	{
		Key:         "create-agent-session",
		Aliases:     []string{"create-agent-task"},
		StructField: "CreateAgentSessions",
		ToolName:    "create_agent_session",
		NewConfig:   func() any { return &CreateAgentSessionConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateAgentSessions") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "create-discussion",
		StructField: "CreateDiscussions",
		ToolName:    "create_discussion",
		NewConfig:   func() any { return &CreateDiscussionsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateDiscussions") {
				return nil
			}
			return NewPermissionsIssuesWriteDiscussionsWrite()
		},
	},
	{
		Key:         "update-discussion",
		StructField: "UpdateDiscussions",
		ToolName:    "update_discussion",
		NewConfig:   func() any { return &UpdateDiscussionsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "UpdateDiscussions") {
				return nil
			}
			return NewPermissionsDiscussionsWrite()
		},
	},
	{
		Key:         "close-discussion",
		StructField: "CloseDiscussions",
		ToolName:    "close_discussion",
		NewConfig:   func() any { return &CloseDiscussionsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CloseDiscussions") {
				return nil
			}
			return NewPermissionsDiscussionsWrite()
		},
	},
	{
		Key:         "close-issue",
		StructField: "CloseIssues",
		ToolName:    "close_issue",
		NewConfig:   func() any { return &CloseIssuesConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CloseIssues") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "close-pull-request",
		StructField: "ClosePullRequests",
		ToolName:    "close_pull_request",
		NewConfig:   func() any { return &ClosePullRequestsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "ClosePullRequests") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "mark-pull-request-as-ready-for-review",
		StructField: "MarkPullRequestAsReadyForReview",
		ToolName:    "mark_pull_request_as_ready_for_review",
		NewConfig:   func() any { return &MarkPullRequestAsReadyForReviewConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "MarkPullRequestAsReadyForReview") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "approve-workflow-run",
		StructField: "ApproveWorkflowRun",
		ToolName:    "approve_workflow_run",
		NewConfig:   func() any { return &ApproveWorkflowRunConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "ApproveWorkflowRun") {
				return nil
			}
			// pull-requests write is only needed when the handler posts a comment on the
			// pull request associated with the approved run; otherwise read is sufficient.
			pullRequestsLevel := PermissionRead
			if safeOutputs.ApproveWorkflowRun != nil && safeOutputs.ApproveWorkflowRun.Comment {
				pullRequestsLevel = PermissionWrite
			}
			return NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
				PermissionActions:      PermissionWrite,
				PermissionPullRequests: pullRequestsLevel,
			})
		},
	},
	{
		Key:         "dismiss-pull-request-review",
		Aliases:     []string{"dismiss-review"},
		StructField: "DismissPullRequestReview",
		ToolName:    "dismiss_pull_request_review",
		NewConfig:   func() any { return &DismissPullRequestReviewConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "DismissPullRequestReview") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "add-comment",
		StructField: "AddComments",
		ToolName:    "add_comment",
		NewConfig:   func() any { return &AddCommentsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AddComments") {
				return nil
			}
			return buildAddCommentPermissions(safeOutputs.AddComments)
		},
	},
	{
		Key:         "create-pull-request",
		StructField: "CreatePullRequests",
		ToolName:    "create_pull_request",
		NewConfig:   func() any { return &CreatePullRequestsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreatePullRequests") {
				return nil
			}
			if getFallbackAsIssue(safeOutputs.CreatePullRequests) {
				permissions := NewPermissionsContentsWriteIssuesWritePRWrite()
				if safeOutputs.CreatePullRequests.AllowWorkflows {
					permissions.Set(PermissionWorkflows, PermissionWrite)
				}
				return permissions
			}
			permissions := NewPermissionsContentsWritePRWrite()
			if safeOutputs.CreatePullRequests.AllowWorkflows {
				permissions.Set(PermissionWorkflows, PermissionWrite)
			}
			// close-older-pull-requests requires issues: write to add closing comments
			if isCloseOlderPullRequestsEnabled(safeOutputs.CreatePullRequests) {
				permissions.Set(PermissionIssues, PermissionWrite)
			}
			return permissions
		},
	},
	{
		Key:         "create-pull-request-review-comment",
		StructField: "CreatePullRequestReviewComments",
		ToolName:    "create_pull_request_review_comment",
		NewConfig:   func() any { return &CreatePullRequestReviewCommentsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreatePullRequestReviewComments") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "submit-pull-request-review",
		StructField: "SubmitPullRequestReview",
		ToolName:    "submit_pull_request_review",
		NewConfig:   func() any { return &SubmitPullRequestReviewConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "SubmitPullRequestReview") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "reply-to-pull-request-review-comment",
		StructField: "ReplyToPullRequestReviewComment",
		ToolName:    "reply_to_pull_request_review_comment",
		NewConfig:   func() any { return &ReplyToPullRequestReviewCommentConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "ReplyToPullRequestReviewComment") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "resolve-pull-request-review-thread",
		StructField: "ResolvePullRequestReviewThread",
		ToolName:    "resolve_pull_request_review_thread",
		NewConfig:   func() any { return &ResolvePullRequestReviewThreadConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "ResolvePullRequestReviewThread") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "create-code-scanning-alert",
		StructField: "CreateCodeScanningAlerts",
		ToolName:    "create_code_scanning_alert",
		NewConfig:   func() any { return &CreateCodeScanningAlertsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateCodeScanningAlerts") {
				return nil
			}
			return NewPermissionsSecurityEventsWrite()
		},
	},
	{
		Key:         "autofix-code-scanning-alert",
		StructField: "AutofixCodeScanningAlert",
		ToolName:    "autofix_code_scanning_alert",
		NewConfig:   func() any { return &AutofixCodeScanningAlertConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AutofixCodeScanningAlert") {
				return nil
			}
			return NewPermissionsSecurityEventsWriteActionsRead()
		},
	},
	{
		Key:         "create-check-run",
		StructField: "CreateCheckRun",
		ToolName:    "create_check_run",
		NewConfig:   func() any { return &CreateCheckRunConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateCheckRun") {
				return nil
			}
			if safeOutputs.CreateCheckRun != nil && safeOutputs.CreateCheckRun.Target != "" {
				return NewPermissionsChecksWritePRRead()
			}
			return NewPermissionsChecksWrite()
		},
	},
	{
		Key:         "add-labels",
		StructField: "AddLabels",
		ToolName:    "add_labels",
		NewConfig:   func() any { return &AddLabelsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AddLabels") {
				return nil
			}
			return buildAddLabelsPermissions(safeOutputs.AddLabels)
		},
	},
	{
		Key:         "remove-labels",
		StructField: "RemoveLabels",
		ToolName:    "remove_labels",
		NewConfig:   func() any { return &RemoveLabelsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "RemoveLabels") {
				return nil
			}
			return buildRemoveLabelsPermissions(safeOutputs.RemoveLabels)
		},
	},
	{
		Key:         "replace-label",
		StructField: "ReplaceLabel",
		ToolName:    "replace_label",
		NewConfig:   func() any { return &ReplaceLabelConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "ReplaceLabel") {
				return nil
			}
			return NewPermissionsIssuesWritePRWrite()
		},
	},
	{
		Key:         "add-reviewer",
		StructField: "AddReviewer",
		ToolName:    "add_reviewer",
		NewConfig:   func() any { return &AddReviewerConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AddReviewer") {
				return nil
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "assign-milestone",
		StructField: "AssignMilestone",
		ToolName:    "assign_milestone",
		NewConfig:   func() any { return &AssignMilestoneConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AssignMilestone") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "assign-to-agent",
		StructField: "AssignToAgent",
		ToolName:    "assign_to_agent",
		NewConfig:   func() any { return &AssignToAgentConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AssignToAgent") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "assign-to-user",
		StructField: "AssignToUser",
		ToolName:    "assign_to_user",
		NewConfig:   func() any { return &AssignToUserConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "AssignToUser") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "unassign-from-user",
		StructField: "UnassignFromUser",
		ToolName:    "unassign_from_user",
		NewConfig:   func() any { return &UnassignFromUserConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "UnassignFromUser") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "update-issue",
		StructField: "UpdateIssues",
		ToolName:    "update_issue",
		NewConfig:   func() any { return &UpdateIssuesConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "UpdateIssues") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "update-pull-request",
		StructField: "UpdatePullRequests",
		ToolName:    "update_pull_request",
		NewConfig:   func() any { return &UpdatePullRequestsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "UpdatePullRequests") {
				return nil
			}
			if safeOutputs.UpdatePullRequests.UpdateBranch != nil && *safeOutputs.UpdatePullRequests.UpdateBranch {
				return NewPermissionsContentsWritePRWrite()
			}
			return NewPermissionsPRWrite()
		},
	},
	{
		Key:         "merge-pull-request",
		StructField: "MergePullRequest",
		ToolName:    "merge_pull_request",
		NewConfig:   func() any { return &MergePullRequestConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "MergePullRequest") {
				return nil
			}
			return NewPermissionsContentsWritePRWrite()
		},
	},
	{
		Key:         "push-to-pull-request-branch",
		StructField: "PushToPullRequestBranch",
		ToolName:    "push_to_pull_request_branch",
		NewConfig:   func() any { return &PushToPullRequestBranchConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "PushToPullRequestBranch") {
				return nil
			}
			permissions := NewPermissions()
			if getPushFallbackAsPullRequest(safeOutputs.PushToPullRequestBranch) {
				permissions.Merge(NewPermissionsContentsWritePRWrite())
			} else {
				permissions.Merge(NewPermissionsContentsWrite())
			}
			if safeOutputs.PushToPullRequestBranch.AllowWorkflows {
				permissions.Set(PermissionWorkflows, PermissionWrite)
			}
			if getCheckBranchProtection(safeOutputs.PushToPullRequestBranch) {
				permissions.Set(PermissionAdministration, PermissionRead)
			}
			return permissions
		},
	},
	{
		Key:         "upload-asset",
		StructField: "UploadAssets",
		ToolName:    "upload_asset",
		NewConfig:   func() any { return &UploadAssetsConfig{} },
	},
	{
		Key:         "upload-artifact",
		StructField: "UploadArtifact",
		ToolName:    "upload_artifact",
	},
	{
		Key:         "upload-code-coverage",
		StructField: "UploadCodeCoverage",
		ToolName:    "upload_code_coverage",
	},
	{
		Key:         "update-release",
		StructField: "UpdateRelease",
		ToolName:    "update_release",
		NewConfig:   func() any { return &UpdateReleaseConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "UpdateRelease") {
				return nil
			}
			return NewPermissionsContentsWrite()
		},
	},
	{
		Key:         "update-project",
		StructField: "UpdateProjects",
		ToolName:    "update_project",
		NewConfig:   func() any { return &UpdateProjectConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "UpdateProjects") {
				return nil
			}
			return NewPermissionsOrganizationProjWriteIssuesRead()
		},
	},
	{
		Key:         "create-project",
		StructField: "CreateProjects",
		ToolName:    "create_project",
		NewConfig:   func() any { return &CreateProjectsConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateProjects") {
				return nil
			}
			return NewPermissionsOrganizationProjWriteIssuesRead()
		},
	},
	{
		Key:         "create-project-status-update",
		StructField: "CreateProjectStatusUpdates",
		ToolName:    "create_project_status_update",
		NewConfig:   func() any { return &CreateProjectStatusUpdateConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "CreateProjectStatusUpdates") {
				return nil
			}
			return NewPermissionsOrganizationProjWrite()
		},
	},
	{
		Key:         "link-sub-issue",
		StructField: "LinkSubIssue",
		ToolName:    "link_sub_issue",
		NewConfig:   func() any { return &LinkSubIssueConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "LinkSubIssue") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "hide-comment",
		StructField: "HideComment",
		ToolName:    "hide_comment",
		NewConfig:   func() any { return &HideCommentConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "HideComment") {
				return nil
			}
			if safeOutputs.HideComment.Discussions != nil && *safeOutputs.HideComment.Discussions {
				return NewPermissionsIssuesWriteDiscussionsWrite()
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "dispatch-workflow",
		StructField: "DispatchWorkflow",
		ToolName:    "dispatch_workflow",
		NewConfig:   func() any { return &DispatchWorkflowConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "DispatchWorkflow") {
				return nil
			}
			return NewPermissionsActionsWrite()
		},
	},
	{
		Key:         "dispatch_repository",
		StructField: "DispatchRepository",
		ToolName:    "dispatch_repository",
		NewConfig:   func() any { return &DispatchRepositoryConfig{} },
	},
	{
		Key:         "call-workflow",
		StructField: "CallWorkflow",
		ToolName:    "call_workflow",
		NewConfig:   func() any { return &CallWorkflowConfig{} },
	},
	{
		Key:         "missing-tool",
		StructField: "MissingTool",
		ToolName:    "missing_tool",
		Builtin:     true,
	},
	{
		Key:         "missing-data",
		StructField: "MissingData",
		ToolName:    "missing_data",
		Builtin:     true,
	},
	{
		Key:         "set-issue-type",
		StructField: "SetIssueType",
		ToolName:    "set_issue_type",
		NewConfig:   func() any { return &SetIssueTypeConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "SetIssueType") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "set-issue-field",
		StructField: "SetIssueField",
		ToolName:    "set_issue_field",
		NewConfig:   func() any { return &SetIssueFieldConfig{} },
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "SetIssueField") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "noop",
		StructField: "NoOp",
		ToolName:    "noop",
		Builtin:     true,
	},
	{
		Key:         "report-incomplete",
		StructField: "ReportIncomplete",
		PermissionBuilder: func(safeOutputs *SafeOutputsConfig) *Permissions {
			if !isSafeOutputHandlerEnabledAndUnstaged(safeOutputs, "ReportIncomplete") || safeOutputs.ReportIncomplete == nil {
				return nil
			}
			if safeOutputs.ReportIncomplete.CreateIssue != nil && strings.EqualFold(strings.TrimSpace(*safeOutputs.ReportIncomplete.CreateIssue), "false") {
				return nil
			}
			return NewPermissionsIssuesWrite()
		},
	},
	{
		Key:         "threat-detection",
		StructField: "ThreatDetection",
	},
}

var safeOutputHandlersByKey = buildSafeOutputHandlersByKey()

func buildSafeOutputHandlersByKey() map[string]safeOutputHandlerDescriptor {
	result := make(map[string]safeOutputHandlerDescriptor, typeutil.SafeAllocationCapacity(len(safeOutputHandlers), 1))
	for _, handler := range safeOutputHandlers {
		if handler.Key != "" {
			result[handler.Key] = handler
		}
		for _, alias := range handler.Aliases {
			result[alias] = handler
		}
	}
	return result
}

func buildSafeOutputFieldMapping() map[string]string {
	mapping := make(map[string]string)
	for _, handler := range safeOutputHandlers {
		if handler.ToolName == "" {
			continue
		}
		mapping[handler.StructField] = handler.ToolName
	}
	return mapping
}

func getSafeOutputHandlerByKey(key string) (safeOutputHandlerDescriptor, bool) {
	handler, ok := safeOutputHandlersByKey[key]
	if !ok {
		safeOutputHandlerLog.Printf("No safe-output handler registered for key: %s", key)
	}
	return handler, ok
}

func safeOutputPointerFieldValue(config *SafeOutputsConfig, fieldName string) (reflect.Value, bool) {
	if config == nil {
		return reflect.Value{}, false
	}

	value := reflect.ValueOf(config)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return reflect.Value{}, false
	}

	field := value.Elem().FieldByName(fieldName)
	if !field.IsValid() || field.Kind() != reflect.Pointer {
		return reflect.Value{}, false
	}

	return field, true
}

func hasSafeOutputFieldSet(config *SafeOutputsConfig, fieldName string) bool {
	field, ok := safeOutputPointerFieldValue(config, fieldName)
	return ok && !field.IsNil()
}

func setSafeOutputField(config *SafeOutputsConfig, fieldName string, value any) bool {
	field, ok := safeOutputPointerFieldValue(config, fieldName)
	if !ok {
		return false
	}

	newValue := reflect.ValueOf(value)
	if !newValue.IsValid() || !newValue.Type().AssignableTo(field.Type()) {
		safeOutputHandlerLog.Printf("Cannot set safe-output field %s: value not assignable to field type", fieldName)
		return false
	}

	field.Set(newValue)
	return true
}

// getHandlerGitHubApp extracts the GitHubApp from the handler config at the given
// SafeOutputsConfig field using reflection. It first looks for a direct GitHubApp
// field, then falls back to the embedded BaseSafeOutputConfig.GitHubApp. Returns
// nil if the field is absent, the handler is not configured, or no GitHubApp is set.
func getHandlerGitHubApp(config *SafeOutputsConfig, fieldName string) *GitHubAppConfig {
	field, ok := safeOutputPointerFieldValue(config, fieldName)
	if !ok || field.IsNil() {
		return nil
	}
	inner := field.Elem()
	if !inner.IsValid() || inner.Kind() != reflect.Struct {
		return nil
	}
	// Try direct GitHubApp field (for structs with an explicit GitHubApp field)
	appField := inner.FieldByName("GitHubApp")
	if !appField.IsValid() {
		// Fall back to embedded BaseSafeOutputConfig.GitHubApp
		baseField := inner.FieldByName("BaseSafeOutputConfig")
		if !baseField.IsValid() || baseField.Kind() != reflect.Struct {
			return nil
		}
		appField = baseField.FieldByName("GitHubApp")
	}
	if !appField.IsValid() || appField.IsNil() {
		return nil
	}
	app, ok := appField.Interface().(*GitHubAppConfig)
	if !ok || app == nil {
		return nil
	}
	return app
}

func mergeSafeOutputFieldIfNil(result, imported *SafeOutputsConfig, fieldName string) {
	resultField, ok := safeOutputPointerFieldValue(result, fieldName)
	if !ok || !resultField.IsNil() {
		return
	}

	importedField, ok := safeOutputPointerFieldValue(imported, fieldName)
	if !ok || importedField.IsNil() {
		return
	}

	resultField.Set(importedField)
}

func isSafeOutputHandlerEnabledAndUnstaged(safeOutputs *SafeOutputsConfig, fieldName string) bool {
	field, ok := safeOutputPointerFieldValue(safeOutputs, fieldName)
	if !ok || field.IsNil() {
		return false
	}

	return !isHandlerStaged(templatableBoolIsTrue(safeOutputs.Staged), safeOutputHandlerStaged(field))
}

func safeOutputHandlerStaged(field reflect.Value) *TemplatableBool {
	if !field.IsValid() || field.IsNil() {
		return nil
	}

	elem := field.Elem()
	if !elem.IsValid() || elem.Kind() != reflect.Struct {
		return nil
	}

	if staged := elem.FieldByName("Staged"); staged.IsValid() {
		if value, ok := templatableBoolFromReflectValue(staged); ok {
			return value
		}
	}

	baseConfig := elem.FieldByName("BaseSafeOutputConfig")
	if !baseConfig.IsValid() || baseConfig.Kind() != reflect.Struct {
		return nil
	}

	staged := baseConfig.FieldByName("Staged")
	if value, ok := templatableBoolFromReflectValue(staged); ok {
		return value
	}
	return nil
}

func templatableBoolFromReflectValue(value reflect.Value) (*TemplatableBool, bool) {
	if !value.IsValid() {
		return nil, false
	}
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return nil, false
	}
	typed, ok := value.Interface().(*TemplatableBool)
	return typed, ok
}

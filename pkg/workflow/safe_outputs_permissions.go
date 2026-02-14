package workflow

import "github.com/github/gh-aw/pkg/logger"

var safeOutputsPermissionsLog = logger.New("workflow:safe_outputs_permissions")

// computePermissionsForSafeOutputs computes the minimal required permissions
// based on the configured safe-outputs. This function is used by both the
// consolidated safe outputs job and the conclusion job to ensure they only
// request the permissions they actually need.
//
// This implements the principle of least privilege by only including
// permissions that are required by the configured safe outputs.
func computePermissionsForSafeOutputs(safeOutputs *SafeOutputsConfig) *Permissions {
	if safeOutputs == nil {
		safeOutputsPermissionsLog.Print("No safe outputs configured, returning empty permissions")
		return NewPermissions()
	}

	permissions := NewPermissions()

	// Merge permissions for all handler-managed types
	if safeOutputs.CreateIssues != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-issue")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.CreateDiscussions != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-discussion")
		permissions.Merge(NewPermissionsContentsReadIssuesWriteDiscussionsWrite())
	}
	if safeOutputs.AddComments != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for add-comment")
		permissions.Merge(NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite())
	}
	if safeOutputs.CloseIssues != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for close-issue")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.CloseDiscussions != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for close-discussion")
		permissions.Merge(NewPermissionsContentsReadDiscussionsWrite())
	}
	if safeOutputs.AddLabels != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for add-labels")
		permissions.Merge(NewPermissionsContentsReadIssuesWritePRWrite())
	}
	if safeOutputs.RemoveLabels != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for remove-labels")
		permissions.Merge(NewPermissionsContentsReadIssuesWritePRWrite())
	}
	if safeOutputs.UpdateIssues != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for update-issue")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.UpdateDiscussions != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for update-discussion")
		permissions.Merge(NewPermissionsContentsReadDiscussionsWrite())
	}
	if safeOutputs.LinkSubIssue != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for link-sub-issue")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.UpdateRelease != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for update-release")
		permissions.Merge(NewPermissionsContentsWrite())
	}
	if safeOutputs.CreatePullRequestReviewComments != nil || safeOutputs.SubmitPullRequestReview != nil || safeOutputs.ResolvePullRequestReviewThread != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-pr-review-comment, submit-pr-review, or resolve-pr-review-thread")
		permissions.Merge(NewPermissionsContentsReadPRWrite())
	}
	if safeOutputs.CreatePullRequests != nil {
		// Check fallback-as-issue setting to determine permissions
		if getFallbackAsIssue(safeOutputs.CreatePullRequests) {
			safeOutputsPermissionsLog.Print("Adding permissions for create-pull-request with fallback-as-issue")
			permissions.Merge(NewPermissionsContentsWriteIssuesWritePRWrite())
		} else {
			safeOutputsPermissionsLog.Print("Adding permissions for create-pull-request")
			permissions.Merge(NewPermissionsContentsWritePRWrite())
		}
	}
	if safeOutputs.PushToPullRequestBranch != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for push-to-pull-request-branch")
		permissions.Merge(NewPermissionsContentsWriteIssuesWritePRWrite())
	}
	if safeOutputs.UpdatePullRequests != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for update-pull-request")
		permissions.Merge(NewPermissionsContentsReadPRWrite())
	}
	if safeOutputs.ClosePullRequests != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for close-pull-request")
		permissions.Merge(NewPermissionsContentsReadPRWrite())
	}
	if safeOutputs.MarkPullRequestAsReadyForReview != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for mark-pull-request-as-ready-for-review")
		permissions.Merge(NewPermissionsContentsReadPRWrite())
	}
	if safeOutputs.HideComment != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for hide-comment")
		permissions.Merge(NewPermissionsContentsReadIssuesWritePRWriteDiscussionsWrite())
	}
	if safeOutputs.DispatchWorkflow != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for dispatch-workflow")
		permissions.Merge(NewPermissionsActionsWrite())
	}
	// Project-related types
	if safeOutputs.CreateProjects != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-project")
		permissions.Merge(NewPermissionsContentsReadProjectsWrite())
	}
	if safeOutputs.UpdateProjects != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for update-project")
		permissions.Merge(NewPermissionsContentsReadProjectsWrite())
	}
	if safeOutputs.CreateProjectStatusUpdates != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-project-status-update")
		permissions.Merge(NewPermissionsContentsReadProjectsWrite())
	}
	if safeOutputs.AssignToAgent != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for assign-to-agent")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.CreateAgentSessions != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-agent-session")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.CreateCodeScanningAlerts != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for create-code-scanning-alert")
		permissions.Merge(NewPermissionsContentsReadSecurityEventsWrite())
	}
	if safeOutputs.AutofixCodeScanningAlert != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for autofix-code-scanning-alert")
		permissions.Merge(NewPermissionsContentsReadSecurityEventsWriteActionsRead())
	}
	if safeOutputs.AssignToUser != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for assign-to-user")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.UnassignFromUser != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for unassign-from-user")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.AssignMilestone != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for assign-milestone")
		permissions.Merge(NewPermissionsContentsReadIssuesWrite())
	}
	if safeOutputs.AddReviewer != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for add-reviewer")
		permissions.Merge(NewPermissionsContentsReadPRWrite())
	}
	if safeOutputs.UploadAssets != nil {
		safeOutputsPermissionsLog.Print("Adding permissions for upload-asset")
		permissions.Merge(NewPermissionsContentsWrite())
	}

	// NoOp and MissingTool don't require write permissions beyond what's already included
	// They only need to comment if add-comment is already configured

	safeOutputsPermissionsLog.Printf("Computed permissions with %d scopes", len(permissions.permissions))
	return permissions
}

package workflow

import (
	"reflect"
	"sort"
	"testing"
)

func TestHandlerRegistryDomainComposition(t *testing.T) {
	domains := []struct {
		name     string
		registry map[string]handlerBuilder
		wantKeys []string
	}{
		{name: "issueHandlerRegistry", registry: issueHandlerRegistry, wantKeys: []string{"create_issue", "close_issue", "update_issue", "link_sub_issue", "set_issue_type", "set_issue_field", "add_labels", "remove_labels", "replace_label", "assign_milestone"}},
		{name: "discussionHandlerRegistry", registry: discussionHandlerRegistry, wantKeys: []string{"create_discussion", "close_discussion", "update_discussion"}},
		{name: "pullRequestHandlerRegistry", registry: pullRequestHandlerRegistry, wantKeys: []string{"create_pull_request", "push_to_pull_request_branch", "update_pull_request", "merge_pull_request", "close_pull_request", "mark_pull_request_as_ready_for_review", "add_reviewer", "dismiss_pull_request_review", "submit_pull_request_review", "resolve_pull_request_review_thread", "create_pull_request_review_comment", "reply_to_pull_request_review_comment"}},
		{name: "workflowHandlerRegistry", registry: workflowHandlerRegistry, wantKeys: []string{"approve_workflow_run", "create_code_scanning_alert", "create_check_run", "dispatch_workflow", "dispatch_repository", "call_workflow", "autofix_code_scanning_alert", "upload_code_coverage", "upload_asset", "upload_artifact"}},
		{name: "projectHandlerRegistry", registry: projectHandlerRegistry, wantKeys: []string{"create_project", "update_project", "create_project_status_update"}},
		{name: "assignmentHandlerRegistry", registry: assignmentHandlerRegistry, wantKeys: []string{"assign_to_agent", "assign_to_user", "unassign_from_user", "create_agent_session"}},
		{name: "commentHandlerRegistry", registry: commentHandlerRegistry, wantKeys: []string{"add_comment", "hide_comment"}},
		{name: "jiraHandlerRegistry", registry: jiraHandlerRegistry, wantKeys: []string{"jira_create_issue", "jira_update_issue", "jira_add_comment", "jira_add_label"}},
		{name: "releaseHandlerRegistry", registry: releaseHandlerRegistry, wantKeys: []string{"update_release"}},
		{name: "diagnosticHandlerRegistry", registry: diagnosticHandlerRegistry, wantKeys: []string{"missing_tool", "missing_data", "noop", "report_incomplete", "create_report_incomplete_issue"}},
		{name: "azureDevOpsWorkItemHandlerRegistry", registry: azureDevOpsWorkItemHandlerRegistry, wantKeys: []string{"ado_create_work_item", "ado_update_work_item", "ado_comment_on_work_item", "ado_assign_work_item", "ado_link_work_items", "ado_upload_workitem_attachment"}},
		{name: "linearHandlerRegistry", registry: linearHandlerRegistry, wantKeys: []string{"linear_create_issue", "linear_add_comment", "linear_update_issue"}},
	}

	wantAll := map[string]struct{}{}
	for _, domain := range domains {
		t.Run(domain.name, func(t *testing.T) {
			gotKeys := sortedHandlerKeys(domain.registry)
			wantKeys := append([]string(nil), domain.wantKeys...)
			sort.Strings(wantKeys)
			if !reflect.DeepEqual(gotKeys, wantKeys) {
				t.Fatalf("keys mismatch: got %v, want %v", gotKeys, wantKeys)
			}
		})
		for _, key := range domain.wantKeys {
			if _, exists := wantAll[key]; exists {
				t.Fatalf("duplicate domain handler key %q", key)
			}
			wantAll[key] = struct{}{}
		}
	}

	if len(handlerRegistry) != len(wantAll) {
		t.Fatalf("handlerRegistry length = %d, want %d", len(handlerRegistry), len(wantAll))
	}
	for key := range wantAll {
		if _, exists := handlerRegistry[key]; !exists {
			t.Fatalf("handlerRegistry missing %q", key)
		}
	}
}

func TestHandlerRegistryBuilders(t *testing.T) {
	tests := []struct {
		name string
		cfg  *SafeOutputsConfig
	}{
		{name: "create_issue", cfg: &SafeOutputsConfig{CreateIssues: &CreateIssuesConfig{}}},
		{name: "close_issue", cfg: &SafeOutputsConfig{CloseIssues: &CloseIssuesConfig{}}},
		{name: "update_issue", cfg: &SafeOutputsConfig{UpdateIssues: &UpdateIssuesConfig{}}},
		{name: "link_sub_issue", cfg: &SafeOutputsConfig{LinkSubIssue: &LinkSubIssueConfig{}}},
		{name: "set_issue_type", cfg: &SafeOutputsConfig{SetIssueType: &SetIssueTypeConfig{}}},
		{name: "set_issue_field", cfg: &SafeOutputsConfig{SetIssueField: &SetIssueFieldConfig{}}},
		{name: "add_labels", cfg: &SafeOutputsConfig{AddLabels: &AddLabelsConfig{}}},
		{name: "remove_labels", cfg: &SafeOutputsConfig{RemoveLabels: &RemoveLabelsConfig{}}},
		{name: "replace_label", cfg: &SafeOutputsConfig{ReplaceLabel: &ReplaceLabelConfig{}}},
		{name: "assign_milestone", cfg: &SafeOutputsConfig{AssignMilestone: &AssignMilestoneConfig{}}},
		{name: "create_discussion", cfg: &SafeOutputsConfig{CreateDiscussions: &CreateDiscussionsConfig{}}},
		{name: "close_discussion", cfg: &SafeOutputsConfig{CloseDiscussions: &CloseDiscussionsConfig{}}},
		{name: "update_discussion", cfg: &SafeOutputsConfig{UpdateDiscussions: &UpdateDiscussionsConfig{}}},
		{name: "create_pull_request", cfg: &SafeOutputsConfig{CreatePullRequests: &CreatePullRequestsConfig{}}},
		{name: "push_to_pull_request_branch", cfg: &SafeOutputsConfig{PushToPullRequestBranch: &PushToPullRequestBranchConfig{}}},
		{name: "update_pull_request", cfg: &SafeOutputsConfig{UpdatePullRequests: &UpdatePullRequestsConfig{}}},
		{name: "merge_pull_request", cfg: &SafeOutputsConfig{MergePullRequest: &MergePullRequestConfig{}}},
		{name: "close_pull_request", cfg: &SafeOutputsConfig{ClosePullRequests: &ClosePullRequestsConfig{}}},
		{name: "mark_pull_request_as_ready_for_review", cfg: &SafeOutputsConfig{MarkPullRequestAsReadyForReview: &MarkPullRequestAsReadyForReviewConfig{}}},
		{name: "add_reviewer", cfg: &SafeOutputsConfig{AddReviewer: &AddReviewerConfig{}}},
		{name: "dismiss_pull_request_review", cfg: &SafeOutputsConfig{DismissPullRequestReview: &DismissPullRequestReviewConfig{}}},
		{name: "submit_pull_request_review", cfg: &SafeOutputsConfig{SubmitPullRequestReview: &SubmitPullRequestReviewConfig{}}},
		{name: "resolve_pull_request_review_thread", cfg: &SafeOutputsConfig{ResolvePullRequestReviewThread: &ResolvePullRequestReviewThreadConfig{}}},
		{name: "create_pull_request_review_comment", cfg: &SafeOutputsConfig{CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{}}},
		{name: "reply_to_pull_request_review_comment", cfg: &SafeOutputsConfig{ReplyToPullRequestReviewComment: &ReplyToPullRequestReviewCommentConfig{}}},
		{name: "approve_workflow_run", cfg: &SafeOutputsConfig{ApproveWorkflowRun: &ApproveWorkflowRunConfig{}}},
		{name: "create_code_scanning_alert", cfg: &SafeOutputsConfig{CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{}}},
		{name: "create_check_run", cfg: &SafeOutputsConfig{CreateCheckRun: &CreateCheckRunConfig{}}},
		{name: "dispatch_workflow", cfg: &SafeOutputsConfig{DispatchWorkflow: &DispatchWorkflowConfig{}}},
		{name: "dispatch_repository", cfg: &SafeOutputsConfig{DispatchRepository: &DispatchRepositoryConfig{Tools: map[string]*DispatchRepositoryToolConfig{"dispatch": {}}}}},
		{name: "call_workflow", cfg: &SafeOutputsConfig{CallWorkflow: &CallWorkflowConfig{}}},
		{name: "autofix_code_scanning_alert", cfg: &SafeOutputsConfig{AutofixCodeScanningAlert: &AutofixCodeScanningAlertConfig{}}},
		{name: "upload_code_coverage", cfg: &SafeOutputsConfig{UploadCodeCoverage: &UploadCodeCoverageConfig{}}},
		{name: "upload_asset", cfg: &SafeOutputsConfig{UploadAssets: &UploadAssetsConfig{}}},
		{name: "upload_artifact", cfg: &SafeOutputsConfig{UploadArtifact: &UploadArtifactConfig{}}},
		{name: "create_project", cfg: &SafeOutputsConfig{CreateProjects: &CreateProjectsConfig{}}},
		{name: "update_project", cfg: &SafeOutputsConfig{UpdateProjects: &UpdateProjectConfig{}}},
		{name: "create_project_status_update", cfg: &SafeOutputsConfig{CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{}}},
		{name: "assign_to_agent", cfg: &SafeOutputsConfig{AssignToAgent: &AssignToAgentConfig{}}},
		{name: "assign_to_user", cfg: &SafeOutputsConfig{AssignToUser: &AssignToUserConfig{}}},
		{name: "unassign_from_user", cfg: &SafeOutputsConfig{UnassignFromUser: &UnassignFromUserConfig{}}},
		{name: "create_agent_session", cfg: &SafeOutputsConfig{CreateAgentSessions: &CreateAgentSessionConfig{}}},
		{name: "ado_create_work_item", cfg: &SafeOutputsConfig{CreateWorkItems: &CreateWorkItemConfig{}}},
		{name: "ado_update_work_item", cfg: &SafeOutputsConfig{UpdateWorkItems: &UpdateWorkItemConfig{}}},
		{name: "ado_comment_on_work_item", cfg: &SafeOutputsConfig{CommentOnWorkItems: &CommentOnWorkItemConfig{}}},
		{name: "ado_assign_work_item", cfg: &SafeOutputsConfig{AssignWorkItems: &AssignWorkItemConfig{}}},
		{name: "ado_link_work_items", cfg: &SafeOutputsConfig{LinkWorkItems: &LinkWorkItemsConfig{}}},
		{name: "ado_upload_workitem_attachment", cfg: &SafeOutputsConfig{UploadWorkItemAttachments: &UploadWorkItemAttachmentConfig{}}},
		{name: "add_comment", cfg: &SafeOutputsConfig{AddComments: &AddCommentsConfig{}}},
		{name: "jira_create_issue", cfg: &SafeOutputsConfig{JiraCreateIssue: &JiraSafeOutputConfig{}}},
		{name: "jira_update_issue", cfg: &SafeOutputsConfig{JiraUpdateIssue: &JiraSafeOutputConfig{}}},
		{name: "jira_add_comment", cfg: &SafeOutputsConfig{JiraAddComment: &JiraSafeOutputConfig{}}},
		{name: "jira_add_label", cfg: &SafeOutputsConfig{JiraAddLabel: &JiraSafeOutputConfig{}}},
		{name: "hide_comment", cfg: &SafeOutputsConfig{HideComment: &HideCommentConfig{}}},
		{name: "update_release", cfg: &SafeOutputsConfig{UpdateRelease: &UpdateReleaseConfig{}}},
		{name: "missing_tool", cfg: &SafeOutputsConfig{MissingTool: &MissingToolConfig{}}},
		{name: "missing_data", cfg: &SafeOutputsConfig{MissingData: &MissingDataConfig{}}},
		{name: "noop", cfg: &SafeOutputsConfig{NoOp: &NoOpConfig{}}},
		{name: "report_incomplete", cfg: &SafeOutputsConfig{ReportIncomplete: &ReportIncompleteConfig{}}},
		{name: "create_report_incomplete_issue", cfg: &SafeOutputsConfig{ReportIncomplete: &ReportIncompleteConfig{CreateIssue: strPtr("true")}}},
		{name: "linear_create_issue", cfg: &SafeOutputsConfig{LinearCreateIssue: &LinearCreateIssueConfig{}}},
		{name: "linear_add_comment", cfg: &SafeOutputsConfig{LinearAddComment: &LinearTargetConfig{}}},
		{name: "linear_update_issue", cfg: &SafeOutputsConfig{LinearUpdateIssue: &LinearUpdateIssueConfig{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, exists := handlerRegistry[tt.name]
			if !exists {
				t.Fatalf("handler %q not found", tt.name)
			}
			if got := builder(&SafeOutputsConfig{}); got != nil {
				t.Fatalf("disabled handler returned %v, want nil", got)
			}
			if got := builder(tt.cfg); got == nil {
				t.Fatal("enabled handler returned nil config")
			}
		})
	}
}

func TestMergeHandlerMapsKeepsFirstDuplicateKey(t *testing.T) {
	first := func(*SafeOutputsConfig) map[string]any { return map[string]any{"source": "first"} }
	second := func(*SafeOutputsConfig) map[string]any { return map[string]any{"source": "second"} }

	got := mergeHandlerMaps(
		map[string]handlerBuilder{"duplicate": first},
		map[string]handlerBuilder{"duplicate": second},
	)

	if len(got) != 1 {
		t.Fatalf("mergeHandlerMaps length = %d, want 1", len(got))
	}
	if got["duplicate"](&SafeOutputsConfig{})["source"] != "first" {
		t.Fatal("mergeHandlerMaps did not keep the first duplicate builder")
	}
}

func TestResolveHandlerGitHubToken(t *testing.T) {
	app := &GitHubAppConfig{}

	tests := []struct {
		name         string
		app          *GitHubAppConfig
		handlerKey   string
		fallback     string
		want         string
		wantSupports bool
	}{
		{
			name:         "falls back without app",
			handlerKey:   "create-issue",
			fallback:     "fallback-token",
			want:         "fallback-token",
			wantSupports: true,
		},
		{
			name:         "uses per-handler app token for supported handler",
			app:          app,
			handlerKey:   "create-issue",
			fallback:     "fallback-token",
			want:         "${{ steps.create-issue-app-token.outputs.token }}",
			wantSupports: true,
		},
		{
			name:         "falls back for unsupported handler",
			app:          app,
			handlerKey:   "missing-tool",
			fallback:     "fallback-token",
			want:         "fallback-token",
			wantSupports: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveHandlerGitHubToken(tt.app, tt.handlerKey, tt.fallback); got != tt.want {
				t.Fatalf("resolveHandlerGitHubToken() = %q, want %q", got, tt.want)
			}
			if got := handlerSupportsPerHandlerGitHubAppToken(tt.handlerKey); got != tt.wantSupports {
				t.Fatalf("handlerSupportsPerHandlerGitHubAppToken() = %v, want %v", got, tt.wantSupports)
			}
		})
	}

	if got := handlerSupportsPerHandlerGitHubAppToken("unknown-handler"); got {
		t.Fatal("unknown handler unexpectedly supports per-handler GitHub App token")
	}
	if got := resolveHandlerGitHubTokenWithStepID(app, "custom-token-step", "fallback-token"); got != "${{ steps.custom-token-step.outputs.token }}" {
		t.Fatalf("resolveHandlerGitHubTokenWithStepID() = %q", got)
	}
	if got := resolveHandlerGitHubTokenWithStepID(app, "", "fallback-token"); got != "fallback-token" {
		t.Fatalf("resolveHandlerGitHubTokenWithStepID() with empty step = %q, want fallback-token", got)
	}
}

func TestResolveApproveWorkflowRunGitHubToken(t *testing.T) {
	app := &GitHubAppConfig{}

	tests := []struct {
		name   string
		cfg    *SafeOutputsConfig
		config *ApproveWorkflowRunConfig
		want   string
	}{
		{
			name:   "handler token wins",
			cfg:    &SafeOutputsConfig{GitHubToken: "global-token"},
			config: &ApproveWorkflowRunConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubToken: "handler-token"}},
			want:   "handler-token",
		},
		{
			name:   "handler app token wins",
			cfg:    &SafeOutputsConfig{GitHubToken: "global-token"},
			config: &ApproveWorkflowRunConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{GitHubApp: app, GitHubToken: "handler-token"}},
			want:   "${{ steps.approve-workflow-run-app-token.outputs.token }}",
		},
		{
			name:   "global app token fallback",
			cfg:    &SafeOutputsConfig{GitHubApp: app, GitHubToken: "global-token"},
			config: &ApproveWorkflowRunConfig{},
			want:   "${{ steps.safe-outputs-app-token.outputs.token }}",
		},
		{
			name:   "global token fallback",
			cfg:    &SafeOutputsConfig{GitHubToken: "global-token"},
			config: &ApproveWorkflowRunConfig{},
			want:   "global-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveApproveWorkflowRunGitHubToken(tt.cfg, tt.config); got != tt.want {
				t.Fatalf("resolveApproveWorkflowRunGitHubToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func sortedHandlerKeys(registry map[string]handlerBuilder) []string {
	keys := make([]string, 0, len(registry))
	for key := range registry {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

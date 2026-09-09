//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddHandlerManagerConfigEnvVar tests handler config JSON generation
func TestAddHandlerManagerConfigEnvVar(t *testing.T) {
	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		commentMemory *CommentMemoryConfig
		checkContains []string
		checkJSON     bool
		expectedKeys  []string
	}{
		{
			name: "create issue config",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
					SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"bug", "feature"}},
					Labels:                        []string{"ai-generated"},
					TitlePrefix:                   "[AI] ",
					Assignees:                     []string{"user1"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue"},
		},
		{
			name: "add comment config",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("3"),
					},
					Target:            "issue",
					HideOlderComments: strPtr("true"),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"add_comment"},
		},
		{
			name: "create discussion config",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("2"),
					},
					Category:    "general",
					TitlePrefix: "[Discussion] ",
					Labels:      []string{"ai"},
					CloseOlderConfig: CloseOlderConfig{
						Enabled: strPtr("true"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_discussion"},
		},
		{
			name: "close issue config",
			safeOutputs: &SafeOutputsConfig{
				CloseIssues: &CloseEntityConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("10"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"close_issue"},
		},
		{
			name: "add labels config",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
						Allowed: []string{"bug", "enhancement", "documentation"},
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"add_labels"},
		},
		{
			name: "update issue config",
			safeOutputs: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: strPtr("5"),
						},
					},
					Status: new(true),
					Title:  new(true),
					Body:   new(true),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"update_issue"},
		},
		{
			name: "create pull request config",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("3"),
					},
					TitlePrefix: "[PR] ",
					Labels:      []string{"automated"},
					Draft:       strPtr("true"),
					IfNoChanges: "skip",
					AllowEmpty:  strPtr("true"),
					Expires:     7,
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_pull_request"},
		},
		{
			name: "create pull request with reviewers",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Reviewers: []string{"user1", "user2"},
					Labels:    []string{"automated"},
					Draft:     strPtr("false"),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_pull_request"},
		},
		{
			name: "push to PR branch config",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
					Target:            "pull_request",
					TitlePrefix:       "[Update] ",
					Labels:            []string{"update"},
					IfNoChanges:       "skip",
					CommitTitleSuffix: " - Auto Update",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"push_to_pull_request_branch"},
		},
		{
			name: "push to PR branch staged config",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Staged: templatableBoolPtr("true"),
					},
					Target:      "*",
					IfNoChanges: "warn",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"push_to_pull_request_branch"},
		},
		{
			name: "close pull request staged config",
			safeOutputs: &SafeOutputsConfig{
				ClosePullRequests: &ClosePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max:    strPtr("1"),
						Staged: templatableBoolPtr("true"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"close_pull_request"},
		},
		{
			name: "multiple safe output types",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Issue] ",
				},
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("3"),
					},
				},
				AddLabels: &AddLabelsConfig{
					SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
						Allowed: []string{"bug"},
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue", "add_comment", "add_labels"},
		},
		{
			name: "config with target-repo",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TargetRepoSlug: "org/repo",
					TitlePrefix:    "[Test] ",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue"},
		},
		{
			name: "config with allowed repos",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					AllowedRepos: []string{"org/repo1", "org/repo2"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_issue"},
		},
		{
			name: "call_workflow config",
			safeOutputs: &SafeOutputsConfig{
				CallWorkflow: &CallWorkflowConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Workflows:     []string{"worker-a", "worker-b"},
					WorkflowFiles: map[string]string{"worker-a": "./.github/workflows/worker-a.lock.yml", "worker-b": "./.github/workflows/worker-b.lock.yml"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"call_workflow"},
		},
		{
			name: "submit_pull_request_review config",
			safeOutputs: &SafeOutputsConfig{
				SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"submit_pull_request_review"},
		},
		{
			name: "reply_to_pull_request_review_comment config",
			safeOutputs: &SafeOutputsConfig{
				ReplyToPullRequestReviewComment: &ReplyToPullRequestReviewCommentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"reply_to_pull_request_review_comment"},
		},
		{
			name: "resolve_pull_request_review_thread config",
			safeOutputs: &SafeOutputsConfig{
				ResolvePullRequestReviewThread: &ResolvePullRequestReviewThreadConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("10"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"resolve_pull_request_review_thread"},
		},
		{
			name: "create_code_scanning_alert config",
			safeOutputs: &SafeOutputsConfig{
				CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("3"),
					},
					Driver: "Test Scanner",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_code_scanning_alert"},
		},
		{
			name: "remove_labels config",
			safeOutputs: &SafeOutputsConfig{
				RemoveLabels: &RemoveLabelsConfig{
					SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
						Allowed: []string{"bug", "wontfix"},
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"remove_labels"},
		},
		{
			name: "update_pull_request config",
			safeOutputs: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: strPtr("1"),
						},
					},
					Title: boolPtr(true),
					Body:  boolPtr(true),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"update_pull_request"},
		},
		{
			name: "update_project config",
			safeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"update_project"},
		},
		{
			name: "create_project config",
			safeOutputs: &SafeOutputsConfig{
				CreateProjects: &CreateProjectsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_project"},
		},
		{
			name: "create_project_status_update config",
			safeOutputs: &SafeOutputsConfig{
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_project_status_update"},
		},
		{
			name: "link_sub_issue config",
			safeOutputs: &SafeOutputsConfig{
				LinkSubIssue: &LinkSubIssueConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"link_sub_issue"},
		},
		{
			name: "dispatch_workflow config",
			safeOutputs: &SafeOutputsConfig{
				DispatchWorkflow: &DispatchWorkflowConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Workflows: []string{"worker-a"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"dispatch_workflow"},
		},
		{
			name: "dispatch_repository config",
			safeOutputs: &SafeOutputsConfig{
				DispatchRepository: &DispatchRepositoryConfig{
					Tools: map[string]*DispatchRepositoryToolConfig{
						"example_tool": {
							Description: "Test dispatch",
							Workflow:    "test-workflow",
							EventType:   "test_event",
							Repository:  "github/example",
							Max:         strPtr("1"),
						},
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"dispatch_repository"},
		},
		{
			name: "update_discussion config",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: strPtr("1"),
						},
					},
					Title: boolPtr(true),
					Body:  boolPtr(true),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"update_discussion"},
		},
		{
			name: "close_discussion config",
			safeOutputs: &SafeOutputsConfig{
				CloseDiscussions: &CloseEntityConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"close_discussion"},
		},
		{
			name: "mark_pull_request_as_ready_for_review config",
			safeOutputs: &SafeOutputsConfig{
				MarkPullRequestAsReadyForReview: &MarkPullRequestAsReadyForReviewConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"mark_pull_request_as_ready_for_review"},
		},
		{
			name: "approve_workflow_run config",
			safeOutputs: &SafeOutputsConfig{
				ApproveWorkflowRun: &ApproveWorkflowRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"approve_workflow_run"},
		},
		{
			name: "create_pull_request_review_comment config",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("10"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_pull_request_review_comment"},
		},
		{
			name: "autofix_code_scanning_alert config",
			safeOutputs: &SafeOutputsConfig{
				AutofixCodeScanningAlert: &AutofixCodeScanningAlertConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("10"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"autofix_code_scanning_alert"},
		},
		{
			name: "add_reviewer config",
			safeOutputs: &SafeOutputsConfig{
				AddReviewer: &AddReviewerConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("3"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"add_reviewer"},
		},
		{
			name: "assign_milestone config",
			safeOutputs: &SafeOutputsConfig{
				AssignMilestone: &AssignMilestoneConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"assign_milestone"},
		},
		{
			name: "assign_to_agent config",
			safeOutputs: &SafeOutputsConfig{
				AssignToAgent: &AssignToAgentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					DefaultAgent: "copilot",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"assign_to_agent"},
		},
		{
			name: "upload_asset config",
			safeOutputs: &SafeOutputsConfig{
				UploadAssets: &UploadAssetsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"upload_asset"},
		},
		{
			name: "upload_artifact config",
			safeOutputs: &SafeOutputsConfig{
				UploadArtifact: &UploadArtifactConfig{
					MaxUploads:   1,
					MaxSizeBytes: 104857600,
					AllowedPaths: []string{"output/**"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"upload_artifact"},
		},
		{
			name: "upload_code_coverage config",
			safeOutputs: &SafeOutputsConfig{
				UploadCodeCoverage: &UploadCodeCoverageConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"upload_code_coverage"},
		},
		{
			name: "update_release config",
			safeOutputs: &SafeOutputsConfig{
				UpdateRelease: &UpdateReleaseConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: strPtr("1"),
						},
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"update_release"},
		},
		{
			name: "create_agent_session config",
			safeOutputs: &SafeOutputsConfig{
				CreateAgentSessions: &CreateAgentSessionConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_agent_session"},
		},
		{
			name: "hide_comment config",
			safeOutputs: &SafeOutputsConfig{
				HideComment: &HideCommentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"hide_comment"},
		},
		{
			name: "set_issue_type config",
			safeOutputs: &SafeOutputsConfig{
				SetIssueType: &SetIssueTypeConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"set_issue_type"},
		},
		{
			name: "set_issue_field config",
			safeOutputs: &SafeOutputsConfig{
				SetIssueField: &SetIssueFieldConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"set_issue_field"},
		},
		{
			name: "noop config",
			safeOutputs: &SafeOutputsConfig{
				NoOp: &NoOpConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"noop"},
		},
		{
			name: "assign_to_user config",
			safeOutputs: &SafeOutputsConfig{
				AssignToUser: &AssignToUserConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
					SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
						Allowed: []string{"user1", "user2"},
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"assign_to_user"},
		},
		{
			name: "unassign_from_user config",
			safeOutputs: &SafeOutputsConfig{
				UnassignFromUser: &UnassignFromUserConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"unassign_from_user"},
		},
		{
			name: "missing_tool config",
			safeOutputs: &SafeOutputsConfig{
				MissingTool: &MissingToolConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"missing_tool"},
		},
		{
			name: "missing_data config",
			safeOutputs: &SafeOutputsConfig{
				MissingData: &MissingDataConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"missing_data"},
		},
		{
			name: "report_incomplete config",
			safeOutputs: &SafeOutputsConfig{
				ReportIncomplete: &ReportIncompleteConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"report_incomplete"},
		},
		{
			name: "merge_pull_request config",
			safeOutputs: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					RequiredLabels:  []string{"automerge"},
					AllowedBranches: []string{"feature/*", "fix/*"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"merge_pull_request"},
		},
		{
			name: "create_check_run config",
			safeOutputs: &SafeOutputsConfig{
				CreateCheckRun: &CreateCheckRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Name: "Copilot Analysis",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"create_check_run"},
		},
		{
			name:        "comment_memory config",
			safeOutputs: &SafeOutputsConfig{},
			commentMemory: &CommentMemoryConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
				},
				MemoryID: "test-memory",
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"comment_memory"},
		},
		{
			name: "dismiss_pull_request_review config",
			safeOutputs: &SafeOutputsConfig{
				DismissPullRequestReview: &DismissPullRequestReviewConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("10"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"dismiss_pull_request_review"},
		},
		{
			name: "replace_label config",
			safeOutputs: &SafeOutputsConfig{
				ReplaceLabel: &ReplaceLabelConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"replace_label"},
		},
		{
			name: "mentions config",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
				Mentions: &MentionsConfig{
					Allowed: []string{"copilot-bot"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"add_comment", "mentions"},
		},
		{
			name: "ado create work item config",
			safeOutputs: &SafeOutputsConfig{
				CreateWorkItems: &CreateWorkItemConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					WorkItemType: "Task",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"ado_create_work_item"},
		},
		{
			name: "ado update work item config",
			safeOutputs: &SafeOutputsConfig{
				UpdateWorkItems: &UpdateWorkItemConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Target: "*",
					Title:  true,
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"ado_update_work_item"},
		},
		{
			name: "ado comment on work item config",
			safeOutputs: &SafeOutputsConfig{
				CommentOnWorkItems: &CommentOnWorkItemConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Target: "*",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"ado_comment_on_work_item"},
		},
		{
			name: "ado assign work item config",
			safeOutputs: &SafeOutputsConfig{
				AssignWorkItems: &AssignWorkItemConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Target:  "*",
					Allowed: []string{"owner@example.com"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"ado_assign_work_item"},
		},
		{
			name: "ado link work items config",
			safeOutputs: &SafeOutputsConfig{
				LinkWorkItems: &LinkWorkItemsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
					Target:           "*",
					AllowedLinkTypes: []string{"parent", "child", "related"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"ado_link_work_items"},
		},
		{
			name: "ado upload workitem attachment config",
			safeOutputs: &SafeOutputsConfig{
				UploadWorkItemAttachments: &UploadWorkItemAttachmentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Target:            "*",
					AllowedExtensions: []string{".txt", ".log"},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"ado_upload_workitem_attachment"},
		},
		{
			name: "linear create issue config",
			safeOutputs: &SafeOutputsConfig{
				LinearCreateIssue: &LinearCreateIssueConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					TeamID: "9cfb482a-81e3-4154-b5b9-2c805e70a02d",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"linear_create_issue"},
		},
		{
			name: "linear add comment config",
			safeOutputs: &SafeOutputsConfig{
				LinearAddComment: &LinearTargetConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
					Target: "ENG-123",
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"linear_add_comment"},
		},
		{
			name: "linear update issue config",
			safeOutputs: &SafeOutputsConfig{
				LinearUpdateIssue: &LinearUpdateIssueConfig{
					LinearTargetConfig: LinearTargetConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: strPtr("1"),
						},
						Target: "ENG-123",
					},
					Title: boolPtr(true),
					Body:  boolPtr(true),
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"linear_update_issue"},
		},
		{
			name: "jira create issue config",
			safeOutputs: &SafeOutputsConfig{
				JiraCreateIssue: &JiraSafeOutputConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"jira_create_issue"},
		},
		{
			name: "jira update issue config",
			safeOutputs: &SafeOutputsConfig{
				JiraUpdateIssue: &JiraSafeOutputConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"jira_update_issue"},
		},
		{
			name: "jira add comment config",
			safeOutputs: &SafeOutputsConfig{
				JiraAddComment: &JiraSafeOutputConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("1"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"jira_add_comment"},
		},
		{
			name: "jira add label config",
			safeOutputs: &SafeOutputsConfig{
				JiraAddLabel: &JiraSafeOutputConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("3"),
					},
				},
			},
			checkContains: []string{
				"GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG",
			},
			checkJSON:    true,
			expectedKeys: []string{"jira_add_label"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:                "Test Workflow",
				SafeOutputs:         tt.safeOutputs,
				CommentMemoryConfig: tt.commentMemory,
			}

			var steps []string
			compiler.addHandlerManagerConfigEnvVar(&steps, workflowData)

			require.NotEmpty(t, steps)

			stepsContent := strings.Join(steps, "")

			for _, expected := range tt.checkContains {
				assert.Contains(t, stepsContent, expected, "Expected to find: "+expected)
			}

			// Extract and validate JSON if requested
			if tt.checkJSON {
				// Extract JSON from the env var line
				for _, step := range steps {
					if strings.Contains(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG") {
						// Extract the JSON value
						parts := strings.Split(step, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG: ")
						if len(parts) == 2 {
							jsonStr := strings.TrimSpace(parts[1])
							jsonStr = strings.Trim(jsonStr, "\"")
							jsonStr = strings.ReplaceAll(jsonStr, "\\\"", "\"")

							var config map[string]map[string]any
							err := json.Unmarshal([]byte(jsonStr), &config)
							require.NoError(t, err, "Config JSON should be valid")

							// Check expected keys
							for _, key := range tt.expectedKeys {
								assert.Contains(t, config, key, "Expected config key: "+key)
							}
						}
					}
				}
			}
		})
	}
}

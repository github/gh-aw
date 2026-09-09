//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputePermissionsForSafeOutputs(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    map[PermissionScope]PermissionLevel
	}{
		{
			name:        "nil safe outputs returns empty permissions",
			safeOutputs: nil,
			expected:    map[PermissionScope]PermissionLevel{},
		},
		{
			name: "create-issue only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "create-discussion requires discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:      PermissionWrite,
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "close-discussion requires discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CloseDiscussions: &CloseDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "update-discussion requires discussions permission",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "add-comment default - includes pull-requests, excludes discussions",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "add-comment with discussions:true - includes discussions permission",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Discussions:          new(true),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
				PermissionDiscussions:  PermissionWrite,
			},
		},
		{
			name: "add-comment with discussions:false - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Discussions:          new(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "add-comment with pull-requests:false - no pull-requests permission and no discussions by default",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					PullRequests:         new(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "add-comment with issues:false - no issues permission and no discussions by default",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Issues:               ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "hide-comment default - excludes discussions permission",
			safeOutputs: &SafeOutputsConfig{
				HideComment: &HideCommentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "hide-comment with discussions:true - includes discussions permission",
			safeOutputs: &SafeOutputsConfig{
				HideComment: &HideCommentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Discussions:          ptrBool(true),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:      PermissionWrite,
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "hide-comment with discussions:false - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				HideComment: &HideCommentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Discussions:          ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "add-labels only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "add-labels with pull-requests:false - no pull-requests permission",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					PullRequests:         ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "add-labels with issues:false - no issues permission",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					Issues:               ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "remove-labels only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				RemoveLabels: &RemoveLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "remove-labels with pull-requests:false - no pull-requests permission",
			safeOutputs: &SafeOutputsConfig{
				RemoveLabels: &RemoveLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
					PullRequests:         ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "remove-labels with issues:false - no issues permission",
			safeOutputs: &SafeOutputsConfig{
				RemoveLabels: &RemoveLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("2")},
					Issues:               ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "close-issue only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CloseIssues: &CloseIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "close-pull-request only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				ClosePullRequests: &ClosePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "update-pull-request without update-branch only requires pull-requests write",
			safeOutputs: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "update-pull-request with update-branch requires contents write",
			safeOutputs: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					},
					UpdateBranch: boolPtr(true),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "create-pull-request with fallback-as-issue (default) - includes issues permission",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "create-pull-request with fallback-as-issue false - no issues permission",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					FallbackAsIssue:      boolPtr(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "push-to-pull-request-branch default fallback - requires pull-requests write and administration read",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:       PermissionWrite,
				PermissionPullRequests:   PermissionWrite,
				PermissionAdministration: PermissionRead,
			},
		},
		{
			name: "push-to-pull-request-branch with fallback-as-pull-request false - requires pull-requests read and administration read",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig:  BaseSafeOutputConfig{},
					FallbackAsPullRequest: boolPtr(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:       PermissionWrite,
				PermissionPullRequests:   PermissionRead,
				PermissionAdministration: PermissionRead,
			},
		},
		{
			name: "push-to-pull-request-branch with check-branch-protection false - no administration permission",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig:  BaseSafeOutputConfig{},
					CheckBranchProtection: boolPtr(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "push-to-pull-request-branch with check-branch-protection explicit true - includes administration read",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig:  BaseSafeOutputConfig{},
					CheckBranchProtection: boolPtr(true),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:       PermissionWrite,
				PermissionPullRequests:   PermissionWrite,
				PermissionAdministration: PermissionRead,
			},
		},
		{
			name: "multiple safe outputs without discussions - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
				},
				AssignToUser: &AssignToUserConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "multiple safe outputs with one discussion - includes discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
				PermissionDiscussions:  PermissionWrite,
			},
		},
		{
			name: "upload-asset requires no permissions in safe_outputs job",
			safeOutputs: &SafeOutputsConfig{
				UploadAssets: &UploadAssetsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name: "create-code-scanning-alert requires security-events write",
			safeOutputs: &SafeOutputsConfig{
				CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionSecurityEvents: PermissionWrite,
			},
		},
		{
			name: "autofix-code-scanning-alert requires security-events and actions",
			safeOutputs: &SafeOutputsConfig{
				AutofixCodeScanningAlert: &AutofixCodeScanningAlertConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionSecurityEvents: PermissionWrite,
				PermissionActions:        PermissionRead,
			},
		},
		{
			name: "dispatch-workflow requires actions write",
			safeOutputs: &SafeOutputsConfig{
				DispatchWorkflow: &DispatchWorkflowConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionActions: PermissionWrite,
			},
		},
		{
			name: "approve-workflow-run with comment enabled requires actions write and pull-requests write",
			safeOutputs: &SafeOutputsConfig{
				ApproveWorkflowRun: &ApproveWorkflowRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Comment:              true,
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionActions:      PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "approve-workflow-run with comment disabled requires actions write and pull-requests read",
			safeOutputs: &SafeOutputsConfig{
				ApproveWorkflowRun: &ApproveWorkflowRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Comment:              false,
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionActions:      PermissionWrite,
				PermissionPullRequests: PermissionRead,
			},
		},
		{
			name: "create-project requires organization-projects write and issues read",
			safeOutputs: &SafeOutputsConfig{
				CreateProjects: &CreateProjectsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionOrganizationProj: PermissionWrite,
				PermissionIssues:           PermissionRead,
			},
		},
		{
			name: "update-project requires organization-projects write and issues read",
			safeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionOrganizationProj: PermissionWrite,
				PermissionIssues:           PermissionRead,
			},
		},
		{
			name: "update-project does not downgrade issues write required by comment and labels handlers",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Issues:               ptrBool(true),
					PullRequests:         ptrBool(false),
					Discussions:          ptrBool(false),
				},
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("4")},
				},
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:           PermissionWrite,
				PermissionPullRequests:     PermissionWrite,
				PermissionOrganizationProj: PermissionWrite,
			},
		},
		{
			name: "create-project-status-update requires only organization-projects write",
			safeOutputs: &SafeOutputsConfig{
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionOrganizationProj: PermissionWrite,
			},
		},
		{
			name: "create-check-run without target requires only checks write",
			safeOutputs: &SafeOutputsConfig{
				CreateCheckRun: &CreateCheckRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionChecks: PermissionWrite,
			},
		},
		{
			name: "create-check-run with target requires checks write and pull-requests read",
			safeOutputs: &SafeOutputsConfig{
				CreateCheckRun: &CreateCheckRunConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					Target:               "triggering",
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionChecks:       PermissionWrite,
				PermissionPullRequests: PermissionRead,
			},
		},
		{
			name: "merge-pull-request requires contents write and pull-requests write",
			safeOutputs: &SafeOutputsConfig{
				MergePullRequest: &MergePullRequestConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "update-release requires only contents write",
			safeOutputs: &SafeOutputsConfig{
				UpdateRelease: &UpdateReleaseConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
			},
		},
		{
			name: "create-agent-session requires only issues write",
			safeOutputs: &SafeOutputsConfig{
				CreateAgentSessions: &CreateAgentSessionConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := ComputePermissionsForSafeOutputs(tt.safeOutputs)
			require.NotNil(t, permissions, "Permissions should not be nil")

			// Check that all expected permissions are present
			for scope, expectedLevel := range tt.expected {
				actualLevel, exists := permissions.Get(scope)
				assert.True(t, exists, "Permission scope %s should exist", scope)
				assert.Equal(t, expectedLevel, actualLevel, "Permission level for %s should match", scope)
			}

			// Check that no unexpected permissions are present
			for scope := range permissions.permissions {
				_, expected := tt.expected[scope]
				assert.True(t, expected, "Unexpected permission scope: %s", scope)
			}
		})
	}
}

func TestComputePermissionsForSafeOutputsExcludesPerHandlerAppsFromGlobalAppToken(t *testing.T) {
	safeOutputs := &SafeOutputsConfig{
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Max:       strPtr("1"),
				GitHubApp: &GitHubAppConfig{AppID: "issue-app", PrivateKey: "issue-key"},
			},
		},
		DispatchWorkflow: &DispatchWorkflowConfig{
			Workflows: []string{"downstream.yml"},
			BaseSafeOutputConfig: BaseSafeOutputConfig{
				Max: strPtr("1"),
			},
		},
	}

	perms := computePermissionsForSafeOutputs(safeOutputs, true)
	require.NotNil(t, perms)
	assert.Equal(t, PermissionWrite, perms.permissions[PermissionActions])
	assert.NotContains(t, perms.permissions, PermissionIssues)
}

func TestComputePermissionsForSafeOutputsDispatchRepositoryAppSplit(t *testing.T) {
	safeOutputs := &SafeOutputsConfig{
		DispatchRepository: &DispatchRepositoryConfig{
			Tools: map[string]*DispatchRepositoryToolConfig{
				"with-app": {
					Workflow:   "ci.yml",
					EventType:  "ci_trigger",
					Repository: "github/gh-aw",
					GitHubApp:  &GitHubAppConfig{AppID: "dispatch-app", PrivateKey: "dispatch-key"},
				},
				"without-app": {
					Workflow:   "ci.yml",
					EventType:  "ci_trigger",
					Repository: "github/gh-aw",
				},
			},
		},
	}

	perms := computePermissionsForSafeOutputs(safeOutputs, true)
	require.NotNil(t, perms)
	assert.Equal(t, PermissionWrite, perms.permissions[PermissionContents])
}

func TestComputePermissionsForSafeOutputsExcludesParsedPerHandlerApps(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "parsed-safe-outputs.md")
	content := `---
on: issues
safe-outputs:
  github-app:
    app-id: ${{ vars.GLOBAL_APP_ID }}
    private-key: ${{ secrets.GLOBAL_APP_PRIVATE_KEY }}
  add-comment:
    github-app:
      app-id: ${{ vars.ISSUE_APP_ID }}
      private-key: ${{ secrets.ISSUE_APP_PRIVATE_KEY }}
    pull-requests: false
  report-incomplete:
    github-app:
      app-id: ${{ vars.INCOMPLETE_APP_ID }}
      private-key: ${{ secrets.INCOMPLETE_APP_PRIVATE_KEY }}
  dispatch-repository:
    trigger-ci:
      workflow: ci.yml
      event_type: ci_trigger
      repository: github/gh-aw
---

Test workflow.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)
	require.NotNil(t, getHandlerGitHubApp(workflowData.SafeOutputs, "AddComments"))

	perms := computePermissionsForSafeOutputs(workflowData.SafeOutputs, true)
	require.NotNil(t, perms)
	assert.NotContains(t, perms.permissions, PermissionIssues)
	assert.NotContains(t, perms.permissions, PermissionPullRequests)
	assert.Equal(t, PermissionWrite, perms.permissions[PermissionContents])
}

func TestBuildPreambleTokenStepsExcludesParsedPerHandlerApps(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "parsed-safe-outputs.md")
	content := `---
on: issues
safe-outputs:
  github-app:
    app-id: ${{ vars.GLOBAL_APP_ID }}
    private-key: ${{ secrets.GLOBAL_APP_PRIVATE_KEY }}
  add-comment:
    github-app:
      app-id: ${{ vars.ISSUE_APP_ID }}
      private-key: ${{ secrets.ISSUE_APP_PRIVATE_KEY }}
    pull-requests: false
  report-incomplete:
    github-app:
      app-id: ${{ vars.INCOMPLETE_APP_ID }}
      private-key: ${{ secrets.INCOMPLETE_APP_PRIVATE_KEY }}
  dispatch-repository:
    trigger-ci:
      workflow: ci.yml
      event_type: ci_trigger
      repository: github/gh-aw
---

Test workflow.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)

	steps := compiler.buildPreambleTokenSteps(workflowData, map[string]string{})
	joined := strings.Join(steps, "")
	assert.Contains(t, joined, "permission-contents: write")
	assert.NotContains(t, joined, "permission-issues: write")
	assert.NotContains(t, joined, "permission-pull-requests: write")
}

func TestGenerateYAMLDoesNotReintroduceParsedPerHandlerPermissions(t *testing.T) {
	compiler := NewCompiler(WithVersion("1.0.0"))
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "parsed-safe-outputs.md")
	content := `---
on: issues
safe-outputs:
  github-app:
    app-id: ${{ vars.GLOBAL_APP_ID }}
    private-key: ${{ secrets.GLOBAL_APP_PRIVATE_KEY }}
  add-comment:
    github-app:
      app-id: ${{ vars.ISSUE_APP_ID }}
      private-key: ${{ secrets.ISSUE_APP_PRIVATE_KEY }}
    pull-requests: false
  report-incomplete:
    github-app:
      app-id: ${{ vars.INCOMPLETE_APP_ID }}
      private-key: ${{ secrets.INCOMPLETE_APP_PRIVATE_KEY }}
  dispatch-repository:
    trigger-ci:
      workflow: ci.yml
      event_type: ci_trigger
      repository: github/gh-aw
engine: copilot
---

Test workflow.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	workflowData, err := compiler.ParseWorkflowFile(testFile)
	require.NoError(t, err)

	yamlContent, _, _, err := compiler.generateYAML(workflowData, testFile)
	require.NoError(t, err)
	require.NotNil(t, workflowData.SafeOutputs.AddComments)
	require.NotNil(t, workflowData.SafeOutputs.AddComments.GitHubApp)
	require.Equal(t, "${{ vars.ISSUE_APP_ID }}", workflowData.SafeOutputs.AddComments.GitHubApp.AppID)
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete)
	require.NotNil(t, workflowData.SafeOutputs.ReportIncomplete.GitHubApp)
	require.Equal(t, "${{ vars.INCOMPLETE_APP_ID }}", workflowData.SafeOutputs.ReportIncomplete.GitHubApp.AppID)
	globalStep := compiledLastStepBlockForTest(yamlContent, "safe-outputs-app-token")
	require.NotEmpty(t, globalStep)
	assert.Contains(t, globalStep, "permission-contents: write")
	assert.NotContains(t, globalStep, "permission-issues: write")
	assert.NotContains(t, globalStep, "permission-pull-requests: write")

	steps := compiler.buildPreambleTokenSteps(workflowData, map[string]string{})
	joined := strings.Join(steps, "")
	assert.Contains(t, joined, "permission-contents: write")
	assert.NotContains(t, joined, "permission-issues: write")
	assert.NotContains(t, joined, "permission-pull-requests: write")
}

func compiledStepBlockForTest(compiled, stepID string) string {
	marker := "id: " + stepID
	start := strings.Index(compiled, marker)
	if start == -1 {
		return ""
	}
	rest := compiled[start:]
	next := strings.Index(rest[len(marker):], "\n      - name: ")
	if next == -1 {
		return rest
	}
	return rest[:len(marker)+next]
}

func compiledLastStepBlockForTest(compiled, stepID string) string {
	marker := "id: " + stepID
	start := strings.LastIndex(compiled, marker)
	if start == -1 {
		return ""
	}
	return compiledStepBlockForTest(compiled[start:], stepID)
}

func TestComputePermissionsForSafeOutputs_NoOpAndMissingTool(t *testing.T) {
	// NoOp and MissingTool don't add any permissions on their own
	// They rely on add-comment permissions if comments are needed
	safeOutputs := &SafeOutputsConfig{
		NoOp: &NoOpConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
		},
		MissingTool: &MissingToolConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
		},
	}

	permissions := ComputePermissionsForSafeOutputs(safeOutputs)
	require.NotNil(t, permissions, "Permissions should not be nil")

	// NoOp and MissingTool alone don't require any permissions
	// The conclusion job will handle commenting through add-comment if configured
	assert.Empty(t, permissions.permissions, "NoOp and MissingTool alone should not add permissions")
}

func TestStepsRequireIDToken(t *testing.T) {
	tests := []struct {
		name     string
		steps    []any
		expected bool
	}{
		{
			name:     "nil steps",
			steps:    nil,
			expected: false,
		},
		{
			name:     "empty steps",
			steps:    []any{},
			expected: false,
		},
		{
			name: "no uses field",
			steps: []any{
				map[string]any{"name": "run something", "run": "echo hello"},
			},
			expected: false,
		},
		{
			name: "aws-actions/configure-aws-credentials with version",
			steps: []any{
				map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
			},
			expected: true,
		},
		{
			name: "azure/login",
			steps: []any{
				map[string]any{"uses": "azure/login@v2"},
			},
			expected: true,
		},
		{
			name: "google-github-actions/auth",
			steps: []any{
				map[string]any{"uses": "google-github-actions/auth@v2"},
			},
			expected: true,
		},
		{
			name: "hashicorp/vault-action",
			steps: []any{
				map[string]any{"uses": "hashicorp/vault-action@v3"},
			},
			expected: true,
		},
		{
			name: "cyberark/conjur-action",
			steps: []any{
				map[string]any{"uses": "cyberark/conjur-action@v2"},
			},
			expected: true,
		},
		{
			name: "non-vault action",
			steps: []any{
				map[string]any{"uses": "actions/checkout@v4"},
			},
			expected: false,
		},
		{
			name: "mixed steps - vault action present",
			steps: []any{
				map[string]any{"uses": "actions/checkout@v4"},
				map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
				map[string]any{"run": "echo hello"},
			},
			expected: true,
		},
		{
			name: "mixed steps - no vault action",
			steps: []any{
				map[string]any{"uses": "actions/checkout@v4"},
				map[string]any{"run": "echo hello"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stepsRequireIDToken(tt.steps)
			assert.Equal(t, tt.expected, result, "stepsRequireIDToken result")
		})
	}
}

func TestComputePermissionsForSafeOutputs_IDToken(t *testing.T) {
	writeStr := "write"
	noneStr := "none"

	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		expectIDToken bool
	}{
		{
			name: "no steps - no id-token permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectIDToken: false,
		},
		{
			name: "step with vault action - auto-detects id-token: write",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				Steps: []any{
					map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
				},
			},
			expectIDToken: true,
		},
		{
			name: "step with vault action but id-token: none overrides",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				IDToken:      &noneStr,
				Steps: []any{
					map[string]any{"uses": "aws-actions/configure-aws-credentials@v4"},
				},
			},
			expectIDToken: false,
		},
		{
			name: "no vault action but id-token: write explicitly set",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				IDToken:      &writeStr,
				Steps: []any{
					map[string]any{"uses": "actions/checkout@v4"},
				},
			},
			expectIDToken: true,
		},
		{
			name: "no steps with id-token: write explicitly set",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				IDToken:      &writeStr,
			},
			expectIDToken: true,
		},
		{
			name: "id-token: none with no steps",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				IDToken:      &noneStr,
			},
			expectIDToken: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := ComputePermissionsForSafeOutputs(tt.safeOutputs)
			require.NotNil(t, permissions, "Permissions should not be nil")

			level, exists := permissions.Get(PermissionIdToken)
			if tt.expectIDToken {
				assert.True(t, exists, "Expected id-token permission to be set")
				assert.Equal(t, PermissionWrite, level, "Expected id-token: write")
			} else {
				assert.False(t, exists, "Expected id-token permission NOT to be set")
			}
		})
	}
}

func TestComputePermissionsForSafeOutputs_Checkout(t *testing.T) {
	tests := []struct {
		name           string
		safeOutputs    *SafeOutputsConfig
		expectContents bool
	}{
		{
			name: "no steps - no contents permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectContents: false,
		},
		{
			name: "step with actions/checkout - auto-detects contents: read",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				Steps: []any{
					map[string]any{"uses": "actions/checkout@v4"},
				},
			},
			expectContents: true,
		},
		{
			name: "step with actions/checkout versioned pin - auto-detects contents: read",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				Steps: []any{
					map[string]any{"uses": "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683"},
				},
			},
			expectContents: true,
		},
		{
			name: "step without checkout - no contents permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				Steps: []any{
					map[string]any{"run": "echo hello"},
				},
			},
			expectContents: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := ComputePermissionsForSafeOutputs(tt.safeOutputs)
			require.NotNil(t, permissions, "Permissions should not be nil")

			level, exists := permissions.Get(PermissionContents)
			if tt.expectContents {
				assert.True(t, exists, "Expected contents permission to be set")
				assert.Equal(t, PermissionRead, level, "Expected contents: read")
			} else {
				assert.False(t, exists, "Expected contents permission NOT to be set")
			}
		})
	}
}

func TestComputePermissionsForSafeOutputs_CheckoutDoesNotDowngradeContentsWrite(t *testing.T) {
	// create-pull-request contributes contents: write; a checkout step in safe-outputs.steps
	// must not downgrade that to contents: read.
	safeOutputs := &SafeOutputsConfig{
		CreatePullRequests: &CreatePullRequestsConfig{},
		Steps: []any{
			map[string]any{
				"uses": "actions/checkout@v4",
				"with": map[string]any{"repository": "example/target"},
			},
		},
	}
	permissions := ComputePermissionsForSafeOutputs(safeOutputs)
	require.NotNil(t, permissions)

	level, exists := permissions.Get(PermissionContents)
	assert.True(t, exists, "Expected contents permission to be set")
	assert.Equal(t, PermissionWrite, level, "Checkout auto-detection must not downgrade contents: write to contents: read")
}

func TestComputePermissionsForSafeOutputs_Staged(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    map[PermissionScope]PermissionLevel
	}{
		{
			name: "global staged=true - no permissions for any handler",
			safeOutputs: &SafeOutputsConfig{
				Staged:            templatableBoolPtr("true"),
				CreateIssues:      &CreateIssuesConfig{},
				CreateDiscussions: &CreateDiscussionsConfig{},
				AddLabels:         &AddLabelsConfig{},
			},
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name: "per-handler staged=true - staged handler contributes no permissions",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")},
				},
				AddLabels: &AddLabelsConfig{},
			},
			// create-issue is staged so it contributes nothing; add-labels is not staged
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "all handlers per-handler staged - no permissions",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")},
				},
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")},
				},
			},
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name: "global staged=true overrides per-handler staged=false",
			safeOutputs: &SafeOutputsConfig{
				Staged: templatableBoolPtr("true"),
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("false")},
				},
				DispatchWorkflow: &DispatchWorkflowConfig{},
			},
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name: "global staged=false, one handler staged=true",
			safeOutputs: &SafeOutputsConfig{
				Staged: templatableBoolPtr("false"),
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")},
				},
				CloseIssues: &CloseIssuesConfig{},
			},
			// create-pull-request is staged; close-issue is not
			expected: map[PermissionScope]PermissionLevel{
				PermissionIssues: PermissionWrite,
			},
		},
		{
			name: "global staged=true - upload-asset staged, no contents:write",
			safeOutputs: &SafeOutputsConfig{
				Staged:       templatableBoolPtr("true"),
				UploadAssets: &UploadAssetsConfig{},
			},
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name: "pr review operations - all staged via global flag",
			safeOutputs: &SafeOutputsConfig{
				Staged:                          templatableBoolPtr("true"),
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{},
				SubmitPullRequestReview:         &SubmitPullRequestReviewConfig{},
			},
			expected: map[PermissionScope]PermissionLevel{},
		},
		{
			name: "pr review operations - one staged, one not",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")},
				},
				SubmitPullRequestReview: &SubmitPullRequestReviewConfig{},
			},
			// submit-pull-request-review is not staged, so PR write permissions are added
			expected: map[PermissionScope]PermissionLevel{
				PermissionPullRequests: PermissionWrite,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := ComputePermissionsForSafeOutputs(tt.safeOutputs)
			require.NotNil(t, permissions, "Permissions should not be nil")

			// Check that all expected permissions are present
			for scope, expectedLevel := range tt.expected {
				actualLevel, exists := permissions.Get(scope)
				assert.True(t, exists, "Permission scope %s should exist", scope)
				assert.Equal(t, expectedLevel, actualLevel, "Permission level for %s should match", scope)
			}

			// Check that no unexpected permissions are present
			for scope := range permissions.permissions {
				_, expected := tt.expected[scope]
				assert.True(t, expected, "Unexpected permission scope: %s", scope)
			}
		})
	}
}

// TestComputePermissionsForSafeOutputs_StagedYAMLRendering validates that fully-staged
// safe output configurations produce explicit "permissions: {}" in YAML rendering,
// rather than an empty string that would cause the job to inherit workflow-level permissions.
func TestComputePermissionsForSafeOutputs_StagedYAMLRendering(t *testing.T) {
	tests := []struct {
		name             string
		safeOutputs      *SafeOutputsConfig
		expectedRendered string
	}{
		{
			name: "globally staged - renders permissions: {}",
			safeOutputs: &SafeOutputsConfig{
				Staged:       templatableBoolPtr("true"),
				CreateIssues: &CreateIssuesConfig{},
				AddLabels:    &AddLabelsConfig{},
			},
			expectedRendered: "permissions: {}",
		},
		{
			name: "all per-handler staged - renders permissions: {}",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
				AddLabels:    &AddLabelsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Staged: templatableBoolPtr("true")}},
			},
			expectedRendered: "permissions: {}",
		},
		{
			name: "staged PR handlers - renders permissions: {}",
			safeOutputs: &SafeOutputsConfig{
				Staged:             templatableBoolPtr("true"),
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectedRendered: "permissions: {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := ComputePermissionsForSafeOutputs(tt.safeOutputs)
			require.NotNil(t, permissions, "Permissions should not be nil")
			rendered := permissions.RenderToYAML()
			assert.Equal(t, tt.expectedRendered, rendered, "Fully-staged safe-outputs must render explicit empty permissions block")
		})
	}
}

func TestValidateAddLabelsPermissions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		wantErr     bool
	}{
		{
			name:        "nil config - no error",
			safeOutputs: nil,
			wantErr:     false,
		},
		{
			name: "add-labels not configured - no error",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			wantErr: false,
		},
		{
			name: "both nil (defaults) - no error",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{},
			},
			wantErr: false,
		},
		{
			name: "issues:false, pull-requests:true - no error",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{Issues: ptrBool(false), PullRequests: ptrBool(true)},
			},
			wantErr: false,
		},
		{
			name: "issues:true, pull-requests:false - no error",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{Issues: ptrBool(true), PullRequests: ptrBool(false)},
			},
			wantErr: false,
		},
		{
			name: "both issues:false and pull-requests:false - error",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{Issues: ptrBool(false), PullRequests: ptrBool(false)},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateAddLabelsPermissions(tt.safeOutputs)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "at least one of 'issues' or 'pull-requests' must be enabled")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRemoveLabelsPermissions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		wantErr     bool
	}{
		{name: "nil config - no error", safeOutputs: nil},
		{
			name:        "both nil (defaults) - no error",
			safeOutputs: &SafeOutputsConfig{RemoveLabels: &RemoveLabelsConfig{}},
		},
		{
			name:        "issues:false, pull-requests:true - no error",
			safeOutputs: &SafeOutputsConfig{RemoveLabels: &RemoveLabelsConfig{Issues: ptrBool(false), PullRequests: ptrBool(true)}},
		},
		{
			name:        "issues:true, pull-requests:false - no error",
			safeOutputs: &SafeOutputsConfig{RemoveLabels: &RemoveLabelsConfig{Issues: ptrBool(true), PullRequests: ptrBool(false)}},
		},
		{
			name:        "both issues:false and pull-requests:false - error",
			safeOutputs: &SafeOutputsConfig{RemoveLabels: &RemoveLabelsConfig{Issues: ptrBool(false), PullRequests: ptrBool(false)}},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateRemoveLabelsPermissions(tt.safeOutputs)
			if tt.wantErr {
				require.ErrorContains(t, err, "safe-outputs.remove-labels: at least one of 'issues' or 'pull-requests' must be enabled")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCompileRemoveLabelsRejectsAllPermissionsDisabled(t *testing.T) {
	t.Parallel()
	testFile := filepath.Join(t.TempDir(), "test.md")
	content := `---
on: issues
strict: false
safe-outputs:
  remove-labels:
    issues: false
    pull-requests: false
---
Test workflow
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	err := NewCompiler(WithVersion("1.0.0")).CompileWorkflow(testFile)
	require.ErrorContains(t, err, "safe-outputs.remove-labels: at least one of 'issues' or 'pull-requests' must be enabled")
}

func TestCompileRemoveLabelsRejectsMalformedPermissionsConfig(t *testing.T) {
	t.Parallel()
	testFile := filepath.Join(t.TempDir(), "test.md")
	content := `---
on: issues
strict: false
safe-outputs:
  remove-labels:
    pull-requests: nope
---
Test workflow
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0600))

	err := NewCompiler(WithVersion("1.0.0")).CompileWorkflow(testFile)
	require.ErrorContains(t, err, "expected null or boolean")
}

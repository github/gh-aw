package workflow

import (
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
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionWrite,
			},
		},
		{
			name: "create-discussion requires discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:    PermissionRead,
				PermissionIssues:      PermissionWrite,
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "close-discussion requires discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CloseDiscussions: &CloseDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:    PermissionRead,
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "update-discussion requires discussions permission",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
					},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:    PermissionRead,
				PermissionDiscussions: PermissionWrite,
			},
		},
		{
			name: "add-comment includes all write permissions including discussions",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
				PermissionDiscussions:  PermissionWrite,
			},
		},
		{
			name: "hide-comment includes all write permissions including discussions",
			safeOutputs: &SafeOutputsConfig{
				HideComment: &HideCommentConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
				PermissionDiscussions:  PermissionWrite,
			},
		},
		{
			name: "add-labels only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "remove-labels only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				RemoveLabels: &RemoveLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 2},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "close-issue only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CloseIssues: &CloseIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionRead,
				PermissionIssues:   PermissionWrite,
			},
		},
		{
			name: "close-pull-request only - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				ClosePullRequests: &ClosePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "create-pull-request with fallback-as-issue (default) - includes issues permission",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
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
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
					FallbackAsIssue:      ptrBool(false),
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "multiple safe outputs without discussions - no discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
				AssignToUser: &AssignToUserConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
			},
		},
		{
			name: "multiple safe outputs with one discussion - includes discussions permission",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:     PermissionRead,
				PermissionIssues:       PermissionWrite,
				PermissionPullRequests: PermissionWrite,
				PermissionDiscussions:  PermissionWrite,
			},
		},
		{
			name: "upload-asset requires contents write",
			safeOutputs: &SafeOutputsConfig{
				UploadAssets: &UploadAssetsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents: PermissionWrite,
			},
		},
		{
			name: "create-code-scanning-alert requires security-events write",
			safeOutputs: &SafeOutputsConfig{
				CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:       PermissionRead,
				PermissionSecurityEvents: PermissionWrite,
			},
		},
		{
			name: "autofix-code-scanning-alert requires security-events and actions",
			safeOutputs: &SafeOutputsConfig{
				AutofixCodeScanningAlert: &AutofixCodeScanningAlertConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:       PermissionRead,
				PermissionSecurityEvents: PermissionWrite,
				PermissionActions:        PermissionRead,
			},
		},
		{
			name: "dispatch-workflow requires actions write",
			safeOutputs: &SafeOutputsConfig{
				DispatchWorkflow: &DispatchWorkflowConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionActions: PermissionWrite,
			},
		},
		{
			name: "create-project requires organization-projects write",
			safeOutputs: &SafeOutputsConfig{
				CreateProjects: &CreateProjectsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expected: map[PermissionScope]PermissionLevel{
				PermissionContents:         PermissionRead,
				PermissionOrganizationProj: PermissionWrite,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			permissions := computePermissionsForSafeOutputs(tt.safeOutputs)
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

func TestComputePermissionsForSafeOutputs_NoOpAndMissingTool(t *testing.T) {
	// NoOp and MissingTool don't add any permissions on their own
	// They rely on add-comment permissions if comments are needed
	safeOutputs := &SafeOutputsConfig{
		NoOp: &NoOpConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
		},
		MissingTool: &MissingToolConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
		},
	}

	permissions := computePermissionsForSafeOutputs(safeOutputs)
	require.NotNil(t, permissions, "Permissions should not be nil")

	// NoOp and MissingTool alone don't require any permissions
	// The conclusion job will handle commenting through add-comment if configured
	assert.Empty(t, permissions.permissions, "NoOp and MissingTool alone should not add permissions")
}

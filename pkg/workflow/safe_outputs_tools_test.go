//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateFilteredToolsJSON(t *testing.T) {
	tests := []struct {
		name          string
		safeOutputs   *SafeOutputsConfig
		expectedTools []string
	}{
		{
			name:          "nil safe outputs returns empty array",
			safeOutputs:   nil,
			expectedTools: []string{},
		},
		{
			name:          "empty safe outputs returns empty array",
			safeOutputs:   &SafeOutputsConfig{},
			expectedTools: []string{},
		},
		{
			name: "create issues enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			expectedTools: []string{"create_issue"},
		},
		{
			name: "create agent sessions enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateAgentSessions: &CreateAgentSessionConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
				},
			},
			expectedTools: []string{"create_agent_session"},
		},
		{
			name: "create discussions enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 2},
				},
			},
			expectedTools: []string{"create_discussion"},
		},
		{
			name: "add comments enabled",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
				},
			},
			expectedTools: []string{"add_comment"},
		},
		{
			name: "create pull requests enabled",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectedTools: []string{"create_pull_request"},
		},
		{
			name: "create pull request review comments enabled",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			expectedTools: []string{"create_pull_request_review_comment"},
		},
		{
			name: "create code scanning alerts enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 100},
				},
			},
			expectedTools: []string{"create_code_scanning_alert"},
		},
		{
			name: "add labels enabled",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			expectedTools: []string{"add_labels"},
		},
		{
			name: "update issues enabled",
			safeOutputs: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
					},
				},
			},
			expectedTools: []string{"update_issue"},
		},
		{
			name: "push to pull request branch enabled",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			expectedTools: []string{"push_to_pull_request_branch"},
		},
		{
			name: "upload assets enabled",
			safeOutputs: &SafeOutputsConfig{
				UploadAssets: &UploadAssetsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
				},
			},
			expectedTools: []string{"upload_asset"},
		},
		{
			name: "missing tool enabled",
			safeOutputs: &SafeOutputsConfig{
				MissingTool: &MissingToolConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			expectedTools: []string{"missing_tool"},
		},
		{
			name: "multiple tools enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
				AddComments:  &AddCommentsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10}},
				AddLabels:    &AddLabelsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3}},
			},
			expectedTools: []string{"create_issue", "add_comment", "add_labels"},
		},
		{
			name: "all tools enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues:                    &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
				CreateAgentSessions:             &CreateAgentSessionConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3}},
				CreateDiscussions:               &CreateDiscussionsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 2}},
				AddComments:                     &AddCommentsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10}},
				CreatePullRequests:              &CreatePullRequestsConfig{},
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
				CreateCodeScanningAlerts:        &CreateCodeScanningAlertsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 100}},
				AddLabels:                       &AddLabelsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3}},
				AddReviewer:                     &AddReviewerConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3}},
				UpdateIssues:                    &UpdateIssuesConfig{UpdateEntityConfig: UpdateEntityConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3}}},
				PushToPullRequestBranch:         &PushToPullRequestBranchConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
				UploadAssets:                    &UploadAssetsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10}},
				MissingTool:                     &MissingToolConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
			},
			expectedTools: []string{
				"create_issue",
				"create_agent_session",
				"create_discussion",
				"add_comment",
				"create_pull_request",
				"create_pull_request_review_comment",
				"create_code_scanning_alert",
				"add_labels",
				"add_reviewer",
				"update_issue",
				"push_to_pull_request_branch",
				"upload_asset",
				"missing_tool",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				SafeOutputs: tt.safeOutputs,
			}

			result, err := generateFilteredToolsJSON(workflowData, ".github/workflows/test-workflow.md")
			require.NoError(t, err, "generateFilteredToolsJSON should not error")

			// Parse the JSON result
			var tools []map[string]any
			err = json.Unmarshal([]byte(result), &tools)
			require.NoError(t, err, "Result should be valid JSON")

			// Extract tool names from the result
			var actualTools []string
			for _, tool := range tools {
				if name, ok := tool["name"].(string); ok {
					actualTools = append(actualTools, name)
				}
			}

			// Check that the expected tools are present
			assert.ElementsMatch(t, tt.expectedTools, actualTools, "Tool names should match")

			// Verify each tool has required fields
			for _, tool := range tools {
				assert.Contains(t, tool, "name", "Tool should have name field")
				assert.Contains(t, tool, "description", "Tool should have description field")
				assert.Contains(t, tool, "inputSchema", "Tool should have inputSchema field")
			}
		})
	}
}

func TestGenerateFilteredToolsJSONValidStructure(t *testing.T) {
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
			AddComments:  &AddCommentsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10}},
		},
	}

	result, err := generateFilteredToolsJSON(workflowData, ".github/workflows/test-workflow.md")
	require.NoError(t, err)

	// Parse the JSON result
	var tools []map[string]any
	err = json.Unmarshal([]byte(result), &tools)
	require.NoError(t, err)

	// Verify create_issue tool structure
	var createIssueTool map[string]any
	for _, tool := range tools {
		if tool["name"] == "create_issue" {
			createIssueTool = tool
			break
		}
	}
	require.NotNil(t, createIssueTool, "create_issue tool should be present")

	// Check inputSchema structure
	inputSchema, ok := createIssueTool["inputSchema"].(map[string]any)
	require.True(t, ok, "inputSchema should be a map")

	assert.Equal(t, "object", inputSchema["type"], "inputSchema type should be object")

	properties, ok := inputSchema["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")

	// Verify required properties exist
	assert.Contains(t, properties, "title", "Should have title property")
	assert.Contains(t, properties, "body", "Should have body property")

	// Verify required field
	required, ok := inputSchema["required"].([]any)
	require.True(t, ok, "required should be an array")
	assert.Contains(t, required, "title", "title should be required")
	assert.Contains(t, required, "body", "body should be required")
}

func TestGetSafeOutputsToolsJSON(t *testing.T) {
	// Test that the embedded JSON can be retrieved and parsed
	toolsJSON := GetSafeOutputsToolsJSON()
	require.NotEmpty(t, toolsJSON, "Tools JSON should not be empty")

	// Parse the JSON to ensure it's valid
	var tools []map[string]any
	err := json.Unmarshal([]byte(toolsJSON), &tools)
	require.NoError(t, err, "Tools JSON should be valid")
	require.NotEmpty(t, tools, "Tools array should not be empty")

	// Verify all expected tools are present
	expectedTools := []string{
		"create_issue",
		"create_agent_session",
		"create_discussion",
		"update_discussion",
		"close_discussion",
		"close_issue",
		"close_pull_request",
		"mark_pull_request_as_ready_for_review",
		"add_comment",
		"create_pull_request",
		"create_pull_request_review_comment",
		"create_code_scanning_alert",
		"add_labels",
		"remove_labels",
		"add_reviewer",
		"assign_milestone",
		"assign_to_agent",
		"assign_to_user",
		"update_issue",
		"update_pull_request",
		"push_to_pull_request_branch",
		"upload_asset",
		"update_release",
		"link_sub_issue",
		"hide_comment",
		"update_project",
		"create_project",
		"create_project_status_update",
		"autofix_code_scanning_alert",
		"missing_tool",
		"missing_data",
		"noop",
	}

	var actualTools []string
	for _, tool := range tools {
		if name, ok := tool["name"].(string); ok {
			actualTools = append(actualTools, name)
		}
	}

	assert.ElementsMatch(t, expectedTools, actualTools, "All expected tools should be present")

	// Verify each tool has the required structure
	for _, tool := range tools {
		name := tool["name"].(string)
		t.Run("tool_"+name, func(t *testing.T) {
			assert.Contains(t, tool, "name", "Tool should have name")
			assert.Contains(t, tool, "description", "Tool should have description")
			assert.Contains(t, tool, "inputSchema", "Tool should have inputSchema")

			// Verify inputSchema structure
			inputSchema, ok := tool["inputSchema"].(map[string]any)
			require.True(t, ok, "inputSchema should be a map")
			assert.Equal(t, "object", inputSchema["type"], "inputSchema type should be object")
			assert.Contains(t, inputSchema, "properties", "inputSchema should have properties")
		})
	}
}

func TestEnhanceToolDescription(t *testing.T) {
	tests := []struct {
		name            string
		toolName        string
		baseDescription string
		safeOutputs     *SafeOutputsConfig
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:            "nil safe outputs returns base description",
			toolName:        "create_issue",
			baseDescription: "Create a new GitHub issue.",
			safeOutputs:     nil,
			wantContains:    []string{"Create a new GitHub issue."},
			wantNotContains: []string{"CONSTRAINTS:"},
		},
		{
			name:            "create_issue with max",
			toolName:        "create_issue",
			baseDescription: "Create a new GitHub issue.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				},
			},
			wantContains:    []string{"CONSTRAINTS:", "Maximum 5 issue(s)"},
			wantNotContains: nil,
		},
		{
			name:            "create_issue with title prefix",
			toolName:        "create_issue",
			baseDescription: "Create a new GitHub issue.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[ai] ",
				},
			},
			wantContains: []string{"CONSTRAINTS:", `Title will be prefixed with "[ai] "`},
		},
		{
			name:            "create_issue with labels",
			toolName:        "create_issue",
			baseDescription: "Create a new GitHub issue.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					Labels: []string{"bug", "enhancement"},
				},
			},
			wantContains: []string{"CONSTRAINTS:", "Labels [bug enhancement] will be automatically added"},
		},
		{
			name:            "create_issue with multiple constraints",
			toolName:        "create_issue",
			baseDescription: "Create a new GitHub issue.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
					TitlePrefix:          "[bot] ",
					Labels:               []string{"automated"},
					TargetRepoSlug:       "owner/repo",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 3 issue(s)",
				`Title will be prefixed with "[bot] "`,
				"Labels [automated]",
				`Issues will be created in repository "owner/repo"`,
			},
		},
		{
			name:            "add_labels with allowed labels",
			toolName:        "add_labels",
			baseDescription: "Add labels to an issue.",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
					Allowed:              []string{"bug", "enhancement", "question"},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 5 label(s)",
				"Only these labels are allowed: [bug enhancement question]",
			},
		},
		{
			name:            "create_discussion with category",
			toolName:        "create_discussion",
			baseDescription: "Create a discussion.",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{
					Category: "general",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				`Discussions will be created in category "general"`,
			},
		},
		{
			name:            "noop has no constraints",
			toolName:        "noop",
			baseDescription: "Log a message.",
			safeOutputs: &SafeOutputsConfig{
				NoOp: &NoOpConfig{},
			},
			wantContains:    []string{"Log a message."},
			wantNotContains: []string{"CONSTRAINTS:"},
		},
		{
			name:            "unknown tool returns base description",
			toolName:        "unknown_tool",
			baseDescription: "Unknown tool.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
			},
			wantContains:    []string{"Unknown tool."},
			wantNotContains: []string{"CONSTRAINTS:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := enhanceToolDescription(tt.toolName, tt.baseDescription, tt.safeOutputs)

			for _, want := range tt.wantContains {
				assert.Contains(t, result, want, "Result should contain %q", want)
			}

			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, result, notWant, "Result should not contain %q", notWant)
			}
		})
	}
}

func TestGenerateFilteredToolsJSONWithEnhancedDescriptions(t *testing.T) {
	workflowData := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
				TitlePrefix:          "[automated] ",
				Labels:               []string{"bot", "enhancement"},
			},
			AddLabels: &AddLabelsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
				Allowed:              []string{"bug", "enhancement"},
			},
		},
	}

	result, err := generateFilteredToolsJSON(workflowData, ".github/workflows/test-workflow.md")
	require.NoError(t, err)

	// Parse the JSON result
	var tools []map[string]any
	err = json.Unmarshal([]byte(result), &tools)
	require.NoError(t, err)

	// Find and verify create_issue tool has enhanced description
	var createIssueTool map[string]any
	for _, tool := range tools {
		if tool["name"] == "create_issue" {
			createIssueTool = tool
			break
		}
	}
	require.NotNil(t, createIssueTool, "create_issue tool should be present")

	description, ok := createIssueTool["description"].(string)
	require.True(t, ok, "description should be a string")
	assert.Contains(t, description, "CONSTRAINTS:", "Description should contain constraints")
	assert.Contains(t, description, "Maximum 5 issue(s)", "Description should include max constraint")
	assert.Contains(t, description, `Title will be prefixed with "[automated] "`, "Description should include title prefix")
	assert.Contains(t, description, "Labels [bot enhancement]", "Description should include labels")

	// Find and verify add_labels tool has enhanced description
	var addLabelsTool map[string]any
	for _, tool := range tools {
		if tool["name"] == "add_labels" {
			addLabelsTool = tool
			break
		}
	}
	require.NotNil(t, addLabelsTool, "add_labels tool should be present")

	labelsDescription, ok := addLabelsTool["description"].(string)
	require.True(t, ok, "description should be a string")
	assert.Contains(t, labelsDescription, "CONSTRAINTS:", "Description should contain constraints")
	assert.Contains(t, labelsDescription, "Only these labels are allowed: [bug enhancement]", "Description should include allowed labels")
}

func TestRepoParameterAddedOnlyWithAllowedRepos(t *testing.T) {
	tests := []struct {
		name           string
		safeOutputs    *SafeOutputsConfig
		toolName       string
		expectRepo     bool
		expectRepoDesc string
	}{
		{
			name: "create_issue without allowed-repos should not have repo parameter",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
					TargetRepoSlug:       "org/target-repo",
				},
			},
			toolName:   "create_issue",
			expectRepo: false,
		},
		{
			name: "create_issue with allowed-repos should have repo parameter",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
					TargetRepoSlug:       "org/target-repo",
					AllowedRepos:         []string{"org/other-repo"},
				},
			},
			toolName:       "create_issue",
			expectRepo:     true,
			expectRepoDesc: "org/target-repo",
		},
		{
			name: "add_comment with allowed-repos should have repo parameter",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
					AllowedRepos:         []string{"org/repo-a", "org/repo-b"},
				},
			},
			toolName:   "add_comment",
			expectRepo: true,
		},
		{
			name: "create_pull_request without allowed-repos should not have repo parameter",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
				},
			},
			toolName:   "create_pull_request",
			expectRepo: false,
		},
		{
			name: "create_pull_request with allowed-repos should have repo parameter",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
					AllowedRepos:         []string{"org/repo-c"},
				},
			},
			toolName:   "create_pull_request",
			expectRepo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				SafeOutputs: tt.safeOutputs,
			}

			result, err := generateFilteredToolsJSON(workflowData, ".github/workflows/test-workflow.md")
			require.NoError(t, err)

			// Parse the JSON result
			var tools []map[string]any
			err = json.Unmarshal([]byte(result), &tools)
			require.NoError(t, err)

			// Find the tool
			var targetTool map[string]any
			for _, tool := range tools {
				if tool["name"] == tt.toolName {
					targetTool = tool
					break
				}
			}
			require.NotNil(t, targetTool, "%s tool should be present", tt.toolName)

			// Check inputSchema
			inputSchema, ok := targetTool["inputSchema"].(map[string]any)
			require.True(t, ok, "inputSchema should exist")

			properties, ok := inputSchema["properties"].(map[string]any)
			require.True(t, ok, "properties should exist")

			// Check if repo parameter exists
			repoParam, hasRepo := properties["repo"]
			if tt.expectRepo {
				assert.True(t, hasRepo, "Tool %s should have repo parameter when allowed-repos is configured", tt.toolName)
				if hasRepo {
					repoMap, ok := repoParam.(map[string]any)
					require.True(t, ok, "repo parameter should be a map")
					assert.Equal(t, "string", repoMap["type"], "repo type should be string")

					description, ok := repoMap["description"].(string)
					require.True(t, ok, "repo description should be a string")
					assert.Contains(t, description, "Target repository", "repo description should mention target repository")
					assert.Contains(t, description, "allowed-repos", "repo description should mention allowed-repos")

					if tt.expectRepoDesc != "" {
						assert.Contains(t, description, tt.expectRepoDesc, "repo description should include target-repo value")
					}
				}
			} else {
				assert.False(t, hasRepo, "Tool %s should not have repo parameter when allowed-repos is not configured", tt.toolName)
			}
		})
	}
}

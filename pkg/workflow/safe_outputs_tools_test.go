//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
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
			wantContains: []string{"CONSTRAINTS:", `Labels ["bug" "enhancement"] will be automatically added`},
		},
		{
			name:            "create_issue with multiple constraints",
			toolName:        "create_issue",
			baseDescription: "Create a new GitHub issue.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
					TitlePrefix:          "[bot] ",
					Labels:               []string{"automated"},
					TargetRepoSlug:       "owner/repo",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 3 issue(s)",
				`Title will be prefixed with "[bot] "`,
				`Labels ["automated"]`,
				`Issues will be created in repository "owner/repo"`,
			},
		},
		{
			name:            "add_labels with allowed labels",
			toolName:        "add_labels",
			baseDescription: "Add labels to an issue.",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
						Allowed: []string{"bug", "enhancement", "question"},
					},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 5 label(s) can be added per call.",
				"Maximum 5 add_labels call(s) can be made.",
				`Only these labels are allowed: ["bug" "enhancement" "question"]`,
			},
		},
		{
			name:            "add_labels with spaces in label names",
			toolName:        "add_labels",
			baseDescription: "Add labels to an issue or pull request.",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
					SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
						Allowed: []string{"bug", "feature request", "good first issue", "help wanted"},
					},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 3 label(s) can be added per call.",
				"Maximum 3 add_labels call(s) can be made.",
				`Only these labels are allowed: ["bug" "feature request" "good first issue" "help wanted"]`,
			},
		},
		{
			name:            "add_labels caps the per-call limit at 100",
			toolName:        "add_labels",
			baseDescription: "Add labels to an issue.",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("101")},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 100 label(s) can be added per call.",
				"Maximum 101 add_labels call(s) can be made.",
			},
		},
		{
			name:            "add_labels with dynamic max explains runtime limits",
			toolName:        "add_labels",
			baseDescription: "Add labels to an issue.",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("${{ inputs.max_labels }}")},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"The runtime max expression controls both labels added per call and add_labels calls; labels per call are capped at 100.",
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
			name:            "update_project with max",
			toolName:        "update_project",
			baseDescription: "Manage GitHub Projects.",
			safeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("10")},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 10 project operation(s)",
			},
		},
		{
			name:            "update_project with project URL",
			toolName:        "update_project",
			baseDescription: "Manage GitHub Projects.",
			safeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					Project: "https://github.com/orgs/myorg/projects/42",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				`Default project URL: "https://github.com/orgs/myorg/projects/42"`,
			},
		},
		{
			name:            "update_project with max and project URL",
			toolName:        "update_project",
			baseDescription: "Manage GitHub Projects.",
			safeOutputs: &SafeOutputsConfig{
				UpdateProjects: &UpdateProjectConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					Project:              "https://github.com/users/username/projects/1",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 5 project operation(s)",
				`Default project URL: "https://github.com/users/username/projects/1"`,
			},
		},
		{
			name:            "create_project_status_update with max",
			toolName:        "create_project_status_update",
			baseDescription: "Post a status update to a GitHub Project.",
			safeOutputs: &SafeOutputsConfig{
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 3 status update(s)",
			},
		},
		{
			name:            "create_project_status_update with project URL",
			toolName:        "create_project_status_update",
			baseDescription: "Post a status update to a GitHub Project.",
			safeOutputs: &SafeOutputsConfig{
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					Project: "https://github.com/orgs/myorg/projects/99",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				`Default project URL: "https://github.com/orgs/myorg/projects/99"`,
			},
		},
		{
			name:            "create_project_status_update with max and project URL",
			toolName:        "create_project_status_update",
			baseDescription: "Post a status update to a GitHub Project.",
			safeOutputs: &SafeOutputsConfig{
				CreateProjectStatusUpdates: &CreateProjectStatusUpdateConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("8")},
					Project:              "https://github.com/orgs/example/projects/50",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 8 status update(s)",
				`Default project URL: "https://github.com/orgs/example/projects/50"`,
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
			name:            "push_to_pull_request_branch with title prefix",
			toolName:        "push_to_pull_request_branch",
			baseDescription: "Push changes to a pull request branch.",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					TitlePrefix: "[bot] ",
				},
			},
			wantContains: []string{"CONSTRAINTS:", `The target pull request title must start with "[bot] "`},
		},
		{
			name:            "update_issue with title prefix",
			toolName:        "update_issue",
			baseDescription: "Update an issue.",
			safeOutputs: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{
					TitlePrefix: "[bot] ",
				},
			},
			wantContains: []string{"CONSTRAINTS:", `The target issue title must start with "[bot] "`},
		},
		{
			name:            "close_issue with required title prefix",
			toolName:        "close_issue",
			baseDescription: "Close an issue.",
			safeOutputs: &SafeOutputsConfig{
				CloseIssues: &CloseIssuesConfig{
					SafeOutputFilterConfig: SafeOutputFilterConfig{
						RequiredTitlePrefix: "[bot] ",
					},
				},
			},
			wantContains: []string{"CONSTRAINTS:", `Only issues with title prefix "[bot] " can be closed.`},
		},
		{
			name:            "close_discussion with required title prefix",
			toolName:        "close_discussion",
			baseDescription: "Close a discussion.",
			safeOutputs: &SafeOutputsConfig{
				CloseDiscussions: &CloseDiscussionsConfig{
					SafeOutputFilterConfig: SafeOutputFilterConfig{
						RequiredTitlePrefix: "[bot] ",
					},
				},
			},
			wantContains: []string{"CONSTRAINTS:", `Only discussions with title prefix "[bot] " can be closed.`},
		},
		{
			name:            "link_sub_issue with parent and sub title prefixes",
			toolName:        "link_sub_issue",
			baseDescription: "Link a sub-issue.",
			safeOutputs: &SafeOutputsConfig{
				LinkSubIssue: &LinkSubIssueConfig{
					ParentTitlePrefix: "[parent] ",
					SubTitlePrefix:    "[sub] ",
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				`The parent issue title must start with "[parent] "`,
				`The sub-issue title must start with "[sub] "`,
			},
		},
		{
			name:            "unknown tool returns base description",
			toolName:        "unknown_tool",
			baseDescription: "Unknown tool.",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")}},
			},
			wantContains:    []string{"Unknown tool."},
			wantNotContains: []string{"CONSTRAINTS:"},
		},
		{
			name:            "update_discussion with labels only",
			toolName:        "update_discussion",
			baseDescription: "Update a discussion.",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"Label1", "Label2"}},
					Labels:                        boolPtr(true),
				},
			},
			wantContains:    []string{"CONSTRAINTS:", `Only these labels are allowed: ["Label1" "Label2"]`},
			wantNotContains: []string{"Title updates are allowed.", "Body updates are allowed."},
		},
		{
			name:            "update_discussion with title and body",
			toolName:        "update_discussion",
			baseDescription: "Update a discussion.",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					Title: boolPtr(true),
					Body:  boolPtr(true),
				},
			},
			wantContains:    []string{"CONSTRAINTS:", "Title updates are allowed.", "Body updates are allowed."},
			wantNotContains: []string{"Label updates are allowed."},
		},
		{
			name:            "update_discussion with all fields",
			toolName:        "update_discussion",
			baseDescription: "Update a discussion.",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					UpdateEntityConfig: UpdateEntityConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("3")},
					},
					Title:                         boolPtr(true),
					Body:                          boolPtr(true),
					Labels:                        boolPtr(true),
					SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"bug"}},
				},
			},
			wantContains: []string{
				"CONSTRAINTS:",
				"Maximum 3 discussion(s) can be updated.",
				"Title updates are allowed.",
				"Body updates are allowed.",
				`Only these labels are allowed: ["bug"]`,
			},
		},
		{
			name:            "update_discussion with labels (no allowed list)",
			toolName:        "update_discussion",
			baseDescription: "Update a discussion.",
			safeOutputs: &SafeOutputsConfig{
				UpdateDiscussions: &UpdateDiscussionsConfig{
					Labels: boolPtr(true),
				},
			},
			wantContains:    []string{"CONSTRAINTS:", "Label updates are allowed."},
			wantNotContains: []string{"Only these labels are allowed"},
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

// TestSafeOutputsToolsJSONInSync verifies that the compiler-embedded copy of
// safe_outputs_tools.json (pkg/workflow/js/) and the runtime copy
// (actions/setup/js/) define the same tools in the same order.
// If this test fails, update both files together.
func TestSafeOutputsToolsJSONInSync(t *testing.T) {
	// Parse the embedded compiler copy (already loaded as safeOutputsToolsJSONContent).
	var compilerTools []map[string]any
	require.NoError(t, json.Unmarshal([]byte(safeOutputsToolsJSONContent), &compilerTools), "failed to parse compiler copy of safe_outputs_tools.json")

	// Locate the runtime copy relative to this file's position in the repo.
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "could not determine source file path")
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	runtimePath := filepath.Join(repoRoot, "actions", "setup", "js", "safe_outputs_tools.json")

	runtimeBytes, err := os.ReadFile(runtimePath)
	require.NoError(t, err, "failed to read runtime copy of safe_outputs_tools.json at %s", runtimePath)

	var runtimeTools []map[string]any
	require.NoError(t, json.Unmarshal(runtimeBytes, &runtimeTools), "failed to parse runtime copy of safe_outputs_tools.json")

	assert.Equal(t, compilerTools, runtimeTools,
		"pkg/workflow/js/safe_outputs_tools.json and actions/setup/js/safe_outputs_tools.json must define identical tool objects in the same order; update both files together")
}

func TestLargeContentFieldsHaveExpectedMaxLength(t *testing.T) {
	var tools []map[string]any
	require.NoError(t, json.Unmarshal([]byte(safeOutputsToolsJSONContent), &tools), "failed to parse compiler copy of safe_outputs_tools.json")

	findTool := func(toolName string) map[string]any {
		for _, tool := range tools {
			if name, ok := tool["name"].(string); ok && name == toolName {
				return tool
			}
		}
		return nil
	}

	fieldCases := []struct {
		tool  string
		field string
	}{
		{tool: "create_issue", field: "body"},
		{tool: "create_discussion", field: "body"},
		{tool: "add_comment", field: "body"},
		{tool: "update_discussion", field: "body"},
		{tool: "update_issue", field: "body"},
		{tool: "update_pull_request", field: "body"},
		{tool: "update_release", field: "body"},
		{tool: "create_pull_request", field: "body"},
		{tool: "close_discussion", field: "body"},
		{tool: "close_issue", field: "body"},
		{tool: "close_pull_request", field: "body"},
		{tool: "create_pull_request_review_comment", field: "body"},
		{tool: "submit_pull_request_review", field: "body"},
		{tool: "reply_to_pull_request_review_comment", field: "body"},
		{tool: "create_agent_session", field: "body"},
		{tool: "create_project_status_update", field: "body"},
		{tool: "update_project", field: "draft_body"},
		{tool: "create_check_run", field: "summary"},
		{tool: "create_check_run", field: "text"},
		{tool: "autofix_code_scanning_alert", field: "fix_description"},
		{tool: "autofix_code_scanning_alert", field: "fix_code"},
		{tool: "merge_pull_request", field: "commit_message"},
		{tool: "push_to_pull_request_branch", field: "message"},
		{tool: "dismiss_pull_request_review", field: "justification"},
		{tool: "noop", field: "message"},
		{tool: "report_incomplete", field: "details"},
		{tool: "mark_pull_request_as_ready_for_review", field: "reason"},
	}

	for _, tc := range fieldCases {
		t.Run(tc.tool+"."+tc.field, func(t *testing.T) {
			tool := findTool(tc.tool)
			require.NotNil(t, tool, "expected tool %q", tc.tool)

			inputSchema, ok := tool["inputSchema"].(map[string]any)
			require.True(t, ok, "expected inputSchema object for tool %q", tc.tool)

			properties, ok := inputSchema["properties"].(map[string]any)
			require.True(t, ok, "expected properties object for tool %q", tc.tool)

			fieldValue, ok := properties[tc.field].(map[string]any)
			require.True(t, ok, "expected field %q for tool %q", tc.field, tc.tool)

			maxLength, ok := fieldValue["maxLength"].(float64)
			require.True(t, ok, "expected maxLength for %s.%s", tc.tool, tc.field)
			assert.InDelta(t, float64(65536), maxLength, 0, "expected maxLength=65536 for %s.%s", tc.tool, tc.field)
		})
	}
}

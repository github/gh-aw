//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
)

// ========================================
// HasSafeOutputsEnabled Tests
// ========================================

// TestHasSafeOutputsEnabled tests the HasSafeOutputsEnabled function
func TestHasSafeOutputsEnabled(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    bool
	}{
		{
			name:        "nil safe outputs returns false",
			safeOutputs: nil,
			expected:    false,
		},
		{
			name:        "empty safe outputs returns false",
			safeOutputs: &SafeOutputsConfig{},
			expected:    false,
		},
		{
			name: "create issues enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expected: true,
		},
		{
			name: "create agent sessions enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateAgentSessions: &CreateAgentSessionConfig{},
			},
			expected: true,
		},
		{
			name: "create discussions enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateDiscussions: &CreateDiscussionsConfig{},
			},
			expected: true,
		},
		{
			name: "close discussions enabled",
			safeOutputs: &SafeOutputsConfig{
				CloseDiscussions: &CloseDiscussionsConfig{},
			},
			expected: true,
		},
		{
			name: "close issues enabled",
			safeOutputs: &SafeOutputsConfig{
				CloseIssues: &CloseIssuesConfig{},
			},
			expected: true,
		},
		{
			name: "add comments enabled",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{},
			},
			expected: true,
		},
		{
			name: "create pull requests enabled",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expected: true,
		},
		{
			name: "create PR review comments enabled",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{},
			},
			expected: true,
		},
		{
			name: "create code scanning alerts enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{},
			},
			expected: true,
		},
		{
			name: "add labels enabled",
			safeOutputs: &SafeOutputsConfig{
				AddLabels: &AddLabelsConfig{},
			},
			expected: true,
		},
		{
			name: "add reviewer enabled",
			safeOutputs: &SafeOutputsConfig{
				AddReviewer: &AddReviewerConfig{},
			},
			expected: true,
		},
		{
			name: "assign milestone enabled",
			safeOutputs: &SafeOutputsConfig{
				AssignMilestone: &AssignMilestoneConfig{},
			},
			expected: true,
		},
		{
			name: "assign to agent enabled",
			safeOutputs: &SafeOutputsConfig{
				AssignToAgent: &AssignToAgentConfig{},
			},
			expected: true,
		},
		{
			name: "assign to user enabled",
			safeOutputs: &SafeOutputsConfig{
				AssignToUser: &AssignToUserConfig{},
			},
			expected: true,
		},
		{
			name: "update issues enabled",
			safeOutputs: &SafeOutputsConfig{
				UpdateIssues: &UpdateIssuesConfig{},
			},
			expected: true,
		},
		{
			name: "update pull requests enabled",
			safeOutputs: &SafeOutputsConfig{
				UpdatePullRequests: &UpdatePullRequestsConfig{},
			},
			expected: true,
		},
		{
			name: "push to pull request branch enabled",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expected: true,
		},
		{
			name: "upload assets enabled",
			safeOutputs: &SafeOutputsConfig{
				UploadAssets: &UploadAssetsConfig{},
			},
			expected: true,
		},
		{
			name: "missing tool enabled",
			safeOutputs: &SafeOutputsConfig{
				MissingTool: &MissingToolConfig{},
			},
			expected: true,
		},
		{
			name: "noop enabled",
			safeOutputs: &SafeOutputsConfig{
				NoOp: &NoOpConfig{},
			},
			expected: true,
		},
		{
			name: "link sub issue enabled",
			safeOutputs: &SafeOutputsConfig{
				LinkSubIssue: &LinkSubIssueConfig{},
			},
			expected: true,
		},
		{
			name: "jobs enabled",
			safeOutputs: &SafeOutputsConfig{
				Jobs: map[string]*SafeJobConfig{
					"custom_job": {},
				},
			},
			expected: true,
		},
		{
			name: "multiple outputs enabled",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				AddComments:  &AddCommentsConfig{},
				AddLabels:    &AddLabelsConfig{},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasSafeOutputsEnabled(tt.safeOutputs)
			if result != tt.expected {
				t.Errorf("HasSafeOutputsEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// ========================================
// NormalizeSafeOutputIdentifier Tests
// ========================================

// TestNormalizeSafeOutputIdentifier tests the normalizeSafeOutputIdentifier function
func TestNormalizeSafeOutputIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		expected   string
	}{
		{
			name:       "dash to underscore conversion",
			identifier: "create-issue",
			expected:   "create_issue",
		},
		{
			name:       "already underscore format",
			identifier: "create_issue",
			expected:   "create_issue",
		},
		{
			name:       "multiple dashes",
			identifier: "create-pull-request-review-comment",
			expected:   "create_pull_request_review_comment",
		},
		{
			name:       "no dashes or underscores",
			identifier: "noop",
			expected:   "noop",
		},
		{
			name:       "empty string",
			identifier: "",
			expected:   "",
		},
		{
			name:       "add-comment",
			identifier: "add-comment",
			expected:   "add_comment",
		},
		{
			name:       "link-sub-issue",
			identifier: "link-sub-issue",
			expected:   "link_sub_issue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringutil.NormalizeSafeOutputIdentifier(tt.identifier)
			if result != tt.expected {
				t.Errorf("stringutil.NormalizeSafeOutputIdentifier(%q) = %q, want %q", tt.identifier, result, tt.expected)
			}
		})
	}
}

// ========================================
// ParseMessagesConfig Tests
// ========================================

// TestParseMessagesConfig tests the parseMessagesConfig function
func TestParseMessagesConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]any
		expected *SafeOutputMessagesConfig
	}{
		{
			name:  "empty map",
			input: map[string]any{},
			expected: &SafeOutputMessagesConfig{
				Footer:            "",
				FooterInstall:     "",
				StagedTitle:       "",
				StagedDescription: "",
				RunStarted:        "",
				RunSuccess:        "",
				RunFailure:        "",
			},
		},
		{
			name: "footer only",
			input: map[string]any{
				"footer": "Powered by AI",
			},
			expected: &SafeOutputMessagesConfig{
				Footer: "Powered by AI",
			},
		},
		{
			name: "footer-install only",
			input: map[string]any{
				"footer-install": "Install gh-aw to use",
			},
			expected: &SafeOutputMessagesConfig{
				FooterInstall: "Install gh-aw to use",
			},
		},
		{
			name: "staged-title and staged-description",
			input: map[string]any{
				"staged-title":       "Preview Mode",
				"staged-description": "This is a preview of the changes",
			},
			expected: &SafeOutputMessagesConfig{
				StagedTitle:       "Preview Mode",
				StagedDescription: "This is a preview of the changes",
			},
		},
		{
			name: "run status messages",
			input: map[string]any{
				"run-started": "Starting workflow...",
				"run-success": "Workflow completed!",
				"run-failure": "Workflow failed!",
			},
			expected: &SafeOutputMessagesConfig{
				RunStarted: "Starting workflow...",
				RunSuccess: "Workflow completed!",
				RunFailure: "Workflow failed!",
			},
		},
		{
			name: "all fields",
			input: map[string]any{
				"footer":                            "Powered by AI",
				"footer-install":                    "Install now",
				"footer-workflow-recompile":         "Recompile footer",
				"footer-workflow-recompile-comment": "Recompile comment footer",
				"staged-title":                      "Preview",
				"staged-description":                "Preview description",
				"run-started":                       "Started",
				"run-success":                       "Success",
				"run-failure":                       "Failure",
			},
			expected: &SafeOutputMessagesConfig{
				Footer:                         "Powered by AI",
				FooterInstall:                  "Install now",
				FooterWorkflowRecompile:        "Recompile footer",
				FooterWorkflowRecompileComment: "Recompile comment footer",
				StagedTitle:                    "Preview",
				StagedDescription:              "Preview description",
				RunStarted:                     "Started",
				RunSuccess:                     "Success",
				RunFailure:                     "Failure",
			},
		},
		{
			name: "non-string values are ignored",
			input: map[string]any{
				"footer":      123, // Should be ignored
				"run-started": "Valid string",
			},
			expected: &SafeOutputMessagesConfig{
				Footer:     "",
				RunStarted: "Valid string",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMessagesConfig(tt.input)

			if result.Footer != tt.expected.Footer {
				t.Errorf("Footer: got %q, want %q", result.Footer, tt.expected.Footer)
			}
			if result.FooterInstall != tt.expected.FooterInstall {
				t.Errorf("FooterInstall: got %q, want %q", result.FooterInstall, tt.expected.FooterInstall)
			}
			if result.FooterWorkflowRecompile != tt.expected.FooterWorkflowRecompile {
				t.Errorf("FooterWorkflowRecompile: got %q, want %q", result.FooterWorkflowRecompile, tt.expected.FooterWorkflowRecompile)
			}
			if result.FooterWorkflowRecompileComment != tt.expected.FooterWorkflowRecompileComment {
				t.Errorf("FooterWorkflowRecompileComment: got %q, want %q", result.FooterWorkflowRecompileComment, tt.expected.FooterWorkflowRecompileComment)
			}
			if result.StagedTitle != tt.expected.StagedTitle {
				t.Errorf("StagedTitle: got %q, want %q", result.StagedTitle, tt.expected.StagedTitle)
			}
			if result.StagedDescription != tt.expected.StagedDescription {
				t.Errorf("StagedDescription: got %q, want %q", result.StagedDescription, tt.expected.StagedDescription)
			}
			if result.RunStarted != tt.expected.RunStarted {
				t.Errorf("RunStarted: got %q, want %q", result.RunStarted, tt.expected.RunStarted)
			}
			if result.RunSuccess != tt.expected.RunSuccess {
				t.Errorf("RunSuccess: got %q, want %q", result.RunSuccess, tt.expected.RunSuccess)
			}
			if result.RunFailure != tt.expected.RunFailure {
				t.Errorf("RunFailure: got %q, want %q", result.RunFailure, tt.expected.RunFailure)
			}
		})
	}
}

// ========================================
// SerializeMessagesConfig Tests
// ========================================

// TestSerializeMessagesConfigComprehensive tests the serializeMessagesConfig function
func TestSerializeMessagesConfigComprehensive(t *testing.T) {
	tests := []struct {
		name       string
		messages   *SafeOutputMessagesConfig
		expectJSON bool
		expectErr  bool
	}{
		{
			name:       "nil config returns empty string",
			messages:   nil,
			expectJSON: false,
			expectErr:  false,
		},
		{
			name:       "empty config returns valid JSON",
			messages:   &SafeOutputMessagesConfig{},
			expectJSON: true,
			expectErr:  false,
		},
		{
			name: "populated config returns valid JSON",
			messages: &SafeOutputMessagesConfig{
				Footer:            "Test footer",
				FooterInstall:     "Install instructions",
				StagedTitle:       "Staged",
				StagedDescription: "Description",
				RunStarted:        "Started",
				RunSuccess:        "Success",
				RunFailure:        "Failure",
			},
			expectJSON: true,
			expectErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := serializeMessagesConfig(tt.messages)

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
				return
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if tt.messages == nil {
				if result != "" {
					t.Errorf("Expected empty string for nil config, got %q", result)
				}
				return
			}

			if tt.expectJSON {
				// Verify it's valid JSON
				var parsed map[string]any
				if err := json.Unmarshal([]byte(result), &parsed); err != nil {
					t.Errorf("Result is not valid JSON: %v", err)
				}
			}
		})
	}
}

// ========================================
// GenerateSafeOutputsConfig Tests
// ========================================

// TestGenerateSafeOutputsConfig tests the generateSafeOutputsConfig function
func TestGenerateSafeOutputsConfig(t *testing.T) {
	tests := []struct {
		name           string
		workflowData   *WorkflowData
		expectEmpty    bool
		expectedKeys   []string
		unexpectedKeys []string
	}{
		{
			name: "nil safe outputs returns empty string",
			workflowData: &WorkflowData{
				SafeOutputs: nil,
			},
			expectEmpty: true,
		},
		{
			name: "create-issue config",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5},
					},
				},
			},
			expectedKeys: []string{"create_issue"},
		},
		{
			name: "create-agent-session config",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateAgentSessions: &CreateAgentSessionConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3},
					},
				},
			},
			expectedKeys: []string{"create_agent_session"},
		},
		{
			name: "add-comment with target",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 2},
						Target:               "issue",
					},
				},
			},
			expectedKeys: []string{"add_comment"},
		},
		{
			name: "multiple safe outputs",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues:       &CreateIssuesConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
					AddComments:        &AddCommentsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 3}},
					CreatePullRequests: &CreatePullRequestsConfig{},
					AddLabels:          &AddLabelsConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 5}},
				},
			},
			expectedKeys: []string{"create_issue", "add_comment", "create_pull_request", "add_labels"},
		},
		{
			name: "safe-jobs included",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					Jobs: map[string]*SafeJobConfig{
						"custom_job": {
							Description: "Custom job description",
							Output:      "string",
						},
					},
				},
			},
			expectedKeys: []string{"custom_job"},
		},
		{
			name: "close-discussion with required fields",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					CloseDiscussions: &CloseDiscussionsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1},
						SafeOutputDiscussionFilterConfig: SafeOutputDiscussionFilterConfig{
							RequiredCategory: "general",
							SafeOutputFilterConfig: SafeOutputFilterConfig{
								RequiredLabels:      []string{"resolved"},
								RequiredTitlePrefix: "[resolved] ",
							},
						},
					},
				},
			},
			expectedKeys: []string{"close_discussion"},
		},
		{
			name: "noop config",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					NoOp: &NoOpConfig{BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 1}},
				},
			},
			expectedKeys: []string{"noop"},
		},
		{
			name: "update-project config",
			workflowData: &WorkflowData{
				SafeOutputs: &SafeOutputsConfig{
					UpdateProjects: &UpdateProjectConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: 10},
					},
				},
			},
			expectedKeys: []string{"update_project"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := generateSafeOutputsConfig(tt.workflowData)

			if tt.expectEmpty {
				if result != "" {
					t.Errorf("Expected empty string, got %q", result)
				}
				return
			}

			// Verify it's valid JSON
			var parsed map[string]any
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Errorf("Result is not valid JSON: %v (result: %s)", err, result)
				return
			}

			// Check expected keys are present
			for _, key := range tt.expectedKeys {
				if _, exists := parsed[key]; !exists {
					t.Errorf("Expected key %q not found in config", key)
				}
			}

			// Check unexpected keys are NOT present
			for _, key := range tt.unexpectedKeys {
				if _, exists := parsed[key]; exists {
					t.Errorf("Unexpected key %q found in config", key)
				}
			}
		})
	}
}

// ========================================
// FormatSafeOutputsRunsOn Tests
// ========================================

// TestFormatSafeOutputsRunsOn tests the formatSafeOutputsRunsOn function
func TestFormatSafeOutputsRunsOn(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		safeOutputs    *SafeOutputsConfig
		expectedRunsOn string
	}{
		{
			name:           "nil safe outputs returns default",
			safeOutputs:    nil,
			expectedRunsOn: "runs-on: " + constants.DefaultActivationJobRunnerImage,
		},
		{
			name:           "empty runs-on returns default",
			safeOutputs:    &SafeOutputsConfig{RunsOn: ""},
			expectedRunsOn: "runs-on: " + constants.DefaultActivationJobRunnerImage,
		},
		{
			name:           "custom runs-on",
			safeOutputs:    &SafeOutputsConfig{RunsOn: "ubuntu-latest"},
			expectedRunsOn: "runs-on: ubuntu-latest",
		},
		{
			name:           "self-hosted runs-on",
			safeOutputs:    &SafeOutputsConfig{RunsOn: "self-hosted"},
			expectedRunsOn: "runs-on: self-hosted",
		},
		{
			name:           "windows-latest runs-on",
			safeOutputs:    &SafeOutputsConfig{RunsOn: "windows-latest"},
			expectedRunsOn: "runs-on: windows-latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compiler.formatSafeOutputsRunsOn(tt.safeOutputs)
			if result != tt.expectedRunsOn {
				t.Errorf("formatSafeOutputsRunsOn() = %q, want %q", result, tt.expectedRunsOn)
			}
		})
	}
}

// ========================================
// BuildWorkflowMetadataEnvVarsWithTrackerID Tests
// ========================================

// TestBuildWorkflowMetadataEnvVarsWithTrackerID tests the buildWorkflowMetadataEnvVarsWithTrackerID function
func TestBuildWorkflowMetadataEnvVarsWithTrackerID(t *testing.T) {
	tests := []struct {
		name           string
		workflowName   string
		workflowSource string
		trackerID      string
		expectedVars   []string
		unexpectedVars []string
	}{
		{
			name:         "workflow name only",
			workflowName: "Test Workflow",
			expectedVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
			},
			unexpectedVars: []string{
				"GH_AW_TRACKER_ID",
			},
		},
		{
			name:           "workflow name and source",
			workflowName:   "Test Workflow",
			workflowSource: "owner/repo/workflow.md@main",
			expectedVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
				"GH_AW_WORKFLOW_SOURCE: \"owner/repo/workflow.md@main\"",
			},
		},
		{
			name:         "with tracker ID",
			workflowName: "Test Workflow",
			trackerID:    "issue-123",
			expectedVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
				"GH_AW_TRACKER_ID: \"issue-123\"",
			},
		},
		{
			name:           "all fields",
			workflowName:   "Test Workflow",
			workflowSource: "owner/repo/workflow.md@main",
			trackerID:      "issue-123",
			expectedVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
				"GH_AW_WORKFLOW_SOURCE:",
				"GH_AW_TRACKER_ID: \"issue-123\"",
			},
		},
		{
			name:         "empty tracker ID not included",
			workflowName: "Test Workflow",
			trackerID:    "",
			expectedVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
			},
			unexpectedVars: []string{
				"GH_AW_TRACKER_ID",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWorkflowMetadataEnvVarsWithTrackerID(tt.workflowName, tt.workflowSource, tt.trackerID)
			resultStr := strings.Join(result, "")

			for _, expected := range tt.expectedVars {
				if !strings.Contains(resultStr, expected) {
					t.Errorf("Expected %q in result, got: %s", expected, resultStr)
				}
			}

			for _, unexpected := range tt.unexpectedVars {
				if strings.Contains(resultStr, unexpected) {
					t.Errorf("Unexpected %q in result, got: %s", unexpected, resultStr)
				}
			}
		})
	}
}

// ========================================
// BuildGitHubScriptStepWithoutDownload Tests
// ========================================

// TestBuildGitHubScriptStepWithoutDownload verifies the buildGitHubScriptStepWithoutDownload function
func TestBuildGitHubScriptStepWithoutDownload(t *testing.T) {
	compiler := &Compiler{}

	tests := []struct {
		name            string
		workflowData    *WorkflowData
		config          GitHubScriptStepConfig
		expectedInSteps []string
		notExpected     []string
	}{
		{
			name: "basic script step without download",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
			},
			config: GitHubScriptStepConfig{
				StepName:    "Test Step",
				StepID:      "test_step",
				MainJobName: "main_job",
				Script:      "console.log('test');",
				Token:       "",
			},
			expectedInSteps: []string{
				"- name: Test Step",
				"id: test_step",
				"uses: actions/github-script@",
				"env:",
				"GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}",
				"with:",
				"script: |",
				"console.log('test');",
			},
			notExpected: []string{
				"Download agent output artifact",
			},
		},
		{
			name: "script step without download with custom env vars",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
			},
			config: GitHubScriptStepConfig{
				StepName:    "Custom Step",
				StepID:      "custom",
				MainJobName: "main",
				CustomEnvVars: []string{
					"          CUSTOM_VAR: value\n",
				},
				Script: "const x = 1;",
				Token:  "",
			},
			expectedInSteps: []string{
				"- name: Custom Step",
				"id: custom",
				"CUSTOM_VAR: value",
			},
			notExpected: []string{
				"Download agent output artifact",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := compiler.buildGitHubScriptStepWithoutDownload(tt.workflowData, tt.config)
			stepsStr := strings.Join(steps, "")

			for _, expected := range tt.expectedInSteps {
				if !strings.Contains(stepsStr, expected) {
					t.Errorf("Expected step to contain %q, but it was not found.\nGenerated steps:\n%s", expected, stepsStr)
				}
			}

			for _, notExpected := range tt.notExpected {
				if strings.Contains(stepsStr, notExpected) {
					t.Errorf("Step should not contain %q, but it was found.\nGenerated steps:\n%s", notExpected, stepsStr)
				}
			}
		})
	}
}

// ========================================
// BuildStandardSafeOutputEnvVars Tests
// ========================================

// TestBuildStandardSafeOutputEnvVars tests the buildStandardSafeOutputEnvVars method
func TestBuildStandardSafeOutputEnvVars(t *testing.T) {
	tests := []struct {
		name            string
		workflowData    *WorkflowData
		targetRepoSlug  string
		trialMode       bool
		trialRepoSlug   string
		expectedInVars  []string
		notExpectedVars []string
	}{
		{
			name: "basic workflow data",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
			},
			targetRepoSlug: "",
			expectedInVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
			},
		},
		{
			name: "with source and tracker ID",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				Source:      "owner/repo/workflow.md@main",
				TrackerID:   "issue-123",
				SafeOutputs: &SafeOutputsConfig{},
			},
			targetRepoSlug: "",
			expectedInVars: []string{
				"GH_AW_WORKFLOW_NAME: \"Test Workflow\"",
				"GH_AW_WORKFLOW_SOURCE:",
				"GH_AW_TRACKER_ID: \"issue-123\"",
			},
		},
		{
			name: "with staged flag",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					Staged: true,
				},
			},
			targetRepoSlug: "",
			expectedInVars: []string{
				"GH_AW_SAFE_OUTPUTS_STAGED: \"true\"",
			},
		},
		{
			name: "with target repo slug",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
			},
			targetRepoSlug: "owner/target-repo",
			expectedInVars: []string{
				"GH_AW_TARGET_REPO_SLUG: \"owner/target-repo\"",
			},
		},
		{
			name: "with messages config",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					Messages: &SafeOutputMessagesConfig{
						Footer: "Powered by AI",
					},
				},
			},
			targetRepoSlug: "",
			expectedInVars: []string{
				"GH_AW_SAFE_OUTPUT_MESSAGES:",
			},
		},
		{
			name: "with engine config",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
				EngineConfig: &EngineConfig{
					ID:      "copilot",
					Version: "1.0.0",
					Model:   "gpt-4",
				},
			},
			targetRepoSlug: "",
			expectedInVars: []string{
				"GH_AW_ENGINE_ID: \"copilot\"",
				"GH_AW_ENGINE_VERSION: \"1.0.0\"",
				"GH_AW_ENGINE_MODEL: \"gpt-4\"",
			},
		},
		{
			name: "with repo-memory",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
				RepoMemoryConfig: &RepoMemoryConfig{
					Memories: []RepoMemoryEntry{{
						ID: "notes",
					}},
				},
			},
			targetRepoSlug: "",
			expectedInVars: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			if tt.trialMode {
				compiler.SetTrialMode(true)
				compiler.SetTrialLogicalRepoSlug(tt.trialRepoSlug)
			}

			result := compiler.buildStandardSafeOutputEnvVars(tt.workflowData, tt.targetRepoSlug)
			resultStr := strings.Join(result, "")

			for _, expected := range tt.expectedInVars {
				if !strings.Contains(resultStr, expected) {
					t.Errorf("Expected %q in result, got: %s", expected, resultStr)
				}
			}

			for _, notExpected := range tt.notExpectedVars {
				if strings.Contains(resultStr, notExpected) {
					t.Errorf("Unexpected %q in result, got: %s", notExpected, resultStr)
				}
			}
		})
	}
}

// ========================================
// GetEnabledSafeOutputToolNames Tests
// ========================================

// TestGetEnabledSafeOutputToolNames tests that tool names are returned in sorted order
func TestGetEnabledSafeOutputToolNames(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
		expected    []string
	}{
		{
			name:        "nil safe outputs returns nil",
			safeOutputs: nil,
			expected:    nil,
		},
		{
			name:        "empty safe outputs returns empty slice",
			safeOutputs: &SafeOutputsConfig{},
			expected:    nil,
		},
		{
			name: "single tool",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expected: []string{"create_issue"},
		},
		{
			name: "multiple tools in alphabetical order",
			safeOutputs: &SafeOutputsConfig{
				AddComments:  &AddCommentsConfig{},
				CreateIssues: &CreateIssuesConfig{},
				UpdateIssues: &UpdateIssuesConfig{},
			},
			expected: []string{"add_comment", "create_issue", "update_issue"},
		},
		{
			name: "custom jobs are sorted with standard tools",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
				UpdateIssues: &UpdateIssuesConfig{},
				Jobs: map[string]*SafeJobConfig{
					"zzz_custom":    {},
					"aaa_custom":    {},
					"middle_custom": {},
				},
			},
			expected: []string{"aaa_custom", "create_issue", "middle_custom", "update_issue", "zzz_custom"},
		},
		{
			name: "all standard tools are sorted",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues:        &CreateIssuesConfig{},
				CreateAgentSessions: &CreateAgentSessionConfig{},
				CreateDiscussions:   &CreateDiscussionsConfig{},
				CloseDiscussions:    &CloseDiscussionsConfig{},
				CloseIssues:         &CloseIssuesConfig{},
				ClosePullRequests:   &ClosePullRequestsConfig{},
				AddComments:         &AddCommentsConfig{},
				CreatePullRequests:  &CreatePullRequestsConfig{},
				AddLabels:           &AddLabelsConfig{},
				AddReviewer:         &AddReviewerConfig{},
				AssignMilestone:     &AssignMilestoneConfig{},
				UpdateIssues:        &UpdateIssuesConfig{},
				UpdatePullRequests:  &UpdatePullRequestsConfig{},
				NoOp:                &NoOpConfig{},
			},
			// Expected order is alphabetical
			expected: []string{
				"add_comment",
				"add_labels",
				"add_reviewer",
				"assign_milestone",
				"close_discussion",
				"close_issue",
				"close_pull_request",
				"create_agent_session",
				"create_discussion",
				"create_issue",
				"create_pull_request",
				"noop",
				"update_issue",
				"update_pull_request",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetEnabledSafeOutputToolNames(tt.safeOutputs)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d tools, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for i, tool := range result {
				if tool != tt.expected[i] {
					t.Errorf("Tool at index %d: expected %q, got %q", i, tt.expected[i], tool)
				}
			}

			// Verify the list is sorted
			for i := 1; i < len(result); i++ {
				if result[i-1] > result[i] {
					t.Errorf("Tools not in sorted order: %q comes after %q", result[i-1], result[i])
				}
			}
		})
	}
}

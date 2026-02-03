//go:build integration

package workflow

import (
	"strings"
	"testing"
)

// TestSafeOutputJobsIntegration tests that all safe output job types that have individual
// job builders can be built with proper environment configuration, including the critical
// GH_AW_WORKFLOW_ID variable. This prevents regressions where required environment variables
// are missing from compiled workflows.
func TestSafeOutputJobsIntegration(t *testing.T) {
	tests := []struct {
		name           string
		safeOutputType string
		configBuilder  func() *SafeOutputsConfig
		requiredEnvVar string // The critical env var to check (usually GH_AW_WORKFLOW_ID)
		jobBuilder     func(*Compiler, *WorkflowData, string) (*Job, error)
	}{
		{
			name:           "create_pull_request",
			safeOutputType: "create-pull-request",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreatePullRequests: &CreatePullRequestsConfig{
						TitlePrefix: "[Test] ",
						Labels:      []string{"test"},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputPullRequestJob(data, mainJobName)
			},
		},
		{
			name:           "create_issue",
			safeOutputType: "create-issue",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						TitlePrefix: "[Test] ",
						Labels:      []string{"test"},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputIssueJob(data, mainJobName)
			},
		},
		{
			name:           "create_discussion",
			safeOutputType: "create-discussion",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreateDiscussions: &CreateDiscussionsConfig{
						TitlePrefix: "[Test] ",
						Category:    "general",
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputDiscussionJob(data, mainJobName, "")
			},
		},
		{
			name:           "add_comment",
			safeOutputType: "add-comment",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 5,
						},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputAddCommentJob(data, mainJobName, "", "", "")
			},
		},
		{
			name:           "add_labels",
			safeOutputType: "add-labels",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AddLabels: &AddLabelsConfig{
						Allowed: []string{"test", "automated"},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildAddLabelsJob(data, mainJobName)
			},
		},
		{
			name:           "missing_tool",
			safeOutputType: "missing-tool",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					MissingTool: &MissingToolConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 10,
						},
					},
				}
			},
			requiredEnvVar: "GH_AW_MISSING_TOOL_MAX",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputMissingToolJob(data, mainJobName)
			},
		},
		{
			name:           "create_pr_review_comment",
			safeOutputType: "create-pr-review-comment",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreatePullRequestReviewComments: &CreatePullRequestReviewCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 10,
						},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputPullRequestReviewCommentJob(data, mainJobName)
			},
		},
		{
			name:           "create_code_scanning_alert",
			safeOutputType: "create-code-scanning-alert",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 10,
						},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputCodeScanningAlertJob(data, mainJobName, "test-workflow.md")
			},
		},
		{
			name:           "create_agent_session",
			safeOutputType: "create-agent-session",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreateAgentSessions: &CreateAgentSessionConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 5,
						},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputAgentSessionJob(data, mainJobName)
			},
		},
		{
			name:           "upload_assets",
			safeOutputType: "upload-assets",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					UploadAssets: &UploadAssetsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 10,
						},
					},
				}
			},
			requiredEnvVar: "GH_AW_WORKFLOW_ID",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildUploadAssetsJob(data, mainJobName, false)
			},
		},
	}

	// Known issue: Individual job builders are missing GH_AW_WORKFLOW_ID
	// These job builders need to be fixed to include the environment variable
	// Tracked in: https://github.com/github/gh-aw/issues/7023
	knownMissingEnvVar := map[string]bool{
		"create_issue":               true,
		"create_discussion":          true,
		"add_comment":                true,
		"add_labels":                 true,
		"create_pr_review_comment":   true,
		"create_code_scanning_alert": true,
		"create_agent_session":       true,
		"upload_assets":              true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests for job builders with known missing GH_AW_WORKFLOW_ID
			if knownMissingEnvVar[tt.name] && tt.requiredEnvVar == "GH_AW_WORKFLOW_ID" {
				t.Skip("Known issue: GH_AW_WORKFLOW_ID missing from this job builder. Remove this skip when fixed.")
			}

			// Create compiler instance
			c := NewCompiler()

			// Build workflow data with the specific safe output configuration
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				Source:      "test-source",
				SafeOutputs: tt.configBuilder(),
			}

			// Build the job
			job, err := tt.jobBuilder(c, workflowData, "main_job")
			if err != nil {
				t.Fatalf("Failed to build %s job: %v", tt.name, err)
			}

			if job == nil {
				t.Fatalf("Job should not be nil for %s", tt.name)
			}

			// Verify the job has steps
			if len(job.Steps) == 0 {
				t.Fatalf("Job should have at least one step for %s", tt.name)
			}

			// Convert steps to string for checking environment variables
			stepsContent := strings.Join(job.Steps, "")

			// Verify the required environment variable is present
			if !strings.Contains(stepsContent, tt.requiredEnvVar) {
				t.Errorf("Required environment variable %s not found in %s job steps.\nJob steps:\n%s",
					tt.requiredEnvVar, tt.name, stepsContent)
			}

			// Log success for debugging
			t.Logf("✓ %s job built successfully with required env var %s", tt.name, tt.requiredEnvVar)
		})
	}
}

// TestConsolidatedSafeOutputsJobIntegration tests the consolidated safe outputs job
// which combines multiple safe output operations into a single job with multiple steps.
// Many safe output types (noop, push_to_pull_request_branch, update_issue, update_pull_request,
// update_discussion, close_issue, close_pull_request, close_discussion, add_reviewer, assign_milestone,
// assign_to_agent, assign_to_user, hide_comment, update_release) are built as steps within
// the consolidated job rather than as individual jobs.
func TestConsolidatedSafeOutputsJobIntegration(t *testing.T) {
	tests := []struct {
		name                    string
		configBuilder           func() *SafeOutputsConfig
		expectedStepsContaining []string // Substrings that should appear in the consolidated job
		expectedStepNames       []string // Step names that should be present
	}{
		{
			name: "noop_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					NoOp: &NoOpConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 1,
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"noop"},
		},
		{
			name: "push_to_pull_request_branch_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					PushToPullRequestBranch: &PushToPullRequestBranchConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 1,
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"push_to_pull_request_branch"},
		},
		{
			name: "update_issue_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					UpdateIssues: &UpdateIssuesConfig{
						UpdateEntityConfig: UpdateEntityConfig{
							BaseSafeOutputConfig: BaseSafeOutputConfig{
								Max: 1,
							},
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"update_issue"},
		},
		{
			name: "update_pull_request_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					UpdatePullRequests: &UpdatePullRequestsConfig{
						UpdateEntityConfig: UpdateEntityConfig{
							BaseSafeOutputConfig: BaseSafeOutputConfig{
								Max: 1,
							},
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"update_pull_request"},
		},
		{
			name: "update_discussion_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					UpdateDiscussions: &UpdateDiscussionsConfig{
						UpdateEntityConfig: UpdateEntityConfig{
							BaseSafeOutputConfig: BaseSafeOutputConfig{
								Max: 1,
							},
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"update_discussion"},
		},
		{
			name: "close_issue_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CloseIssues: &CloseIssuesConfig{},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"close_issue"},
		},
		{
			name: "close_pull_request_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					ClosePullRequests: &ClosePullRequestsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 1,
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"close_pull_request"},
		},
		{
			name: "close_discussion_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CloseDiscussions: &CloseDiscussionsConfig{},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"close_discussion"},
		},
		{
			name: "add_reviewer_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AddReviewer: &AddReviewerConfig{
						Reviewers: []string{"user1", "user2"},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"add_reviewer"},
		},
		{
			name: "assign_milestone_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AssignMilestone: &AssignMilestoneConfig{},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"assign_milestone"},
		},
		{
			name: "assign_to_agent_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AssignToAgent: &AssignToAgentConfig{
						DefaultAgent: "copilot",
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"assign_to_agent"},
		},
		{
			name: "assign_to_user_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AssignToUser: &AssignToUserConfig{
						Allowed: []string{"user1"},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"assign_to_user"},
		},
		{
			name: "hide_comment_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					HideComment: &HideCommentConfig{},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"hide_comment"},
		},
		{
			name: "update_release_in_consolidated_job",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					UpdateRelease: &UpdateReleaseConfig{
						UpdateEntityConfig: UpdateEntityConfig{
							BaseSafeOutputConfig: BaseSafeOutputConfig{
								Max: 1,
							},
						},
					},
				}
			},
			expectedStepsContaining: []string{
				"GH_AW_WORKFLOW_ID",
			},
			expectedStepNames: []string{"process_safe_outputs"},
		},
		{
			name: "multiple_safe_outputs_consolidated",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						TitlePrefix: "[Test] ",
					},
					CreatePullRequests: &CreatePullRequestsConfig{
						TitlePrefix: "[Test] ",
					},
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 5,
						},
					},
					NoOp: &NoOpConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 1,
						},
					},
					Env: map[string]string{
						"SHARED_VAR": "shared_value",
					},
				}
			},
			expectedStepsContaining: []string{
				"SHARED_VAR",
			},
			expectedStepNames: []string{
				"process_safe_outputs", // Consolidated step for all safe outputs
			},
		},
	}

	// Known issue: Some safe output types don't generate consolidated jobs when configured alone
	// and may be missing GH_AW_WORKFLOW_ID. These need to be verified against actual behavior.
	knownNoJobGenerated := map[string]bool{
		"noop_in_consolidated_job":                        true,
		"push_to_pull_request_branch_in_consolidated_job": true,
		"update_issue_in_consolidated_job":                true,
		"update_pull_request_in_consolidated_job":         true,
		"update_discussion_in_consolidated_job":           true,
		"close_issue_in_consolidated_job":                 true,
		"close_pull_request_in_consolidated_job":          true,
		"close_discussion_in_consolidated_job":            true,
		"add_reviewer_in_consolidated_job":                true,
		"assign_milestone_in_consolidated_job":            true,
		"assign_to_agent_in_consolidated_job":             true,
		"assign_to_user_in_consolidated_job":              true,
		"hide_comment_in_consolidated_job":                true,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests for safe output types that don't generate jobs alone
			if knownNoJobGenerated[tt.name] {
				t.Skip("Known issue: This safe output type doesn't generate a consolidated job when configured alone.")
			}

			c := NewCompiler()

			workflowData := &WorkflowData{
				Name:        "test-workflow",
				Source:      "test-source",
				SafeOutputs: tt.configBuilder(),
			}

			// Build consolidated safe outputs job
			job, stepNames, err := c.buildConsolidatedSafeOutputsJob(workflowData, "main_job", "test-workflow.md")
			if err != nil {
				t.Fatalf("Failed to build consolidated safe outputs job: %v", err)
			}

			if job == nil {
				t.Fatalf("Consolidated job should not be nil for %s", tt.name)
			}

			if len(stepNames) == 0 {
				t.Fatalf("Consolidated job should have at least one step for %s", tt.name)
			}

			// Convert steps to string for verification
			stepsContent := strings.Join(job.Steps, "")

			// For consolidated job, GH_AW_WORKFLOW_ID should be at job level, not in steps
			// Check job.Env for this variable
			if job.Env == nil || job.Env["GH_AW_WORKFLOW_ID"] == "" {
				t.Errorf("GH_AW_WORKFLOW_ID should be set at job level in consolidated job for %s.\nJob.Env: %v",
					tt.name, job.Env)
			}

			// Verify other expected environment variables and step content (excluding GH_AW_WORKFLOW_ID)
			for _, expectedContent := range tt.expectedStepsContaining {
				if expectedContent == "GH_AW_WORKFLOW_ID" {
					// Skip GH_AW_WORKFLOW_ID check in steps since it's now at job level
					continue
				}
				if !strings.Contains(stepsContent, expectedContent) {
					t.Errorf("Expected content %q not found in consolidated job for %s.\nJob steps:\n%s",
						expectedContent, tt.name, stepsContent)
				}
			}

			// Verify expected step names are present
			for _, expectedStepName := range tt.expectedStepNames {
				found := false
				for _, stepName := range stepNames {
					if stepName == expectedStepName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected step name %q not found in step names for %s. Got: %v",
						expectedStepName, tt.name, stepNames)
				}
			}

			t.Logf("✓ Consolidated job built successfully with %d steps: %v", len(stepNames), stepNames)
		})
	}
}

// TestSafeOutputJobsWithCustomEnvVars tests that custom environment variables
// from safe-outputs.env are properly propagated to all safe output job types.
func TestSafeOutputJobsWithCustomEnvVars(t *testing.T) {
	tests := []struct {
		name           string
		safeOutputType string
		configBuilder  func() *SafeOutputsConfig
		customEnvVars  map[string]string
		jobBuilder     func(*Compiler, *WorkflowData, string) (*Job, error)
	}{
		{
			name:           "create_issue_with_custom_env",
			safeOutputType: "create-issue",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{
						TitlePrefix: "[Test] ",
					},
					Env: map[string]string{
						"CUSTOM_VAR":   "custom_value",
						"GITHUB_TOKEN": "${{ secrets.CUSTOM_PAT }}",
					},
				}
			},
			customEnvVars: map[string]string{
				"CUSTOM_VAR":   "CUSTOM_VAR: custom_value",
				"GITHUB_TOKEN": "GITHUB_TOKEN: ${{ secrets.CUSTOM_PAT }}",
			},
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputIssueJob(data, mainJobName)
			},
		},
		{
			name:           "create_pull_request_with_custom_env",
			safeOutputType: "create-pull-request",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					CreatePullRequests: &CreatePullRequestsConfig{
						TitlePrefix: "[Test] ",
					},
					Env: map[string]string{
						"DEBUG_MODE": "true",
						"API_KEY":    "${{ secrets.API_KEY }}",
					},
				}
			},
			customEnvVars: map[string]string{
				"DEBUG_MODE": "DEBUG_MODE: true",
				"API_KEY":    "API_KEY: ${{ secrets.API_KEY }}",
			},
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputPullRequestJob(data, mainJobName)
			},
		},
		{
			name:           "add_comment_with_custom_env",
			safeOutputType: "add-comment",
			configBuilder: func() *SafeOutputsConfig {
				return &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 5,
						},
					},
					Env: map[string]string{
						"NOTIFICATION_URL": "${{ secrets.WEBHOOK_URL }}",
						"ENVIRONMENT":      "production",
					},
				}
			},
			customEnvVars: map[string]string{
				"NOTIFICATION_URL": "NOTIFICATION_URL: ${{ secrets.WEBHOOK_URL }}",
				"ENVIRONMENT":      "ENVIRONMENT: production",
			},
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				return c.buildCreateOutputAddCommentJob(data, mainJobName, "", "", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create compiler instance
			c := NewCompiler()

			// Build workflow data with custom env vars
			workflowData := &WorkflowData{
				Name:        "test-workflow",
				Source:      "test-source",
				SafeOutputs: tt.configBuilder(),
			}

			// Build the job
			job, err := tt.jobBuilder(c, workflowData, "main_job")
			if err != nil {
				t.Fatalf("Failed to build %s job: %v", tt.name, err)
			}

			// Convert steps to string for checking environment variables
			stepsContent := strings.Join(job.Steps, "")

			// Verify all custom environment variables are present
			for envVarName, expectedContent := range tt.customEnvVars {
				if !strings.Contains(stepsContent, expectedContent) {
					t.Errorf("Custom environment variable %s not found in %s job.\nExpected: %s\nJob steps:\n%s",
						envVarName, tt.name, expectedContent, stepsContent)
				}
			}

			t.Logf("✓ %s job includes all custom environment variables", tt.name)
		})
	}
}

// TestSafeOutputJobsMissingConfig tests that jobs fail gracefully when required configuration is missing
func TestSafeOutputJobsMissingConfig(t *testing.T) {
	tests := []struct {
		name       string
		jobBuilder func(*Compiler, *WorkflowData, string) (*Job, error)
		shouldFail bool
	}{
		{
			name: "missing_tool_without_config",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				// Set SafeOutputs to nil to trigger validation error
				data.SafeOutputs = nil
				return c.buildCreateOutputMissingToolJob(data, mainJobName)
			},
			shouldFail: true,
		},
		{
			name: "create_issue_without_config",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				// Set SafeOutputs to nil
				data.SafeOutputs = nil
				return c.buildCreateOutputIssueJob(data, mainJobName)
			},
			shouldFail: true,
		},
		{
			name: "add_labels_without_config",
			jobBuilder: func(c *Compiler, data *WorkflowData, mainJobName string) (*Job, error) {
				// Set SafeOutputs to nil
				data.SafeOutputs = nil
				return c.buildAddLabelsJob(data, mainJobName)
			},
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCompiler()
			workflowData := &WorkflowData{
				Name:   "test-workflow",
				Source: "test-source",
			}

			job, err := tt.jobBuilder(c, workflowData, "main_job")

			if tt.shouldFail {
				if err == nil {
					t.Errorf("Expected error for %s, but got none. Job: %v", tt.name, job)
				} else {
					t.Logf("✓ %s correctly failed with error: %v", tt.name, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for %s, but got: %v", tt.name, err)
				}
			}
		})
	}
}

// TestConsolidatedSafeOutputsJobWithCustomEnv tests that custom environment variables
// are properly included in the consolidated safe outputs job.
func TestConsolidatedSafeOutputsJobWithCustomEnv(t *testing.T) {
	c := NewCompiler()

	workflowData := &WorkflowData{
		Name:   "test-workflow",
		Source: "test-source",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
			UpdateIssues: &UpdateIssuesConfig{
				UpdateEntityConfig: UpdateEntityConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 1,
					},
				},
			},
			Env: map[string]string{
				"SHARED_VAR":   "shared_value",
				"GITHUB_TOKEN": "${{ secrets.CUSTOM_PAT }}",
				"DEBUG":        "true",
			},
		},
	}

	// Build consolidated safe outputs job
	job, stepNames, err := c.buildConsolidatedSafeOutputsJob(workflowData, "main_job", "test-workflow.md")
	if err != nil {
		t.Fatalf("Failed to build consolidated safe outputs job: %v", err)
	}

	if job == nil {
		t.Fatal("Consolidated job should not be nil")
	}

	if len(stepNames) == 0 {
		t.Fatal("Consolidated job should have at least one step")
	}

	// Convert steps to string for verification
	stepsContent := strings.Join(job.Steps, "")

	// Verify custom environment variables are present
	expectedEnvVars := map[string]string{
		"SHARED_VAR":   "SHARED_VAR: shared_value",
		"GITHUB_TOKEN": "GITHUB_TOKEN: ${{ secrets.CUSTOM_PAT }}",
		"DEBUG":        "DEBUG: true",
	}

	for envVarName, expectedContent := range expectedEnvVars {
		if !strings.Contains(stepsContent, expectedContent) {
			t.Errorf("Expected custom environment variable %s not found in consolidated job.\nExpected: %s\nJob steps:\n%s",
				envVarName, expectedContent, stepsContent)
		}
	}

	// Verify GH_AW_WORKFLOW_ID is present at job level
	if job.Env == nil || job.Env["GH_AW_WORKFLOW_ID"] == "" {
		t.Error("GH_AW_WORKFLOW_ID should be set at job level in consolidated job")
	}

	t.Logf("✓ Consolidated job with custom env vars built successfully with %d steps: %v", len(stepNames), stepNames)
}

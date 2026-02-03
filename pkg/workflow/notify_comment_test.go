//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestConclusionJob(t *testing.T) {
	tests := []struct {
		name               string
		addCommentConfig   bool
		aiReaction         string
		command            []string
		safeOutputJobNames []string
		expectJob          bool
		expectConditions   []string
		expectNeeds        []string
	}{
		{
			name:               "conclusion job created when add-comment and ai-reaction are configured",
			addCommentConfig:   true,
			aiReaction:         "eyes",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "create_issue", "missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
				"!(needs.add_comment.outputs.comment_id)",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "add_comment", "create_issue", "missing_tool"},
		},
		{
			name:               "conclusion job depends on all safe output jobs",
			addCommentConfig:   true,
			aiReaction:         "eyes",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "create_issue", "missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
				"!(needs.add_comment.outputs.comment_id)",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "add_comment", "create_issue", "missing_tool"},
		},
		{
			name:               "conclusion job not created when add-comment is not configured",
			addCommentConfig:   false,
			aiReaction:         "",
			command:            nil,
			safeOutputJobNames: []string{},
			expectJob:          false,
		},
		{
			name:               "conclusion job created when add-comment is configured but ai-reaction is not",
			addCommentConfig:   true,
			aiReaction:         "",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
				"!(needs.add_comment.outputs.comment_id)",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "add_comment", "missing_tool"},
		},
		{
			name:               "conclusion job created when reaction is explicitly set to none",
			addCommentConfig:   true,
			aiReaction:         "none",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
				"!(needs.add_comment.outputs.comment_id)",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "add_comment", "missing_tool"},
		},
		{
			name:               "conclusion job created when command and reaction are configured (no add-comment)",
			addCommentConfig:   false,
			aiReaction:         "eyes",
			command:            []string{"test-command"},
			safeOutputJobNames: []string{"missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "missing_tool"},
		},
		{
			name:               "conclusion job created when command is configured with push-to-pull-request-branch",
			addCommentConfig:   false,
			aiReaction:         "eyes",
			command:            []string{"mergefest"},
			safeOutputJobNames: []string{"push_to_pull_request_branch", "missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "push_to_pull_request_branch", "missing_tool"},
		},
		{
			name:               "conclusion job created when command is configured but reaction is none",
			addCommentConfig:   false,
			aiReaction:         "none",
			command:            []string{"test-command"},
			safeOutputJobNames: []string{"missing_tool"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "missing_tool"},
		},
		{
			name:               "conclusion job depends on custom safe-jobs",
			addCommentConfig:   true,
			aiReaction:         "eyes",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "create_issue", "my_custom_job", "another_custom_safe_job"},
			expectJob:          true,
			expectConditions: []string{
				"always()",
				"needs.agent.result != 'skipped'",
				"!(needs.add_comment.outputs.comment_id)",
			},
			expectNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName), "add_comment", "create_issue", "my_custom_job", "another_custom_safe_job"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test workflow
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:       "Test Workflow",
				AIReaction: tt.aiReaction,
				Command:    tt.command,
			}

			if tt.addCommentConfig {
				workflowData.SafeOutputs = &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: 1,
						},
					},
				}
			} else if len(tt.safeOutputJobNames) > 0 {
				// If there are safe output jobs but no add-comment, create a minimal SafeOutputs config
				// This represents a scenario where other safe outputs exist (like missing_tool)
				workflowData.SafeOutputs = &SafeOutputsConfig{
					MissingTool: &MissingToolConfig{},
				}
			}

			// Build the conclusion job
			job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), tt.safeOutputJobNames)
			if err != nil {
				t.Fatalf("Failed to build conclusion job: %v", err)
			}

			if tt.expectJob {
				if job == nil {
					t.Fatal("Expected conclusion job to be created, but got nil")
				}

				// Check job name
				if job.Name != "conclusion" {
					t.Errorf("Expected job name 'conclusion', got '%s'", job.Name)
				}

				// Check conditions
				for _, expectedCond := range tt.expectConditions {
					if !strings.Contains(job.If, expectedCond) {
						t.Errorf("Expected condition '%s' to be in job.If, but it wasn't.\nActual If: %s", expectedCond, job.If)
					}
				}

				// Check needs
				if len(job.Needs) != len(tt.expectNeeds) {
					t.Errorf("Expected %d needs, got %d: %v", len(tt.expectNeeds), len(job.Needs), job.Needs)
				}
				for _, expectedNeed := range tt.expectNeeds {
					found := false
					for _, need := range job.Needs {
						if need == expectedNeed {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected need '%s' not found in job.Needs: %v", expectedNeed, job.Needs)
					}
				}

				// Check permissions
				if !strings.Contains(job.Permissions, "issues: write") {
					t.Error("Expected 'issues: write' permission in conclusion job")
				}
				if !strings.Contains(job.Permissions, "pull-requests: write") {
					t.Error("Expected 'pull-requests: write' permission in conclusion job")
				}
				if !strings.Contains(job.Permissions, "discussions: write") {
					t.Error("Expected 'discussions: write' permission in conclusion job")
				}

				// Check that the job has the update reaction step
				stepsYAML := strings.Join(job.Steps, "")
				if !strings.Contains(stepsYAML, "Update reaction comment with completion status") {
					t.Error("Expected 'Update reaction comment with completion status' step in conclusion job")
				}
				if !strings.Contains(stepsYAML, "GH_AW_COMMENT_ID") {
					t.Error("Expected GH_AW_COMMENT_ID environment variable in conclusion job")
				}
				if !strings.Contains(stepsYAML, "GH_AW_AGENT_CONCLUSION") {
					t.Error("Expected GH_AW_AGENT_CONCLUSION environment variable in conclusion job")
				}
			} else {
				if job != nil {
					t.Errorf("Expected no conclusion job, but got one: %v", job)
				}
			}
		})
	}
}

func TestConclusionJobIntegration(t *testing.T) {
	// Test that the job is properly integrated with activation job outputs
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name:       "Test Workflow",
		AIReaction: "eyes", // This causes the activation job to create a comment
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
		},
	}

	// Build the conclusion job with sample safe output job names
	safeOutputJobNames := []string{"add_comment", "missing_tool"}
	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), safeOutputJobNames)
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}

	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	// Convert job to YAML string for checking
	jobYAML := strings.Join(job.Steps, "")

	// Check that environment variables reference activation outputs
	if !strings.Contains(jobYAML, "needs.activation.outputs.comment_id") {
		t.Error("Expected GH_AW_COMMENT_ID to reference activation.outputs.comment_id")
	}
	if !strings.Contains(jobYAML, "needs.activation.outputs.comment_repo") {
		t.Error("Expected GH_AW_COMMENT_REPO to reference activation.outputs.comment_repo")
	}

	// Check that agent result is referenced
	if !strings.Contains(jobYAML, "needs.agent.result") {
		t.Error("Expected GH_AW_AGENT_CONCLUSION to reference needs.agent.result")
	}

	// Check expected conditions are present
	if !strings.Contains(job.If, "always()") {
		t.Error("Expected always() in conclusion condition")
	}
	if !strings.Contains(job.If, "needs.agent.result != 'skipped'") {
		t.Error("Expected agent not skipped check in conclusion condition")
	}
	if !strings.Contains(job.If, "!(needs.add_comment.outputs.comment_id)") {
		t.Error("Expected NOT add_comment.outputs.comment_id check in conclusion condition")
	}

	// Verify job depends on the safe output jobs
	for _, expectedNeed := range safeOutputJobNames {
		found := false
		for _, need := range job.Needs {
			if need == expectedNeed {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected conclusion job to depend on '%s'", expectedNeed)
		}
	}
}

func TestConclusionJobWithMessages(t *testing.T) {
	// Test that the conclusion job includes custom messages when configured
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name:       "Test Workflow",
		AIReaction: "eyes",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
			Messages: &SafeOutputMessagesConfig{
				Footer:     "> Custom footer message",
				RunSuccess: "✅ Custom success: [{workflow_name}]({run_url})",
				RunFailure: "❌ Custom failure: [{workflow_name}]({run_url}) {status}",
			},
		},
	}

	// Build the conclusion job
	safeOutputJobNames := []string{"add_comment", "missing_tool"}
	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), safeOutputJobNames)
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}

	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	// Convert job to YAML string for checking
	jobYAML := strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUT_MESSAGES environment variable is declared in env section
	envVarDeclaration := "          GH_AW_SAFE_OUTPUT_MESSAGES: "
	if !strings.Contains(jobYAML, envVarDeclaration) {
		t.Error("Expected GH_AW_SAFE_OUTPUT_MESSAGES environment variable to be declared when messages are configured")
	}

	// Check that the messages contain expected values
	if !strings.Contains(jobYAML, "Custom footer message") {
		t.Error("Expected custom footer message to be included in the conclusion job")
	}
	if !strings.Contains(jobYAML, "Custom success") {
		t.Error("Expected custom success message to be included in the conclusion job")
	}
	if !strings.Contains(jobYAML, "Custom failure") {
		t.Error("Expected custom failure message to be included in the conclusion job")
	}
}

func TestConclusionJobWithoutMessages(t *testing.T) {
	// Test that the conclusion job does NOT include messages env var when not configured
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name:       "Test Workflow",
		AIReaction: "eyes",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
			// Messages intentionally nil
		},
	}

	// Build the conclusion job
	safeOutputJobNames := []string{"add_comment", "missing_tool"}
	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), safeOutputJobNames)
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}

	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	// Convert job to YAML string for checking
	jobYAML := strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUT_MESSAGES environment variable is NOT set in env section
	// Note: The script itself contains references to GH_AW_SAFE_OUTPUT_MESSAGES (for parsing),
	// but the env: section should not have it when messages are not configured
	// We look for the env var declaration pattern which has a colon followed by a value
	// The bundled script has references like "process.env.GH_AW_SAFE_OUTPUT_MESSAGES" which we don't want to match
	envVarDeclaration := "          GH_AW_SAFE_OUTPUT_MESSAGES: "
	if strings.Contains(jobYAML, envVarDeclaration) {
		t.Error("Expected GH_AW_SAFE_OUTPUT_MESSAGES environment variable to NOT be declared when messages are not configured")
	}
}

func TestActivationJobWithMessages(t *testing.T) {
	// Test that the activation job includes custom messages when configured
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name:       "Test Workflow",
		AIReaction: "eyes",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
			Messages: &SafeOutputMessagesConfig{
				Footer:     "> Custom footer message",
				RunStarted: "🚀 [{workflow_name}]({run_url}) starting on {event_type}",
				RunSuccess: "✅ Custom success: [{workflow_name}]({run_url})",
				RunFailure: "❌ Custom failure: [{workflow_name}]({run_url}) {status}",
			},
		},
	}

	// Build the activation job
	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	if err != nil {
		t.Fatalf("Failed to build activation job: %v", err)
	}

	if job == nil {
		t.Fatal("Expected activation job to be created")
	}

	// Convert job to YAML string for checking
	jobYAML := strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUT_MESSAGES environment variable is declared
	envVarDeclaration := "          GH_AW_SAFE_OUTPUT_MESSAGES: "
	if !strings.Contains(jobYAML, envVarDeclaration) {
		t.Error("Expected GH_AW_SAFE_OUTPUT_MESSAGES environment variable to be declared when messages are configured")
	}

	// Check that the messages contain expected values
	if !strings.Contains(jobYAML, "Custom footer message") {
		t.Error("Expected custom footer message to be included in the activation job")
	}
	if !strings.Contains(jobYAML, "starting on") {
		t.Error("Expected custom run-started message to be included in the activation job")
	}
}

func TestActivationJobWithoutMessages(t *testing.T) {
	// Test that the activation job does NOT include messages env var when not configured
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name:       "Test Workflow",
		AIReaction: "eyes",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
			// Messages intentionally nil
		},
	}

	// Build the activation job
	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	if err != nil {
		t.Fatalf("Failed to build activation job: %v", err)
	}

	if job == nil {
		t.Fatal("Expected activation job to be created")
	}

	// Convert job to YAML string for checking
	jobYAML := strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUT_MESSAGES environment variable is NOT declared
	// Note: The script itself contains references to GH_AW_SAFE_OUTPUT_MESSAGES (for parsing),
	// but the env: section should not have it when messages are not configured
	envVarDeclaration := "          GH_AW_SAFE_OUTPUT_MESSAGES: "
	if strings.Contains(jobYAML, envVarDeclaration) {
		t.Error("Expected GH_AW_SAFE_OUTPUT_MESSAGES environment variable to NOT be declared when messages are not configured")
	}
}

// TestConclusionJobWithGeneratedAssets tests that the conclusion job includes environment variables
// for safe output job URLs when safe output jobs are present
func TestConclusionJobWithGeneratedAssets(t *testing.T) {
	compiler := NewCompiler()

	// Create workflow data with safe outputs configuration
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: 1,
				},
			},
			NoOp: &NoOpConfig{},
		},
		AIReaction: "eyes",
	}

	// Define safe output jobs that should have URL outputs
	safeOutputJobNames := []string{
		"create_issue",
		"add_comment",
		"create_pull_request",
		"create_discussion",
	}

	// Build the conclusion job
	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), safeOutputJobNames)
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}

	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	// Convert job to YAML string for checking
	jobYAML := strings.Join(job.Steps, "")

	// Check that GH_AW_SAFE_OUTPUT_JOBS environment variable is declared with JSON mapping
	if !strings.Contains(jobYAML, "GH_AW_SAFE_OUTPUT_JOBS:") {
		t.Error("Expected GH_AW_SAFE_OUTPUT_JOBS environment variable to be declared")
	}

	// Check that individual URL output environment variables are declared
	expectedEnvVars := []string{
		"GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}",
		"GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL: ${{ needs.add_comment.outputs.comment_url }}",
		"GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL: ${{ needs.create_pull_request.outputs.pull_request_url }}",
		"GH_AW_OUTPUT_CREATE_DISCUSSION_DISCUSSION_URL: ${{ needs.create_discussion.outputs.discussion_url }}",
	}

	for _, expectedVar := range expectedEnvVars {
		if !strings.Contains(jobYAML, expectedVar) {
			t.Errorf("Expected environment variable not found: %s", expectedVar)
		}
	}
}

// TestBuildSafeOutputJobsEnvVars tests the helper function that creates environment variables
// for safe output job URLs
func TestBuildSafeOutputJobsEnvVars(t *testing.T) {
	tests := []struct {
		name          string
		jobNames      []string
		expectJSON    bool
		expectEnvVars int
		checkEnvVars  []string
		checkJSONKeys []string
	}{
		{
			name:          "creates env vars for safe_outputs job",
			jobNames:      []string{"create_issue"},
			expectJSON:    true,
			expectEnvVars: 1,
			checkEnvVars: []string{
				"GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}",
			},
			checkJSONKeys: []string{"create_issue"},
		},
		{
			name:          "creates env vars for multiple jobs",
			jobNames:      []string{"create_issue", "add_comment", "create_pull_request"},
			expectJSON:    true,
			expectEnvVars: 3,
			checkEnvVars: []string{
				"GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}",
				"GH_AW_OUTPUT_ADD_COMMENT_COMMENT_URL: ${{ needs.add_comment.outputs.comment_url }}",
				"GH_AW_OUTPUT_CREATE_PULL_REQUEST_PULL_REQUEST_URL: ${{ needs.create_pull_request.outputs.pull_request_url }}",
			},
			checkJSONKeys: []string{"create_issue", "add_comment", "create_pull_request"},
		},
		{
			name:          "creates env vars for push_to_pull_request_branch job",
			jobNames:      []string{"push_to_pull_request_branch"},
			expectJSON:    true,
			expectEnvVars: 1,
			checkEnvVars: []string{
				"GH_AW_OUTPUT_PUSH_TO_PULL_REQUEST_BRANCH_COMMIT_URL: ${{ needs.push_to_pull_request_branch.outputs.commit_url }}",
			},
			checkJSONKeys: []string{"push_to_pull_request_branch"},
		},
		{
			name:          "skips jobs without URL outputs",
			jobNames:      []string{"create_issue", "detection", "some_custom_job"},
			expectJSON:    true,
			expectEnvVars: 1,
			checkEnvVars: []string{
				"GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}",
			},
			checkJSONKeys: []string{"create_issue"},
		},
		{
			name:          "handles empty job list",
			jobNames:      []string{},
			expectJSON:    false,
			expectEnvVars: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonStr, envVars := buildSafeOutputJobsEnvVars(tt.jobNames)

			// Check JSON output
			if tt.expectJSON {
				if jsonStr == "" {
					t.Error("Expected non-empty JSON string")
				}

				// Check that expected keys are in JSON
				for _, key := range tt.checkJSONKeys {
					if !strings.Contains(jsonStr, key) {
						t.Errorf("Expected JSON to contain key: %s", key)
					}
				}
			} else {
				if jsonStr != "" {
					t.Errorf("Expected empty JSON string, got: %s", jsonStr)
				}
			}

			// Check env vars count
			if len(envVars) != tt.expectEnvVars {
				t.Errorf("Expected %d env vars, got %d", tt.expectEnvVars, len(envVars))
			}

			// Check expected env var strings
			if len(tt.checkEnvVars) > 0 {
				envVarsStr := strings.Join(envVars, "")
				for _, expectedVar := range tt.checkEnvVars {
					if !strings.Contains(envVarsStr, expectedVar) {
						t.Errorf("Expected env var not found: %s", expectedVar)
					}
				}
			}
		})
	}
}

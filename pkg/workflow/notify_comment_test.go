//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
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
		expectUpdateStep   bool // whether to expect the "Update reaction comment" step
	}{
		{
			name:               "conclusion job created when add-comment and ai-reaction are configured",
			addCommentConfig:   true,
			aiReaction:         "eyes",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "create_issue", "missing_tool"},
			expectJob:          true,
			expectUpdateStep:   false, // No automatic bundling - status-comment must be explicit
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
			expectUpdateStep:   false, // No automatic bundling - status-comment must be explicit
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
			expectUpdateStep:   false,
		},
		{
			name:               "conclusion job created when add-comment is configured but ai-reaction is not",
			addCommentConfig:   true,
			aiReaction:         "",
			command:            nil,
			safeOutputJobNames: []string{"add_comment", "missing_tool"},
			expectJob:          true,
			expectUpdateStep:   false, // No update step when no ai-reaction
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
			expectUpdateStep:   false, // No update step when reaction is none
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
			expectUpdateStep:   false, // No automatic bundling - status-comment must be explicit
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
			expectUpdateStep:   false, // No automatic bundling - status-comment must be explicit
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
			expectUpdateStep:   false, // No update step when reaction is none
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
			expectUpdateStep:   false, // No automatic bundling - status-comment must be explicit
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
				AIReaction: ReactionType(tt.aiReaction),
				Command:    tt.command,
			}

			if tt.addCommentConfig {
				workflowData.SafeOutputs = &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{
							Max: strPtr("1"),
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
					found := slices.Contains(job.Needs, expectedNeed)
					if !found {
						t.Errorf("Expected need '%s' not found in job.Needs: %v", expectedNeed, job.Needs)
					}
				}

				// Check permissions based on what safe-outputs are configured
				// When add-comment is configured by default, it requires issues permission
				// (PR comments are issue comments, so only issues: write is needed, not pull-requests: write)
				// When only missing_tool/noop is configured, minimal permissions are needed
				if tt.addCommentConfig {
					// add-comment requires issues write permission by default
					if !strings.Contains(job.Permissions, "issues: write") {
						t.Error("Expected 'issues: write' permission when add-comment is configured")
					}
					if strings.Contains(job.Permissions, "discussions: write") {
						t.Error("Did not expect 'discussions: write' permission when add-comment is configured by default")
					}
				}
				// No need to check for specific permissions when only noop/missing_tool is configured
				// as they don't require write permissions on their own

				// Check that the job has the update reaction step (if expected)
				stepsYAML := strings.Join(job.Steps, "")
				hasUpdateStep := strings.Contains(stepsYAML, "Update reaction comment with completion status")
				if tt.expectUpdateStep {
					if !hasUpdateStep {
						t.Errorf("[%s] Expected 'Update reaction comment with completion status' step in conclusion job", tt.name)
					}
					if !strings.Contains(stepsYAML, "GH_AW_COMMENT_ID") {
						t.Errorf("[%s] Expected GH_AW_COMMENT_ID environment variable in conclusion job", tt.name)
					}
				} else {
					if hasUpdateStep {
						t.Errorf("[%s] Did not expect 'Update reaction comment with completion status' step in conclusion job", tt.name)
					}
				}
				// GH_AW_AGENT_CONCLUSION should always be present for agent failure handling
				if !strings.Contains(stepsYAML, "GH_AW_AGENT_CONCLUSION") {
					t.Errorf("[%s] Expected GH_AW_AGENT_CONCLUSION environment variable in conclusion job", tt.name)
				}
			} else {
				if job != nil {
					t.Errorf("Expected no conclusion job, but got one: %v", job)
				}
			}
		})
	}
}

func TestConclusionJobRunsForSkillInstallFailures(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI:          "copilot",
		Skills:      []string{"owner/repo/skills/example@deadbeef"},
		SafeOutputs: &SafeOutputsConfig{},
	}

	condition := RenderCondition(compiler.buildConclusionJobCondition(data, string(constants.AgentJobName), nil))

	if !strings.Contains(condition, "needs.activation.outputs.skill_install_failure_count != ''") ||
		!strings.Contains(condition, "needs.activation.outputs.skill_install_failure_count != '0'") {
		t.Errorf("Expected conclusion condition to include skill installation failures, got: %q", condition)
	}
}

func TestConclusionJobIntegration(t *testing.T) {
	// Test that the job is properly integrated with activation job outputs
	compiler := NewCompiler()
	statusCommentTrue := true
	workflowData := &WorkflowData{
		Name:          "Test Workflow",
		AIReaction:    "eyes",
		StatusComment: &statusCommentTrue, // Explicitly enable status comments
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
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
		found := slices.Contains(job.Needs, expectedNeed)
		if !found {
			t.Errorf("Expected conclusion job to depend on '%s'", expectedNeed)
		}
	}
}

func TestConclusionJobSafeOutputsResult(t *testing.T) {
	// Test that GH_AW_SAFE_OUTPUTS_RESULT env var is emitted when safe_outputs is in safeOutputJobNames
	compiler := NewCompiler()
	statusCommentTrue := true
	workflowData := &WorkflowData{
		Name:          "Test Workflow",
		AIReaction:    "eyes",
		StatusComment: &statusCommentTrue,
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
				},
			},
		},
	}

	tests := []struct {
		name               string
		safeOutputJobNames []string
		expectEnvVar       bool
	}{
		{
			name:               "GH_AW_SAFE_OUTPUTS_RESULT present when safe_outputs is in job list",
			safeOutputJobNames: []string{"safe_outputs", "add_comment"},
			expectEnvVar:       true,
		},
		{
			name:               "GH_AW_SAFE_OUTPUTS_RESULT absent when safe_outputs is not in job list",
			safeOutputJobNames: []string{"add_comment"},
			expectEnvVar:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), tt.safeOutputJobNames)
			if err != nil {
				t.Fatalf("Failed to build conclusion job: %v", err)
			}
			if job == nil {
				t.Fatal("Expected conclusion job to be created")
			}

			jobYAML := strings.Join(job.Steps, "")
			envVarDeclaration := "GH_AW_SAFE_OUTPUTS_RESULT: ${{ needs.safe_outputs.result }}"
			hasEnvVar := strings.Contains(jobYAML, envVarDeclaration)

			if tt.expectEnvVar && !hasEnvVar {
				t.Errorf("Expected GH_AW_SAFE_OUTPUTS_RESULT env var when safe_outputs is in job list, but it was missing")
			}
			if !tt.expectEnvVar && hasEnvVar {
				t.Errorf("Did not expect GH_AW_SAFE_OUTPUTS_RESULT env var when safe_outputs is not in job list, but it was present")
			}
		})
	}
}

func TestConclusionJobWithMessages(t *testing.T) {
	// Test that the conclusion job includes custom messages when configured
	compiler := NewCompiler()
	statusCommentTrue := true
	workflowData := &WorkflowData{
		Name:          "Test Workflow",
		AIReaction:    "eyes",
		StatusComment: &statusCommentTrue, // Explicitly enable status comments
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
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
					Max: strPtr("1"),
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
	statusCommentTrue := true
	workflowData := &WorkflowData{
		Name:          "Test Workflow",
		AIReaction:    "eyes",
		StatusComment: &statusCommentTrue, // Explicitly enable status comments
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
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
					Max: strPtr("1"),
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

	statusCommentTrue := true
	// Create workflow data with safe outputs configuration
	workflowData := &WorkflowData{
		Name:          "Test Workflow",
		StatusComment: &statusCommentTrue, // Explicitly enable status comments
		SafeOutputs: &SafeOutputsConfig{
			AddComments: &AddCommentsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
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

func TestConclusionJobActionFailureIssueExpiration_DefaultFromRepoConfig(t *testing.T) {
	gitRoot := t.TempDir()

	compiler := NewCompiler()
	compiler.gitRoot = gitRoot
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	jobYAML := strings.Join(job.Steps, "")
	if !strings.Contains(jobYAML, `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "168"`) {
		t.Error("Expected default action failure issue expiration env var (168 hours) in conclusion job")
	}
}

func TestConclusionJobActionFailureIssueExpiration_DisabledWhenFailureReportingIsOff(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			NoOp:                 &NoOpConfig{},
			ReportFailureAsIssue: templatableBoolPtr("false"),
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	jobYAML := strings.Join(job.Steps, "")
	if !strings.Contains(jobYAML, `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "0"`) {
		t.Error("Expected disabled action failure issue expiration env var when failure reporting is disabled")
	}
}

func TestConclusionJobActionFailureIssueExpiration_UsesAWJSONConfig(t *testing.T) {
	gitRoot := t.TempDir()
	workflowsDir := filepath.Join(gitRoot, ".github", "workflows")
	if err := os.MkdirAll(workflowsDir, 0o755); err != nil {
		t.Fatalf("Failed to create workflows directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workflowsDir, "aw.json"), []byte(`{"maintenance":{"action_failure_issue_expires":48}}`), 0o600); err != nil {
		t.Fatalf("Failed to write aw.json: %v", err)
	}

	compiler := NewCompiler()
	compiler.gitRoot = gitRoot
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	jobYAML := strings.Join(job.Steps, "")
	if !strings.Contains(jobYAML, `GH_AW_ACTION_FAILURE_ISSUE_EXPIRES_HOURS: "48"`) {
		t.Error("Expected action failure issue expiration env var from aw.json in conclusion job")
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
			name:          "includes custom jobs and skips system jobs",
			jobNames:      []string{"create_issue", "safe_outputs", "some_custom_job"},
			expectJSON:    true,
			expectEnvVars: 1,
			checkEnvVars: []string{
				"GH_AW_OUTPUT_CREATE_ISSUE_ISSUE_URL: ${{ needs.create_issue.outputs.issue_url }}",
			},
			checkJSONKeys: []string{"create_issue", "some_custom_job"},
		},
		{
			name:          "custom-only jobs produce JSON without URL env vars",
			jobNames:      []string{"safe_outputs", "send_slack_message"},
			expectJSON:    true,
			expectEnvVars: 0,
			checkJSONKeys: []string{"send_slack_message"},
		},
		{
			name:          "system jobs are excluded from JSON",
			jobNames:      []string{"safe_outputs", "upload_assets"},
			expectJSON:    false,
			expectEnvVars: 0,
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

// TestStatusCommentDecoupling tests the decoupling of status-comment from ai-reaction
func TestStatusCommentDecoupling(t *testing.T) {
	tests := []struct {
		name                     string
		aiReaction               string
		statusComment            *bool
		expectActivationComment  bool
		expectConclusionUpdate   bool
		expectActivationReaction bool
		safeOutputJobNames       []string
	}{
		{
			name:                     "ai-reaction without status-comment",
			aiReaction:               "eyes",
			statusComment:            nil,
			expectActivationComment:  false,
			expectConclusionUpdate:   false,
			expectActivationReaction: true,
			safeOutputJobNames:       []string{"missing_tool"},
		},
		{
			name:                     "both ai-reaction and status-comment enabled",
			aiReaction:               "eyes",
			statusComment:            boolPtr(true),
			expectActivationComment:  true,
			expectConclusionUpdate:   true,
			expectActivationReaction: true,
			safeOutputJobNames:       []string{"missing_tool"},
		},
		{
			name:                     "ai-reaction with explicit status-comment: false",
			aiReaction:               "eyes",
			statusComment:            boolPtr(false),
			expectActivationComment:  false,
			expectConclusionUpdate:   false,
			expectActivationReaction: true,
			safeOutputJobNames:       []string{"missing_tool"},
		},
		{
			name:                     "neither ai-reaction nor status-comment",
			aiReaction:               "",
			statusComment:            nil,
			expectActivationComment:  false,
			expectConclusionUpdate:   false,
			expectActivationReaction: false,
			safeOutputJobNames:       []string{"missing_tool"},
		},
		{
			name:                     "status-comment without ai-reaction",
			aiReaction:               "",
			statusComment:            boolPtr(true),
			expectActivationComment:  true,
			expectConclusionUpdate:   true,
			expectActivationReaction: false,
			safeOutputJobNames:       []string{"missing_tool"},
		},
		{
			name:                     "status-comment with ai-reaction: none",
			aiReaction:               "none",
			statusComment:            boolPtr(true),
			expectActivationComment:  true,
			expectConclusionUpdate:   true,
			expectActivationReaction: false,
			safeOutputJobNames:       []string{"missing_tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			// Test activation job
			workflowData := &WorkflowData{
				Name:          "Test Workflow",
				AIReaction:    ReactionType(tt.aiReaction),
				StatusComment: tt.statusComment,
				SafeOutputs: &SafeOutputsConfig{
					MissingTool: &MissingToolConfig{},
				},
			}

			activationJob, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
			if err != nil {
				t.Fatalf("Failed to build activation job: %v", err)
			}

			activationSteps := strings.Join(activationJob.Steps, "")

			// Check for activation comment step
			hasActivationComment := strings.Contains(activationSteps, "Add comment with workflow run link")
			if hasActivationComment != tt.expectActivationComment {
				t.Errorf("Expected activation comment step: %v, got: %v", tt.expectActivationComment, hasActivationComment)
			}

			// Test conclusion job
			conclusionJob, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), tt.safeOutputJobNames)
			if err != nil {
				t.Fatalf("Failed to build conclusion job: %v", err)
			}

			if conclusionJob == nil {
				t.Fatal("Expected conclusion job to be created")
			}

			conclusionSteps := strings.Join(conclusionJob.Steps, "")

			// Check for conclusion update step
			hasConclusionUpdate := strings.Contains(conclusionSteps, "Update reaction comment with completion status")
			if hasConclusionUpdate != tt.expectConclusionUpdate {
				t.Errorf("Expected conclusion update step: %v, got: %v", tt.expectConclusionUpdate, hasConclusionUpdate)
			}

			// Note: Reaction is added in pre-activation job, not activation job
			// We're just checking that the workflow is correctly configured
		})
	}
}

// TestConclusionJobConcurrencyGroup tests that the conclusion job has a concurrency group
// based on the workflow ID to prevent concurrent agents on the same workflow from interfering.
func TestConclusionJobConcurrencyGroup(t *testing.T) {
	tests := []struct {
		name              string
		workflowID        string
		discriminator     string
		features          map[string]any
		expectConcurrency bool
		expectQueue       bool
		expectedGroup     string
	}{
		{
			name:              "concurrency group set when workflow ID is present",
			workflowID:        "my-workflow",
			expectConcurrency: true,
			expectQueue:       true,
			expectedGroup:     "gh-aw-conclusion-my-workflow",
		},
		{
			name:              "no concurrency group when workflow ID is empty",
			workflowID:        "",
			expectConcurrency: false,
		},
		{
			name:              "job-discriminator appended to conclusion concurrency group",
			workflowID:        "my-workflow",
			discriminator:     "${{ github.event.issue.number || github.run_id }}",
			expectConcurrency: true,
			expectQueue:       true,
			expectedGroup:     "gh-aw-conclusion-my-workflow-${{ github.event.issue.number || github.run_id }}",
		},
		{
			name:              "job-discriminator with run_id for universal uniqueness",
			workflowID:        "pentest-triage",
			discriminator:     "${{ github.run_id }}",
			expectConcurrency: true,
			expectQueue:       true,
			expectedGroup:     "gh-aw-conclusion-pentest-triage-${{ github.run_id }}",
		},
		{
			name:              "queue can be disabled with feature flag",
			workflowID:        "my-workflow",
			features:          map[string]any{"group-concurrency-queue": false},
			expectConcurrency: true,
			expectQueue:       false,
			expectedGroup:     "gh-aw-conclusion-my-workflow",
		},
		{
			name:              "queue can be disabled with string false feature flag",
			workflowID:        "my-workflow",
			features:          map[string]any{"group-concurrency-queue": "false"},
			expectConcurrency: true,
			expectQueue:       false,
			expectedGroup:     "gh-aw-conclusion-my-workflow",
		},
		{
			name:              "empty string feature value keeps queue enabled",
			workflowID:        "my-workflow",
			features:          map[string]any{"group-concurrency-queue": ""},
			expectConcurrency: true,
			expectQueue:       true,
			expectedGroup:     "gh-aw-conclusion-my-workflow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			workflowData := &WorkflowData{
				Name:                        "Test Workflow",
				WorkflowID:                  tt.workflowID,
				ConcurrencyJobDiscriminator: tt.discriminator,
				Features:                    tt.features,
				SafeOutputs: &SafeOutputsConfig{
					MissingTool: &MissingToolConfig{},
				},
			}

			job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
			if err != nil {
				t.Fatalf("Failed to build conclusion job: %v", err)
			}
			if job == nil {
				t.Fatal("Expected conclusion job to be created")
			}

			if tt.expectConcurrency {
				if job.Concurrency == "" {
					t.Error("Expected conclusion job to have a concurrency group, but it was empty")
				}
				if !strings.Contains(job.Concurrency, tt.expectedGroup) {
					t.Errorf("Expected concurrency group to contain %q, got: %s", tt.expectedGroup, job.Concurrency)
				}
				if !strings.Contains(job.Concurrency, "cancel-in-progress: false") {
					t.Errorf("Expected concurrency group to have cancel-in-progress: false, got: %s", job.Concurrency)
				}
				if tt.expectQueue && !strings.Contains(job.Concurrency, "queue: max") {
					t.Errorf("Expected concurrency group to have queue: max, got: %s", job.Concurrency)
				}
				if !tt.expectQueue && strings.Contains(job.Concurrency, "queue: max") {
					t.Errorf("Expected concurrency group to not include queue: max, got: %s", job.Concurrency)
				}
			} else {
				if job.Concurrency != "" {
					t.Errorf("Expected no concurrency group, but got: %s", job.Concurrency)
				}
			}
		})
	}
}

// TestConclusionJobPushRepoMemoryResult verifies that when repo-memory is configured,
// GH_AW_PUSH_REPO_MEMORY_RESULT is passed to the conclusion job so the failure handler
// can report push_repo_memory job-level failures.
func TestConclusionJobPushRepoMemoryResult(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			MissingTool: &MissingToolConfig{},
		},
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default", BranchName: "memory/default"},
			},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	allSteps := strings.Join(job.Steps, "\n")
	if !strings.Contains(allSteps, "GH_AW_PUSH_REPO_MEMORY_RESULT") {
		t.Error("Expected conclusion job to include GH_AW_PUSH_REPO_MEMORY_RESULT env var for push job failure reporting")
	}
	if !strings.Contains(allSteps, "${{ needs.push_repo_memory.result }}") {
		t.Error("Expected GH_AW_PUSH_REPO_MEMORY_RESULT to reference needs.push_repo_memory.result")
	}
}

// TestConclusionJobNoPushRepoMemoryResult verifies that when repo-memory is NOT configured,
// GH_AW_PUSH_REPO_MEMORY_RESULT is not added to the conclusion job (no unnecessary env vars).
func TestConclusionJobNoPushRepoMemoryResult(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			MissingTool: &MissingToolConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	allSteps := strings.Join(job.Steps, "\n")
	if strings.Contains(allSteps, "GH_AW_PUSH_REPO_MEMORY_RESULT") {
		t.Error("Expected conclusion job to NOT include GH_AW_PUSH_REPO_MEMORY_RESULT when repo-memory is not configured")
	}
}

// TestConclusionJobWorkflowCallArtifactPrefix verifies that the conclusion job uses the
// prefixed artifact name in workflow_call context to match the upload step.
func TestConclusionJobWorkflowCallArtifactPrefix(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On:   "workflow_call",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	allSteps := strings.Join(job.Steps, "\n")

	// In workflow_call context, the artifact download must use the prefixed name
	// to match the upload step which uses needs.activation.outputs.artifact_prefix.
	prefixedArtifactName := "${{ needs.activation.outputs.artifact_prefix }}agent"
	if !strings.Contains(allSteps, prefixedArtifactName) {
		t.Errorf("Expected conclusion job download step to use prefixed artifact name %q in workflow_call context, but it was not found.\nGenerated steps:\n%s", prefixedArtifactName, allSteps)
	}
	prefixedUsageArtifactName := "${{ needs.activation.outputs.artifact_prefix }}usage"
	if !strings.Contains(allSteps, prefixedUsageArtifactName) {
		t.Errorf("Expected conclusion job usage artifact upload to use prefixed artifact name %q in workflow_call context, but it was not found.\nGenerated steps:\n%s", prefixedUsageArtifactName, allSteps)
	}

	// Ensure the unprefixed artifact name is not used
	if strings.Contains(allSteps, "name: agent\n") {
		t.Errorf("Expected conclusion job NOT to use unprefixed artifact name 'agent' in workflow_call context.\nGenerated steps:\n%s", allSteps)
	}
}

// TestConclusionJobNonWorkflowCallNoArtifactPrefix verifies that the conclusion job uses
// the unprefixed artifact name for non-workflow_call workflows.
func TestConclusionJobNonWorkflowCallNoArtifactPrefix(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On:   "issues",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	allSteps := strings.Join(job.Steps, "\n")

	// In non-workflow_call context, the artifact download should use the plain (unprefixed) name.
	if strings.Contains(allSteps, "needs.activation.outputs.artifact_prefix") {
		t.Errorf("Expected conclusion job NOT to use artifact_prefix in non-workflow_call context.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "name: usage") {
		t.Errorf("Expected conclusion job to upload unprefixed usage artifact name in non-workflow_call context.\nGenerated steps:\n%s", allSteps)
	}
}

// TestConclusionJobCategoriesFilterQuoting verifies that category names containing
// special characters (particularly single quotes) are safely embedded as double-quoted
// YAML scalars, preventing YAML injection via the GH_AW_FAILURE_CATEGORIES_FILTER and
// GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER env vars.
func TestConclusionJobCategoriesFilterQuoting(t *testing.T) {
	t.Run("included categories with single quote", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				NoOp:                           &NoOpConfig{},
				ReportFailureAsIssue:           templatableBoolPtr("true"),
				ReportFailureAsIssueCategories: []string{"it's-a-category", "normal-category"},
			},
		}

		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("Failed to build conclusion job: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be created")
		}

		jobYAML := strings.Join(job.Steps, "")
		// Must use double-quoted YAML scalar (produced by %q), not single-quoted
		if !strings.Contains(jobYAML, `GH_AW_FAILURE_CATEGORIES_FILTER: "[\"it's-a-category\",\"normal-category\"]"`) {
			t.Errorf("Expected GH_AW_FAILURE_CATEGORIES_FILTER to be a safe double-quoted YAML scalar.\nGenerated YAML:\n%s", jobYAML)
		}
		if strings.Contains(jobYAML, "GH_AW_FAILURE_CATEGORIES_FILTER: '") {
			t.Error("GH_AW_FAILURE_CATEGORIES_FILTER must not use single-quoted YAML scalar (injection risk)")
		}
	})

	t.Run("excluded categories with single quote", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				NoOp:                                   &NoOpConfig{},
				ReportFailureAsIssue:                   templatableBoolPtr("true"),
				ReportFailureAsIssueExcludedCategories: []string{"it's-excluded", "other-excluded"},
			},
		}

		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("Failed to build conclusion job: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be created")
		}

		jobYAML := strings.Join(job.Steps, "")
		// Must use double-quoted YAML scalar (produced by %q), not single-quoted
		if !strings.Contains(jobYAML, `GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER: "[\"it's-excluded\",\"other-excluded\"]"`) {
			t.Errorf("Expected GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER to be a safe double-quoted YAML scalar.\nGenerated YAML:\n%s", jobYAML)
		}
		if strings.Contains(jobYAML, "GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER: '") {
			t.Error("GH_AW_FAILURE_EXCLUDED_CATEGORIES_FILTER must not use single-quoted YAML scalar (injection risk)")
		}
	})
}

func TestConclusionJobReportFailureAsIssueTemplatableExpression(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			NoOp:                           &NoOpConfig{},
			ReportFailureAsIssue:           templatableBoolPtr("${{ inputs.report-failure-as-issue }}"),
			ReportFailureAsIssueCategories: []string{"agent_failure"},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	jobYAML := strings.Join(job.Steps, "")
	if !strings.Contains(jobYAML, "GH_AW_FAILURE_REPORT_AS_ISSUE:") || !strings.Contains(jobYAML, "${{ inputs.report-failure-as-issue }}") {
		t.Errorf("Expected templatable GH_AW_FAILURE_REPORT_AS_ISSUE env var in generated conclusion job YAML.\nGenerated YAML:\n%s", jobYAML)
	}
	if strings.Contains(jobYAML, "GH_AW_FAILURE_CATEGORIES_FILTER:") {
		t.Errorf("Expected category filters to be omitted when report-failure-as-issue is templatable.\nGenerated YAML:\n%s", jobYAML)
	}
}

func TestConclusionJobIncludesUsageArtifactSteps(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On:   "issues",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	allSteps := strings.Join(job.Steps, "\n")

	// Read the shell script that implements the collection logic.
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "collect_usage_artifact_files.sh")
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read collect_usage_artifact_files.sh: %v", err)
	}
	script := string(scriptBytes)
	generatorPath := filepath.Join("..", "..", "actions", "setup", "js", "generate_usage_activity_summary.cjs")
	generatorBytes, err := os.ReadFile(generatorPath)
	if err != nil {
		t.Fatalf("Failed to read generate_usage_activity_summary.cjs: %v", err)
	}
	generator := string(generatorBytes)

	// --- YAML assertions: step names, script invocation, and upload artifact paths ---

	if !strings.Contains(allSteps, "Collect usage artifact files") {
		t.Errorf("Expected conclusion job to collect usage artifact files.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, `collect_usage_artifact_files.sh`) {
		t.Errorf("Expected 'Collect usage artifact files' step to invoke collect_usage_artifact_files.sh.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "Upload usage artifact") {
		t.Errorf("Expected conclusion job to upload usage artifact.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/aw_info.json") {
		t.Errorf("Expected usage artifact to include aw_info.json path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/aw-info.jsonl") {
		t.Errorf("Expected usage artifact to include aw-info.jsonl path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/agent_usage.jsonl") {
		t.Errorf("Expected usage artifact to include agent_usage.jsonl path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/agent_usage.json") {
		t.Errorf("Expected usage artifact to include agent_usage.json path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/detection_usage.jsonl") {
		t.Errorf("Expected usage artifact to include detection_usage.jsonl path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/github_rate_limits.jsonl") {
		t.Errorf("Expected usage artifact to include GitHub API rate limit usage path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/agent/token_usage.jsonl") {
		t.Errorf("Expected usage artifact to include agent token usage path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/detection/token_usage.jsonl") {
		t.Errorf("Expected usage artifact to include detection token usage path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/activity/summary.json") {
		t.Errorf("Expected usage artifact to include activity summary path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "Download Safe Outputs Items Manifest") {
		t.Errorf("Expected conclusion job to download safe outputs items manifest before generating usage summary.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "safe-outputs-items") {
		t.Errorf("Expected safe-outputs-items artifact name to appear in the download step.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "pattern: safe-outputs-items") {
		t.Errorf("Expected safe-outputs-items download to use a best-effort pattern instead of exact name.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "merge-multiple: true") {
		t.Errorf("Expected best-effort artifact downloads to merge matches into the target path.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "id: download-safe-outputs-manifest") {
		t.Errorf("Expected download step to have an id field for observability.\nGenerated steps:\n%s", allSteps)
	}
	if strings.Contains(allSteps, "name: safe-outputs-items") {
		t.Errorf("Expected safe-outputs-items download not to use exact artifact name downloads.\nGenerated steps:\n%s", allSteps)
	}
	// Verify download step appears before collect step so the manifest is available
	// when generate_usage_activity_summary.cjs runs.
	downloadIdx := strings.Index(allSteps, "Download Safe Outputs Items Manifest")
	collectIdx := strings.Index(allSteps, "Collect usage artifact files")
	if downloadIdx == -1 || collectIdx == -1 || downloadIdx >= collectIdx {
		t.Errorf("Expected 'Download Safe Outputs Items Manifest' to appear before 'Collect usage artifact files'.\ndownloadIdx=%d collectIdx=%d\nGenerated steps:\n%s", downloadIdx, collectIdx, allSteps)
	}
	uploadIdx := strings.Index(allSteps, "Upload usage artifact")
	if collectIdx == -1 || uploadIdx == -1 || collectIdx >= uploadIdx || !strings.Contains(allSteps[collectIdx:uploadIdx], "continue-on-error: true") {
		t.Errorf("Expected unavailable working-set data not to fail the conclusion job.\nGenerated steps:\n%s", allSteps)
	}

	// --- Script assertions: verify collection logic inside collect_usage_artifact_files.sh ---

	if !strings.Contains(script, "cp /tmp/gh-aw/aw_info.json /tmp/gh-aw/usage/aw_info.json") {
		t.Errorf("Expected collect script to include aw_info.json copy command.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "cp /tmp/gh-aw/agent_usage.json /tmp/gh-aw/usage/agent_usage.json") {
		t.Errorf("Expected collect script to copy agent_usage.json.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "cp /tmp/gh-aw/agent/graders/grader_manifest.json /tmp/gh-aw/usage/graders/grader_manifest.json") {
		t.Errorf("Expected collect script to copy grader manifest into usage.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "cp /tmp/gh-aw/agent/graders/grader_results.json /tmp/gh-aw/usage/graders/grader_results.json") {
		t.Errorf("Expected collect script to copy grader results into usage.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl") {
		t.Errorf("Expected collect script to include firewall audit token usage path for agent.\nScript:\n%s", script)
	}
	// Verify non-empty check (-s) is used for token-usage copies so empty stub files from
	// AWF's audit dir cannot zero out valid data written by the primary proxy-logs dir.
	if !strings.Contains(script, "[ -s /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl ]") {
		t.Errorf("Expected collect script to use non-empty (-s) check for firewall/logs token-usage copy.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "[ -s /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl ]") {
		t.Errorf("Expected collect script to use non-empty (-s) check for firewall/audit token-usage copy.\nScript:\n%s", script)
	}
	// Verify firewall/logs/ copy appears after firewall/audit/ copy so it wins (last non-empty wins).
	logsIdx := strings.Index(script, "[ -s /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl ]")
	auditIdx := strings.Index(script, "[ -s /tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl ]")
	if logsIdx > 0 && auditIdx > 0 && logsIdx <= auditIdx {
		t.Errorf("Expected firewall/logs token-usage copy to appear AFTER firewall/audit copy (logs = higher priority).\nScript:\n%s", script)
	}
	if !strings.Contains(script, "/tmp/gh-aw/threat-detection/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl") {
		t.Errorf("Expected collect script to include firewall audit token usage path for detection.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "Usage artifact source file status:") {
		t.Errorf("Expected collect script to log source file status for diagnostics.\nScript:\n%s", script)
	}
	if !strings.Contains(script, ": > /tmp/gh-aw/usage/agent/token_usage.jsonl") {
		t.Errorf("Expected collect script to ensure agent token usage file exists.\nScript:\n%s", script)
	}
	if !strings.Contains(script, ": > /tmp/gh-aw/usage/detection/token_usage.jsonl") {
		t.Errorf("Expected collect script to ensure detection token usage file exists.\nScript:\n%s", script)
	}
	if !strings.Contains(script, "generate_usage_activity_summary.cjs") {
		t.Errorf("Expected collect script to generate activity summary aggregates.\nScript:\n%s", script)
	}
	if !strings.Contains(generator, "calculateWorkingSetFromJSONL") || !strings.Contains(generator, "summary.working_set = workingSet") {
		t.Errorf("Expected usage activity generator to calculate and persist working-set rebuild metrics.\nGenerator:\n%s", generator)
	}
	if !strings.Contains(script, `node "${RUNNER_TEMP}/gh-aw/actions/generate_usage_activity_summary.cjs"`) {
		t.Errorf("Expected collect script to use quoted shell-safe RUNNER_TEMP form to prevent word-splitting (SC2086).\nScript:\n%s", script)
	}
	if strings.Contains(script, "node ${RUNNER_TEMP}/gh-aw/actions/generate_usage_activity_summary.cjs") {
		t.Errorf("Collect script must use double-quoted RUNNER_TEMP to prevent word-splitting (SC2086).\nScript:\n%s", script)
	}
	if strings.Contains(script, "node ${{ runner.temp }}/gh-aw/actions/generate_usage_activity_summary.cjs") {
		t.Errorf("Collect script must not inline ${{ runner.temp }} in shell code.\nScript:\n%s", script)
	}
}

func TestConclusionJobIncludesEvalsInUsageArtifact(t *testing.T) {
	compiler := NewCompiler()
	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On:   "issues",
		SafeOutputs: &SafeOutputsConfig{
			NoOp: &NoOpConfig{},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it build?"},
			},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
	if err != nil {
		t.Fatalf("Failed to build conclusion job: %v", err)
	}
	if job == nil {
		t.Fatal("Expected conclusion job to be created")
	}

	allSteps := strings.Join(job.Steps, "\n")
	if !strings.Contains(allSteps, "Download evals artifact") {
		t.Errorf("Expected conclusion job to download the evals artifact.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "id: download-evals-artifact") {
		t.Errorf("Expected evals artifact download step to have an id field.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "pattern: evals") {
		t.Errorf("Expected evals artifact download step to use a best-effort pattern.\nGenerated steps:\n%s", allSteps)
	}
	if strings.Contains(allSteps, "name: evals") {
		t.Errorf("Expected evals artifact download step not to use exact artifact name downloads.\nGenerated steps:\n%s", allSteps)
	}

	// The evals copy command lives in the shared script file.
	scriptPath := filepath.Join("..", "..", "actions", "setup", "sh", "collect_usage_artifact_files.sh")
	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read collect_usage_artifact_files.sh: %v", err)
	}
	if !strings.Contains(string(scriptBytes), "cp /tmp/gh-aw/evals/evals.jsonl /tmp/gh-aw/usage/evals.jsonl") {
		t.Errorf("Expected collect script to copy evals results.\nScript:\n%s", string(scriptBytes))
	}

	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/evals.jsonl") {
		t.Errorf("Expected usage artifact upload to include evals results.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/graders/grader_manifest.json") {
		t.Errorf("Expected usage artifact upload to include grader manifest.\nGenerated steps:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "/tmp/gh-aw/usage/graders/grader_results.json") {
		t.Errorf("Expected usage artifact upload to include grader results.\nGenerated steps:\n%s", allSteps)
	}
}

func TestUsageArtifactDownloadsUseExactNamesWithDownloadArtifactV3(t *testing.T) {
	steps := strings.Join(buildUsageArtifactUploadSteps("", true, func(string) string {
		return "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0"
	}), "")

	for _, artifactName := range []string{constants.SafeOutputItemsArtifactName.String(), constants.EvalsArtifactName.String()} {
		if !strings.Contains(steps, "name: "+artifactName) {
			t.Errorf("Expected download-artifact v3 usage download to use exact name for %q.\nGenerated steps:\n%s", artifactName, steps)
		}
		if strings.Contains(steps, "pattern: "+artifactName) {
			t.Errorf("Expected download-artifact v3 usage download not to use pattern for %q.\nGenerated steps:\n%s", artifactName, steps)
		}
	}
	if strings.Contains(steps, "merge-multiple: true") {
		t.Errorf("Expected download-artifact v3 usage downloads not to use merge-multiple.\nGenerated steps:\n%s", steps)
	}
}

// TestConclusionJobNeedsPreActivationFromMessages tests that pre_activation is automatically
// added to the conclusion job's needs when any message template references
// needs.pre_activation.outputs.*, and that it is not duplicated.
func TestConclusionJobNeedsPreActivationFromMessages(t *testing.T) {
	tests := []struct {
		name              string
		messages          *SafeOutputMessagesConfig
		registerPreAct    bool
		expectPreActNeeds bool
	}{
		{
			name: "adds pre_activation when Footer references its outputs",
			messages: &SafeOutputMessagesConfig{
				Footer: "Skill: ${{ needs.pre_activation.outputs.skill_name }}",
			},
			registerPreAct:    true,
			expectPreActNeeds: true,
		},
		{
			name: "adds pre_activation when RunFailure references its outputs",
			messages: &SafeOutputMessagesConfig{
				RunFailure: "Failed in ${{ needs.pre_activation.outputs.skill_name }}",
			},
			registerPreAct:    true,
			expectPreActNeeds: true,
		},
		{
			name: "does not add pre_activation when messages have no reference",
			messages: &SafeOutputMessagesConfig{
				Footer: "No expression here",
			},
			registerPreAct:    true,
			expectPreActNeeds: false,
		},
		{
			name: "does not add pre_activation when job is not registered",
			messages: &SafeOutputMessagesConfig{
				Footer: "Skill: ${{ needs.pre_activation.outputs.skill_name }}",
			},
			registerPreAct:    false,
			expectPreActNeeds: false,
		},
		{
			name:              "does not add pre_activation when messages is nil",
			messages:          nil,
			registerPreAct:    true,
			expectPreActNeeds: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			if tt.registerPreAct {
				preActJob := &Job{Name: string(constants.PreActivationJobName)}
				if err := compiler.jobManager.AddJob(preActJob); err != nil {
					t.Fatalf("Failed to register pre_activation job: %v", err)
				}
			}

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					AddComments: &AddCommentsConfig{
						BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
					},
					Messages: tt.messages,
				},
			}

			job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
			if err != nil {
				t.Fatalf("buildConclusionJob returned error: %v", err)
			}
			if job == nil {
				t.Fatal("Expected conclusion job to be non-nil")
			}

			preActName := string(constants.PreActivationJobName)
			if tt.expectPreActNeeds {
				if !slices.Contains(job.Needs, preActName) {
					t.Errorf("Expected pre_activation in conclusion job needs, got: %v", job.Needs)
				}
				// Ensure no duplicate
				count := 0
				for _, n := range job.Needs {
					if n == preActName {
						count++
					}
				}
				if count != 1 {
					t.Errorf("pre_activation appears %d times in conclusion job needs (expected 1): %v", count, job.Needs)
				}
			} else {
				if slices.Contains(job.Needs, preActName) {
					t.Errorf("Did not expect pre_activation in conclusion job needs, got: %v", job.Needs)
				}
			}
		})
	}
}

// TestConclusionJobActionsWritePermissionForDailyAICCache verifies that the conclusion job
// adds actions: write only when daily-AIC cache steps are included and the job would
// otherwise have no writable scope. Existing writable scopes (for example issues: write
// from add-comments) should be reused instead of broadening permissions.
func TestConclusionJobActionsWritePermissionForDailyAICCache(t *testing.T) {
	compiler := NewCompiler()

	t.Run("has actions: write when WorkflowID set and no other writable scope exists", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:        "Test Workflow",
			WorkflowID:  "my-workflow",
			SafeOutputs: &SafeOutputsConfig{},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if !strings.Contains(job.Permissions, "actions: write") {
			t.Errorf("conclusion job must have 'actions: write' when daily-AIC cache is active, got: %q", job.Permissions)
		}
	})

	t.Run("no actions: write when another writable scope already exists", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:       "Test Workflow",
			WorkflowID: "my-workflow",
			SafeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if strings.Contains(job.Permissions, "actions: write") {
			t.Errorf("conclusion job should reuse existing writable scopes instead of adding 'actions: write', got: %q", job.Permissions)
		}
		if !strings.Contains(job.Permissions, "issues: write") {
			t.Errorf("conclusion job should preserve existing 'issues: write' permission, got: %q", job.Permissions)
		}
	})

	t.Run("no actions: write when WorkflowID is empty", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:       "Test Workflow",
			WorkflowID: "", // no WorkflowID → no daily-AIC cache steps
			SafeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if strings.Contains(job.Permissions, "actions: write") {
			t.Errorf("conclusion job should NOT have 'actions: write' when WorkflowID is empty, got: %q", job.Permissions)
		}
	})

	t.Run("no actions: write when guardrail is explicitly disabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:           "Test Workflow",
			WorkflowID:     "my-workflow",
			RawFrontmatter: disableAICGuardrailFrontmatter(),
			SafeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if strings.Contains(job.Permissions, "actions: write") {
			t.Errorf("conclusion job should NOT have 'actions: write' when the daily-AIC guardrail is disabled, got: %q", job.Permissions)
		}
	})
}

// TestConclusionJobIssuesWritePermissionDerivedFromConfig verifies that the conclusion job's
// issues: write (and actions: read) permissions are derived from the resolved safe-outputs
// configuration rather than emitted unconditionally. See github/gh-aw#54548.
func TestConclusionJobIssuesWritePermissionDerivedFromConfig(t *testing.T) {
	falseVal := "false"
	falseTemplatable := TemplatableBool("false")

	t.Run("no issues: write or actions: read when all conclusion issue-writing paths are disabled", func(t *testing.T) {
		compiler := NewCompiler()
		falseReportFailedJobs := false
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				ReportFailedJobs:     &falseReportFailedJobs,
				ReportFailureAsIssue: &falseTemplatable,
				MissingTool: &MissingToolConfig{
					CreateIssue: &falseVal,
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if strings.Contains(job.Permissions, "issues: write") {
			t.Errorf("conclusion job should NOT have 'issues: write' when every issue-creating mechanism is disabled, got: %q", job.Permissions)
		}
		if strings.Contains(job.Permissions, "actions: read") {
			t.Errorf("conclusion job should NOT have 'actions: read' when report-failed-jobs is disabled, got: %q", job.Permissions)
		}
	})

	t.Run("no issues: write when default threat detection is enabled but issue-writing paths are disabled", func(t *testing.T) {
		compiler := NewCompiler()
		falseReportFailedJobs := false
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				UpdateRelease:        &UpdateReleaseConfig{},
				ReportFailedJobs:     &falseReportFailedJobs,
				ReportFailureAsIssue: &falseTemplatable,
				MissingTool: &MissingToolConfig{
					CreateIssue: &falseVal,
				},
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if strings.Contains(job.Permissions, "issues: write") {
			t.Errorf("conclusion job should NOT have 'issues: write' when only threat detection is enabled, got: %q", job.Permissions)
		}
		if strings.Contains(job.Permissions, "actions: read") {
			t.Errorf("conclusion job should NOT have 'actions: read' when report-failed-jobs is disabled, got: %q", job.Permissions)
		}
		if !strings.Contains(job.Permissions, "contents: write") {
			t.Errorf("conclusion job should preserve update-release permissions, got: %q", job.Permissions)
		}
	})

	t.Run("issues: write present when report-failed-jobs remains enabled by default", func(t *testing.T) {
		compiler := NewCompiler()
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				ReportFailureAsIssue: &falseTemplatable,
				MissingTool: &MissingToolConfig{
					CreateIssue: &falseVal,
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if !strings.Contains(job.Permissions, "issues: write") {
			t.Errorf("conclusion job should have 'issues: write' when report-failed-jobs defaults to enabled, got: %q", job.Permissions)
		}
		if !strings.Contains(job.Permissions, "actions: read") {
			t.Errorf("conclusion job should have 'actions: read' when report-failed-jobs defaults to enabled, got: %q", job.Permissions)
		}
	})

	t.Run("issues: read from safe-outputs is upgraded to issues: write when a conclusion issue path is enabled", func(t *testing.T) {
		compiler := NewCompiler()
		falseReportFailedJobs := false
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				ReportFailedJobs: &falseReportFailedJobs,
				CreateProjects: &CreateProjectsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if !strings.Contains(job.Permissions, "issues: write") {
			t.Errorf("conclusion job should upgrade from 'issues: read' to 'issues: write' when report-failure-as-issue is enabled, got: %q", job.Permissions)
		}
	})

	t.Run("issues: write present when missing-tool issue reporting is enabled", func(t *testing.T) {
		compiler := NewCompiler()
		falseReportFailedJobs := false
		trueVal := "true"
		workflowData := &WorkflowData{
			Name: "Test Workflow",
			SafeOutputs: &SafeOutputsConfig{
				ReportFailedJobs:     &falseReportFailedJobs,
				ReportFailureAsIssue: &falseTemplatable,
				MissingTool: &MissingToolConfig{
					CreateIssue: &trueVal,
				},
			},
		}
		job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), []string{})
		if err != nil {
			t.Fatalf("buildConclusionJob returned error: %v", err)
		}
		if job == nil {
			t.Fatal("Expected conclusion job to be non-nil")
		}
		if !strings.Contains(job.Permissions, "issues: write") {
			t.Errorf("conclusion job should have 'issues: write' when missing-tool issue reporting is enabled, got: %q", job.Permissions)
		}
	})
}

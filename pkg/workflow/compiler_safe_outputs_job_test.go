//go:build !integration

package workflow

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildConsolidatedSafeOutputsJob tests the main job builder function
func TestBuildConsolidatedSafeOutputsJob(t *testing.T) {
	tests := []struct {
		name             string
		safeOutputs      *SafeOutputsConfig
		threatDetection  bool
		expectedJobName  string
		expectedSteps    int
		expectNil        bool
		checkPermissions bool
		expectedPerms    []string
	}{
		{
			name:          "no safe outputs configured",
			safeOutputs:   nil,
			expectNil:     true,
			expectedSteps: 0,
		},
		{
			name: "create issues only",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Test] ",
					Labels:      []string{"test"},
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"issues: write"},
		},
		{
			name: "add comments only",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: strPtr("5"),
					},
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"issues: write", "pull-requests: write"},
		},
		{
			name: "set issue field only",
			safeOutputs: &SafeOutputsConfig{
				SetIssueField: &SetIssueFieldConfig{},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"issues: write"},
		},
		{
			name: "create pull requests with patch",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					TitlePrefix: "[Test] ",
					Labels:      []string{"test"},
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"contents: write", "issues: write", "pull-requests: write"},
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
						Allowed: []string{"bug", "enhancement"},
					},
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"issues: write", "pull-requests: write"},
		},
		{
			name: "with threat detection enabled",
			safeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Test] ",
				},
			},
			threatDetection:  true,
			expectedJobName:  "safe_outputs",
			checkPermissions: false,
		},
		{
			name: "with GitHub App token",
			safeOutputs: &SafeOutputsConfig{
				GitHubApp: &GitHubAppConfig{
					AppID:      "12345",
					PrivateKey: "test-key",
				},
				CreateIssues: &CreateIssuesConfig{
					TitlePrefix: "[Test] ",
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			job, stepNames, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test-workflow.md")

			if tt.expectNil {
				assert.Nil(t, job)
				assert.Nil(t, stepNames)
				assert.NoError(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, job)
			assert.Equal(t, tt.expectedJobName, job.Name)
			assert.NotEmpty(t, job.Steps)
			assert.NotEmpty(t, job.Env)

			// Check job dependencies — safe_outputs depends on agent; when detection enabled, also depends on detection
			assert.Contains(t, job.Needs, string(constants.AgentJobName))
			if tt.threatDetection {
				assert.Contains(t, job.Needs, string(constants.DetectionJobName), "safe_outputs should depend on detection job when threat detection is enabled")
			}

			// Check permissions if specified
			if tt.checkPermissions {
				jobYAML := job.Permissions
				for _, perm := range tt.expectedPerms {
					assert.Contains(t, jobYAML, perm, "Expected permission: "+perm)
				}
			}

			// Verify timeout is set
			assert.Equal(t, 45, job.TimeoutMinutes)

			// Verify job condition is set
			assert.NotEmpty(t, job.If)
		})
	}
}

// TestBuildConsolidatedSafeOutputsJobConcurrencyGroup tests that the concurrency-group field
// is correctly applied to the safe_outputs job
func TestBuildConsolidatedSafeOutputsJobConcurrencyGroup(t *testing.T) {
	tests := []struct {
		name              string
		concurrencyGroup  string
		expectConcurrency bool
	}{
		{
			name:              "no concurrency group",
			concurrencyGroup:  "",
			expectConcurrency: false,
		},
		{
			name:              "simple concurrency group",
			concurrencyGroup:  "my-safe-outputs",
			expectConcurrency: true,
		},
		{
			name:              "concurrency group with expression",
			concurrencyGroup:  "safe-outputs-${{ github.repository }}",
			expectConcurrency: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues:     &CreateIssuesConfig{TitlePrefix: "[Test] "},
					ConcurrencyGroup: tt.concurrencyGroup,
				},
			}

			job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test-workflow.md")
			require.NoError(t, err, "Should build job without error")
			require.NotNil(t, job, "Job should not be nil")

			if tt.expectConcurrency {
				assert.NotEmpty(t, job.Concurrency, "Job should have concurrency set")
				assert.Contains(t, job.Concurrency, tt.concurrencyGroup, "Concurrency should contain the group value")
				assert.Contains(t, job.Concurrency, "cancel-in-progress: false", "Concurrency should have cancel-in-progress: false")
			} else {
				assert.Empty(t, job.Concurrency, "Job should have no concurrency set")
			}
		})
	}
}

func TestAddHandlerManagerOutputs(t *testing.T) {
	data := &WorkflowData{
		SafeOutputs: &SafeOutputsConfig{
			AssignToAgent:       &AssignToAgentConfig{},
			CreateAgentSessions: &CreateAgentSessionConfig{},
			UploadArtifact:      &UploadArtifactConfig{MaxUploads: 2},
			CreateIssues:        &CreateIssuesConfig{},
			CreatePullRequests:  &CreatePullRequestsConfig{},
			AddComments:         &AddCommentsConfig{},
		},
	}
	outputs := make(map[string]string)

	addHandlerManagerOutputs(data, outputs)
	addNamedSafeOutputHandlerOutputs(data, outputs)

	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.processed_count }}", outputs["process_safe_outputs_processed_count"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_applied }}", outputs["process_safe_outputs_items_applied"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_skipped }}", outputs["process_safe_outputs_items_skipped"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_warnings }}", outputs["process_safe_outputs_items_warnings"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_cancelled }}", outputs["process_safe_outputs_items_cancelled"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_deferred }}", outputs["process_safe_outputs_items_deferred"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.assign_to_agent_assigned }}", outputs["assign_to_agent_assigned"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.assign_to_agent_assignment_errors }}", outputs["assign_to_agent_assignment_errors"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.assign_to_agent_assignment_error_count }}", outputs["assign_to_agent_assignment_error_count"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.session_number }}", outputs["create_agent_session_session_number"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.session_url }}", outputs["create_agent_session_session_url"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.upload_artifact_count }}", outputs["upload_artifact_count"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.slot_0_tmp_id }}", outputs["upload_artifact_slot_0_tmp_id"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.slot_1_tmp_id }}", outputs["upload_artifact_slot_1_tmp_id"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.created_issue_number }}", outputs["created_issue_number"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.created_pr_url }}", outputs["created_pr_url"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.comment_id }}", outputs["comment_id"])
}

func TestBuildConsolidatedSafeOutputsJobNeedsIncludesConfiguredDependencies(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name:         "Test Workflow",
		LockForAgent: true,
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
			ThreatDetection: &ThreatDetectionConfig{},
			Needs:           []string{"secrets_fetcher", "vault_bootstrap"},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test-workflow.md")
	require.NoError(t, err, "Should build job without error")
	require.NotNil(t, job, "Job should not be nil")

	assert.Contains(t, job.Needs, string(constants.AgentJobName), "safe_outputs should depend on agent job")
	assert.Contains(t, job.Needs, string(constants.ActivationJobName), "safe_outputs should depend on activation job")
	assert.Contains(t, job.Needs, string(constants.DetectionJobName), "safe_outputs should depend on detection job when enabled")
	assert.Contains(t, job.Needs, "unlock", "safe_outputs should depend on unlock when lock-for-agent is enabled")
	assert.Contains(t, job.Needs, "secrets_fetcher", "safe_outputs should include explicit custom dependency")
	assert.Contains(t, job.Needs, "vault_bootstrap", "safe_outputs should include extra custom dependency")

	secretsFetcherCount := 0
	for _, need := range job.Needs {
		if need == "secrets_fetcher" {
			secretsFetcherCount++
		}
	}
	assert.Equal(t, 1, secretsFetcherCount, "duplicate configured dependencies should be deduplicated")
}

// TestBuildConsolidatedSafeOutputsJobNeedsPreActivationFromMessages tests that
// pre_activation is automatically added to the safe_outputs job's needs when any
// message template references needs.pre_activation.outputs.*.
func TestBuildConsolidatedSafeOutputsJobNeedsPreActivationFromMessages(t *testing.T) {
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
			name: "adds pre_activation when RunSuccess references its outputs",
			messages: &SafeOutputMessagesConfig{
				RunSuccess: "Done — skill: ${{ needs.pre_activation.outputs.skill_name }}",
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
			compiler.jobManager = NewJobManager()

			if tt.registerPreAct {
				preActJob := &Job{Name: string(constants.PreActivationJobName)}
				require.NoError(t, compiler.jobManager.AddJob(preActJob))
			}

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues: &CreateIssuesConfig{TitlePrefix: "[Test] "},
					Messages:     tt.messages,
				},
			}

			job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")
			require.NoError(t, err)
			require.NotNil(t, job)

			preActName := string(constants.PreActivationJobName)
			if tt.expectPreActNeeds {
				assert.Contains(t, job.Needs, preActName, "safe_outputs job should depend on pre_activation when messages reference its outputs")
				// Ensure no duplicate
				count := 0
				for _, n := range job.Needs {
					if n == preActName {
						count++
					}
				}
				assert.Equal(t, 1, count, "pre_activation should appear exactly once in needs")
			} else {
				assert.NotContains(t, job.Needs, preActName, "safe_outputs job should not depend on pre_activation when messages don't reference its outputs")
			}
		})
	}
}

// TestBuildConsolidatedSafeOutputsJobTimeoutMinutes tests that the timeout-minutes field
// is correctly applied to the safe_outputs job, with 45 minutes as the default.
func TestBuildConsolidatedSafeOutputsJobTimeoutMinutes(t *testing.T) {
	tests := []struct {
		name            string
		timeoutMinutes  int
		expectedTimeout int
	}{
		{
			name:            "default timeout (0 = unset) falls back to 45 minutes",
			timeoutMinutes:  0,
			expectedTimeout: 45,
		},
		{
			name:            "custom timeout of 30 minutes",
			timeoutMinutes:  30,
			expectedTimeout: 30,
		},
		{
			name:            "custom timeout of 120 minutes",
			timeoutMinutes:  120,
			expectedTimeout: 120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					CreateIssues:   &CreateIssuesConfig{TitlePrefix: "[Test] "},
					TimeoutMinutes: tt.timeoutMinutes,
				},
			}

			job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test-workflow.md")
			require.NoError(t, err, "Should build job without error")
			require.NotNil(t, job, "Job should not be nil")

			assert.Equal(t, tt.expectedTimeout, job.TimeoutMinutes, "Job timeout should match expected value")
		})
	}
}

func TestCompileSafeOutputsNeedsForCustomCredentialJob(t *testing.T) {
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "safe-needs.md")
	workflowContent := `---
on:
  workflow_dispatch: {}
permissions:
  contents: read
safe-outputs:
  needs: [secrets_fetcher]
  github-app:
    app-id: ${{ needs.secrets_fetcher.outputs.app_id }}
    private-key: ${{ needs.secrets_fetcher.outputs.app_private_key }}
  noop:
    report-as-issue: false
jobs:
  secrets_fetcher:
    runs-on: ubuntu-latest
    outputs:
      app_id: ${{ steps.fetch.outputs.app_id }}
      app_private_key: ${{ steps.fetch.outputs.app_private_key }}
    steps:
      - id: fetch
        run: |
          echo "app_id=placeholder" >> "$GITHUB_OUTPUT"
          echo "app_private_key=placeholder" >> "$GITHUB_OUTPUT"
---

# Safe outputs needs test
`
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0644), "workflow should be written")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(workflowPath), "workflow should compile")

	lockPath := stringutil.MarkdownToLockFile(workflowPath)
	lockContent, err := os.ReadFile(lockPath)
	require.NoError(t, err, "compiled lock file should exist")

	var workflow map[string]any
	require.NoError(t, yaml.Unmarshal(lockContent, &workflow), "lock YAML should parse")

	jobs, ok := workflow["jobs"].(map[string]any)
	require.True(t, ok, "jobs should be present")
	safeOutputsJob, ok := jobs["safe_outputs"].(map[string]any)
	require.True(t, ok, "safe_outputs job should be present")

	needs, ok := safeOutputsJob["needs"].([]any)
	require.True(t, ok, "safe_outputs job should include needs list")

	needsValues := make([]string, 0, len(needs))
	for _, need := range needs {
		if needStr, ok := need.(string); ok {
			needsValues = append(needsValues, needStr)
		}
	}

	assert.Contains(t, needsValues, string(constants.AgentJobName), "safe_outputs should keep built-in dependency on agent")
	assert.Contains(t, needsValues, string(constants.ActivationJobName), "safe_outputs should keep built-in dependency on activation")
	assert.Contains(t, needsValues, "secrets_fetcher", "safe_outputs should include custom credential supplier job")
	assert.Contains(t, string(lockContent), "needs.secrets_fetcher.outputs.app_id", "compiled workflow should retain custom needs output references")
}

func TestCompileSafeOutputsTimeoutMinutesFromFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		frontmatter     string
		expectedTimeout string
	}{
		{
			name: "default timeout is 45 minutes",
			frontmatter: `---
on:
  workflow_dispatch: {}
permissions:
  contents: read
safe-outputs:
  noop:
    report-as-issue: false
---
# Test
`,
			expectedTimeout: "timeout-minutes: 45",
		},
		{
			name: "custom timeout of 30 minutes from frontmatter",
			frontmatter: `---
on:
  workflow_dispatch: {}
permissions:
  contents: read
safe-outputs:
  timeout-minutes: 30
  noop:
    report-as-issue: false
---
# Test
`,
			expectedTimeout: "timeout-minutes: 30",
		},
		{
			name: "custom timeout of 120 minutes from frontmatter",
			frontmatter: `---
on:
  workflow_dispatch: {}
permissions:
  contents: read
safe-outputs:
  timeout-minutes: 120
  noop:
    report-as-issue: false
---
# Test
`,
			expectedTimeout: "timeout-minutes: 120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			workflowPath := filepath.Join(tempDir, "safe-timeout.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(tt.frontmatter), 0644))

			compiler := NewCompiler()
			require.NoError(t, compiler.CompileWorkflow(workflowPath))

			lockPath := stringutil.MarkdownToLockFile(workflowPath)
			lockContent, err := os.ReadFile(lockPath)
			require.NoError(t, err)

			assert.Contains(t, string(lockContent), tt.expectedTimeout,
				"safe_outputs job should have the expected timeout-minutes value")
		})
	}
}

func TestBuildJobLevelSafeOutputEnvVars(t *testing.T) {
	tests := []struct {
		name          string
		workflowData  *WorkflowData
		workflowID    string
		trialMode     bool
		trialRepo     string
		expectedVars  map[string]string
		checkContains bool
	}{
		{
			name: "basic env vars",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
			},
			workflowID: "test-workflow",
			expectedVars: map[string]string{
				"GH_AW_WORKFLOW_ID":        `"test-workflow"`,
				"GH_AW_WORKFLOW_NAME":      `"Test Workflow"`,
				"GH_AW_CALLER_WORKFLOW_ID": `"${{ github.repository }}/test-workflow"`,
			},
			checkContains: true,
		},
		{
			name: "with source metadata",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				Source:      "user/repo",
				SafeOutputs: &SafeOutputsConfig{},
			},
			workflowID: "test-workflow",
			expectedVars: map[string]string{
				"GH_AW_WORKFLOW_SOURCE": `"user/repo"`,
			},
			checkContains: true,
		},
		{
			name: "with tracker ID",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				TrackerID:   "tracker-123",
				SafeOutputs: &SafeOutputsConfig{},
			},
			workflowID: "test-workflow",
			expectedVars: map[string]string{
				"GH_AW_TRACKER_ID": `"tracker-123"`,
			},
			checkContains: true,
		},
		{
			name: "with engine config",
			workflowData: &WorkflowData{
				Name:  "Test Workflow",
				Model: "gpt-4",
				EngineConfig: &EngineConfig{
					ID:      "copilot",
					Version: "0.0.375",
				},
				SafeOutputs: &SafeOutputsConfig{},
			},
			workflowID: "test-workflow",
			expectedVars: map[string]string{
				"GH_AW_ENGINE_ID":      `"copilot"`,
				"GH_AW_ENGINE_VERSION": `"0.0.375"`,
				"GH_AW_ENGINE_MODEL":   `"gpt-4"`,
			},
			checkContains: true,
		},
		{
			name: "staged mode",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					Staged: templatableBoolPtr("true"),
				},
			},
			workflowID: "test-workflow",
			expectedVars: map[string]string{
				"GH_AW_SAFE_OUTPUTS_STAGED": `"true"`,
			},
			checkContains: true,
		},
		{
			name: "trial mode with target repo",
			workflowData: &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{},
			},
			workflowID: "test-workflow",
			trialMode:  true,
			trialRepo:  "org/test-repo",
			expectedVars: map[string]string{
				"GH_AW_TARGET_REPO_SLUG": `"org/test-repo"`,
			},
			checkContains: true,
		},
		{
			name: "with messages config",
			workflowData: &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					Messages: &SafeOutputMessagesConfig{
						Footer: "Custom footer",
					},
				},
			},
			workflowID:    "test-workflow",
			checkContains: true,
		},
		{
			name: "with slash command and placeholder",
			workflowData: &WorkflowData{
				Name:               "Test Workflow",
				Command:            []string{"review-bot"},
				LabelCommand:       []string{"deploy"},
				CommandPlaceholder: "to review this PR",
				SafeOutputs:        &SafeOutputsConfig{},
			},
			workflowID: "test-workflow",
			expectedVars: map[string]string{
				"GH_AW_COMMANDS":            `"[\"review-bot\"]"`,
				"GH_AW_COMMAND_PLACEHOLDER": `"to review this PR"`,
				"GH_AW_LABEL_COMMANDS":      `"[\"deploy\"]"`,
			},
			checkContains: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			if tt.trialMode {
				compiler.SetTrialMode(true)
			}
			if tt.trialRepo != "" {
				compiler.SetTrialLogicalRepoSlug(tt.trialRepo)
			}

			envVars := compiler.buildJobLevelSafeOutputEnvVars(tt.workflowData, tt.workflowID)

			require.NotNil(t, envVars)

			if tt.checkContains {
				for key, expectedValue := range tt.expectedVars {
					actualValue, exists := envVars[key]
					assert.True(t, exists, "Expected env var %s to exist", key)
					if exists {
						assert.Equal(t, expectedValue, actualValue, "Env var %s has incorrect value", key)
					}
				}
			}
		})
	}
}

func TestBuildJobLevelSafeOutputEnvVarsIncludesCompiledProjectUTC(t *testing.T) {
	compiler := NewCompiler()
	compiler.repoConfigLoaded = true
	compiler.repoConfig = &RepoConfig{UTC: "-07:00"}

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}
	envVars := compiler.buildJobLevelSafeOutputEnvVars(workflowData, "test-workflow")

	require.NotNil(t, envVars)
	assert.Equal(t, `"-07:00"`, envVars["GH_AW_PROJECT_UTC"])
}

// TestBuildDetectionSuccessCondition tests the detection condition builder
func TestBuildDetectionSuccessCondition(t *testing.T) {
	condition := buildDetectionSuccessCondition()

	require.NotNil(t, condition)

	rendered := condition.Render()

	// Should check detection job's result (not output variable)
	// The detection job fails (exit 1) when threats are found, so downstream jobs
	// check needs.detection.result == 'success' rather than output variables.
	assert.Contains(t, rendered, "needs."+string(constants.DetectionJobName))
	assert.Contains(t, rendered, ".result")
	assert.Contains(t, rendered, "'success'")
}

// TestJobConditionWithThreatDetection tests job condition building with threat detection
func TestJobConditionWithThreatDetection(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	// Job condition should include detection check referencing detection job result
	assert.Contains(t, job.If, "needs."+string(constants.DetectionJobName))
	assert.Contains(t, job.If, ".result")
	assert.Contains(t, job.If, "'success'")

	// Job should depend on detection job (detection is in a separate job)
	assert.Contains(t, job.Needs, string(constants.DetectionJobName), "safe_outputs job should depend on detection job when threat detection enabled")
}

// TestJobWithGitHubApp tests job building with GitHub App configuration
func TestJobWithGitHubApp(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "12345",
				PrivateKey: "test-key",
			},
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	stepsContent := strings.Join(job.Steps, "")

	// Should include app token minting step
	assert.Contains(t, stepsContent, "Generate GitHub App token")

	// Explicit app token invalidation step should not be generated.
	// actions/create-github-app-token handles revocation in its post step.
	assert.NotContains(t, stepsContent, "Invalidate GitHub App token")
}

// TestAssignToAgentWithGitHubAppUsesAgentToken tests that when github-app: is configured,
// assign-to-agent uses the agent token (GH_AW_ASSIGN_TO_AGENT_TOKEN in the handler manager step)
// rather than the App installation token. The Copilot assignment API only accepts PATs.
func TestAssignToAgentWithGitHubAppUsesAgentToken(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "12345",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			AssignToAgent: &AssignToAgentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	stepsContent := strings.Join(job.Steps, "")

	// App token minting step should be present (github-app: is configured)
	assert.Contains(t, stepsContent, "Generate GitHub App token", "App token minting step should be present")

	// assign_to_agent is now handled in the process_safe_outputs step
	assert.NotContains(t, stepsContent, "id: assign_to_agent",
		"assign_to_agent should not have a dedicated step — it is handled in the handler manager")

	// Find the process_safe_outputs step section
	processSafeOutputsStart := strings.Index(stepsContent, "id: process_safe_outputs")
	require.Greater(t, processSafeOutputsStart, -1, "process_safe_outputs step should exist")

	nextStepOffset := strings.Index(stepsContent[processSafeOutputsStart:], "\n      - ")
	var processSafeOutputsSection string
	if nextStepOffset == -1 {
		processSafeOutputsSection = stepsContent[processSafeOutputsStart:]
	} else {
		processSafeOutputsSection = stepsContent[processSafeOutputsStart : processSafeOutputsStart+nextStepOffset]
	}

	// The process_safe_outputs step should have GH_AW_ASSIGN_TO_AGENT_TOKEN using the agent token chain
	assert.Contains(t, processSafeOutputsSection, "GH_AW_ASSIGN_TO_AGENT_TOKEN",
		"process_safe_outputs step should set GH_AW_ASSIGN_TO_AGENT_TOKEN for the assign-to-agent handler")
	assert.Contains(t, processSafeOutputsSection, "GH_AW_AGENT_TOKEN",
		"GH_AW_ASSIGN_TO_AGENT_TOKEN should use the agent token chain (GH_AW_AGENT_TOKEN)")
	// Verify GH_AW_ASSIGN_TO_AGENT_TOKEN value specifically does not use the App token
	// (only the step-level github-token uses the App token, which is expected)
	tokenLineStart := strings.Index(processSafeOutputsSection, "GH_AW_ASSIGN_TO_AGENT_TOKEN:")
	require.Greater(t, tokenLineStart, -1, "GH_AW_ASSIGN_TO_AGENT_TOKEN line should exist")
	tokenLineEnd := strings.Index(processSafeOutputsSection[tokenLineStart:], "\n")
	if tokenLineEnd == -1 {
		tokenLineEnd = len(processSafeOutputsSection) - tokenLineStart
	}
	tokenLine := processSafeOutputsSection[tokenLineStart : tokenLineStart+tokenLineEnd]
	assert.NotContains(t, tokenLine, "safe-outputs-app-token.outputs.token",
		"GH_AW_ASSIGN_TO_AGENT_TOKEN value should not use the GitHub App token")
}

// TestAssignToAgentWithGitHubAppAndExplicitToken tests that an explicit github-token
// on assign-to-agent takes precedence over both the App token and GH_AW_AGENT_TOKEN.
func TestAssignToAgentWithGitHubAppAndExplicitToken(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "12345",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			AssignToAgent: &AssignToAgentConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max:         strPtr("1"),
					GitHubToken: "${{ secrets.MY_CUSTOM_TOKEN }}",
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	stepsContent := strings.Join(job.Steps, "")

	// assign_to_agent is now handled in the process_safe_outputs step
	assert.NotContains(t, stepsContent, "id: assign_to_agent",
		"assign_to_agent should not have a dedicated step — it is handled in the handler manager")

	// Find the process_safe_outputs step section
	processSafeOutputsStart := strings.Index(stepsContent, "id: process_safe_outputs")
	require.Greater(t, processSafeOutputsStart, -1, "process_safe_outputs step should exist")

	nextStepOffset := strings.Index(stepsContent[processSafeOutputsStart:], "\n      - ")
	var processSafeOutputsSection string
	if nextStepOffset == -1 {
		processSafeOutputsSection = stepsContent[processSafeOutputsStart:]
	} else {
		processSafeOutputsSection = stepsContent[processSafeOutputsStart : processSafeOutputsStart+nextStepOffset]
	}

	// GH_AW_ASSIGN_TO_AGENT_TOKEN should use the explicit token, not the agent chain or App token
	assert.Contains(t, processSafeOutputsSection, "GH_AW_ASSIGN_TO_AGENT_TOKEN",
		"process_safe_outputs step should set GH_AW_ASSIGN_TO_AGENT_TOKEN for the assign-to-agent handler")
	assert.Contains(t, processSafeOutputsSection, "secrets.MY_CUSTOM_TOKEN",
		"GH_AW_ASSIGN_TO_AGENT_TOKEN should use the explicitly configured github-token")
	assert.NotContains(t, processSafeOutputsSection, "GH_AW_AGENT_TOKEN",
		"GH_AW_ASSIGN_TO_AGENT_TOKEN should not use the default agent chain when an explicit token is set")
}

// TestJobOutputs tests that job outputs are correctly configured
func TestJobOutputs(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err)
	require.NotNil(t, job)

	// Handler manager outputs
	assert.NotContains(t, job.Outputs, "process_safe_outputs_temporary_id_map")
	assert.Contains(t, job.Outputs, "process_safe_outputs_processed_count")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_succeeded")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_applied")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_skipped")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_warnings")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_cancelled")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_deferred")
	assert.Contains(t, job.Outputs, "process_safe_outputs_items_failed")
	assert.Contains(t, job.Outputs, "process_safe_outputs_status")

	// Check output format
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_succeeded }}", job.Outputs["process_safe_outputs_items_succeeded"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_applied }}", job.Outputs["process_safe_outputs_items_applied"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_skipped }}", job.Outputs["process_safe_outputs_items_skipped"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_warnings }}", job.Outputs["process_safe_outputs_items_warnings"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_cancelled }}", job.Outputs["process_safe_outputs_items_cancelled"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_deferred }}", job.Outputs["process_safe_outputs_items_deferred"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.items_failed }}", job.Outputs["process_safe_outputs_items_failed"])
	assert.Equal(t, "${{ steps.process_safe_outputs.outputs.status }}", job.Outputs["process_safe_outputs_status"])
}

// TestJobDependencies tests that job dependencies are correctly set
func TestJobDependencies(t *testing.T) {
	tests := []struct {
		name             string
		safeOutputs      *SafeOutputsConfig
		expectedNeeds    []string
		notExpectedNeeds []string
	}{
		{
			name: "basic safe outputs",
			safeOutputs: &SafeOutputsConfig{
				CreateIssues: &CreateIssuesConfig{},
			},
			expectedNeeds:    []string{string(constants.AgentJobName), string(constants.ActivationJobName)},
			notExpectedNeeds: []string{string(constants.DetectionJobName)},
		},
		{
			name: "with threat detection",
			safeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
				CreateIssues:    &CreateIssuesConfig{},
			},
			expectedNeeds:    []string{string(constants.AgentJobName), string(constants.DetectionJobName)}, // detection is a separate job
			notExpectedNeeds: []string{},
		},
		{
			name: "with create pull request",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{},
			},
			expectedNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName)},
		},
		{
			name: "with push to PR branch",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
			},
			expectedNeeds: []string{string(constants.AgentJobName), string(constants.ActivationJobName)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

			require.NoError(t, err)
			require.NotNil(t, job)

			for _, need := range tt.expectedNeeds {
				assert.Contains(t, job.Needs, need)
			}

			for _, notNeed := range tt.notExpectedNeeds {
				assert.NotContains(t, job.Needs, notNeed)
			}
		})
	}
}

// TestGitHubAppWithPushToPRBranch tests that GitHub App token step is not duplicated
// when both app and push-to-pull-request-branch are configured
// Regression test for duplicate step bug reported in issue
func TestGitHubAppWithPushToPRBranch(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.ACTIONS_APP_ID }}",
				PrivateKey: "${{ secrets.ACTIONS_PRIVATE_KEY }}",
			},
			PushToPullRequestBranch: &PushToPullRequestBranchConfig{},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err, "Should successfully build job")
	require.NotNil(t, job, "Job should not be nil")

	stepsContent := strings.Join(job.Steps, "")

	// Should include app token minting step exactly once
	tokenMintCount := strings.Count(stepsContent, "Generate GitHub App token")
	assert.Equal(t, 1, tokenMintCount, "App token minting step should appear exactly once, found %d times", tokenMintCount)

	// Explicit app token invalidation step should not be generated
	tokenInvalidateCount := strings.Count(stepsContent, "Invalidate GitHub App token")
	assert.Equal(t, 0, tokenInvalidateCount, "App token invalidation step should not be generated, found %d times", tokenInvalidateCount)

	// Token step should come before checkout step (checkout references the token)
	tokenIndex := strings.Index(stepsContent, "Generate GitHub App token")
	checkoutIndex := strings.Index(stepsContent, "Checkout repository")
	assert.Less(t, tokenIndex, checkoutIndex, "Token minting step should come before checkout step")

	// Verify step ID is set correctly
	assert.Contains(t, stepsContent, "id: safe-outputs-app-token")
}

func TestBuildSafeOutputJobPreStepsRunBeforeGitHubAppToken(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.ACTIONS_APP_ID }}",
				PrivateKey: "${{ secrets.ACTIONS_PRIVATE_KEY }}",
			},
		},
	}

	job, err := compiler.buildSafeOutputJob(workflowData, SafeOutputJobConfig{
		JobName:     "add_comment",
		StepName:    "Add Comment",
		StepID:      "add_comment",
		MainJobName: string(constants.AgentJobName),
		Script:      "console.log('test')",
		Permissions: NewPermissionsFromMap(map[PermissionScope]PermissionLevel{
			PermissionIssues: PermissionWrite,
		}),
		PreSteps: []string{
			"      - name: Pre setup\n",
			"        run: echo \"pre\"\n",
		},
	})

	require.NoError(t, err, "Should successfully build safe output job")
	require.NotNil(t, job, "Job should not be nil")

	stepsContent := strings.Join(job.Steps, "")
	preIdx := strings.Index(stepsContent, "Pre setup")
	tokenIdx := strings.Index(stepsContent, "Generate GitHub App token")
	scriptIdx := strings.Index(stepsContent, "Add Comment")

	require.NotEqual(t, -1, preIdx, "Pre-step should be present")
	require.NotEqual(t, -1, tokenIdx, "GitHub App token step should be present")
	require.NotEqual(t, -1, scriptIdx, "Main safe-output step should be present")

	assert.Less(t, preIdx, tokenIdx, "Pre-steps should run before GitHub App token minting")
	assert.Less(t, tokenIdx, scriptIdx, "GitHub App token minting should still happen before the main safe-output step")
}

// TestGitHubAppTokenStepWithOTLPHeaders ensures the app token step insertion point
// stays aligned when OTLP header masking steps are present.
func TestGitHubAppTokenStepWithOTLPHeaders(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		Env: `"env":
  OTEL_EXPORTER_OTLP_HEADERS: "Authorization=******"`,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.ACTIONS_APP_ID }}",
				PrivateKey: "${{ secrets.ACTIONS_PRIVATE_KEY }}",
			},
			AddComments: &AddCommentsConfig{},
			AddLabels: &AddLabelsConfig{
				SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
					Allowed: []string{"bug"},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")
	require.NoError(t, err, "Should successfully build job")
	require.NotNil(t, job, "Job should not be nil")

	stepsContent := strings.Join(job.Steps, "")
	maskIndex := strings.Index(stepsContent, "Mask OTLP telemetry headers")
	downloadIndex := strings.Index(stepsContent, "Download agent output artifact")
	setupEnvEchoIndex := strings.Index(stepsContent, "GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json")
	tokenIndex := strings.Index(stepsContent, "Generate GitHub App token")

	require.NotEqual(t, -1, maskIndex, "OTLP headers mask step should be present")
	require.NotEqual(t, -1, downloadIndex, "agent output download step should be present")
	require.NotEqual(t, -1, setupEnvEchoIndex, "setup-agent-output-env run command should be present")
	require.NotEqual(t, -1, tokenIndex, "GitHub App token step should be present")

	assert.Less(t, maskIndex, downloadIndex, "masking should happen before artifact download")
	assert.Less(t, setupEnvEchoIndex, tokenIndex, "app token step must be inserted after setup-agent-output-env run block")
	// Regression guard: app token insertion must not split setup-agent-output-env and leave
	// an `echo ...` run line nested under a previous `with:` block.
	assert.NotContains(t, stepsContent, "with:\n          echo \"GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json\" >> \"$GITHUB_OUTPUT\"",
		"app token step must not split setup-agent-output-env into a nested echo under with:")
}

// TestGitHubAppTokenStepWithOTLPAttributes ensures the app token step insertion point
// stays aligned when the OTLP attributes masking step is present (GH_AW_OTLP_ATTRIBUTES path).
func TestGitHubAppTokenStepWithOTLPAttributes(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		Env: `"env":
  GH_AW_OTLP_ATTRIBUTES: "{\"service.name\":\"test\"}"`,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.ACTIONS_APP_ID }}",
				PrivateKey: "${{ secrets.ACTIONS_PRIVATE_KEY }}",
			},
			AddComments: &AddCommentsConfig{},
			AddLabels: &AddLabelsConfig{
				SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
					Allowed: []string{"bug"},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")
	require.NoError(t, err, "Should successfully build job")
	require.NotNil(t, job, "Job should not be nil")

	stepsContent := strings.Join(job.Steps, "")
	attrMaskIndex := strings.Index(stepsContent, "Mask OTLP custom attribute values")
	downloadIndex := strings.Index(stepsContent, "Download agent output artifact")
	setupEnvEchoIndex := strings.Index(stepsContent, "GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json")
	tokenIndex := strings.Index(stepsContent, "Generate GitHub App token")

	require.NotEqual(t, -1, attrMaskIndex, "OTLP attributes mask step should be present")
	require.NotEqual(t, -1, downloadIndex, "agent output download step should be present")
	require.NotEqual(t, -1, setupEnvEchoIndex, "setup-agent-output-env run command should be present")
	require.NotEqual(t, -1, tokenIndex, "GitHub App token step should be present")

	assert.Less(t, attrMaskIndex, downloadIndex, "attributes masking should happen before artifact download")
	assert.Less(t, setupEnvEchoIndex, tokenIndex, "app token step must be inserted after setup-agent-output-env run block")
	assert.NotContains(t, stepsContent, "with:\n          echo \"GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json\" >> \"$GITHUB_OUTPUT\"",
		"app token step must not split setup-agent-output-env into a nested echo under with:")
}

// TestGitHubAppTokenStepWithOTLPHeadersAndAttributes ensures the app token step insertion
// point stays aligned when both OTLP header masking and attribute masking steps are present
// (insertIndex is incremented twice — highest-risk composition).
func TestGitHubAppTokenStepWithOTLPHeadersAndAttributes(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		Env: `"env":
  OTEL_EXPORTER_OTLP_HEADERS: "Authorization=******"
  GH_AW_OTLP_ATTRIBUTES: "{\"service.name\":\"test\"}"`,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.ACTIONS_APP_ID }}",
				PrivateKey: "${{ secrets.ACTIONS_PRIVATE_KEY }}",
			},
			AddComments: &AddCommentsConfig{},
			AddLabels: &AddLabelsConfig{
				SafeOutputAllowBlockConfig: SafeOutputAllowBlockConfig{
					Allowed: []string{"bug"},
				},
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")
	require.NoError(t, err, "Should successfully build job")
	require.NotNil(t, job, "Job should not be nil")

	stepsContent := strings.Join(job.Steps, "")
	headerMaskIndex := strings.Index(stepsContent, "Mask OTLP telemetry headers")
	attrMaskIndex := strings.Index(stepsContent, "Mask OTLP custom attribute values")
	downloadIndex := strings.Index(stepsContent, "Download agent output artifact")
	setupEnvEchoIndex := strings.Index(stepsContent, "GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json")
	tokenIndex := strings.Index(stepsContent, "Generate GitHub App token")

	require.NotEqual(t, -1, headerMaskIndex, "OTLP headers mask step should be present")
	require.NotEqual(t, -1, attrMaskIndex, "OTLP attributes mask step should be present")
	require.NotEqual(t, -1, downloadIndex, "agent output download step should be present")
	require.NotEqual(t, -1, setupEnvEchoIndex, "setup-agent-output-env run command should be present")
	require.NotEqual(t, -1, tokenIndex, "GitHub App token step should be present")

	assert.Less(t, headerMaskIndex, downloadIndex, "header masking should happen before artifact download")
	assert.Less(t, attrMaskIndex, downloadIndex, "attributes masking should happen before artifact download")
	assert.Less(t, setupEnvEchoIndex, tokenIndex, "app token step must be inserted after setup-agent-output-env run block")
	assert.NotContains(t, stepsContent, "with:\n          echo \"GH_AW_AGENT_OUTPUT=/tmp/gh-aw/agent_output.json\" >> \"$GITHUB_OUTPUT\"",
		"app token step must not split setup-agent-output-env into a nested echo under with:")
}

// TestHeadGitHubAppTokenStepGenerated verifies that when head-github-app is configured on
// create-pull-request or push-to-pull-request-branch, the compiler generates a separate
// safe-outputs-head-app-token step that comes before the main safe-outputs handler step.
func TestHeadGitHubAppTokenStepGenerated(t *testing.T) {
	tests := []struct {
		name        string
		safeOutputs *SafeOutputsConfig
	}{
		{
			name: "create-pull-request with head-github-app",
			safeOutputs: &SafeOutputsConfig{
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					HeadRepoSlug:         "fork-owner/test-repo",
					HeadGitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.HEAD_APP_ID }}",
						PrivateKey: "${{ secrets.HEAD_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
		{
			name: "push-to-pull-request-branch with head-github-app",
			safeOutputs: &SafeOutputsConfig{
				PushToPullRequestBranch: &PushToPullRequestBranchConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					HeadRepoSlug:         "fork-owner/test-repo",
					HeadGitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.HEAD_APP_ID }}",
						PrivateKey: "${{ secrets.HEAD_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
		{
			name: "create-pull-request with both main and head github-app",
			safeOutputs: &SafeOutputsConfig{
				GitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.MAIN_APP_ID }}",
					PrivateKey: "${{ secrets.MAIN_APP_PRIVATE_KEY }}",
				},
				CreatePullRequests: &CreatePullRequestsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("5")},
					HeadRepoSlug:         "fork-owner/test-repo",
					HeadGitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.HEAD_APP_ID }}",
						PrivateKey: "${{ secrets.HEAD_APP_PRIVATE_KEY }}",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			workflowData := &WorkflowData{
				Name:        "Test Workflow",
				SafeOutputs: tt.safeOutputs,
			}

			job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")
			require.NoError(t, err, "Should build job without error")
			require.NotNil(t, job, "Job should not be nil")

			stepsContent := strings.Join(job.Steps, "")

			// The head app token step must be present with the correct ID.
			require.Contains(t, stepsContent, "id: safe-outputs-head-app-token",
				"head app token step should be emitted with correct step ID")

			// The head app token step should reference the configured app credentials.
			require.Contains(t, stepsContent, "${{ vars.HEAD_APP_ID }}",
				"head app token step should contain head app-id expression")
			require.Contains(t, stepsContent, "${{ secrets.HEAD_APP_PRIVATE_KEY }}",
				"head app token step should contain head private-key expression")

			// The head app token step must come before the safe-outputs handler step.
			headTokenIdx := strings.Index(stepsContent, "id: safe-outputs-head-app-token")
			handlerStepIdx := strings.LastIndex(stepsContent, "Process Safe Outputs")
			require.Greater(t, handlerStepIdx, -1, "handler step must be present")
			assert.Less(t, headTokenIdx, handlerStepIdx,
				"head app token step must be inserted before the safe outputs handler step")
		})
	}
}

// a safe-output job compiled for a workflow_call trigger uses
// needs.activation.outputs.target_repo_name (repo name only, no owner prefix) as the repositories
// fallback for the GitHub App token mint step, instead of the full target_repo slug.
// This prevents actions/create-github-app-token from receiving an invalid owner/repo slug
// in the repositories field when owner is also set.
func TestJobWithGitHubAppWorkflowCallUsesTargetRepoNameFallback(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On: `"on":
  workflow_call:`,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			CreateIssues: &CreateIssuesConfig{
				TitlePrefix: "[Test] ",
			},
		},
	}

	job, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test.md")

	require.NoError(t, err, "Should successfully build job")
	require.NotNil(t, job, "Job should not be nil")

	stepsContent := strings.Join(job.Steps, "")

	// Must use the repo-name-only output, NOT the full slug
	assert.Contains(t, stepsContent, "repositories: ${{ needs.activation.outputs.target_repo_name }}",
		"GitHub App token step must use target_repo_name (repo name only) for workflow_call workflows")
	assert.NotContains(t, stepsContent, "repositories: ${{ needs.activation.outputs.target_repo }}",
		"GitHub App token step must not use target_repo (full slug) for workflow_call workflows")
}

// TestConclusionJobWithGitHubAppWorkflowCallUsesTargetRepoNameFallback is a regression test
// verifying that the conclusion job compiled for a workflow_call trigger uses
// needs.activation.outputs.target_repo_name (repo name only) as the repositories fallback
// for the GitHub App token mint step.
func TestConclusionJobWithGitHubAppWorkflowCallUsesTargetRepoNameFallback(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	compiler.SetActionMode(ActionModeDev)

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On: `"on":
  workflow_call:`,
		SafeOutputs: &SafeOutputsConfig{
			GitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
			},
			AddComments: &AddCommentsConfig{},
		},
	}

	job, err := compiler.buildConclusionJob(workflowData, string(constants.AgentJobName), nil)

	require.NoError(t, err, "Should successfully build conclusion job")
	require.NotNil(t, job, "Conclusion job should not be nil")

	stepsContent := strings.Join(job.Steps, "")

	// Must use the repo-name-only output, NOT the full slug
	assert.Contains(t, stepsContent, "repositories: ${{ needs.activation.outputs.target_repo_name }}",
		"Conclusion job GitHub App token step must use target_repo_name (repo name only) for workflow_call workflows")
	assert.NotContains(t, stepsContent, "repositories: ${{ needs.activation.outputs.target_repo }}",
		"Conclusion job GitHub App token step must not use target_repo (full slug) for workflow_call workflows")
}

// TestCallWorkflowOnly_UsesHandlerManagerStep asserts that a workflow configured with only
// call-workflow (no other handler-manager types) still compiles a "Process Safe Outputs" step.
func TestCallWorkflowOnly_UsesHandlerManagerStep(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			CallWorkflow: &CallWorkflowConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Max: strPtr("1"),
				},
				Workflows: []string{"worker-a"},
			},
		},
	}

	job, stepNames, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test-workflow.md")
	require.NoError(t, err, "Should compile without error")
	require.NotNil(t, job, "safe_outputs job should be generated when only call-workflow is configured")
	require.NotNil(t, stepNames, "Step names should not be nil")

	stepsContent := strings.Join(job.Steps, "")
	assert.Contains(t, stepsContent, "Process Safe Outputs", "Compiled job should include 'Process Safe Outputs' step")
	assert.Contains(t, stepsContent, "GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG", "Compiled job should include handler config env var")
	assert.Contains(t, stepsContent, "call_workflow", "Handler config should reference call_workflow")
}

// TestCreateCodeScanningAlertUploadJob verifies that when create-code-scanning-alert is configured,
// a dedicated upload_code_scanning_sarif job is created (separate from safe_outputs) and that
// the safe_outputs job:
//   - exports sarif_file output for the upload job
//   - uploads the SARIF file as a GitHub Actions artifact so the upload job
//     (which runs in a fresh workspace) can download it
//
// Token handling: the upload job computes tokens directly (static PAT or minted GitHub App token)
// rather than reading from safe_outputs job outputs, because GitHub Actions masks secret references
// in job outputs — "Skip output 'x' since it may contain secret".
func TestCreateCodeScanningAlertUploadJob(t *testing.T) {
	tests := []struct {
		name                   string
		config                 *CreateCodeScanningAlertsConfig
		checkoutConfigs        []*CheckoutConfig
		expectUploadJob        bool
		expectTokenInSteps     string // expected token expression in upload job steps
		expectAppTokenMintStep bool   // expect a GitHub App token minting step in upload job
		safeOutputsGitHubToken string
	}{
		{
			name: "default config creates separate upload job with static token computed directly",
			config: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
			expectUploadJob:    true,
			expectTokenInSteps: "${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
		},
		{
			name: "custom per-config github-token is used in upload step token",
			config: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					GitHubToken: "${{ secrets.GHAS_TOKEN }}",
				},
			},
			expectUploadJob:    true,
			expectTokenInSteps: "${{ secrets.GHAS_TOKEN }}",
		},
		{
			name: "safe-outputs-level github-token is used in upload step token",
			config: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
			expectUploadJob:        true,
			expectTokenInSteps:     "${{ secrets.SO_TOKEN }}",
			safeOutputsGitHubToken: "${{ secrets.SO_TOKEN }}",
		},
		{
			name: "checkout with github-app mints a fresh app token in the upload job",
			config: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
			checkoutConfigs: []*CheckoutConfig{
				{
					GitHubApp: &GitHubAppConfig{
						AppID:      "${{ vars.APP_ID }}",
						PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
					},
				},
			},
			expectUploadJob:        true,
			expectTokenInSteps:     "${{ steps.checkout-restore-app-token.outputs.token }}",
			expectAppTokenMintStep: true,
		},
		{
			name: "checkout with github-token PAT uses that PAT directly in upload job",
			config: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{},
			},
			checkoutConfigs: []*CheckoutConfig{
				{
					GitHubToken: "${{ secrets.MY_CHECKOUT_PAT }}",
				},
			},
			expectUploadJob:    true,
			expectTokenInSteps: "${{ secrets.MY_CHECKOUT_PAT }}",
		},
		{
			name: "staged mode does not create upload job",
			config: &CreateCodeScanningAlertsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{
					Staged: templatableBoolPtr("true"),
				},
			},
			expectUploadJob: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.jobManager = NewJobManager()

			workflowData := &WorkflowData{
				Name: "Test Workflow",
				SafeOutputs: &SafeOutputsConfig{
					CreateCodeScanningAlerts: tt.config,
					GitHubToken:              tt.safeOutputsGitHubToken,
				},
				CheckoutConfigs: tt.checkoutConfigs,
			}

			// 1. Verify safe_outputs job exports sarif_file and uploads the artifact
			safeOutputsJob, _, err := compiler.buildConsolidatedSafeOutputsJob(workflowData, string(constants.AgentJobName), "test-workflow.md")
			require.NoError(t, err, "safe_outputs job should build without error")
			require.NotNil(t, safeOutputsJob, "safe_outputs job should be generated")

			safeOutputsSteps := strings.Join(safeOutputsJob.Steps, "")

			if tt.expectUploadJob {
				// safe_outputs must export sarif_file so the upload job can check if there is work to do
				assert.Contains(t, safeOutputsJob.Outputs, "sarif_file",
					"safe_outputs job must export sarif_file output")
				assert.Contains(t, safeOutputsJob.Outputs["sarif_file"], "steps.process_safe_outputs.outputs.sarif_file",
					"sarif_file output must reference process_safe_outputs step")

				// safe_outputs must NOT export checkout_token — GitHub Actions masks secret
				// references in job outputs, making them arrive empty in downstream jobs.
				assert.NotContains(t, safeOutputsJob.Outputs, "checkout_token",
					"safe_outputs job must NOT export checkout_token (secret refs are masked in job outputs)")

				// safe_outputs must upload the SARIF file as an artifact so the upload job
				// (running in a fresh workspace) can download it
				assert.Contains(t, safeOutputsSteps, constants.SarifArtifactName,
					"safe_outputs job must upload the SARIF file as a GitHub Actions artifact")
				assert.Contains(t, safeOutputsSteps, "Upload SARIF artifact",
					"safe_outputs job must have a SARIF artifact upload step")
				assert.Contains(t, safeOutputsSteps, "steps.process_safe_outputs.outputs.sarif_file != ''",
					"SARIF artifact upload must be conditional on sarif_file being non-empty")

				// The SARIF upload-sarif steps must NOT be in safe_outputs itself
				assert.NotContains(t, safeOutputsSteps, "upload-sarif",
					"SARIF codeql upload must NOT be a step in safe_outputs job")
				assert.NotContains(t, safeOutputsSteps, "Upload SARIF to GitHub Code Scanning",
					"SARIF upload step must NOT appear in safe_outputs job")

				// 2. Verify the dedicated upload job is built correctly
				uploadJob, buildErr := compiler.buildCodeScanningUploadJob(workflowData)
				require.NoError(t, buildErr, "upload_code_scanning_sarif job should build without error")
				require.NotNil(t, uploadJob, "upload_code_scanning_sarif job should be created")

				assert.Equal(t, string(constants.UploadCodeScanningJobName), uploadJob.Name,
					"Upload job must be named upload_code_scanning_sarif")
				assert.Contains(t, uploadJob.Needs, string(constants.SafeOutputsJobName),
					"Upload job must depend on safe_outputs")
				assert.Contains(t, uploadJob.If, "sarif_file != ''",
					"Upload job must only run when sarif_file is non-empty")
				assert.Contains(t, uploadJob.If, string(constants.SafeOutputsJobName),
					"Upload job if-condition must reference safe_outputs outputs")
				assert.Contains(t, uploadJob.Permissions, "actions: read",
					"Upload job permissions must include actions: read for private-repo SARIF uploads")

				uploadSteps := strings.Join(uploadJob.Steps, "")

				// The upload job must NOT use needs.safe_outputs.outputs.checkout_token — it
				// would arrive empty because GitHub Actions masks secret refs in job outputs.
				assert.NotContains(t, uploadSteps, "needs.safe_outputs.outputs.checkout_token",
					"Upload job must NOT read checkout_token from safe_outputs outputs (would be masked)")

				// Restore checkout step must be present in the upload job
				assert.Contains(t, uploadSteps, "Restore checkout to triggering commit",
					"Upload job must restore workspace to triggering commit")
				assert.Contains(t, uploadSteps, "ref: ${{ github.sha }}",
					"Restore checkout must check out github.sha")
				assert.Contains(t, uploadSteps, "persist-credentials: false",
					"Restore checkout must disable credential persistence")
				assert.NotContains(t, uploadSteps, "git checkout ${{ github.sha }}",
					"Must use actions/checkout, not a raw git command")

				if tt.expectAppTokenMintStep {
					// GitHub App checkout: a token minting step must appear before the restore checkout
					assert.Contains(t, uploadSteps, "checkout-restore-app-token",
						"Upload job must mint a GitHub App token before restoring checkout")
					mintPos := strings.Index(uploadSteps, "checkout-restore-app-token")
					restoreCheckoutPos := strings.Index(uploadSteps, "Restore checkout to triggering commit")
					require.NotEqual(t, -1, mintPos, "App token minting step must be present in upload job steps")
					require.NotEqual(t, -1, restoreCheckoutPos, "Restore checkout step must be present in upload job steps")
					assert.Less(t, mintPos, restoreCheckoutPos,
						"App token minting step must appear before the restore checkout step")
				}

				// Download SARIF artifact step must be present in the upload job
				assert.Contains(t, uploadSteps, "Download SARIF artifact",
					"Upload job must download the SARIF artifact before uploading to Code Scanning")
				assert.Contains(t, uploadSteps, constants.SarifArtifactName,
					"Upload job must download the code-scanning-sarif artifact")
				assert.Contains(t, uploadSteps, constants.SarifArtifactDownloadPath,
					"Upload job must download artifact to the expected path")

				// Upload SARIF step must be present
				assert.Contains(t, uploadSteps, "Upload SARIF to GitHub Code Scanning",
					"Upload job must have SARIF upload step")
				assert.Contains(t, uploadSteps, "upload-sarif",
					"Upload job must use github/codeql-action/upload-sarif")
				assert.Contains(t, uploadSteps, "wait-for-processing: true",
					"Upload step must wait for processing")
				// ref and sha pin the upload to the triggering commit
				assert.Contains(t, uploadSteps, "ref: ${{ github.ref }}",
					"Upload step must include ref input")
				assert.Contains(t, uploadSteps, "sha: ${{ github.sha }}",
					"Upload step must include sha input")
				// sarif_file must be the local path from the downloaded artifact (not a job output reference)
				localSarifPath := path.Join(constants.SarifArtifactDownloadPath.String(), constants.SarifFileName.String())
				assert.Contains(t, uploadSteps, localSarifPath,
					"Upload step must use the locally downloaded SARIF file path")
				assert.NotContains(t, uploadSteps, "needs.safe_outputs.outputs.sarif_file",
					"Upload step must NOT reference sarif_file from job outputs (use local artifact path instead)")
				// Upload-sarif uses 'token' not 'github-token'
				assert.Contains(t, uploadSteps, "token:",
					"Upload step must use 'token' input (not 'github-token')")
				assert.NotContains(t, uploadSteps, "github-token:",
					"Upload step must not use 'github-token' - upload-sarif only accepts 'token'")

				// Step ordering: restore → download → upload
				restorePos := strings.Index(uploadSteps, "Restore checkout to triggering commit")
				downloadPos := strings.Index(uploadSteps, "Download SARIF artifact")
				uploadPos := strings.Index(uploadSteps, "Upload SARIF to GitHub Code Scanning")
				require.Greater(t, restorePos, -1, "Restore checkout step must exist")
				require.Greater(t, downloadPos, -1, "Download SARIF artifact step must exist")
				require.Greater(t, uploadPos, -1, "Upload SARIF step must exist")
				assert.Less(t, restorePos, downloadPos,
					"Restore checkout must appear before SARIF download in the job steps")
				assert.Less(t, downloadPos, uploadPos,
					"SARIF download must appear before SARIF upload in the job steps")

				// Verify the expected token expression appears in the upload job steps
				if tt.expectTokenInSteps != "" {
					assert.Contains(t, uploadSteps, tt.expectTokenInSteps,
						"Upload job must use the expected token in its steps")
				}
			} else {
				// staged: safe_outputs should NOT export sarif_file
				assert.NotContains(t, safeOutputsJob.Outputs, "sarif_file",
					"staged mode: safe_outputs must not export sarif_file")
				assert.NotContains(t, safeOutputsJob.Outputs, "checkout_token",
					"staged mode: safe_outputs must not export checkout_token")
			}
		})
	}
}

func TestCreateCodeScanningAlertUploadJobWorkflowCallDependsOnActivation(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		On: `"on":
  workflow_call:`,
		SafeOutputs: &SafeOutputsConfig{
			CreateCodeScanningAlerts: &CreateCodeScanningAlertsConfig{},
		},
	}

	uploadJob, err := compiler.buildCodeScanningUploadJob(workflowData)
	require.NoError(t, err)
	require.NotNil(t, uploadJob)
	assert.Equal(t, []string{
		string(constants.ActivationJobName),
		string(constants.SafeOutputsJobName),
	}, uploadJob.Needs)
	assert.Contains(t, strings.Join(uploadJob.Steps, ""), "${{ needs.activation.outputs.artifact_prefix }}")
}

// TestBuildSafeOutputItemsManifestUploadStep verifies that the upload step includes
// the manifest and temporary ID map but excludes process stdout/stderr logs.
func TestBuildSafeOutputItemsManifestUploadStep(t *testing.T) {
	steps := buildSafeOutputItemsManifestUploadStep("", func(action string) string {
		return action + "@test-pin"
	})

	content := strings.Join(steps, "")

	assert.Contains(t, content, "Upload Safe Outputs Items")
	assert.Contains(t, content, "if: always()")
	assert.Contains(t, content, "/tmp/gh-aw/safe-output-items.jsonl")
	assert.NotContains(t, content, "process-safe-outputs.stdout.log")
	assert.NotContains(t, content, "process-safe-outputs.stderr.log")
}

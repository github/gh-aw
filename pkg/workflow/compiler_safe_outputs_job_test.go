//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
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
			expectedPerms:    []string{"contents: read", "issues: write"},
		},
		{
			name: "add comments only",
			safeOutputs: &SafeOutputsConfig{
				AddComments: &AddCommentsConfig{
					BaseSafeOutputConfig: BaseSafeOutputConfig{
						Max: 5,
					},
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"contents: read", "issues: write", "pull-requests: write", "discussions: write"},
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
						Max: 3,
					},
				},
				AddLabels: &AddLabelsConfig{
					Allowed: []string{"bug", "enhancement"},
				},
			},
			expectedJobName:  "safe_outputs",
			checkPermissions: true,
			expectedPerms:    []string{"contents: read", "issues: write", "pull-requests: write", "discussions: write"},
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
				App: &GitHubAppConfig{
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

			// Check job dependencies
			assert.Contains(t, job.Needs, string(constants.AgentJobName))
			if tt.threatDetection {
				assert.Contains(t, job.Needs, string(constants.DetectionJobName))
			}

			// Check permissions if specified
			if tt.checkPermissions {
				jobYAML := job.Permissions
				for _, perm := range tt.expectedPerms {
					assert.Contains(t, jobYAML, perm, "Expected permission: "+perm)
				}
			}

			// Verify timeout is set
			assert.Equal(t, 15, job.TimeoutMinutes)

			// Verify job condition is set
			assert.NotEmpty(t, job.If)
		})
	}
}

// TestBuildJobLevelSafeOutputEnvVars tests job-level environment variable generation
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
				"GH_AW_WORKFLOW_ID":   `"test-workflow"`,
				"GH_AW_WORKFLOW_NAME": `"Test Workflow"`,
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
				Name: "Test Workflow",
				EngineConfig: &EngineConfig{
					ID:      "copilot",
					Version: "0.0.375",
					Model:   "gpt-4",
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
					Staged: true,
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

// TestBuildDetectionSuccessCondition tests the detection condition builder
func TestBuildDetectionSuccessCondition(t *testing.T) {
	condition := buildDetectionSuccessCondition()

	require.NotNil(t, condition)

	rendered := condition.Render()

	// Should check that detection job output 'success' equals 'true'
	assert.Contains(t, rendered, "needs."+string(constants.DetectionJobName))
	assert.Contains(t, rendered, "outputs.success")
	assert.Contains(t, rendered, "'true'")
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

	// Job condition should include detection check
	assert.Contains(t, job.If, "needs."+string(constants.DetectionJobName))
	assert.Contains(t, job.If, "outputs.success")

	// Job should depend on detection job
	assert.Contains(t, job.Needs, string(constants.DetectionJobName))
}

// TestJobWithGitHubApp tests job building with GitHub App configuration
func TestJobWithGitHubApp(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	workflowData := &WorkflowData{
		Name: "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{
			App: &GitHubAppConfig{
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

	// Should include app token invalidation step
	assert.Contains(t, stepsContent, "Invalidate GitHub App token")
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
	assert.Contains(t, job.Outputs, "process_safe_outputs_temporary_id_map")
	assert.Contains(t, job.Outputs, "process_safe_outputs_processed_count")

	// Check output format
	assert.Contains(t, job.Outputs["process_safe_outputs_temporary_id_map"], "steps.process_safe_outputs.outputs")
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
			expectedNeeds:    []string{string(constants.AgentJobName)},
			notExpectedNeeds: []string{string(constants.DetectionJobName), string(constants.ActivationJobName)},
		},
		{
			name: "with threat detection",
			safeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
				CreateIssues:    &CreateIssuesConfig{},
			},
			expectedNeeds: []string{string(constants.AgentJobName), string(constants.DetectionJobName)},
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

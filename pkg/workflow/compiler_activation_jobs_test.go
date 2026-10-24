//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildPreActivationJob_WithPermissionCheck tests building pre-activation job with permission checks
func TestBuildPreActivationJob_WithPermissionCheck(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:    "Test Workflow",
		Command: []string{"test"},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	job, err := compiler.buildPreActivationJob(workflowData, true)
	require.NoError(t, err, "buildPreActivationJob should succeed with permission check")
	require.NotNil(t, job)

	assert.Equal(t, string(constants.PreActivationJobName), job.Name)
	assert.NotNil(t, job.Outputs, "Job should have outputs")

	// Check for activated output
	_, hasActivated := job.Outputs["activated"]
	assert.True(t, hasActivated, "Job should have 'activated' output")

	// Check that steps contain membership check
	stepsStr := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsStr, constants.CheckMembershipStepID.String(),
		"Steps should include membership check")
}

// TestBuildPreActivationJob_WithoutPermissionCheck tests building pre-activation job without permission checks
func TestBuildPreActivationJob_WithoutPermissionCheck(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:    "Test Workflow",
		Command: []string{"test"},
	}

	job, err := compiler.buildPreActivationJob(workflowData, false)
	require.NoError(t, err, "buildPreActivationJob should succeed without permission check")
	require.NotNil(t, job)

	assert.Equal(t, string(constants.PreActivationJobName), job.Name)

	// Job should still have basic structure even without permission checks
	assert.NotEmpty(t, job.Steps, "Job should have steps")
}

// TestBuildPreActivationJob_WithStopTime tests building pre-activation job with stop-time validation
func TestBuildPreActivationJob_WithStopTime(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:     "Test Workflow",
		Command:  []string{"test"},
		StopTime: "2024-12-31T23:59:59Z",
	}

	job, err := compiler.buildPreActivationJob(workflowData, false)
	require.NoError(t, err, "buildPreActivationJob should succeed with stop-time")
	require.NotNil(t, job)

	// Check that steps contain stop-time check
	stepsStr := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsStr, constants.CheckStopTimeStepID.String(),
		"Steps should include stop-time check")
	assert.Contains(t, stepsStr, "GH_AW_STOP_TIME",
		"Steps should include stop-time environment variable")
	assert.Contains(t, stepsStr, workflowData.StopTime,
		"Steps should include the actual stop-time value")
}

// TestBuildPreActivationJob_WithReaction tests that reaction step is NOT in pre-activation (it moved to activation)
func TestBuildPreActivationJob_WithReaction(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name               string
		reaction           string
		shouldHaveReaction bool
	}{
		{
			name:               "with eyes reaction",
			reaction:           "eyes",
			shouldHaveReaction: true,
		},
		{
			name:               "with rocket reaction",
			reaction:           "rocket",
			shouldHaveReaction: true,
		},
		{
			name:               "with none reaction",
			reaction:           "none",
			shouldHaveReaction: false,
		},
		{
			name:               "empty reaction",
			reaction:           "",
			shouldHaveReaction: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:       "Test Workflow",
				Command:    []string{"test"},
				AIReaction: ReactionType(tt.reaction),
			}

			// Pre-activation job should NOT contain the reaction step any more
			preJob, err := compiler.buildPreActivationJob(workflowData, false)
			require.NoError(t, err)
			require.NotNil(t, preJob)
			preStepsStr := strings.Join(preJob.Steps, "\n")
			assert.NotContains(t, preStepsStr, "GH_AW_REACTION",
				"Pre-activation job should not contain reaction step")

			// Activation job should contain the reaction step
			activationJob, err := compiler.buildActivationJob(workflowData, true, "", "test.lock.yml")
			require.NoError(t, err)
			require.NotNil(t, activationJob)
			stepsStr := strings.Join(activationJob.Steps, "\n")

			if tt.shouldHaveReaction {
				assert.Contains(t, stepsStr, "Add "+tt.reaction+" reaction",
					"Activation job steps should include reaction step for %s", tt.reaction)
				assert.Contains(t, stepsStr, "GH_AW_REACTION",
					"Activation job steps should include reaction environment variable")

				// Check activation job permissions include reaction permissions
				assert.Contains(t, activationJob.Permissions, "issues: write",
					"Activation job permissions should include issues: write for reactions")
			} else {
				// When reaction is "none" or empty, no reaction step should be added
				if tt.reaction == "none" || tt.reaction == "" {
					assert.NotContains(t, stepsStr, "GH_AW_REACTION",
						"Activation job steps should not include reaction for %s", tt.reaction)
				}
			}
		})
	}
}

// TestBuildPreActivationJob_WithCustomStepsAndOutputs tests custom steps/outputs extraction
func TestBuildPreActivationJob_WithCustomStepsAndOutputs(t *testing.T) {
	compiler := NewCompiler()

	// Create workflow data with custom pre-activation job
	workflowData := &WorkflowData{
		Name:    "Test Workflow",
		Command: []string{"test"},
		Jobs: map[string]any{
			"pre-activation": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "Custom setup",
						"run":  "echo 'custom'",
					},
				},
				"outputs": map[string]any{
					"custom_output": "${{ steps.custom.outputs.value }}",
				},
			},
		},
	}

	job, err := compiler.buildPreActivationJob(workflowData, false)
	require.NoError(t, err, "buildPreActivationJob should succeed with custom fields")
	require.NotNil(t, job)

	// Check that custom steps are included
	stepsStr := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsStr, "Custom setup", "Should include custom step")

	// Check that custom outputs are included
	_, hasCustomOutput := job.Outputs["custom_output"]
	assert.True(t, hasCustomOutput, "Should include custom output")
}

// TestBuildActivationJob_Basic tests building a basic activation job
func TestBuildActivationJob_Basic(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err, "buildActivationJob should succeed")
	require.NotNil(t, job)

	assert.Equal(t, string(constants.ActivationJobName), job.Name)
	assert.NotNil(t, job.Outputs, "Job should have outputs")
}

// TestBuildActivationJob_WithPreActivation tests activation job when pre-activation exists
func TestBuildActivationJob_WithPreActivation(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, true, "", "test.lock.yml")
	require.NoError(t, err, "buildActivationJob should succeed with pre-activation")
	require.NotNil(t, job)

	// When pre-activation exists, activation job should have needs dependency
	assert.Contains(t, job.Needs, string(constants.PreActivationJobName),
		"Activation job should depend on pre-activation job")
}

// TestBuildActivationJob_WithReaction tests activation job with reaction configuration
func TestBuildActivationJob_WithReaction(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
		AIReaction:      "rocket",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	// Activation job should handle reactions appropriately
	stepsStr := strings.Join(job.Steps, "\n")
	// The reaction is actually added in pre-activation, but activation may reference it
	assert.NotEmpty(t, stepsStr, "Activation job should have steps")
}

// TestBuildActivationJob_ReactionAfterSetupScripts verifies that the reaction step is placed
// after generate_aw_info (so aw_info is captured even if reaction fails) and that both appear
// early in the job before secret validation steps.
func TestBuildActivationJob_ReactionAfterSetupScripts(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		AIReaction:      "heart",
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err, "buildActivationJob should succeed")
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")

	setupIdx := strings.Index(stepsStr, "Setup Scripts")
	awInfoIdx := strings.Index(stepsStr, "id: generate_aw_info")
	reactIdx := strings.Index(stepsStr, "id: react")

	assert.Greater(t, setupIdx, -1, "Setup Scripts step should be present")
	assert.Greater(t, awInfoIdx, -1, "Generate aw_info step should be present")
	assert.Greater(t, reactIdx, -1, "Reaction step should be present")

	// generate_aw_info runs first (after setup) so its data is captured even if reaction fails.
	// Reaction runs right after generate_aw_info for fast user feedback.
	assert.Less(t, setupIdx, awInfoIdx, "generate_aw_info should appear after Setup Scripts")
	assert.Less(t, awInfoIdx, reactIdx, "Reaction step should appear after generate_aw_info")
}

// TestBuildActivationJob_SkipsSecretValidationWithEnvironment verifies that top-level
// environment configuration disables activation token validation.
func TestBuildActivationJob_SkipsSecretValidationWithEnvironment(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		Environment: "environment: production",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err, "buildActivationJob should succeed")
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")
	assert.NotContains(t, stepsStr, "id: validate-secret", "Activation job should skip validate-secret when top-level environment is configured")
	assert.NotContains(t, job.Outputs, "secret_verification_result", "Activation job should not expose secret_verification_result output when validate-secret is skipped")
}

// TestBuildActivationJob_Environment verifies that manual-approval takes precedence over the workflow environment.
func TestBuildActivationJob_Environment(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		approval    string
		want        string
	}{
		{
			name: "no environment",
		},
		{
			name:        "inherits workflow environment",
			environment: "environment: production",
			want:        "environment: production",
		},
		{
			name:        "indents workflow environment object",
			environment: "environment:\n  name: production\n  url: https://example.com",
			want:        "environment:\n      name: production\n      url: https://example.com",
		},
		{
			name:        "manual approval environment takes precedence",
			environment: "environment: production",
			approval:    "approval",
			want:        "environment: approval",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				Environment:    tt.environment,
				ManualApproval: tt.approval,
			}
			job, err := NewCompiler().buildActivationJob(data, false, "", "test.lock.yml")
			require.NoError(t, err)
			assert.Equal(t, tt.want, job.Environment)
		})
	}
}

// TestBuildActivationJob_OAuthTokenCheckStep verifies that the activation job
// includes a step to check for OAuth tokens in COPILOT_GITHUB_TOKEN, GH_AW_GITHUB_TOKEN,
// and GH_AW_GITHUB_MCP_SERVER_TOKEN.
func TestBuildActivationJob_OAuthTokenCheckStep(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err, "buildActivationJob should succeed")
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "id: check-oauth-tokens", "Activation job should contain OAuth token check step")
	assert.Contains(t, stepsStr, "check_oauth_tokens.sh", "OAuth token check step should call the check_oauth_tokens.sh script")
	assert.Contains(t, stepsStr, constants.CopilotGitHubToken+":", "OAuth token check step should pass COPILOT_GITHUB_TOKEN env var")
	assert.Contains(t, stepsStr, constants.EnvVarGitHubToken+":", "OAuth token check step should pass GH_AW_GITHUB_TOKEN env var")
	assert.Contains(t, stepsStr, constants.EnvVarGitHubMCPServerToken+":", "OAuth token check step should pass GH_AW_GITHUB_MCP_SERVER_TOKEN env var")
}

// TestBuildActivationJob_OAuthTokenCheckStep_DefaultTokenExprs verifies that default
// secret expressions are used when no engine env overrides are set.
func TestBuildActivationJob_OAuthTokenCheckStep_DefaultTokenExprs(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}", "Default COPILOT_GITHUB_TOKEN expression should reference secrets.COPILOT_GITHUB_TOKEN")
	assert.Contains(t, stepsStr, "GH_AW_GITHUB_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN }}", "Default GH_AW_GITHUB_TOKEN expression should reference secrets.GH_AW_GITHUB_TOKEN")
	assert.Contains(t, stepsStr, "GH_AW_GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}", "Default GH_AW_GITHUB_MCP_SERVER_TOKEN expression should reference secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN")
}

// TestBuildActivationJob_OAuthTokenCheckStep_EngineEnvOverride verifies that when
// engine.env overrides COPILOT_GITHUB_TOKEN, the OAuth check uses the overridden expression.
func TestBuildActivationJob_OAuthTokenCheckStep_EngineEnvOverride(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test\n\nContent",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				constants.CopilotGitHubToken: "${{ secrets.MY_ORG_COPILOT_TOKEN }}",
			},
		},
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "COPILOT_GITHUB_TOKEN: ${{ secrets.MY_ORG_COPILOT_TOKEN }}", "Overridden COPILOT_GITHUB_TOKEN expression should be used in OAuth check step")
	assert.NotContains(t, stepsStr, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}", "Default COPILOT_GITHUB_TOKEN expression should not appear when overridden")
}

// TestBuildActivationJob_OAuthTokenCheckStep_EngineEnvOverrideMultiline verifies that
// multiline engine.env overrides are emitted as a YAML literal block scalar in the OAuth check step.
func TestBuildActivationJob_OAuthTokenCheckStep_EngineEnvOverrideMultiline(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test\n\nContent",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				constants.CopilotGitHubToken: "${{ secrets.EXAMPLE_TOKEN_A != '' && secrets.EXAMPLE_TOKEN_A ||\n    secrets.EXAMPLE_TOKEN_B != '' && secrets.EXAMPLE_TOKEN_B ||\n    secrets.EXAMPLE_TOKEN_C }}",
			},
		},
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")

	assert.Contains(t, stepsStr, "id: check-oauth-tokens", "Activation job should contain OAuth token check step")
	assert.Contains(t, stepsStr, "          COPILOT_GITHUB_TOKEN: |\n", "Multiline override should be emitted as a literal block scalar")
	assert.Contains(t, stepsStr, "            ${{ secrets.EXAMPLE_TOKEN_A != '' && secrets.EXAMPLE_TOKEN_A ||\n", "First line should be indented under the block scalar")
	assert.Contains(t, stepsStr, "                secrets.EXAMPLE_TOKEN_B != '' && secrets.EXAMPLE_TOKEN_B ||\n", "Continuation indentation should be preserved")
	assert.Contains(t, stepsStr, "                secrets.EXAMPLE_TOKEN_C }}\n", "Final continuation line should be preserved")
	assert.NotContains(t, stepsStr, "COPILOT_GITHUB_TOKEN: ${{ secrets.EXAMPLE_TOKEN_A != '' && secrets.EXAMPLE_TOKEN_A ||", "Multiline override should not be emitted inline")
}

// TestBuildActivationJob_OAuthCheckAfterSecretValidation verifies the step order:
// the OAuth token check appears after the secret validation step.
func TestBuildActivationJob_OAuthCheckAfterSecretValidation(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "")

	validateIdx := strings.Index(stepsStr, "id: validate-secret")
	oauthIdx := strings.Index(stepsStr, "id: check-oauth-tokens")

	require.Greater(t, oauthIdx, -1, "check-oauth-tokens step should be present")

	if validateIdx > -1 {
		assert.Greater(t, oauthIdx, validateIdx, "OAuth token check step should appear after validate-secret step")
	}
}

// TestBuildActivationJob_OAuthTokenCheckFailedOutput verifies that the activation job
// exposes oauth_token_check_failed as an output derived from the check-oauth-tokens step.
func TestBuildActivationJob_OAuthTokenCheckFailedOutput(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		MarkdownContent: "# Test\n\nContent",
	}

	job, err := compiler.buildActivationJob(workflowData, false, "", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	outputVal, ok := job.Outputs["oauth_token_check_failed"]
	require.True(t, ok, "activation job should expose oauth_token_check_failed output")
	assert.Contains(t, outputVal, "steps.check-oauth-tokens.outputs.oauth_token_check_failed", "output should reference the check-oauth-tokens step")
}

// TestBuildMainJob_Basic tests building a basic main job
func TestBuildMainJob_Basic(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
		AI:              "copilot",
	}

	job, err := compiler.buildMainJob(workflowData, false)
	require.NoError(t, err, "buildMainJob should succeed")
	require.NotNil(t, job)

	assert.Equal(t, string(constants.AgentJobName), job.Name)
	assert.NotEmpty(t, job.Steps, "Main job should have steps")
}

// TestBuildMainJob_WithActivation tests main job when activation job exists
func TestBuildMainJob_WithActivation(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
		AI:              "copilot",
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed with activation")
	require.NotNil(t, job)

	// When activation exists, main job should depend on it
	assert.Contains(t, job.Needs, string(constants.ActivationJobName),
		"Main job should depend on activation job")
}

// TestBuildMainJob_WithPermissions tests main job permission handling
func TestBuildMainJob_WithPermissions(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
		AI:              "copilot",
		Permissions:     "contents: read\nissues: write",
	}

	job, err := compiler.buildMainJob(workflowData, false)
	require.NoError(t, err)
	require.NotNil(t, job)

	// Check permissions are set
	assert.NotEmpty(t, job.Permissions, "Main job should have permissions")
	assert.Contains(t, job.Permissions, "contents:",
		"Permissions should include contents")
}

// TestExtractPreActivationCustomFields_NoCustomJob tests extraction when no custom job exists
func TestExtractPreActivationCustomFields_NoCustomJob(t *testing.T) {
	compiler := NewCompiler()

	jobs := map[string]any{
		"other-job": map[string]any{
			"runs-on": "ubuntu-latest",
		},
	}

	steps, outputs, err := compiler.extractPreActivationCustomFields(jobs)
	require.NoError(t, err)
	assert.Empty(t, steps, "Should have no custom steps")
	assert.Empty(t, outputs, "Should have no custom outputs")
}

// TestExtractPreActivationCustomFields_WithCustomFields tests extraction with custom fields
func TestExtractPreActivationCustomFields_WithCustomFields(t *testing.T) {
	compiler := NewCompiler()

	jobs := map[string]any{
		"pre-activation": map[string]any{
			"steps": []any{
				map[string]any{
					"name": "Custom step",
					"run":  "echo 'test'",
				},
			},
			"outputs": map[string]any{
				"result": "${{ steps.test.outputs.value }}",
			},
		},
	}

	steps, outputs, err := compiler.extractPreActivationCustomFields(jobs)
	require.NoError(t, err)
	assert.NotEmpty(t, steps, "Should have custom steps")
	assert.NotEmpty(t, outputs, "Should have custom outputs")

	// Check step content
	stepsStr := strings.Join(steps, "\n")
	assert.Contains(t, stepsStr, "Custom step")

	// Check output content
	result, hasResult := outputs["result"]
	assert.True(t, hasResult, "Should have result output")
	assert.Contains(t, result, "steps.test.outputs.value")
}

// TestExtractPreActivationCustomFields_InvalidSteps tests error handling for invalid steps
func TestExtractPreActivationCustomFields_InvalidSteps(t *testing.T) {
	compiler := NewCompiler()

	jobs := map[string]any{
		"pre-activation": map[string]any{
			"steps": "invalid", // Should be an array
		},
	}

	steps, outputs, err := compiler.extractPreActivationCustomFields(jobs)
	require.Error(t, err, "Should return error for invalid steps format")
	require.ErrorContains(t, err, "must be an array", "Error should mention array requirement")
	assert.Empty(t, steps, "Should have no steps with invalid format")
	assert.Empty(t, outputs, "Should have no outputs with invalid format")
}

// TestBuildPreActivationJob_Integration tests complete pre-activation job with multiple features
func TestBuildPreActivationJob_Integration(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:       "Integration Test Workflow",
		Command:    []string{"test"},
		StopTime:   "2024-12-31T23:59:59Z",
		AIReaction: "eyes",
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{},
		},
	}

	job, err := compiler.buildPreActivationJob(workflowData, true)
	require.NoError(t, err, "buildPreActivationJob should succeed with all features")
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "\n")

	// Should have membership check and stop-time check (reaction moved to activation job)
	assert.Contains(t, stepsStr, "setup", "Should include setup step")
	assert.Contains(t, stepsStr, constants.CheckMembershipStepID.String(), "Should include membership check")
	assert.Contains(t, stepsStr, constants.CheckStopTimeStepID.String(), "Should include stop-time check")
	// Reaction step is in activation job now, not pre-activation
	assert.NotContains(t, stepsStr, "GH_AW_REACTION", "Reaction step should NOT be in pre-activation job")

	// Pre-activation job should NOT have reaction write permissions (reaction moved to activation)
	assert.NotContains(t, job.Permissions, "issues: write", "Pre-activation should not have issues write for reaction")

	// Should have activated output
	_, hasActivated := job.Outputs["activated"]
	assert.True(t, hasActivated, "Should have activated output")
}

// TestBuildActivationJob_WithWorkflowRunRepoSafety tests activation with workflow_run repo safety
func TestBuildActivationJob_WithWorkflowRunRepoSafety(t *testing.T) {
	compiler := NewCompiler()

	workflowData := &WorkflowData{
		Name:            "Test Workflow",
		Command:         []string{"echo", "test"},
		MarkdownContent: "# Test\n\nContent",
	}

	// Test with workflow_run repo safety enabled
	job, err := compiler.buildActivationJob(workflowData, false, "workflow_run", "test.lock.yml")
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsStr := strings.Join(job.Steps, "\n")
	// Should include repository validation for workflow_run
	assert.NotEmpty(t, stepsStr)
}

// TestBuildMainJob_EngineSpecific tests main job with different engines
func TestBuildMainJob_EngineSpecific(t *testing.T) {
	tests := []struct {
		name   string
		engine string
	}{
		{
			name:   "copilot engine",
			engine: "copilot",
		},
		{
			name:   "claude engine",
			engine: "claude",
		},
		{
			name:   "codex engine",
			engine: "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()

			workflowData := &WorkflowData{
				Name:            "Test Workflow",
				Command:         []string{"echo", "test"},
				MarkdownContent: "# Test\n\nContent",
				AI:              tt.engine,
			}

			job, err := compiler.buildMainJob(workflowData, false)
			require.NoError(t, err, "buildMainJob should succeed for engine %s", tt.engine)
			require.NotNil(t, job)
			assert.NotEmpty(t, job.Steps, "Should have steps for engine %s", tt.engine)
		})
	}
}

//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// extractCustomJobNeeds Tests
// ========================================

func TestExtractCustomJobNeeds(t *testing.T) {
	tests := []struct {
		name           string
		configMap      map[string]any
		expectedNeeds  []string
		expectedReturn bool
	}{
		{
			name:           "no needs field",
			configMap:      map[string]any{"runs-on": "ubuntu-latest"},
			expectedNeeds:  nil,
			expectedReturn: false,
		},
		{
			name:           "needs as single string",
			configMap:      map[string]any{"needs": "activation"},
			expectedNeeds:  []string{"activation"},
			expectedReturn: true,
		},
		{
			name:           "needs as array of strings",
			configMap:      map[string]any{"needs": []any{"job1", "job2"}},
			expectedNeeds:  []string{"job1", "job2"},
			expectedReturn: true,
		},
		{
			name:           "needs as array with non-string element ignored",
			configMap:      map[string]any{"needs": []any{"job1", 42}},
			expectedNeeds:  []string{"job1"},
			expectedReturn: true,
		},
		{
			name:           "needs as empty array",
			configMap:      map[string]any{"needs": []any{}},
			expectedNeeds:  nil,
			expectedReturn: true,
		},
		{
			name:           "needs as integer (not parsed as list)",
			configMap:      map[string]any{"needs": 42},
			expectedNeeds:  nil,
			expectedReturn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test-job"}
			result := extractCustomJobNeeds(job, tt.configMap)
			assert.Equal(t, tt.expectedReturn, result)
			assert.Equal(t, tt.expectedNeeds, job.Needs)
		})
	}
}

// ========================================
// buildCustomJob Tests
// ========================================

func TestBuildCustomJob_AutoActivationDependency(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: string(constants.ActivationJobName)}))

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"runs-on": "ubuntu-latest",
		"steps":   []any{map[string]any{"run": "echo hello"}},
	}

	job, err := compiler.buildCustomJob(
		"my-job",
		configMap,
		data,
		true, // activation job was created
		map[string]struct{}{},
		map[string]struct{}{},
	)

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "my-job", job.Name)
	assert.Contains(t, job.Needs, string(constants.ActivationJobName),
		"job with no explicit needs should auto-depend on activation")
}

func TestBuildCustomJob_ExplicitNeedsPreventAutoActivation(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: string(constants.ActivationJobName)}))

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"runs-on": "ubuntu-latest",
		"needs":   "some-other-job",
		"steps":   []any{map[string]any{"run": "echo hello"}},
	}

	job, err := compiler.buildCustomJob(
		"my-job",
		configMap,
		data,
		true,
		map[string]struct{}{},
		map[string]struct{}{},
	)

	require.NoError(t, err)
	require.NotNil(t, job)
	// explicit needs means activation is NOT auto-added
	assert.NotContains(t, job.Needs, string(constants.ActivationJobName))
	assert.Contains(t, job.Needs, "some-other-job")
}

func TestBuildCustomJob_PromptReferencedJobNoAutoActivation(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: string(constants.ActivationJobName)}))

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"runs-on": "ubuntu-latest",
		"steps":   []any{map[string]any{"run": "echo compute"}},
	}

	// "precompute" is referenced in markdown body; must NOT get activation dep
	promptReferenced := map[string]struct{}{"precompute": {}}
	job, err := compiler.buildCustomJob(
		"precompute",
		configMap,
		data,
		true,
		promptReferenced,
		map[string]struct{}{},
	)

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.NotContains(t, job.Needs, string(constants.ActivationJobName))
}

func TestBuildCustomJob_NoActivationJobCreated(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"runs-on": "ubuntu-latest",
	}

	job, err := compiler.buildCustomJob(
		"my-job",
		configMap,
		data,
		false, // activation job was NOT created
		map[string]struct{}{},
		map[string]struct{}{},
	)

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Empty(t, job.Needs, "no activation → no auto needs")
}

func TestBuildCustomJob_InvalidTimeoutMinutesError(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"runs-on":         "ubuntu-latest",
		"timeout-minutes": "not-an-expression",
	}

	_, err := compiler.buildCustomJob(
		"my-job",
		configMap,
		data,
		false,
		map[string]struct{}{},
		map[string]struct{}{},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "timeout-minutes")
}

func TestBuildCustomJob_InputsNotSupportedError(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"runs-on": "ubuntu-latest",
		"inputs": map[string]any{
			"my-input": map[string]any{"description": "an input"},
		},
		"steps": []any{map[string]any{"run": "echo hello"}},
	}

	_, err := compiler.buildCustomJob(
		"my-job",
		configMap,
		data,
		false,
		map[string]struct{}{},
		map[string]struct{}{},
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "jobs.my-job.inputs")
	require.ErrorContains(t, err, "inputs are not supported on jobs")
}

func TestBuildCustomJob_UsesReusableWorkflow(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: string(constants.ActivationJobName)}))

	data := &WorkflowData{Name: "Test"}
	configMap := map[string]any{
		"uses": "./.github/workflows/reusable.yml",
		"with": map[string]any{"input1": "value1"},
	}

	job, err := compiler.buildCustomJob(
		"call-reusable",
		configMap,
		data,
		true,
		map[string]struct{}{},
		map[string]struct{}{},
	)

	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, "./.github/workflows/reusable.yml", job.Uses)
	assert.Equal(t, map[string]any{"input1": "value1"}, job.With)
}

// ========================================
// configureCustomJobSteps Tests
// ========================================

func TestConfigureCustomJobSteps_NoStepsFields(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{"runs-on": "ubuntu-latest"}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.NoError(t, err)
	assert.Empty(t, job.Steps, "no steps fields means no steps added")
}

func TestConfigureCustomJobSteps_WithStepsOnly(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"steps": []any{
			map[string]any{"name": "Step 1", "run": "echo hello"},
		},
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.NoError(t, err)
	assert.NotEmpty(t, job.Steps)
	// GHES host step should be present
	assert.True(t, func() bool {
		for _, s := range job.Steps {
			if strings.Contains(s, "GH_HOST") {
				return true
			}
		}
		return false
	}(), "GHES host config step should be injected")
}

func TestConfigureCustomJobSteps_WithSetupStepsOnly(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"setup-steps": []any{
			map[string]any{"name": "Setup Step", "run": "echo setup"},
		},
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.NoError(t, err)
	assert.NotEmpty(t, job.Steps)
}

func TestConfigureCustomJobSteps_WithPreStepsOnly(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"pre-steps": []any{
			map[string]any{"name": "Pre Step", "run": "echo pre"},
		},
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.NoError(t, err)
	assert.NotEmpty(t, job.Steps)
}

func TestConfigureCustomJobSteps_AllThreeStepTypes(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"setup-steps": []any{map[string]any{"name": "S", "run": "echo setup"}},
		"pre-steps":   []any{map[string]any{"name": "P", "run": "echo pre"}},
		"steps":       []any{map[string]any{"name": "R", "run": "echo regular"}},
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.NoError(t, err)
	assert.NotEmpty(t, job.Steps)
	all := strings.Join(job.Steps, "\n")
	// setup step name, pre step name, and regular step name should all appear
	assert.Contains(t, all, "echo setup")
	assert.Contains(t, all, "echo pre")
	assert.Contains(t, all, "echo regular")
}

func TestConfigureCustomJobSteps_InvalidStepsType(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"steps": "not-a-list", // invalid: must be an array
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.Error(t, err)
	require.ErrorContains(t, err, "steps")
}

func TestConfigureCustomJobSteps_InvalidPreStepsType(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"pre-steps": "not-a-list",
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.Error(t, err)
	require.ErrorContains(t, err, "pre-steps")
}

func TestConfigureCustomJobSteps_InvalidSetupStepsType(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Name: "Test"}
	job := &Job{Name: "my-job"}
	configMap := map[string]any{
		"setup-steps": "not-a-list",
	}

	err := compiler.configureCustomJobSteps(job, "my-job", configMap, data)

	require.Error(t, err)
	require.ErrorContains(t, err, "setup-steps")
}

// ========================================
// applyBuiltinJobAugmentations Tests
// ========================================

func TestApplyBuiltinJobNeedsAugmentations_NilData(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.applyBuiltinJobAugmentations(nil)
	require.NoError(t, err)
}

func TestApplyBuiltinJobNeedsAugmentations_NilJobs(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{Jobs: nil}
	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
}

func TestApplyBuiltinJobNeedsAugmentations_NonBuiltinJobIgnored(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Jobs: map[string]any{
			"my-custom-job": map[string]any{
				"needs": "some-dependency",
			},
		},
	}

	// Non-builtin job in Jobs map should be silently skipped
	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
}

func TestApplyBuiltinJobNeedsAugmentations_BuiltinNoNeeds(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	activationJob := &Job{Name: string(constants.ActivationJobName), Needs: []string{"pre_activation"}}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			// activation job config without a "needs" key
			string(constants.ActivationJobName): map[string]any{
				"pre-steps": []any{map[string]any{"run": "echo hi"}},
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// Existing needs unchanged
	assert.Equal(t, []string{"pre_activation"}, activationJob.Needs)
}

func TestApplyBuiltinJobNeedsAugmentations_AddsNeedsAsString(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	customJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(customJob))
	activationJob := &Job{Name: string(constants.ActivationJobName), Needs: []string{"pre_activation"}}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				"needs": "build",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	assert.Contains(t, activationJob.Needs, "build")
	assert.Contains(t, activationJob.Needs, "pre_activation", "existing needs should be preserved")
}

func TestApplyBuiltinJobNeedsAugmentations_AddsNeedsAsArray(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	customJob1 := &Job{Name: "build"}
	customJob2 := &Job{Name: "test"}
	require.NoError(t, compiler.jobManager.AddJob(customJob1))
	require.NoError(t, compiler.jobManager.AddJob(customJob2))
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				"needs": []any{"build", "test"},
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	assert.Contains(t, activationJob.Needs, "build")
	assert.Contains(t, activationJob.Needs, "test")
}

func TestApplyBuiltinJobNeedsAugmentations_DeduplicatesNeeds(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	customJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(customJob))
	activationJob := &Job{Name: string(constants.ActivationJobName), Needs: []string{"build"}}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				"needs": "build", // already in needs
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// Should only have "build" once
	count := 0
	for _, n := range activationJob.Needs {
		if n == "build" {
			count++
		}
	}
	assert.Equal(t, 1, count, "duplicate needs should be de-duplicated")
}

func TestApplyBuiltinJobNeedsAugmentations_UnknownJobError(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				"needs": "nonexistent-job",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "unknown job")
}

func TestApplyBuiltinJobNeedsAugmentations_SelfReferenceError(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				"needs": string(constants.ActivationJobName),
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "should not depend on itself")
}

func TestApplyBuiltinJobNeedsAugmentations_TargetJobNotInManagerError(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	// activation job NOT added to job manager; only a custom job is
	customJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(customJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				"needs": "build",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "requires an existing built-in job")
}

func TestApplyBuiltinJobNeedsAugmentations_InvalidConfigNotMap(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): "not-a-map",
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "expects an object")
}

func TestApplyBuiltinJobNeedsAugmentations_HyphenAliasNormalized(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	customJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(customJob))
	// pre_activation (underscore) is the canonical builtin name
	preActivationJob := &Job{Name: string(constants.PreActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(preActivationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			// Use hyphen alias "pre-activation" → should normalize to "pre_activation"
			string(constants.PreActivationHyphenJobName): map[string]any{
				"needs": "build",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	assert.Contains(t, preActivationJob.Needs, "build")
}

func TestApplyBuiltinJobNeedsAugmentations_AddsIfCondition(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{Name: string(constants.AgentJobName)}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"if": "needs.build.outputs.outcome == 'failure'",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	assert.Equal(t, "needs.build.outputs.outcome == 'failure'", agentJob.If)
}

func TestApplyBuiltinJobAugmentations_OverridesTimeoutMinutes(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{
		Name:                     string(constants.AgentJobName),
		TimeoutMinutesExpression: "${{ fromJSON(vars.GH_AW_DEFAULT_TIMEOUT_MINUTES || '60') }}",
	}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"timeout-minutes": 90,
			},
		},
	}

	require.NoError(t, compiler.applyBuiltinJobAugmentations(data))
	assert.Equal(t, 90, agentJob.TimeoutMinutes)
	assert.Empty(t, agentJob.TimeoutMinutesExpression)
}

func TestApplyBuiltinJobAugmentations_RejectsTimeoutMinutesExpression(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	detectionJob := &Job{
		Name:                     string(constants.DetectionJobName),
		TimeoutMinutesExpression: "${{ fromJSON(vars.GH_AW_DEFAULT_TIMEOUT_MINUTES || '60') }}",
	}
	require.NoError(t, compiler.jobManager.AddJob(detectionJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.DetectionJobName): map[string]any{
				"timeout-minutes": "${{ inputs.detection-timeout }}",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a positive integer")
}

func TestApplyBuiltinJobNeedsAugmentations_CombinesIfCondition(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{
		Name: string(constants.AgentJobName),
		If:   "${{ github.event_name == 'pull_request' }}",
	}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"if": "needs.build.outputs.outcome == 'failure'",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	assert.Equal(t, "(github.event_name == 'pull_request') && (needs.build.outputs.outcome == 'failure')", agentJob.If)
}

func TestApplyBuiltinJobNeedsAugmentations_InvalidIfTypeError(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{Name: string(constants.AgentJobName)}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"if": true,
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "jobs.agent.if expects a string")
}

func TestApplyBuiltinJobNeedsAugmentations_IfPrefixStripped(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{Name: string(constants.AgentJobName)}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				// Simulate a user writing "if: <expression>" inside the frontmatter value.
				"if": "if: needs.build.outputs.outcome == 'failure'",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// The "if: " prefix must be stripped; Job.If holds only the bare expression.
	assert.Equal(t, "needs.build.outputs.outcome == 'failure'", agentJob.If)
	assert.NotContains(t, agentJob.If, "if: ", "Job.If must not contain the 'if: ' prefix")
}

func TestApplyBuiltinJobNeedsAugmentations_TargetJobNotInManager_IfOnlyReportsIfField(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	// Activation job is NOT in the manager; only a custom build job is.
	buildJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(buildJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				// Only "if" is configured, no "needs".
				"if": "needs.build.outputs.outcome == 'failure'",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "jobs.activation.if")
	require.ErrorContains(t, err, "requires an existing built-in job")
}

func TestApplyBuiltinJobNeedsAugmentations_TargetJobNotInManager_NeedsOnlyReportsNeedsField(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	buildJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(buildJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.ActivationJobName): map[string]any{
				// Only "needs" is configured, no "if".
				"needs": "build",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.Error(t, err)
	require.ErrorContains(t, err, "jobs.activation.needs")
	require.ErrorContains(t, err, "requires an existing built-in job")
}

func TestApplyBuiltinJobNeedsAugmentations_StatusFuncAddsSuccessGuards(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	// Simulate agent job with activation as a compiler-owned prerequisite.
	agentJob := &Job{
		Name:  string(constants.AgentJobName),
		Needs: []string{string(constants.ActivationJobName)},
	}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))
	buildJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(buildJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"needs": "build",
				// failure() is a status function; without a guard, the implicit
				// success() on "activation" would be silently dropped by GitHub Actions.
				"if": "needs.build.outputs.outcome == 'failure'",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// The user condition does not contain a status function, so no extra guards are needed.
	assert.Equal(t, "needs.build.outputs.outcome == 'failure'", agentJob.If)
}

func TestApplyBuiltinJobNeedsAugmentations_StatusFuncFailureAddsSuccessGuards(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{
		Name:  string(constants.AgentJobName),
		Needs: []string{string(constants.ActivationJobName)},
	}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))
	buildJob := &Job{Name: "build"}
	require.NoError(t, compiler.jobManager.AddJob(buildJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"needs": "build",
				// failure() is a status function; without a guard, the implicit
				// success() on the compiler-owned "activation" job would be dropped.
				"if": "failure()",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// Explicit success guard must be added for the compiler-owned activation prerequisite.
	assert.Contains(t, agentJob.If, "needs.activation.result == 'success'")
	assert.Contains(t, agentJob.If, "failure()")
}

func TestApplyBuiltinJobNeedsAugmentations_StatusFuncAlwaysAddsSuccessGuards(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	agentJob := &Job{
		Name:  string(constants.AgentJobName),
		Needs: []string{string(constants.ActivationJobName)},
	}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			string(constants.AgentJobName): map[string]any{
				"if": "always()",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// always() must not bypass activation's implicit success check.
	assert.Contains(t, agentJob.If, "needs.activation.result == 'success'")
	assert.Contains(t, agentJob.If, "always()")
}

func TestApplyBuiltinJobNeedsAugmentations_StatusFuncKeepsCustomJobUnguarded(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()
	// probe is a custom job auto-wired as an agent prerequisite by the compiler.
	agentJob := &Job{
		Name:  string(constants.AgentJobName),
		Needs: []string{string(constants.ActivationJobName), "probe"},
	}
	require.NoError(t, compiler.jobManager.AddJob(agentJob))
	activationJob := &Job{Name: string(constants.ActivationJobName)}
	require.NoError(t, compiler.jobManager.AddJob(activationJob))
	probeJob := &Job{Name: "probe"}
	require.NoError(t, compiler.jobManager.AddJob(probeJob))

	data := &WorkflowData{
		Jobs: map[string]any{
			"probe": map[string]any{"runs-on": "ubuntu-latest"},
			string(constants.AgentJobName): map[string]any{
				"if": "always()",
			},
		},
	}

	err := compiler.applyBuiltinJobAugmentations(data)
	require.NoError(t, err)
	// activation is compiler-owned and stays guarded.
	assert.Contains(t, agentJob.If, "needs.activation.result == 'success'")
	// probe is author-owned, so always() keeps the agent running after a failing probe.
	assert.NotContains(t, agentJob.If, "needs.probe.result == 'success'")
}

// ========================================
// normalizeBuiltinJobAlias Tests
// ========================================

func TestNormalizeBuiltinJobAlias(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{string(constants.PreActivationHyphenJobName), string(constants.PreActivationJobName)},
		{string(constants.SafeOutputsHyphenJobName), string(constants.SafeOutputsJobName)},
		{"activation", "activation"},
		{"custom-job", "custom-job"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeBuiltinJobAlias(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ========================================
// extractBuiltinJobNeedsAugmentation Tests
// ========================================

func TestExtractBuiltinJobNeedsAugmentation(t *testing.T) {
	tests := []struct {
		name          string
		configMap     map[string]any
		expectedNeeds []string
		expectError   bool
	}{
		{
			name:          "no needs key",
			configMap:     map[string]any{},
			expectedNeeds: nil,
		},
		{
			name:          "needs is nil",
			configMap:     map[string]any{"needs": nil},
			expectedNeeds: nil,
		},
		{
			name:          "needs as string",
			configMap:     map[string]any{"needs": "build"},
			expectedNeeds: []string{"build"},
		},
		{
			name:          "needs as slice of strings",
			configMap:     map[string]any{"needs": []any{"build", "test"}},
			expectedNeeds: []string{"build", "test"},
		},
		{
			name:        "needs slice with non-string element",
			configMap:   map[string]any{"needs": []any{"build", 42}},
			expectError: true,
		},
		{
			name:        "needs as unsupported type",
			configMap:   map[string]any{"needs": 123},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needs, err := extractBuiltinJobNeedsAugmentation("activation", tt.configMap)
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedNeeds, needs)
			}
		})
	}
}

// ========================================
// insertSetupStepsAtStart Tests
// ========================================

func TestInsertSetupStepsAtStartCustomJobs(t *testing.T) {
	tests := []struct {
		name        string
		steps       []string
		setupSteps  []string
		expectFirst string
	}{
		{
			name:       "empty setup steps returns original",
			steps:      []string{"step1", "step2"},
			setupSteps: nil,
		},
		{
			name:        "setup steps prepended before existing steps",
			steps:       []string{"step1", "step2"},
			setupSteps:  []string{"setup1"},
			expectFirst: "setup1",
		},
		{
			name:        "setup steps with empty original",
			steps:       nil,
			setupSteps:  []string{"setup1"},
			expectFirst: "setup1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := insertSetupStepsAtStart(tt.steps, tt.setupSteps)
			if len(tt.setupSteps) == 0 {
				assert.Equal(t, tt.steps, result)
				return
			}
			require.NotEmpty(t, result)
			assert.Equal(t, tt.expectFirst, result[0])
			// All original steps should still be present
			for _, s := range tt.steps {
				assert.Contains(t, result, s)
			}
		})
	}
}

// ========================================
// insertPreStepsAtEarliestBoundary Tests
// ========================================

func TestInsertPreStepsAtEarliestBoundary(t *testing.T) {
	tests := []struct {
		name           string
		steps          []string
		preSteps       []string
		expectedLength int
		expectContains []string
	}{
		{
			name:           "empty pre-steps returns original unchanged",
			steps:          []string{"- run: echo step1"},
			preSteps:       nil,
			expectedLength: 1,
			expectContains: []string{"- run: echo step1"},
		},
		{
			name:           "no existing steps just returns pre-steps",
			steps:          nil,
			preSteps:       []string{"- run: echo pre"},
			expectedLength: 1,
			expectContains: []string{"- run: echo pre"},
		},
		{
			name: "inserts before checkout step",
			steps: []string{
				"- name: other step",
				"  run: echo other",
				"- uses: actions/checkout@abc123",
				"  with:",
				"    persist-credentials: false",
			},
			preSteps:       []string{"- name: pre", "  run: echo pre"},
			expectedLength: 7,
		},
		{
			name: "inserts at end when no checkout or token-mint",
			steps: []string{
				"- run: echo step1",
			},
			preSteps:       []string{"- run: echo pre"},
			expectedLength: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := insertPreStepsAtEarliestBoundary(tt.steps, tt.preSteps)
			assert.Len(t, result, tt.expectedLength)
			for _, expected := range tt.expectContains {
				assert.Contains(t, result, expected)
			}
		})
	}
}

func TestInsertActivationStepsBeforeArtifactStaging(t *testing.T) {
	steps := []string{
		"      - name: Generate prompt\n",
		"        run: echo prompt\n",
		"      - name: Stage ambient folders for activation artifact\n",
		"        run: echo stage\n",
		"      - name: Upload activation artifact\n",
		"        run: echo upload\n",
	}
	activationSteps := []string{
		"      - name: Initialize Squad team\n",
		"        run: echo squad\n",
	}

	result := insertActivationStepsBeforeArtifactStaging(string(constants.ActivationJobName), steps, activationSteps)

	squadIndex := indexOfStep(result, "Initialize Squad team")
	stageIndex := indexOfStep(result, "Stage ambient folders for activation artifact")
	uploadIndex := indexOfStep(result, "Upload activation artifact")
	require.NotEqual(t, -1, squadIndex)
	require.NotEqual(t, -1, stageIndex)
	require.NotEqual(t, -1, uploadIndex)
	assert.Less(t, squadIndex, stageIndex, "activation steps should run before ambient folders are staged")
	assert.Less(t, stageIndex, uploadIndex, "ambient folders should still stage before upload")
}

func indexOfStep(steps []string, needle string) int {
	for i, step := range steps {
		if strings.Contains(step, needle) {
			return i
		}
	}
	return -1
}

// ========================================
// extractCustomJobTimeoutMinutes Tests
// ========================================

func TestExtractCustomJobTimeoutMinutes(t *testing.T) {
	tests := []struct {
		name            string
		configMap       map[string]any
		expectedTimeout int
		expectedExpr    string
		expectError     bool
	}{
		{
			name:      "no timeout field",
			configMap: map[string]any{},
		},
		{
			name:            "timeout as int",
			configMap:       map[string]any{"timeout-minutes": 30},
			expectedTimeout: 30,
		},
		{
			name:            "timeout as float64",
			configMap:       map[string]any{"timeout-minutes": float64(45)},
			expectedTimeout: 45,
		},
		{
			name:         "timeout as GitHub expression",
			configMap:    map[string]any{"timeout-minutes": "${{ inputs.timeout }}"},
			expectedExpr: "${{ inputs.timeout }}",
		},
		{
			name:        "timeout as non-expression string",
			configMap:   map[string]any{"timeout-minutes": "not-a-number"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test-job"}
			err := extractCustomJobTimeoutMinutes(job, "test-job", tt.configMap)
			if tt.expectError {
				require.Error(t, err)
				require.ErrorContains(t, err, "timeout-minutes")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedTimeout, job.TimeoutMinutes)
				assert.Equal(t, tt.expectedExpr, job.TimeoutMinutesExpression)
			}
		})
	}
}

// ========================================
// extractCustomJobConcurrency Tests
// ========================================

func TestExtractCustomJobConcurrency(t *testing.T) {
	tests := []struct {
		name                   string
		configMap              map[string]any
		expectedConcurrency    string
		expectCancelInProgress bool
	}{
		{
			name:      "no concurrency field",
			configMap: map[string]any{},
		},
		{
			name:                "concurrency as string",
			configMap:           map[string]any{"concurrency": "my-group"},
			expectedConcurrency: "concurrency: my-group",
		},
		{
			name: "concurrency as map without cancel-in-progress defaults to false",
			configMap: map[string]any{
				"concurrency": map[string]any{"group": "my-group"},
			},
			expectCancelInProgress: true, // default should be injected
		},
		{
			name: "concurrency as map with explicit cancel-in-progress keeps it",
			configMap: map[string]any{
				"concurrency": map[string]any{
					"group":              "my-group",
					"cancel-in-progress": true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test-job"}
			err := extractCustomJobConcurrency(job, "test-job", tt.configMap)
			require.NoError(t, err)
			if tt.expectedConcurrency != "" {
				assert.Equal(t, tt.expectedConcurrency, job.Concurrency)
			}
			if tt.expectCancelInProgress {
				// The concurrency map should now include cancel-in-progress: false
				assert.Contains(t, job.Concurrency, "cancel-in-progress: false")
			}
		})
	}
}

// ========================================
// extractCustomJobContinueOnError Tests
// ========================================

func TestExtractCustomJobContinueOnError(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		expected *bool
	}{
		{
			name:     "not set",
			config:   map[string]any{},
			expected: nil,
		},
		{
			name:     "set to true",
			config:   map[string]any{"continue-on-error": true},
			expected: func() *bool { v := true; return &v }(),
		},
		{
			name:     "set to false",
			config:   map[string]any{"continue-on-error": false},
			expected: func() *bool { v := false; return &v }(),
		},
		{
			name:     "non-bool value ignored",
			config:   map[string]any{"continue-on-error": "yes"},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test"}
			extractCustomJobContinueOnError(job, tt.config)
			assert.Equal(t, tt.expected, job.ContinueOnError)
		})
	}
}

// ========================================
// extractCustomJobEnvironment Tests
// ========================================

func TestExtractCustomJobEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		configMap   map[string]any
		expectedEnv string
	}{
		{
			name:        "no environment field",
			configMap:   map[string]any{},
			expectedEnv: "",
		},
		{
			name:        "string environment",
			configMap:   map[string]any{"environment": "production"},
			expectedEnv: "environment: production",
		},
		{
			name: "object environment",
			configMap: map[string]any{
				"environment": map[string]any{
					"name": "production",
					"url":  "https://example.com",
				},
			},
			expectedEnv: "environment:\n      name: production\n      url: https://example.com",
		},
		{
			name:        "invalid environment is ignored",
			configMap:   map[string]any{"environment": []any{"production"}},
			expectedEnv: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test"}
			require.NoError(t, extractCustomJobEnvironment(job, "test", tt.configMap))
			assert.Equal(t, tt.expectedEnv, job.Environment)
		})
	}
}

// ========================================
// extractCustomJobOutputs Tests
// ========================================

func TestExtractCustomJobOutputs(t *testing.T) {
	tests := []struct {
		name            string
		configMap       map[string]any
		expectedOutputs map[string]string
	}{
		{
			name:            "no outputs field",
			configMap:       map[string]any{},
			expectedOutputs: nil,
		},
		{
			name: "outputs map with string values",
			configMap: map[string]any{
				"outputs": map[string]any{
					"result": "${{ steps.run.outputs.result }}",
				},
			},
			expectedOutputs: map[string]string{
				"result": "${{ steps.run.outputs.result }}",
			},
		},
		{
			name: "outputs map with non-string value ignored",
			configMap: map[string]any{
				"outputs": map[string]any{
					"valid":   "some-value",
					"invalid": 42,
				},
			},
			expectedOutputs: map[string]string{"valid": "some-value"},
		},
		{
			name:      "outputs not a map",
			configMap: map[string]any{"outputs": "not-a-map"},
			// no outputs set (non-map is silently ignored)
			expectedOutputs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test"}
			extractCustomJobOutputs(job, "test", tt.configMap)
			assert.Equal(t, tt.expectedOutputs, job.Outputs)
		})
	}
}

// ========================================
// extractCustomJobEnv Tests
// ========================================

func TestExtractCustomJobEnv(t *testing.T) {
	tests := []struct {
		name        string
		configMap   map[string]any
		expectedEnv map[string]string
	}{
		{
			name:        "no env field",
			configMap:   map[string]any{},
			expectedEnv: nil,
		},
		{
			name: "env with string values",
			configMap: map[string]any{
				"env": map[string]any{
					"FOO": "bar",
					"BAZ": "qux",
				},
			},
			expectedEnv: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			name: "env with non-string value serialized",
			configMap: map[string]any{
				"env": map[string]any{
					"ITEMS": []any{"a", "b"},
				},
			},
			// non-string values get marshaled; just verify the key is present
			expectedEnv: nil, // we'll check separately
		},
		{
			name:        "env not a map",
			configMap:   map[string]any{"env": "not-a-map"},
			expectedEnv: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{Name: "test"}
			extractCustomJobEnv(job, tt.configMap)
			if tt.name == "env with non-string value serialized" {
				require.NotNil(t, job.Env)
				assert.Contains(t, job.Env, "ITEMS")
			} else {
				assert.Equal(t, tt.expectedEnv, job.Env)
			}
		})
	}
}

// ========================================
// ensureCheckoutPersistCredentials Tests
// ========================================

func TestEnsureCheckoutPersistCredentials(t *testing.T) {
	tests := []struct {
		name     string
		stepMap  map[string]any
		expected any // expected persist-credentials value under with
	}{
		{
			name:     "non-checkout action unchanged",
			stepMap:  map[string]any{"uses": "some/action@v1"},
			expected: nil, // no with.persist-credentials added
		},
		{
			name:     "checkout without with gets persist-credentials: false",
			stepMap:  map[string]any{"uses": "actions/checkout@abc123"},
			expected: false,
		},
		{
			name: "checkout with explicit persist-credentials kept",
			stepMap: map[string]any{
				"uses": "actions/checkout@abc123",
				"with": map[string]any{"persist-credentials": true},
			},
			expected: true,
		},
		{
			name: "checkout with empty with gets persist-credentials: false",
			stepMap: map[string]any{
				"uses": "actions/checkout@abc123",
				"with": map[string]any{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ensureCheckoutPersistCredentials(tt.stepMap)

			if tt.expected == nil {
				// non-checkout: no with.persist-credentials should have been set
				if withRaw, ok := tt.stepMap["with"]; ok {
					if withMap, ok := withRaw.(map[string]any); ok {
						_, hasCred := withMap["persist-credentials"]
						assert.False(t, hasCred)
					}
				}
			} else {
				withRaw, ok := tt.stepMap["with"]
				require.True(t, ok, "with field should exist")
				withMap, ok := withRaw.(map[string]any)
				require.True(t, ok)
				assert.Equal(t, tt.expected, withMap["persist-credentials"])
			}
		})
	}
}

// ========================================
// isCheckoutAction Tests
// ========================================

func TestIsCheckoutAction(t *testing.T) {
	tests := []struct {
		uses     string
		expected bool
	}{
		{"actions/checkout@abc123", true},
		{"actions/checkout@v4", true},
		{"actions/checkout", true},
		{"ACTIONS/CHECKOUT@V4", true}, // case-insensitive
		{"actions/setup-node@v4", false},
		{"", false},
		{"custom/checkout@v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.uses, func(t *testing.T) {
			assert.Equal(t, tt.expected, isCheckoutAction(tt.uses))
		})
	}
}

// ========================================
// validateRestrictedBuiltinSetupSteps Tests
// ========================================

func TestValidateRestrictedBuiltinSetupSteps(t *testing.T) {
	tests := []struct {
		name          string
		jobName       string
		hasSetupSteps bool
		expectError   bool
	}{
		{
			name:          "no setup steps allowed always",
			jobName:       string(constants.ActivationJobName),
			hasSetupSteps: false,
			expectError:   false,
		},
		{
			name:          "activation job with setup steps forbidden",
			jobName:       string(constants.ActivationJobName),
			hasSetupSteps: true,
			expectError:   true,
		},
		{
			name:          "pre_activation job with setup steps forbidden",
			jobName:       string(constants.PreActivationJobName),
			hasSetupSteps: true,
			expectError:   true,
		},
		{
			name:          "pre-activation hyphen alias with setup steps forbidden",
			jobName:       "pre-activation",
			hasSetupSteps: true,
			expectError:   true,
		},
		{
			name:          "custom job with setup steps allowed",
			jobName:       "my-custom-job",
			hasSetupSteps: true,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRestrictedBuiltinSetupSteps(tt.jobName, tt.hasSetupSteps)
			if tt.expectError {
				require.Error(t, err)
				require.ErrorContains(t, err, "setup-steps")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ========================================
// shouldSkipCustomJob Tests
// ========================================

func TestShouldSkipCustomJob(t *testing.T) {
	t.Run("pre_activation job always skipped", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.jobManager = NewJobManager()
		assert.True(t, compiler.shouldSkipCustomJob(string(constants.PreActivationJobName)))
	})

	t.Run("pre-activation hyphen alias skipped", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.jobManager = NewJobManager()
		assert.True(t, compiler.shouldSkipCustomJob("pre-activation"))
	})

	t.Run("builtin job that already exists is skipped", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.jobManager = NewJobManager()
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: string(constants.ActivationJobName)}))
		assert.True(t, compiler.shouldSkipCustomJob(string(constants.ActivationJobName)))
	})

	t.Run("custom job that does not exist is not skipped", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.jobManager = NewJobManager()
		assert.False(t, compiler.shouldSkipCustomJob("my-new-job"))
	})
}

// ========================================
// configureCustomReusableWorkflow Tests
// ========================================

func TestConfigureCustomReusableWorkflow_BasicUses(t *testing.T) {
	job := &Job{Name: "call-worker"}
	configMap := map[string]any{
		"uses": "./.github/workflows/worker.yml",
	}

	err := configureCustomReusableWorkflow(job, "call-worker", "./.github/workflows/worker.yml", configMap)

	require.NoError(t, err)
	assert.Equal(t, "./.github/workflows/worker.yml", job.Uses)
}

func TestConfigureCustomReusableWorkflow_WithParameters(t *testing.T) {
	job := &Job{Name: "call-worker"}
	configMap := map[string]any{
		"uses": "./.github/workflows/worker.yml",
		"with": map[string]any{"env": "production"},
	}

	err := configureCustomReusableWorkflow(job, "call-worker", "./.github/workflows/worker.yml", configMap)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"env": "production"}, job.With)
}

func TestConfigureCustomReusableWorkflow_SecretsInherit(t *testing.T) {
	job := &Job{Name: "call-worker"}
	configMap := map[string]any{
		"uses":    "./.github/workflows/worker.yml",
		"secrets": "inherit",
	}

	err := configureCustomReusableWorkflow(job, "call-worker", "./.github/workflows/worker.yml", configMap)

	require.NoError(t, err)
	assert.True(t, job.SecretsInherit)
}

func TestConfigureCustomReusableWorkflow_RestoreMemoryNotSupported(t *testing.T) {
	job := &Job{Name: "call-worker"}
	configMap := map[string]any{
		"uses":           "./.github/workflows/worker.yml",
		"restore-memory": true,
	}

	err := configureCustomReusableWorkflow(job, "call-worker", "./.github/workflows/worker.yml", configMap)

	require.Error(t, err)
	require.ErrorContains(t, err, "restore-memory")
}

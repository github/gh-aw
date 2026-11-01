//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// State Push Job Tests (repo memory, cache memory, experiments, evals)
// ========================================

// TestPushRepoMemoryJobConditionalDetection verifies that push_repo_memory already uses
// always() and buildDetectionPassedCondition() (accepting 'success' or 'skipped') when
// detection is expression-controlled, so the job still runs when detection is skipped at runtime.
func TestPushRepoMemoryJobConditionalDetection(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	expr := "${{ inputs.enable-threat-detection }}"
	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		RepoMemoryConfig: &RepoMemoryConfig{
			Memories: []RepoMemoryEntry{
				{ID: "default"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{TitlePrefix: "[bot] "},
			ThreatDetection: &ThreatDetectionConfig{
				EnabledExpr: &expr,
			},
		},
	}

	// When detection is conditional, IsDetectionJobEnabled returns true
	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)
	if !threatDetectionEnabled {
		t.Fatal("IsDetectionJobEnabled should be true for conditional detection")
	}

	job, err := compiler.buildPushRepoMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		t.Fatalf("buildPushRepoMemoryJob returned error: %v", err)
	}
	if job == nil {
		t.Fatal("expected non-nil push_repo_memory job")
	}

	// Job condition must use always() so it runs even when detection is skipped at runtime
	if !strings.Contains(job.If, "always()") {
		t.Errorf("push_repo_memory if: %q should contain 'always()'", job.If)
	}
	// Job condition must accept detection being skipped
	if !strings.Contains(job.If, "'skipped'") {
		t.Errorf("push_repo_memory if: %q should accept 'skipped' detection result", job.If)
	}
	// Detection must be in Needs
	if !slices.Contains(job.Needs, string(constants.DetectionJobName)) {
		t.Errorf("push_repo_memory Needs %v should contain detection job", job.Needs)
	}
}

// TestUpdateCacheMemoryJobConditionalDetection verifies that update_cache_memory keeps always()
// but requires detection success (not skipped) when detection is expression-controlled.
// Detection always runs when the agent ran (even for noop), so 'success' is sufficient.
func TestUpdateCacheMemoryJobConditionalDetection(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	expr := "${{ inputs.enable-threat-detection }}"
	data := &WorkflowData{
		Name:   "Test Workflow",
		AI:     "copilot",
		RunsOn: "runs-on: ubuntu-latest",
		CacheMemoryConfig: &CacheMemoryConfig{
			Caches: []CacheMemoryEntry{
				{ID: "default"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			CreateIssues: &CreateIssuesConfig{TitlePrefix: "[bot] "},
			ThreatDetection: &ThreatDetectionConfig{
				EnabledExpr: &expr,
			},
		},
	}

	// When detection is conditional, IsDetectionJobEnabled returns true
	threatDetectionEnabled := IsDetectionJobEnabled(data.SafeOutputs)
	if !threatDetectionEnabled {
		t.Fatal("IsDetectionJobEnabled should be true for conditional detection")
	}

	job, err := compiler.buildUpdateCacheMemoryJob(data, threatDetectionEnabled)
	if err != nil {
		t.Fatalf("buildUpdateCacheMemoryJob returned error: %v", err)
	}
	if job == nil {
		t.Fatal("expected non-nil update_cache_memory job")
	}

	// Job condition must include always() so explicit condition checks are evaluated.
	if !strings.Contains(job.If, "always()") {
		t.Errorf("update_cache_memory if: %q should contain 'always()'", job.If)
	}
	// Job condition must require detection success and must not accept skipped.
	if !strings.Contains(job.If, "needs.detection.result == 'success'") {
		t.Errorf("update_cache_memory if: %q should require detection success", job.If)
	}
	if strings.Contains(job.If, "'skipped'") {
		t.Errorf("update_cache_memory if: %q must not accept skipped detection result", job.If)
	}
	// Detection must be in Needs
	if !slices.Contains(job.Needs, string(constants.DetectionJobName)) {
		t.Errorf("update_cache_memory Needs %v should contain detection job", job.Needs)
	}
}

// TestBuildPushExperimentsStateJob_RepoStorage verifies that buildPushExperimentsStateJob
// creates the push_experiments_state job when experiments are configured with repo storage.
func TestBuildPushExperimentsStateJob_RepoStorage(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:               "Test Workflow",
		WorkflowID:         "my-workflow",
		AI:                 "copilot",
		RunsOn:             "runs-on: ubuntu-latest",
		ExperimentsStorage: ExperimentsStorageRepo,
		Experiments: map[string][]string{
			"prompt_style": {"concise", "detailed"},
		},
	}

	job, err := compiler.buildPushExperimentsStateJob(data)
	require.NoError(t, err, "buildPushExperimentsStateJob should not return an error")
	require.NotNil(t, job, "buildPushExperimentsStateJob should return a job for repo storage")

	assert.Equal(t, "push_experiments_state", job.Name, "job name should be push_experiments_state")
	assert.Contains(t, job.If, "always()", "job condition should use always()")
	assert.Contains(t, job.Permissions, "contents: write", "job should have contents: write permission")
	assert.Contains(t, job.Needs, string(constants.ActivationJobName), "job should depend on activation job")

	// Branch name should use sanitized workflow ID
	stepsYAML := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsYAML, "experiments/myworkflow", "steps should reference sanitized branch name")
	assert.Contains(t, stepsYAML, "pattern: myworkflow-experiment", "experiment download should tolerate missing artifacts with pattern matching")
	assert.Contains(t, stepsYAML, "merge-multiple: true", "experiment download should preserve direct extraction into the state directory")
	assert.NotContains(t, stepsYAML, "name: myworkflow-experiment", "experiment download should not use exact artifact name downloads")
	assert.Contains(t, stepsYAML, "push_experiment_state.cjs", "steps should use push_experiment_state.cjs helper")
}

func TestBuildPushExperimentsStateJob_GHESUsesExactArtifactName(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetGHESCompat(true)
	compiler.configureGHESCompatibility()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:               "Test Workflow",
		WorkflowID:         "my-workflow",
		AI:                 "copilot",
		RunsOn:             "runs-on: ubuntu-latest",
		ExperimentsStorage: ExperimentsStorageRepo,
		Experiments: map[string][]string{
			"prompt_style": {"concise", "detailed"},
		},
	}

	job, err := compiler.buildPushExperimentsStateJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsYAML := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsYAML, "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0")
	assert.Contains(t, stepsYAML, "name: myworkflow-experiment")
	assert.NotContains(t, stepsYAML, "pattern: myworkflow-experiment")
	assert.NotContains(t, stepsYAML, "merge-multiple: true")
}

// TestBuildPushExperimentsStateJob_CacheStorage verifies that buildPushExperimentsStateJob
// returns nil when experiments use cache storage (no extra job needed).
func TestBuildPushExperimentsStateJob_CacheStorage(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:               "Test Workflow",
		WorkflowID:         "my-workflow",
		AI:                 "copilot",
		RunsOn:             "runs-on: ubuntu-latest",
		ExperimentsStorage: ExperimentsStorageCache,
		Experiments: map[string][]string{
			"prompt_style": {"concise", "detailed"},
		},
	}

	job, err := compiler.buildPushExperimentsStateJob(data)
	require.NoError(t, err, "buildPushExperimentsStateJob should not return an error")
	assert.Nil(t, job, "buildPushExperimentsStateJob should return nil for cache storage")
}

// TestBuildPushExperimentsStateJob_NoExperiments verifies that buildPushExperimentsStateJob
// returns nil when no experiments are configured.
func TestBuildPushExperimentsStateJob_NoExperiments(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:               "Test Workflow",
		WorkflowID:         "my-workflow",
		AI:                 "copilot",
		RunsOn:             "runs-on: ubuntu-latest",
		ExperimentsStorage: ExperimentsStorageRepo,
		Experiments:        map[string][]string{},
	}

	job, err := compiler.buildPushExperimentsStateJob(data)
	require.NoError(t, err, "buildPushExperimentsStateJob should not return an error")
	assert.Nil(t, job, "buildPushExperimentsStateJob should return nil when no experiments are defined")
}

// TestBuildMemoryManagementJobs_PushExperimentsIncludedInConclusion verifies that when
// experiments use repo storage, push_experiments_state is wired into conclusion job needs.
func TestBuildMemoryManagementJobs_PushExperimentsIncludedInConclusion(t *testing.T) {
	tmpDir := testutil.TempDir(t, "push-experiments-conclusion-test")

	frontmatter := `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
experiments:
  storage: repo
  prompt_style: [concise, detailed]
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(testFile, []byte(frontmatter), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	content, err := os.ReadFile(filepath.Join(tmpDir, "test.lock.yml"))
	require.NoError(t, err, "lock file should be created")

	yamlStr := string(content)

	// push_experiments_state job should exist
	assert.True(t, containsInNonCommentLines(yamlStr, "push_experiments_state:"), "push_experiments_state job should be present")

	// conclusion job should depend on push_experiments_state
	conclusionSection := extractJobSection(yamlStr, "conclusion")
	require.NotEmpty(t, conclusionSection, "conclusion job should be present")
	assert.Contains(t, conclusionSection, "push_experiments_state", "conclusion job should depend on push_experiments_state")
}

func TestBuildPushEvalsStateJob_WithEvals(t *testing.T) {
	compiler := NewCompiler()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:       "Test Workflow",
		WorkflowID: "my-workflow",
		AI:         "copilot",
		RunsOn:     "runs-on: ubuntu-latest",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it build?"},
			},
		},
	}

	job, err := compiler.buildPushEvalsStateJob(data)
	require.NoError(t, err, "buildPushEvalsStateJob should not return an error")
	require.NotNil(t, job, "buildPushEvalsStateJob should return a job when evals are configured")

	assert.Equal(t, "push_evals_state", job.Name, "job name should be push_evals_state")
	assert.Contains(t, job.If, "always()", "job condition should use always()")
	assert.Contains(t, job.If, "skipped", "job condition should run when evals is not skipped")
	assert.Contains(t, job.Permissions, "contents: write", "job should have contents: write permission")
	assert.Contains(t, job.Needs, string(constants.EvalsJobName), "job should depend on evals job")
	assert.Contains(t, job.Needs, string(constants.ActivationJobName), "job should depend on activation job")

	stepsYAML := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsYAML, "evals/myworkflow", "steps should reference sanitized evals branch name")
	assert.Contains(t, stepsYAML, "pattern: evals", "evals download should tolerate missing artifacts with pattern matching")
	assert.Contains(t, stepsYAML, "merge-multiple: true", "evals download should preserve direct extraction into the state directory")
	assert.NotContains(t, stepsYAML, "name: evals", "evals download should not use exact artifact name downloads")
	assert.Contains(t, stepsYAML, "GH_AW_STATE_FILES: evals.jsonl", "steps should configure evals filename")
	assert.NotContains(t, stepsYAML, "GH_AW_STATE_APPEND_FILES", "steps should use the state file itself for append-only history")
	assert.Contains(t, stepsYAML, "push_evals_state.cjs", "steps should guard the generic state writer with push_evals_state.cjs")
	assert.NotContains(t, stepsYAML, "evals_status.json", "the not-evaluated marker must not be pushed as answer history")
}

func TestBuildPushEvalsStateJob_GHESUsesExactArtifactName(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetGHESCompat(true)
	compiler.configureGHESCompatibility()
	compiler.jobManager = NewJobManager()

	data := &WorkflowData{
		Name:       "Test Workflow",
		WorkflowID: "my-workflow",
		AI:         "copilot",
		RunsOn:     "runs-on: ubuntu-latest",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it build?"},
			},
		},
	}

	job, err := compiler.buildPushEvalsStateJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)

	stepsYAML := strings.Join(job.Steps, "\n")
	assert.Contains(t, stepsYAML, "actions/download-artifact@a9bc5e6ef2cb54c177f32aa5726adaa15e7e2d59 # v3.1.0")
	assert.Contains(t, stepsYAML, "name: evals")
	assert.NotContains(t, stepsYAML, "pattern: evals")
	assert.NotContains(t, stepsYAML, "merge-multiple: true")
}

func TestBuildMemoryManagementJobs_PushEvalsIncludedInConclusion(t *testing.T) {
	tmpDir := testutil.TempDir(t, "push-evals-conclusion-test")

	frontmatter := `---
on: issues
permissions:
  contents: read
engine: copilot
strict: false
evals:
  - id: builds
    question: Does the generated code compile?
---

# Test Workflow

Test content`

	testFile := filepath.Join(tmpDir, "test-evals.md")
	require.NoError(t, os.WriteFile(testFile, []byte(frontmatter), 0644))

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile))

	content, err := os.ReadFile(filepath.Join(tmpDir, "test-evals.lock.yml"))
	require.NoError(t, err, "lock file should be created")

	yamlStr := string(content)

	assert.True(t, containsInNonCommentLines(yamlStr, "push_evals_state:"), "push_evals_state job should be present")
	conclusionSection := extractJobSection(yamlStr, "conclusion")
	require.NotEmpty(t, conclusionSection, "conclusion job should be present")
	assert.Contains(t, conclusionSection, "push_evals_state", "conclusion job should depend on push_evals_state")
	assert.Contains(t, conclusionSection, "GH_AW_EVALS_AIC: ${{ needs.evals.outputs.aic }}", "conclusion job should pass evals AIC to footer generation")
}

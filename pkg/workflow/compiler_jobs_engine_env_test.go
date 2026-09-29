//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========================================
// Engine Env Needs Expression Tests
// ========================================

// TestBuildMainJobEngineEnvNeedsExpression verifies that when engine.env values contain
// needs.<customJob>.outputs.* expressions, the referenced custom job is added as a direct
// dependency of the agent job (issue: agent 'needs' does not incorporate jobs in engine.env).
func TestBuildMainJobEngineEnvNeedsExpression(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				"RECEIVED_VALUE": "${{ needs.provide_value_to_agent.outputs.provided_value }}",
			},
		},
		Jobs: map[string]any{
			"provide_value_to_agent": map[string]any{
				"runs-on": "ubuntu-latest",
				"needs":   "pre_activation",
				"steps": []any{
					map[string]any{
						"run": `echo "provided_value=hello" >> "$GITHUB_OUTPUT"`,
					},
				},
			},
		},
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed")

	// The agent job must directly depend on provide_value_to_agent because engine.env
	// references its outputs; without this, needs.provide_value_to_agent would be undefined.
	assert.Contains(t, job.Needs, "provide_value_to_agent",
		"agent job must directly depend on provide_value_to_agent referenced in engine.env")
	assert.Contains(t, workflowData.AgentJobNeeds, "provide_value_to_agent",
		"agent dependencies must be available while generating safe-outputs config")
	assert.Contains(t, job.Needs, string(constants.ActivationJobName),
		"agent job must also depend on activation")
}

// TestBuildMainJobEngineEnvNeedsNotDuplicated verifies that a job referenced in both
// engine.env and regular job dependencies is not duplicated in the agent's needs list.
func TestBuildMainJobEngineEnvNeedsNotDuplicated(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				"MY_VALUE": "${{ needs.custom_job.outputs.result }}",
			},
		},
		Jobs: map[string]any{
			// custom_job has no explicit needs so it becomes a direct agent dependency
			"custom_job": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{"run": "echo result=hello >> $GITHUB_OUTPUT"},
				},
			},
		},
	}

	job, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed")

	count := 0
	for _, need := range job.Needs {
		if need == "custom_job" {
			count++
		}
	}
	assert.Equal(t, 1, count, "custom_job should appear exactly once in agent needs")
}

// TestBuildMainJobEngineEnvNeedsIntegration is an end-to-end integration test that compiles
// a workflow where engine.env references a custom job output, and verifies that the
// compiled lock file includes the custom job as a direct dependency of the agent job.
func TestBuildMainJobEngineEnvNeedsIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "engine_env_needs_test")

	// This workflow matches the bug report: engine.env references provide_value_to_agent
	// which in turn depends on pre_activation. Without the fix, the agent job would only
	// have `needs: activation` and runtime evaluation of needs.provide_value_to_agent
	// would silently return an empty string.
	frontmatter := `---
on: issues
permissions:
  contents: read
  issues: read
engine:
  id: copilot
  env:
    RECEIVED_VALUE: ${{ needs.provide_value_to_agent.outputs.provided_value }}
strict: false
jobs:
  provide_value_to_agent:
    runs-on: ubuntu-latest
    needs: pre_activation
    outputs:
      provided_value: ${{ steps.provide.outputs.provided_value }}
    steps:
      - id: provide
        run: echo "provided_value=hello" >> "$GITHUB_OUTPUT"
---

# Test Workflow

This workflow tests that engine.env needs expressions create agent job dependencies.
`

	testFile := filepath.Join(tmpDir, "engine-env-needs.md")
	require.NoError(t, os.WriteFile(testFile, []byte(frontmatter), 0644), "write test file")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile), "compile workflow")

	lockFile := filepath.Join(tmpDir, "engine-env-needs.lock.yml")
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "read lock file")

	yamlStr := string(content)

	// The agent job must directly depend on provide_value_to_agent
	agentSection := extractJobSection(yamlStr, "agent")
	require.NotEmpty(t, agentSection, "agent job section should be present in lock file")

	assert.Contains(t, agentSection, "provide_value_to_agent",
		"agent job must list provide_value_to_agent in its needs (referenced via engine.env)")
}

// TestBuildMainJobEngineEnvActivationNoFalseWarning verifies that referencing the activation
// built-in job in engine.env does NOT emit a warning, since activation is always a direct
// dependency of the agent job and the expression is valid.
func TestBuildMainJobEngineEnvActivationNoFalseWarning(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				// activation is a valid direct dependency — no warning should be emitted
				"MODEL": "${{ needs.activation.outputs.model }}",
			},
		},
	}

	initialWarnings := compiler.GetWarningCount()
	_, err := compiler.buildMainJob(workflowData, true)
	require.NoError(t, err, "buildMainJob should succeed")

	assert.Equal(t, initialWarnings, compiler.GetWarningCount(),
		"no warning should be emitted for activation which is already a direct agent dependency")
}

// TestBuildDetectionJobEngineEnvBuiltinWarning verifies that detection-job engine.env
// references to built-in jobs that are not direct detection dependencies emit a compiler warning.
func TestBuildDetectionJobEngineEnvBuiltinWarning(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						"BUILTIN_JOB_REF": "${{ needs.safe_outputs.outputs.anything }}",
					},
				},
			},
			Jobs: map[string]*SafeJobConfig{"create-issue": {}},
		},
	}

	initialWarnings := compiler.GetWarningCount()
	_, err := compiler.buildDetectionJob(workflowData)
	require.NoError(t, err, "buildDetectionJob should succeed")

	assert.Equal(t, initialWarnings+1, compiler.GetWarningCount(),
		"built-in jobs that cannot become direct detection dependencies should emit a warning")
}

// TestBuildDetectionJobEngineEnvNeedsExpression verifies that the detection job scans the
// effective merged detection engine env (main + detection-specific overrides) when resolving
// custom job dependencies. Both the detection-specific env and the main engine env are
// included so that expressions merged into the detection step have their jobs listed in needs.
func TestBuildDetectionJobEngineEnvNeedsExpression(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				"IGNORED_MODEL": "${{ needs.ignored_job.outputs.model }}",
			},
		},
		Jobs: map[string]any{
			"ignored_job": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{"run": `echo "model=ignored" >> "$GITHUB_OUTPUT"`},
				},
			},
			"select_model": map[string]any{
				"runs-on": "ubuntu-latest",
				"needs":   "pre_activation",
				"outputs": map[string]any{
					"model": "${{ steps.pick.outputs.model }}",
				},
				"steps": []any{
					map[string]any{
						"id":  "pick",
						"run": `echo "model=claude-sonnet-4.6" >> "$GITHUB_OUTPUT"`,
					},
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					ID: "copilot",
					Env: map[string]string{
						"COPILOT_MODEL": "${{ needs.select_model.outputs.model }}",
					},
				},
			},
			Jobs: map[string]*SafeJobConfig{
				"create-issue": {},
			},
		},
	}

	job, err := compiler.buildDetectionJob(workflowData)
	require.NoError(t, err, "buildDetectionJob should succeed")
	require.NotNil(t, job, "detection job should be created")

	// The detection job must scan the merged env (main + detection-specific overrides).
	// Both select_model (from detection env) and ignored_job (from main env, merged in)
	// must appear in needs so their expressions resolve correctly in the detection step.
	assert.Contains(t, job.Needs, "select_model",
		"detection job must directly depend on select_model referenced in threat-detection.engine.env")
	assert.Contains(t, job.Needs, "ignored_job",
		"detection job must also depend on ignored_job merged from main engine.env")
	assert.Contains(t, job.Needs, string(constants.AgentJobName),
		"detection job must still depend on agent")
	assert.Contains(t, job.Needs, string(constants.ActivationJobName),
		"detection job must still depend on activation")
}

// TestBuildDetectionJobEngineEnvNeedsNotDuplicated verifies that a job referenced in
// engine.env is not duplicated in the detection job's needs list.
func TestBuildDetectionJobEngineEnvNeedsNotDuplicated(t *testing.T) {
	compiler := NewCompiler()
	compiler.stepOrderTracker = NewStepOrderTracker()

	workflowData := &WorkflowData{
		Name:        "Test Workflow",
		AI:          "copilot",
		RunsOn:      "runs-on: ubuntu-latest",
		Permissions: "permissions:\n  contents: read",
		EngineConfig: &EngineConfig{
			ID: "copilot",
			Env: map[string]string{
				"VALUE_A": "${{ needs.custom_job.outputs.result }}",
				"VALUE_B": "${{ needs.custom_job.outputs.other }}",
			},
		},
		Jobs: map[string]any{
			"custom_job": map[string]any{
				"runs-on": "ubuntu-latest",
				"steps": []any{
					map[string]any{"run": "echo result=hello >> $GITHUB_OUTPUT"},
				},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
			Jobs:            map[string]*SafeJobConfig{"create-issue": {}},
		},
	}

	job, err := compiler.buildDetectionJob(workflowData)
	require.NoError(t, err, "buildDetectionJob should succeed")
	require.NotNil(t, job, "detection job should be created")

	count := 0
	for _, need := range job.Needs {
		if need == "custom_job" {
			count++
		}
	}
	assert.Equal(t, 1, count, "custom_job should appear exactly once in detection needs")
}

// TestBuildDetectionJobEngineEnvNeedsIntegration is an end-to-end integration test that
// compiles a workflow where engine.env references a custom job output, and verifies that the
// compiled lock file includes the custom job as a direct dependency of the detection job.
// With merged-env dependency scanning, both the detection-specific env and main engine env
// references appear in detection needs.
func TestBuildDetectionJobEngineEnvNeedsIntegration(t *testing.T) {
	tmpDir := testutil.TempDir(t, "detection_engine_env_needs_test")

	frontmatter := `---
on:
  issues:
    types: [opened]
  permissions: {}
permissions:
  contents: read
  issues: read
engine:
  id: copilot
  env:
    MAIN_MODEL: ${{ needs.main_job.outputs.model }}
strict: false
jobs:
  main_job:
    runs-on: ubuntu-latest
    outputs:
      model: ${{ steps.pick.outputs.model }}
    steps:
      - id: pick
        run: echo "model=from-main" >> "$GITHUB_OUTPUT"
  select_model:
    runs-on: ubuntu-latest
    needs: pre_activation
    outputs:
      model: ${{ steps.pick.outputs.model }}
    steps:
      - id: pick
        run: echo "model=claude-sonnet-4.6" >> "$GITHUB_OUTPUT"
safe-outputs:
  create-issue: {}
  threat-detection:
    engine:
      id: copilot
      env:
        COPILOT_MODEL: ${{ needs.select_model.outputs.model }}
---

# Test Detection Needs

This workflow tests that engine.env needs expressions create detection job dependencies.
`

	testFile := filepath.Join(tmpDir, "detection-engine-env-needs.md")
	require.NoError(t, os.WriteFile(testFile, []byte(frontmatter), 0644), "write test file")

	compiler := NewCompiler()
	require.NoError(t, compiler.CompileWorkflow(testFile), "compile workflow")

	lockFile := filepath.Join(tmpDir, "detection-engine-env-needs.lock.yml")
	content, err := os.ReadFile(lockFile)
	require.NoError(t, err, "read lock file")

	var lockFileYAML map[string]any
	require.NoError(t, yaml.Unmarshal(content, &lockFileYAML), "parse lock file yaml")

	jobsNode, ok := lockFileYAML["jobs"].(map[string]any)
	require.True(t, ok, "lock file should contain jobs map")

	detectionNode, ok := jobsNode["detection"].(map[string]any)
	require.True(t, ok, "lock file should contain detection job")

	rawNeeds, ok := detectionNode["needs"].([]any)
	require.True(t, ok, "detection job should render needs as a YAML list")

	detectionNeeds := make([]string, 0, len(rawNeeds))
	for _, need := range rawNeeds {
		needStr, ok := need.(string)
		require.True(t, ok, "detection job needs entries should be strings")
		detectionNeeds = append(detectionNeeds, needStr)
	}

	assert.Contains(t, detectionNeeds, "select_model",
		"detection job must list select_model in needs when referenced via threat-detection.engine.env")
	assert.Contains(t, detectionNeeds, "main_job",
		"detection job must also list main_job in needs when main engine.env is merged into detection env")
}

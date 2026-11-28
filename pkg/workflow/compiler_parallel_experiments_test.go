//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

func parallelExperimentFixture(t *testing.T) (*Compiler, *WorkflowData, string) {
	t.Helper()
	source, err := os.ReadFile("testdata/parallel-experiments.md")
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "parallel-experiments.md")
	compiler := NewCompiler(WithVersion("v1.0.0"), WithNoEmit(true))
	compiler.gitRoot = filepath.Dir(file)
	data, err := compiler.ParseWorkflowString(string(source), file)
	require.NoError(t, err)
	return compiler, data, file
}

func TestParallelExperimentsCompiledFixture(t *testing.T) {
	compiler, data, file := parallelExperimentFixture(t)
	generated, err := compiler.CompileToYAML(data, file)
	require.NoError(t, err)
	var document yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte(generated), &document))
	jobs := artifactMappingValue(document.Content[0], "jobs")
	agent := artifactMappingValue(jobs, "agent")
	parallel := artifactMappingValue(jobs, parallelExperimentsJobName)
	require.NotNil(t, parallel)
	assert.Equal(t, artifactMappingValue(agent, "if").Value, artifactMappingValue(parallel, "if").Value)
	var agentNeeds, parallelNeeds any
	require.NoError(t, artifactMappingValue(agent, "needs").Decode(&agentNeeds))
	require.NoError(t, artifactMappingValue(parallel, "needs").Decode(&parallelNeeds))
	assert.Equal(t, agentNeeds, parallelNeeds)
	assert.Equal(t, "true", artifactMappingValue(parallel, "continue-on-error").Value)
	assert.Equal(t, "10", artifactMappingValue(parallel, "timeout-minutes").Value)
	assert.Nil(t, artifactMappingValue(parallel, "outputs"))
	assert.Nil(t, artifactMappingValue(parallel, "concurrency"))
	strategy := artifactMappingValue(parallel, "strategy")
	assert.Equal(t, "false", artifactMappingValue(strategy, "fail-fast").Value)
	assert.Equal(t, "2", artifactMappingValue(strategy, "max-parallel").Value)
	matrix := artifactMappingValue(strategy, "matrix")
	require.Len(t, matrix.Content, 2, "include only, never Cartesian axes")
	include := artifactMappingValue(matrix, "include")
	require.Len(t, include.Content, 2)
	assert.Equal(t, "true", artifactMappingValue(include.Content[0], "admitted").Value)
	suffix := generated[strings.Index(generated, "  parallel_experiments:"):]
	assert.Contains(t, suffix, "maxAiCredits")
	assert.Contains(t, suffix, "GH_AW_PARALLEL_EXPERIMENT")
	assert.Contains(t, suffix, "GH_AW_PARALLEL_ADMISSION")
	assert.Contains(t, suffix, "admission-limit")
	assert.Contains(t, suffix, "parallel_experiment.cjs")
	assert.Contains(t, suffix, "entry_1_agentic_execution")
	assert.Contains(t, suffix, "entry_2_agentic_execution")
	assert.NotContains(t, suffix, "entry_3_agentic_execution")
	assert.NotContains(t, suffix, "steps.pick-experiment.")
	assert.Contains(t, suffix, "parallel-experiment-${{ github.run_attempt }}-${{ matrix.id }}-agent")
	assert.Contains(t, suffix, "redact_parallel_experiment.outcome")
	for line := range strings.SplitSeq(suffix, "\n") {
		if strings.Contains(line, "secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN") {
			t.Errorf("unexpected custom credential: %s", line)
		}
	}
	assert.NotContains(t, suffix, "actions/cache/save@")
	assert.NotContains(t, suffix, "actions/cache@")
	assert.Contains(t, suffix, "cache-mode: read")
	assert.Contains(t, suffix, "additionalDirectories: [process.env.GH_AW_PARALLEL_EXPERIMENT_DIR]")
	assert.Contains(t, suffix, "redact_secrets.cjs")
	assert.NotContains(t, suffix, "/redact_parallel_experiment.cjs")
	assert.NotContains(t, suffix, "/validate_parallel_recovery_envelopes.cjs")
	assert.Contains(t, suffix, "parallel_experiment.cjs\" start")
	assert.Contains(t, suffix, "parallel_experiment.cjs\" end || true;")
	assert.Contains(t, suffix, "INPUT_JOB_NAME: agent")
	assert.NotContains(t, suffix, "INPUT_JOB_NAME: ${{ github.job }}")
	var matrixContract any
	require.NoError(t, strategy.Decode(&matrixContract))
	got, err := json.MarshalIndent(matrixContract, "", "  ")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/parallel-experiments-matrix.golden.json")
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(got))
}

func TestParallelExperimentsAppendPreservesProductionBytes(t *testing.T) {
	compiler, data, file := parallelExperimentFixture(t)
	configuration := data.RawFrontmatter["parallel-experiments"]
	data.RawFrontmatter["parallel-experiments"] = false
	production, _, _, err := compiler.generateYAML(data, file)
	require.NoError(t, err)
	disabled, err := compiler.appendParallelExperiments(production, data)
	require.NoError(t, err)
	require.Equal(t, production, disabled)
	delete(data.RawFrontmatter, "parallel-experiments")
	absent, err := compiler.appendParallelExperiments(production, data)
	require.NoError(t, err)
	require.Equal(t, production, absent)
	data.RawFrontmatter["parallel-experiments"] = configuration
	var before, after strings.Builder
	compiler.jobManager.WriteJobsYAML(&before)
	enabled, err := compiler.appendParallelExperiments(production, data)
	require.NoError(t, err)
	index := strings.Index(enabled, "  parallel_experiments:")
	require.Positive(t, index)
	require.Equal(t, production, enabled[:index])
	compiler.jobManager.WriteJobsYAML(&after)
	require.Equal(t, before.String(), after.String())
}

func TestParallelExperimentsAdmissionAndValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config map[string]any
		error  string
	}{
		{"empty", map[string]any{"include": []any{}}, "between 1 and 16"},
		{"unknown", map[string]any{"include": []any{}, "unknown-control": 2}, "unknown field"},
		{"unbounded", map[string]any{"max-ai-credits": -1}, "integer from 1"},
		{"fractional", map[string]any{"timeout-minutes": 0.5}, "cannot unmarshal"},
		{"unknown variant", map[string]any{"include": []any{map[string]any{"experiment": "model", "variant": "absent"}}}, "must belong"},
		{"unknown engine", map[string]any{"include": []any{map[string]any{"experiment": "model", "variant": "gpt-5", "engine": "custom"}}}, "only the sandboxed copilot"},
		{"harness", map[string]any{"include": []any{map[string]any{"experiment": "model", "variant": "gpt-5", "harness": "evil.cjs"}}}, "harness must"},
		{"expression model", map[string]any{"include": []any{map[string]any{"experiment": "model", "variant": "gpt-5", "model": "${{ secrets.KEY }}"}}}, "literal model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, data, _ := parallelExperimentFixture(t)
			data.RawFrontmatter["parallel-experiments"] = tc.config
			_, err := parseParallelExperiments(data)
			require.ErrorContains(t, err, tc.error)
		})
	}
	_, data, _ := parallelExperimentFixture(t)
	config := data.RawFrontmatter["parallel-experiments"].(map[string]any)
	config["max-total-ai-credits"] = 150
	parsed, err := parseParallelExperiments(data)
	require.NoError(t, err)
	assert.True(t, parsed.Include[0].Admitted)
	assert.False(t, parsed.Include[1].Admitted)
	assert.Equal(t, "admission-limit", parsed.Include[1].SkipReason)
}

func TestParallelExperimentsIsolationRejectsUnsupported(t *testing.T) {
	for _, field := range []string{"pre-steps", "post-steps", "steps", "env", "sandbox", "mcp-servers", "imports", "jobs", "plugins", "checkout", "runs-on"} {
		t.Run(field, func(t *testing.T) {
			compiler, data, _ := parallelExperimentFixture(t)
			data.RawFrontmatter[field] = "unsupported"
			require.ErrorContains(t, compiler.validateParallelExperimentIsolation(data), field)
		})
	}
}

func TestParallelExperimentsReferenceBoundaries(t *testing.T) {
	experiments := map[string][]string{"foo": {"a", "b"}, "foobar": {"x", "y"}}
	entry := parallelExperimentEntry{Experiment: "foo", Variant: "b"}
	assert.Equal(t, "${{ 'b' }} ${{ needs.activation.outputs.foobar }}",
		parallelExperimentReferences("${{ steps.pick-experiment.outputs.foo }} ${{ needs.activation.outputs.foobar }}", entry, experiments))
}

func TestParallelExperimentsTelemetryAndBudgetIsolation(t *testing.T) {
	source, err := os.ReadFile("testdata/parallel-experiments.md")
	require.NoError(t, err)
	text := strings.Replace(string(source), "strict: false", `strict: false
observability:
  otlp:
    endpoint: ${{ secrets.EXPERIMENT_OTLP_ENDPOINT }}
    headers: ${{ secrets.EXPERIMENT_OTLP_HEADERS }}`, 1)
	file := filepath.Join(t.TempDir(), "telemetry.md")
	compiler := NewCompiler(WithVersion("v1.0.0"), WithNoEmit(true))
	compiler.gitRoot = filepath.Dir(file)
	data, err := compiler.ParseWorkflowString(text, file)
	require.NoError(t, err)
	data.AdmissionEnabled = true
	data.CachedPermissions = NewPermissionsParser("permissions:\n  contents: write\n").ToPermissions()
	require.NotEmpty(t, data.Env, "generated telemetry env must not be mistaken for authored host code")
	config, err := parseParallelExperiments(data)
	require.NoError(t, err)
	job, err := compiler.buildParallelExperimentJob(data, config.Include[0])
	require.NoError(t, err)
	assert.Contains(t, job.Permissions, "contents: read")
	assert.NotContains(t, job.Permissions, "contents: write")
	steps := strings.Join(job.Steps, "")
	assert.NotContains(t, steps, "needs.activation.outputs.admission-max-ai-credits")
	assert.Contains(t, steps, `GH_AW_MAX_AI_CREDITS="100"`)
	assert.Contains(t, steps, "Emit OTLP job conclusion before artifacts")
	assert.Contains(t, steps, "SECRET_EXPERIMENT_OTLP_ENDPOINT")
	assert.Contains(t, steps, "SECRET_EXPERIMENT_OTLP_HEADERS")
	assert.Contains(t, steps, "OTEL_EXPORTER_OTLP_HEADERS")
	assert.True(t, data.AdmissionEnabled)
}

func TestParallelExperimentsDocumentedExample(t *testing.T) {
	source := `---
on: workflow_dispatch
engine:
  id: copilot
model-routing: false
network:
  allowed: []
permissions:
  contents: read
  issues: read
  pull-requests: read
experiments:
  style: [concise, detailed]
parallel-experiments:
  include:
    - experiment: style
      variant: concise
    - experiment: style
      variant: detailed
---
{{#if experiments.style == 'concise'}}
Summarize this repository concisely.
{{else}}
Summarize this repository in detail.
{{/if}}
`
	file := filepath.Join(t.TempDir(), "example.md")
	compiler := NewCompiler(WithVersion("v1.0.0"), WithNoEmit(true))
	compiler.gitRoot = filepath.Dir(file)
	data, err := compiler.ParseWorkflowString(source, file)
	require.NoError(t, err)
	_, err = compiler.CompileToYAML(data, file)
	require.NoError(t, err)
}

func TestParallelExperimentsRejectsNetworkInput(t *testing.T) {
	compiler, data, _ := parallelExperimentFixture(t)
	data.NetworkPermissions.AllowedInput = true
	require.ErrorContains(t, compiler.validateParallelExperimentIsolation(data), "network.allowed")
}

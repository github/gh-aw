package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestConclusionMemorySaveEnvironment(t *testing.T) {
	compiler := NewCompiler()
	for _, memories := range [][]RepoMemoryEntry{nil, {{ID: "default"}, {ID: "notes"}}} {
		data := &WorkflowData{
			SafeOutputs:      &SafeOutputsConfig{},
			RepoMemoryConfig: &RepoMemoryConfig{Memories: memories},
		}
		job, err := compiler.buildConclusionJob(data, "agent", nil)
		require.NoError(t, err)
		env := buildRenderedJobEnv(job)
		if len(memories) == 0 {
			require.NotContains(t, env, "GH_AW_MEMORY_SAVE_RESULTS")
			continue
		}
		var metadata string
		require.NoError(t, json.Unmarshal([]byte(env["GH_AW_MEMORY_SAVE_RESULTS"]), &metadata))
		var results map[string]map[string]string
		require.NoError(t, json.Unmarshal([]byte(metadata), &results))
		require.Len(t, results, len(memories))
		for _, memory := range memories {
			require.Equal(t, map[string]string{
				"jobResult": "${{ needs.push_repo_memory.result }}",
				"outcome":   fmt.Sprintf("${{ needs.push_repo_memory.outputs.save_outcome_%s }}", memory.ID),
				"status":    fmt.Sprintf("${{ needs.push_repo_memory.outputs.save_status_%s }}", memory.ID),
			}, results[memory.ID])
		}
		require.NotContains(t, metadata, "validation_error")
	}
}

func TestConclusionJobResultsEnvironmentExcludesOutputs(t *testing.T) {
	compiler := NewCompiler()
	job, err := compiler.buildConclusionJob(&WorkflowData{
		Name:        "Test Workflow",
		SafeOutputs: &SafeOutputsConfig{},
	}, "agent", []string{"safe_outputs", "send_report"})
	require.NoError(t, err)
	require.NoError(t, compiler.jobManager.AddJob(job))

	// These dependencies are added only after conclusion's steps have been built.
	results := map[string]string{
		"activation":       "success",
		"agent":            "failure",
		"safe_outputs":     "skipped",
		"send_report":      "cancelled",
		"custom-job":       "failure",
		"custom_job":       "success",
		"detection":        "skipped",
		"evals":            "cancelled",
		"push_repo_memory": "success",
	}
	for id := range results {
		require.NoError(t, compiler.jobManager.AddJob(&Job{Name: id}))
	}
	require.NoError(t, compiler.jobManager.AddJob(&Job{Name: "pre_activation"}))
	require.NoError(t, compiler.ensureConclusionIsLastJob())

	var rendered strings.Builder
	compiler.jobManager.WriteJobsYAML(&rendered)
	var workflow struct {
		Jobs map[string]struct {
			Env map[string]string `yaml:"env"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(rendered.String()), &workflow))
	env := workflow.Jobs["conclusion"].Env
	metadata := env["GH_AW_JOB_RESULTS"]
	require.NotEmpty(t, metadata)
	require.Equal(t, 1, strings.Count(rendered.String(), "GH_AW_JOB_RESULTS:"), "both consumers must inherit the same result-only boundary")
	require.Contains(t, rendered.String(), "GH_AW_JOB_DURATIONS: ${{ steps.report_failed_jobs.outputs.job_durations }}")
	require.Len(t, job.Needs, len(results))
	for id := range results {
		require.Contains(t, job.Needs, id)
	}
	require.NotContains(t, metadata, ".outputs")
	require.NotContains(t, metadata, `"outputs":`)
	require.NotContains(t, metadata, "toJSON(needs)")

	for _, privateSize := range []int{1, 1024 * 1024} {
		t.Run(fmt.Sprintf("private_output_bytes_%d", privateSize), func(t *testing.T) {
			needs := make(map[string]map[string]any)
			expected := make(map[string]map[string]string)
			for id, result := range results {
				needs[id] = map[string]any{
					"result":  result,
					"outputs": map[string]string{"private": "PRIVATE_VALUE" + strings.Repeat("x", privateSize)},
				}
				expected[id] = map[string]string{"result": result}
			}
			// Evaluate only the scalar result expressions accepted by this boundary.
			evaluated := metadata
			for id, dependency := range needs {
				evaluated = strings.ReplaceAll(evaluated, fmt.Sprintf("${{ needs.%s.result }}", id), dependency["result"].(string))
			}
			require.NotContains(t, evaluated, "${{", "metadata must not access any other dependency fields")
			require.NotContains(t, evaluated, "PRIVATE_VALUE")
			var actual map[string]map[string]string
			require.NoError(t, json.Unmarshal([]byte(evaluated), &actual))
			require.Equal(t, expected, actual)
			expectedJSON, err := json.Marshal(expected)
			require.NoError(t, err)
			require.Len(t, evaluated, len(expectedJSON), "output size must not affect the process environment")
		})
	}
}

func TestFailedJobsDisplayNames(t *testing.T) {
	names := buildFailedJobsDisplayNames(map[string]*Job{
		"agent":            {DisplayName: "Renamed agent"},
		"build":            {DisplayName: "Build project"},
		"dynamic":          {DisplayName: "Build ${{ inputs.name }}"},
		"unnamed":          {},
		"send_report":      {DisplayName: "Send report"},
		"push_repo_memory": {},
	})
	require.Equal(t, "Renamed agent", names["agent"])
	require.Equal(t, "Build project", names["build"])
	require.Equal(t, "Send report", names["send_report"])
	require.Equal(t, "push_repo_memory", names["push_repo_memory"])
	require.Equal(t, "unnamed", names["unnamed"])
	require.NotContains(t, names, "dynamic", "runtime expressions must not be interpolated into serialized JSON")
	require.Equal(t, "Agent", generatedJobNames["agent"], "overrides must not mutate the shared compiler mapping")
}

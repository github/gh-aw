//go:build !integration

package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestSafeJobOutcomeOutputsPropagation(t *testing.T) {
	c := NewCompiler()
	outputs := map[string]string{
		"status": "${{ steps.publish.outputs.status }}",
		"url":    "${{ steps.publish.outputs.url }}",
	}
	jobs := c.parseSafeJobsConfig(map[string]any{
		"publish-report": map[string]any{
			"outputs": map[string]any{"url": outputs["url"], "status": outputs["status"]},
			"steps":   []any{map[string]any{"id": "publish", "run": "echo published"}},
		},
	})
	require.Equal(t, outputs, jobs["publish-report"].Outputs)
	names, err := c.buildSafeJobs(&WorkflowData{SafeOutputs: &SafeOutputsConfig{Jobs: jobs}}, false)
	require.NoError(t, err)
	require.Equal(t, []string{"publish_report"}, names)
	job, ok := c.jobManager.GetJob("publish_report")
	require.True(t, ok)
	require.Equal(t, outputs, job.Outputs)

	var rendered strings.Builder
	c.jobManager.WriteJobsYAML(&rendered)
	var compiled struct {
		Jobs map[string]struct {
			Outputs map[string]string `yaml:"outputs"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(rendered.String()), &compiled))
	require.Equal(t, outputs, compiled.Jobs["publish_report"].Outputs)
	require.Less(t, strings.Index(rendered.String(), "      status:"), strings.Index(rendered.String(), "      url:"))
}

func TestConclusionSafeJobOutcomeAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name      string
		jobs      map[string]*SafeJobConfig
		workQueue bool
		want      []string
	}{
		{name: "no-jobs"},
		{name: "normalized-sorted", jobs: map[string]*SafeJobConfig{
			"z-report": {Outputs: map[string]string{"status": "${{ steps.publish.outputs.status }}", "url": "${{ steps.publish.outputs.url }}"}},
			"a-report": {Outputs: map[string]string{"status": "${{ steps.publish.outputs.status }}", "url": "${{ steps.publish.outputs.url }}"}},
		}, want: []string{"a_report", "z_report"}},
		{name: "work-queue-prepares-only", jobs: map[string]*SafeJobConfig{"publish": {}}, workQueue: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCompiler()
			data := &WorkflowData{SafeOutputs: &SafeOutputsConfig{Jobs: tc.jobs}}
			if tc.workQueue {
				data.Tools = map[string]any{"work-queue": map[string]any{"worker": true}}
			}

			job, err := c.buildConclusionJob(data, "agent", []string{"safe_outputs", "unrelated"})
			require.NoError(t, err)
			var compiled struct {
				Steps []struct {
					Name string            `yaml:"name"`
					Env  map[string]string `yaml:"env"`
				} `yaml:"steps"`
			}
			require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+strings.Join(job.Steps, "")), &compiled))
			var collectionEnv map[string]string
			for _, step := range compiled.Steps {
				if step.Name == "Generate usage activity summary and unified session" {
					collectionEnv = step.Env
				} else {
					require.NotContains(t, step.Env, "GH_AW_SAFE_JOB_NAMES")
					require.NotContains(t, step.Env, "GH_AW_SAFE_JOB_RESULTS")
				}
				require.NotContains(t, step.Env, "GH_AW_JOB_RESULTS")
			}
			if len(tc.want) == 0 {
				require.NotContains(t, collectionEnv, "GH_AW_SAFE_JOB_NAMES")
				require.NotContains(t, collectionEnv, "GH_AW_SAFE_JOB_RESULTS")
				return
			}
			var names []string
			require.NoError(t, json.Unmarshal([]byte(collectionEnv["GH_AW_SAFE_JOB_NAMES"]), &names))
			require.Equal(t, tc.want, names)
			results := collectionEnv["GH_AW_SAFE_JOB_RESULTS"]
			require.NotContains(t, results, "unrelated")
			require.NotContains(t, results, "safe_outputs")
			require.NotContains(t, results, "toJSON(needs)")
			for _, name := range names {
				for field, value := range map[string]string{"result": "success", "outputs.status": "created", "outputs.url": "https://example.com/\"quoted\"\npath"} {
					encoded, err := json.Marshal(value)
					require.NoError(t, err)
					results = strings.ReplaceAll(results, fmt.Sprintf("${{ toJSON(needs.%s.%s) }}", name, field), string(encoded))
				}
			}
			var parsed map[string]struct {
				Result  string            `json:"result"`
				Outputs map[string]string `json:"outputs"`
			}
			require.NoError(t, json.Unmarshal([]byte(results), &parsed))
			require.Len(t, parsed, len(names))
			for _, outcome := range parsed {
				require.Equal(t, "success", outcome.Result)
				require.Equal(t, "created", outcome.Outputs["status"])
				require.Equal(t, "https://example.com/\"quoted\"\npath", outcome.Outputs["url"])
			}
		})
	}
}

func TestConclusionSafeJobOutcomesPreserveAllJobResults(t *testing.T) {
	c := NewCompiler()
	job, err := c.buildConclusionJob(&WorkflowData{
		SafeOutputs: &SafeOutputsConfig{Jobs: map[string]*SafeJobConfig{"publish-report": {
			Outputs: map[string]string{"status": "${{ steps.publish.outputs.status }}", "url": "${{ steps.publish.outputs.url }}"},
		}}},
	}, "agent", []string{"publish_report", "safe_outputs"})
	require.NoError(t, err)
	// Dependencies can be added after the collection step is constructed.
	job.Needs = append(job.Needs, "late_job")
	require.NoError(t, c.jobManager.AddJob(job))
	var rendered strings.Builder
	c.jobManager.WriteJobsYAML(&rendered)
	var compiled struct {
		Jobs map[string]struct {
			Env   map[string]string `yaml:"env"`
			Steps []struct {
				Name string            `yaml:"name"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}

	require.NoError(t, yaml.Unmarshal([]byte(rendered.String()), &compiled))
	conclusion := compiled.Jobs["conclusion"]
	require.NotContains(t, conclusion.Env["GH_AW_JOB_RESULTS"], ".outputs")
	require.NotContains(t, conclusion.Env["GH_AW_JOB_RESULTS"], `"outputs":`)
	for _, name := range job.Needs {
		require.Contains(t, conclusion.Env["GH_AW_JOB_RESULTS"], fmt.Sprintf("${{ needs.%s.result }}", name))
	}
	for _, step := range conclusion.Steps {
		require.NotContains(t, step.Env, "GH_AW_JOB_RESULTS")
		if step.Name != "Generate usage activity summary and unified session" {
			require.NotContains(t, step.Env, "GH_AW_SAFE_JOB_RESULTS")
			continue
		}
		results := step.Env["GH_AW_SAFE_JOB_RESULTS"]
		require.Contains(t, results, "needs.publish_report.outputs.status")
		require.Contains(t, results, "needs.publish_report.outputs.url")
		require.NotContains(t, results, "needs.late_job")
		require.NotContains(t, results, "needs.safe_outputs")
	}
}

func TestSafeJobOutcomeProjectionOnlyReferencesDeclaredOutputs(t *testing.T) {
	for _, outputs := range []map[string]string{nil, {"url": "${{ steps.publish.outputs.url }}"}, {"status": "${{ steps.publish.outputs.status }}"}} {
		env := buildUsageSafeJobResultsEnv([]string{"publish_report"}, map[string]*SafeJobConfig{"publish-report": {Outputs: outputs}})
		require.Contains(t, env, "needs.publish_report.result")
		for _, field := range []string{"status", "url"} {
			reference := "needs.publish_report.outputs." + field
			if _, declared := outputs[field]; declared {
				require.Contains(t, env, reference)
			} else {
				require.NotContains(t, env, reference)
			}
		}
	}
}

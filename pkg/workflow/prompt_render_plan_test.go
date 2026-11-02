package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type testPromptRenderConfig struct {
	Items []struct {
		ContentEnvVar   string `json:"content_env"`
		File            string `json:"file"`
		AuthorFile      string `json:"author_file"`
		ConditionEnvVar string `json:"condition_env"`
	} `json:"items"`
}

func promptRenderPlan(t *testing.T, output string) string {
	t.Helper()

	type promptStep struct {
		Env map[string]string
	}
	var steps []promptStep
	if err := yaml.Unmarshal([]byte(output), &steps); err != nil {
		var workflow struct {
			Jobs map[string]struct {
				Steps []promptStep
			}
		}
		require.NoError(t, yaml.Unmarshal([]byte(output), &workflow))
		for _, job := range workflow.Jobs {
			steps = append(steps, job.Steps...)
		}
	}
	var env map[string]string
	for _, step := range steps {
		if step.Env["GH_AW_PROMPT_CONFIG"] != "" {
			env = step.Env
			break
		}
	}
	require.NotNil(t, env, "Prompt render config should be present")

	var config testPromptRenderConfig
	require.NoError(t, json.Unmarshal([]byte(env["GH_AW_PROMPT_CONFIG"]), &config))

	var plan strings.Builder
	for _, item := range config.Items {
		if item.AuthorFile != "" {
			plan.WriteString("{{#author-prompt " + item.AuthorFile + "}}\n")
			continue
		}
		if item.File != "" {
			plan.WriteString(item.File)
			plan.WriteByte('\n')
			continue
		}
		require.Contains(t, env, item.ContentEnvVar)
		plan.WriteString(env[item.ContentEnvVar])
	}
	return plan.String()
}

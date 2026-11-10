//go:build !integration

package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// awInfoStepEnv returns the env of the activation job's generate_aw_info step.
func awInfoStepEnv(t *testing.T, compiled string) map[string]string {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				ID  string            `yaml:"id"`
				Env map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(compiled), &workflow))
	for _, step := range workflow.Jobs["activation"].Steps {
		if step.ID == "generate_aw_info" {
			return step.Env
		}
	}
	t.Fatal("generate_aw_info step not found")
	return nil
}

func TestAwInfoRoutingAnalyticsMetadata(t *testing.T) {
	workflowsDir := filepath.Join(testutil.TempDir(t, "aw-info-model-config"), ".github", "workflows")
	require.NoError(t, os.MkdirAll(workflowsDir, 0755))

	tests := []struct {
		name        string
		frontmatter string
		harness     string
		config      map[string]any
		budget      string
	}{
		{
			name: "routed-sdk",
			frontmatter: `engine:
  id: copilot
  copilot-sdk: true
  model-routing:
    goal: cost
    mode: economy
    allowed-models: [gpt-5.6-luna]
`,
			harness: "copilot-sdk",
			config: map[string]any{
				"routing":               map[string]any{"enabled": true, "goal": "cost", "mode": "economy", "allowed_models": []any{"gpt-5.6-luna"}, "candidate_models": []any{"github-copilot/gpt-5.6-luna"}},
				"models":                map[string]any{"allowed": []any{}, "blocked": []any{}},
				"max_ai_credits_source": "default",
			},
			budget: "${{ vars.GH_AW_DEFAULT_MAX_AI_CREDITS || '1000' }}",
		},
		{
			name:        "unrouted-cli-frontmatter-budget",
			frontmatter: "engine:\n  id: copilot\n  model-routing: false\nmax-ai-credits: 250\n",
			harness:     "copilot-cli",
			config: map[string]any{
				"routing":               map[string]any{"enabled": false},
				"models":                map[string]any{"allowed": []any{}, "blocked": []any{}},
				"max_ai_credits_source": "frontmatter",
			},
			budget: "250",
		},
		{
			name:        "non-copilot-engine",
			frontmatter: "engine: claude\n",
			config: map[string]any{
				"routing":               map[string]any{"enabled": false},
				"models":                map[string]any{"allowed": []any{}, "blocked": []any{}},
				"max_ai_credits_source": "default",
			},
			budget: "${{ vars.GH_AW_DEFAULT_MAX_AI_CREDITS || '1000' }}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled := compileAndReadLock(t, filepath.Join(workflowsDir, tt.name+".md"), "---\non: workflow_dispatch\npermissions:\n  contents: read\n"+tt.frontmatter+"---\n# Task\nDo it.\n")
			env := awInfoStepEnv(t, compiled)
			harness, ok := env["GH_AW_INFO_ENGINE_HARNESS"]
			if tt.harness == "" {
				assert.False(t, ok, "only Copilot runs record a harness")
			} else {
				assert.Equal(t, tt.harness, harness)
			}
			var config map[string]any
			require.NoError(t, json.Unmarshal([]byte(env["GH_AW_INFO_MODEL_CONFIG"]), &config))
			assert.Equal(t, tt.config, config)
			assert.Equal(t, tt.budget, env["GH_AW_INFO_MAX_AI_CREDITS"])
		})
	}
}

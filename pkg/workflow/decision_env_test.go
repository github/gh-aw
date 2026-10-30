//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionProviderEnv(t *testing.T) {
	for _, tt := range []struct {
		provider        string
		authKey         string
		authOverrideKey string
		baseKey         string
		baseOverrideKey string
	}{
		{"copilot", "COPILOT_GITHUB_TOKEN", "COPILOT_GITHUB_TOKEN", "GITHUB_COPILOT_BASE_URL", "GITHUB_COPILOT_BASE_URL"},
		{"anthropic", "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_BASE_URL"},
		{"openai", "OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL", "OPENAI_BASE_URL"},
		{"google", "GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL", "GEMINI_API_BASE_URL"},
	} {
		t.Run(tt.provider, func(t *testing.T) {
			data := &WorkflowData{
				Decision: &DecisionConfig{Model: tt.provider + "/custom"},
				EngineConfig: &EngineConfig{ID: "different-agent-engine", Env: map[string]string{
					tt.authOverrideKey: "${{ secrets.CUSTOM_AUTH }}",
					tt.baseOverrideKey: "https://provider.example/v1",
				}},
				ModelCosts: map[string]any{"providers": map[string]any{
					tt.provider: map[string]any{"models": map[string]any{"custom": map[string]any{
						"cost": map[string]any{"input": 1.0, "output": 2.0},
					}}},
				}},
			}
			env := decisionEnv(data)
			assert.Equal(t, "true", env["GH_AW_DECISION_ELIGIBLE"])
			assert.Equal(t, "${{ secrets.CUSTOM_AUTH }}", env[tt.authKey])
			assert.Equal(t, "https://provider.example/v1", env[tt.baseKey])
			assert.NotContains(t, env, "GH_AW_DECISION_PROVIDERS", "pricing metadata is not transport configuration")
		})
	}
}

func TestDecisionProviderAliasEnv(t *testing.T) {
	data := &WorkflowData{
		Decision:      &DecisionConfig{Model: "custom"},
		ModelMappings: map[string][]string{"custom": {"anthropic/private-model", "openai/private-model"}},
	}
	env := decisionEnv(data)
	assert.Contains(t, env, "ANTHROPIC_API_KEY")
	assert.Contains(t, env, "OPENAI_API_KEY")
	assert.NotContains(t, env, "COPILOT_GITHUB_TOKEN")
	assert.JSONEq(t, `{"custom":["anthropic/private-model","openai/private-model"]}`, env["GH_AW_MODEL_ALIASES"])
}

func TestDecisionCompiledRuntimeContract(t *testing.T) {
	dir := testutil.TempDir(t, "decision-contract")
	file := filepath.Join(dir, "decision.md")
	source := `---
on:
  workflow_dispatch:
  roles: all
  decision:
    model: team-decision
    questions:
      - "Is ${{ github.event.issue.title }} actionable?"
models:
  team-decision: [openai/gpt-5.4]
  providers:
    openai:
      models:
        gpt-5.4:
          cost:
            input: 1
            output: 2
engine:
  id: copilot
  env:
    OPENAI_API_KEY: ${{ secrets.DECISION_KEY }}
    OPENAI_BASE_URL: https://provider.example/v1
---
Perform the requested work.
`
	require.NoError(t, os.WriteFile(file, []byte(source), 0600))
	require.NoError(t, NewCompiler().CompileWorkflow(file))
	lock, err := os.ReadFile(filepath.Join(dir, "decision.lock.yml"))
	require.NoError(t, err)
	var document struct {
		Jobs map[string]struct {
			Outputs map[string]string `yaml:"outputs"`
			Steps   []struct {
				ID   string            `yaml:"id"`
				If   string            `yaml:"if"`
				Env  map[string]string `yaml:"env"`
				With struct {
					Script string `yaml:"script"`
				} `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal(lock, &document))
	var env map[string]string
	var script string
	for _, step := range document.Jobs["pre_activation"].Steps {
		if step.ID == decisionStepID {
			env = step.Env
			script = step.With.Script
			assert.Empty(t, step.If, "runtime must record deterministic rejection without inference")
		}
	}
	require.NotNil(t, env)
	assert.Equal(t, "team-decision", env["GH_AW_DECISION_MODEL"])
	assert.Contains(t, env["GH_AW_DECISION_ELIGIBLE"], "github.event.workflow_run.repository.id == github.repository_id")
	assert.Equal(t, "1", env["GH_AW_DECISION_QUESTION_COUNT"])
	assert.Equal(t, "Is ${{ github.event.issue.title }} actionable?", env["GH_AW_DECISION_QUESTION_0"])
	assert.Equal(t, "${{ secrets.DECISION_KEY }}", env["OPENAI_API_KEY"])
	assert.Equal(t, "https://provider.example/v1", env["OPENAI_BASE_URL"])
	assert.NotEmpty(t, env["GH_AW_DECISION_AUDIT_PATH"])
	assert.JSONEq(t, `{"team-decision":["openai/gpt-5.4"]}`, env["GH_AW_MODEL_ALIASES"])
	assert.NotContains(t, env, "GH_AW_DECISION_PROVIDERS")
	assert.Contains(t, script, "decision_gate.cjs")
	assert.NotContains(t, script, "github.event.issue.title")
	output := document.Jobs["pre_activation"].Outputs["activated"]
	assert.Contains(t, output, "steps.decision_gate.outputs.decision_result != 'skip'")
	assert.False(t, strings.Contains(output, "steps.decision_gate.outputs.decision !="))
	assert.Equal(t, "${{ steps.decision_gate.outputs.decision_result }}", document.Jobs["pre_activation"].Outputs["decision_result"])
	assert.Equal(t, "${{ steps.decision_gate.outputs.decision_status }}", document.Jobs["pre_activation"].Outputs["decision_status"])
}

func TestDecisionIneligibleRuntimeStillRecordsReceipt(t *testing.T) {
	data := &WorkflowData{Decision: &DecisionConfig{Model: "decision", Questions: []string{"Run?"}}}
	steps := NewCompiler().appendDecisionStep(data, nil, "${{ false }}")
	var document struct {
		Steps []struct {
			ID  string            `yaml:"id"`
			If  string            `yaml:"if"`
			Env map[string]string `yaml:"env"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("    steps:\n"+strings.Join(steps, "")), &document))
	require.Len(t, document.Steps, 2)
	assert.Equal(t, decisionStepID, document.Steps[0].ID)
	assert.Empty(t, document.Steps[0].If)
	assert.Equal(t, "${{ false }}", document.Steps[0].Env["GH_AW_DECISION_ELIGIBLE"])
	assert.Equal(t, "${{ runner.temp }}/gh-aw/decision-gate.json", document.Steps[0].Env["GH_AW_DECISION_AUDIT_PATH"])
	assert.Equal(t, "always()", document.Steps[1].If)
}

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

func TestDecisionProviderCredentialsWithoutAdditionalOptIn(t *testing.T) {
	for _, tt := range []struct {
		model     string
		authKey   string
		authValue string
	}{
		{"copilot/custom", "COPILOT_GITHUB_TOKEN", "${{ secrets.COPILOT_GITHUB_TOKEN }}"},
		{"anthropic/custom", "ANTHROPIC_API_KEY", "${{ secrets.ANTHROPIC_API_KEY }}"},
		{"openai/custom", "OPENAI_API_KEY", "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}"},
		{"google/custom", "GEMINI_API_KEY", "${{ secrets.GEMINI_API_KEY }}"},
	} {
		t.Run(tt.model, func(t *testing.T) {
			data := &WorkflowData{Decision: &DecisionConfig{Model: tt.model}}
			env := decisionEnv(data)
			assert.Equal(t, tt.authValue, env[tt.authKey], "on.decision is the explicit provider-forwarding opt-in")
			for _, key := range []string{"COPILOT_GITHUB_TOKEN", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"} {
				if key != tt.authKey {
					assert.NotContains(t, env, key, "unselected provider credentials must not be injected")
				}
			}
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

func TestDecisionCodexProviderEnv(t *testing.T) {
	data := &WorkflowData{
		Decision: &DecisionConfig{Model: "codex/gpt-5.4"},
		EngineConfig: &EngineConfig{Env: map[string]string{
			"CODEX_API_KEY":   "${{ secrets.CUSTOM_CODEX_KEY }}",
			"OPENAI_BASE_URL": "https://provider.example/v1",
		}},
	}
	env := decisionEnv(data)
	assert.Equal(t, "${{ secrets.CUSTOM_CODEX_KEY }}", env["OPENAI_API_KEY"])
	assert.Equal(t, "https://provider.example/v1", env["OPENAI_BASE_URL"])
	assert.NotContains(t, env, "COPILOT_GITHUB_TOKEN")
}

func TestDecisionProviderEndpointOverrides(t *testing.T) {
	data := &WorkflowData{Decision: &DecisionConfig{Model: "openai/gpt-5.4"}}
	env := decisionEnv(data)
	assert.Equal(t, "https://api.openai.com/v1", env["OPENAI_BASE_URL"])

	data.EngineConfig = &EngineConfig{Env: map[string]string{
		"OPENAI_BASE_URL": "${{ github.event.issue.title }}",
	}}
	require.ErrorContains(t, validateDecisionProviderEndpoints(data), "event-controlled endpoint")

	data.EngineConfig.Env["OPENAI_BASE_URL"] = "https://provider.example/v1"
	require.NoError(t, validateDecisionProviderEndpoints(data))
}

func TestDecisionProviderEndpointValidation(t *testing.T) {
	for _, provider := range []struct {
		model string
		keys  []string
	}{
		{"copilot/custom", []string{"GITHUB_COPILOT_BASE_URL"}},
		{"anthropic/custom", []string{"ANTHROPIC_BASE_URL"}},
		{"openai/custom", []string{"OPENAI_BASE_URL"}},
		{"codex/custom", []string{"OPENAI_BASE_URL"}},
		{"google/custom", []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_BASE_URL"}},
	} {
		for _, key := range provider.keys {
			t.Run(provider.model+"/"+key, func(t *testing.T) {
				for _, tt := range []struct {
					name  string
					value string
					valid bool
				}{
					{"custom HTTPS endpoint", "https://provider.example/v1", true},
					{"custom HTTPS port", "https://provider.example:8443/inference/v1/", true},
					{"default endpoint", decisionEndpoint(fallbackModelProvider(provider.model, LLMProviderGitHub)).defaultURL, true},
					{"empty", "", false},
					{"HTTP", "http://provider.example/v1", false},
					{"relative", "/v1", false},
					{"scheme relative", "//provider.example/v1", false},
					{"missing host", "https:///v1", false},
					{"empty hostname", "https://:443/v1", false},
					{"opaque", "https:provider.example/v1", false},
					{"invalid port", "https://provider.example:invalid/v1", false},
					{"invalid escape", "https://provider.example/%zz", false},
					{"user information", "https://private-user:private-password@provider.example/v1", false},
					{"query", "https://provider.example/v1?private-key=value", false},
					{"fragment", "https://provider.example/v1#private-fragment", false},
					{"event expression", "${{ github.event.issue.title }}", false},
					{"host expression", "https://${{ github.event.issue.title }}/v1", false},
					{"path expression", "https://provider.example/${{ inputs.path }}", false},
					{"incomplete expression", "https://provider.example/${{ inputs.path", false},
					{"shell expression", "https://${PROVIDER_HOST}/v1", false},
					{"closing expression marker", "https://provider.example/v1}", false},
				} {
					t.Run(tt.name, func(t *testing.T) {
						data := &WorkflowData{
							Decision: &DecisionConfig{Model: provider.model},
							EngineConfig: &EngineConfig{Env: map[string]string{
								key: tt.value,
							}},
						}
						err := validateDecisionProviderEndpoints(data)
						if tt.valid {
							require.NoError(t, err)
							assert.Equal(t, tt.value, decisionEnv(data)[decisionEndpoint(fallbackModelProvider(provider.model, LLMProviderGitHub)).envKey])
						} else {
							require.ErrorContains(t, err, "on.decision provider endpoint engine.env."+key)
							if tt.value != "" {
								assert.NotContains(t, err.Error(), tt.value, "diagnostics must not echo endpoint credentials or expressions")
							}
						}
					})
				}
			})
		}
	}
}

func TestDecisionProviderEndpointValidationScope(t *testing.T) {
	require.NoError(t, validateDecisionProviderEndpoints(nil))
	require.NoError(t, validateDecisionProviderEndpoints(&WorkflowData{}))
	data := &WorkflowData{
		Decision: &DecisionConfig{Model: "custom"},
		ModelMappings: map[string][]string{
			"custom": {"anthropic/private-model", "google/private-model"},
		},
		EngineConfig: &EngineConfig{Env: map[string]string{
			"OPENAI_BASE_URL":        "${{ github.event.issue.title }}",
			"GOOGLE_GEMINI_BASE_URL": "https://provider.example/v1",
		}},
	}
	require.NoError(t, validateDecisionProviderEndpoints(data), "unselected providers must not affect decision endpoints")
	data.EngineConfig.Env["GEMINI_API_BASE_URL"] = "http://provider.example/v1"
	require.ErrorContains(t, validateDecisionProviderEndpoints(data), "engine.env.GEMINI_API_BASE_URL")
	delete(data.EngineConfig.Env, "GEMINI_API_BASE_URL")
	data.EngineConfig.Env["ANTHROPIC_BASE_URL"] = "http://provider.example/v1"
	require.ErrorContains(t, validateDecisionProviderEndpoints(data), "engine.env.ANTHROPIC_BASE_URL")
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
	for _, step := range document.Jobs["decision"].Steps {
		if step.ID == decisionStepID {
			env = step.Env
			script = step.With.Script
			assert.Empty(t, step.If, "runtime must record deterministic rejection without inference")
		}
	}
	require.NotNil(t, env)
	assert.Equal(t, "team-decision", env["GH_AW_DECISION_MODEL"])
	assert.Equal(t, "${{ needs.pre_activation.outputs.decision_eligible == 'true' }}", env["GH_AW_DECISION_ELIGIBLE"])
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
	assert.Contains(t, output, "steps.decision_eligibility.outputs.eligible")
	assert.NotContains(t, output, "steps.decision_gate")
	assert.Equal(t, "${{ steps.decision_gate.outputs.decision_result }}", document.Jobs["decision"].Outputs["decision_result"])
	assert.Equal(t, "${{ steps.decision_gate.outputs.decision_status }}", document.Jobs["decision"].Outputs["decision_status"])
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
	assert.Equal(t, decisionAuditPath, document.Steps[0].Env["GH_AW_DECISION_AUDIT_PATH"])
	assert.Equal(t, "always()", document.Steps[1].If)
}

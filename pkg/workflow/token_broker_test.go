//go:build !integration

package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBrokerCompileOptIn(t *testing.T) {
	t.Setenv(tokenBrokerURLEnv, "https://compile-time.invalid/never")
	for _, optIn := range []string{"", "token-broker: false\n", "token-broker: true\n"} {
		t.Run(strings.TrimSpace(optIn), func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\n" + optIn + "safe-outputs:\n  noop:\n  threat-detection: false\n---\nTest broker."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			enabled := strings.Contains(optIn, "true")
			assert.Equal(t, enabled, data.TokenBrokerEnabled)
			output, err := compiler.CompileToYAML(data, "broker.md")
			if enabled {
				require.ErrorContains(t, err, "renewable token-broker credential provider")
				assert.Empty(t, output)
				return
			}
			require.NoError(t, err)
			assert.NotContains(t, output, "https://compile-time.invalid")
			var compiled struct {
				Jobs map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
					Steps       []struct {
						Env map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
			consumers := 0
			for name, job := range compiled.Jobs {
				usesBroker := false
				for _, step := range job.Steps {
					if url, ok := step.Env[tokenBrokerURLEnv]; ok {
						assert.Equal(t, tokenBrokerURLExpression, url)
						assert.NotContains(t, step.Env, "COPILOT_GITHUB_TOKEN")
						usesBroker = true
						consumers++
					}
				}
				if usesBroker {
					assert.Equal(t, "write", job.Permissions["id-token"], name)
				} else if name != "admission" && name != "conclusion_admission" {
					assert.NotContains(t, job.Permissions, "id-token", name)
				}
			}
			assert.Zero(t, consumers)
			assert.Contains(t, output, "secrets.COPILOT_GITHUB_TOKEN")
		})
	}
}

func TestTokenBrokerBootstrapExcluded(t *testing.T) {
	env := ComputeAWFExcludeEnvVarNames(&WorkflowData{TokenBrokerEnabled: true}, nil)
	assert.Contains(t, env, tokenBrokerURLEnv)
	assert.Contains(t, env, "ACTIONS_ID_TOKEN_REQUEST_URL")
	assert.Contains(t, env, "ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	disabledEnv := ComputeAWFExcludeEnvVarNames(&WorkflowData{}, nil)
	assert.NotContains(t, disabledEnv, tokenBrokerURLEnv)
	assert.Contains(t, disabledEnv, "ACTIONS_ID_TOKEN_REQUEST_URL")
	assert.Contains(t, disabledEnv, "ACTIONS_ID_TOKEN_REQUEST_TOKEN")
}

func TestTokenBrokerHandlerConfigExplicitOptIn(t *testing.T) {
	t.Setenv(tokenBrokerURLEnv, "https://inherited.invalid")
	for _, field := range []string{"", "token-broker: false\n", "token-broker: true\n"} {
		t.Run(strings.TrimSpace(field), func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\n" + field + "safe-outputs:\n  assign-to-agent:\n  threat-detection: false\n---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.NoError(t, err)
			var compiled struct {
				Jobs map[string]struct {
					Steps []struct {
						ID  string            `yaml:"id"`
						Env map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
			found := false
			for _, step := range compiled.Jobs["safe_outputs"].Steps {
				if step.ID != "process_safe_outputs" {
					continue
				}
				found = true
				var config map[string]any
				require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &config))
				if data.TokenBrokerEnabled {
					assert.Equal(t, true, config["_token_broker"])
					assert.Equal(t, tokenBrokerURLExpression, step.Env[tokenBrokerURLEnv])
				} else {
					assert.NotContains(t, config, "_token_broker")
					assert.NotContains(t, step.Env, tokenBrokerURLEnv)
				}
			}
			require.True(t, found)
			assert.NotContains(t, output, "https://inherited.invalid")
		})
	}
}

func TestTokenBrokerTaskOnly(t *testing.T) {
	t.Setenv(tokenBrokerURLEnv, "https://compile-time.invalid/never")
	for _, handler := range []string{
		"  assign-to-agent:\n",
		"  create-agent-session:\n",
		"  create-issue:\n    assignees: [copilot]\n",
		"  create-pull-request:\n    assignees: [copilot]\n",
	} {
		t.Run(strings.TrimSpace(handler), func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker: true\nsafe-outputs:\n  threat-detection: false\n" + handler + "---\nTest task."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			assert.False(t, usesTokenBrokerInference(data))
			assert.True(t, usesTokenBrokerTasks(data))
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.NoError(t, err)
			assert.Contains(t, output, tokenBrokerURLEnv+": "+tokenBrokerURLExpression)
			assert.NotContains(t, output, "https://compile-time.invalid")
			assert.NotContains(t, output, "GH_AW_ASSIGN_TO_AGENT_TOKEN:")
			assert.NotContains(t, output, "GH_AW_AGENT_SESSION_TOKEN:")
			var compiled struct {
				Jobs map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
					Steps       []struct {
						ID  string            `yaml:"id"`
						Env map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
			consumers := 0
			for name, job := range compiled.Jobs {
				for _, step := range job.Steps {
					if _, exists := step.Env[tokenBrokerURLEnv]; exists {
						assert.Equal(t, "process_safe_outputs", step.ID)
						assert.Equal(t, "write", job.Permissions["id-token"])
						var config map[string]any
						require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &config))
						assert.Equal(t, true, config["_token_broker"])
						consumers++
					}
				}
				if name != "safe_outputs" && name != "admission" && name != "conclusion_admission" {
					assert.NotContains(t, job.Permissions, "id-token", name)
				}
			}
			assert.Equal(t, 1, consumers)
		})
	}
}

func TestTokenBrokerFrontmatterModel(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, config.TokenBroker)
	for _, enabled := range []bool{false, true} {
		config, err := ParseFrontmatterConfig(map[string]any{"token-broker": enabled})
		require.NoError(t, err)
		require.NotNil(t, config.TokenBroker)
		assert.Equal(t, enabled, *config.TokenBroker)
	}
}

func TestTokenBrokerDetectionInheritance(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		detection := buildThreatDetectionWorkflowData(&WorkflowData{
			TokenBrokerEnabled: enabled,
			AI:                 "claude",
		}, "copilot")
		assert.Equal(t, enabled, detection.TokenBrokerEnabled)
		assert.Equal(t, enabled, usesTokenBrokerInference(detection))
	}
}

func TestTokenBrokerStrictDefault(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nengine: claude\ntoken-broker: true\nsafe-outputs:\n  assign-to-agent:\n  threat-detection: false\n---\nTest.", "broker.md")
	require.NoError(t, err)
	_, err = compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
}

func TestTokenBrokerRejectsUnsupportedInference(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		TokenBrokerEnabled: true,
		AI:                 "copilot",
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			Type: SandboxTypeAWF, Version: "v0.25.0",
		}},
	}
	require.ErrorContains(t, compiler.validateTokenBrokerInference(data), "renewable token-broker")
	data.TokenBrokerEnabled = false
	require.NoError(t, compiler.validateTokenBrokerInference(data))
}

func TestTokenBrokerDoesNotReplaceBYOK(t *testing.T) {
	data := &WorkflowData{
		TokenBrokerEnabled: true,
		EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{
			"COPILOT_PROVIDER_BASE_URL": "https://provider.invalid",
		}},
	}
	assert.False(t, usesTokenBrokerInference(data))
	steps := NewCopilotEngine().GetExecutionSteps(data, "broker.log")
	assert.NotContains(t, strings.Join(steps[0], "\n"), tokenBrokerURLEnv+":")
}

func TestTokenBrokerRejectsLegacyInferenceFallback(t *testing.T) {
	data := &WorkflowData{
		TokenBrokerEnabled: true,
		EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{
			"COPILOT_GITHUB_TOKEN": "${{ secrets.LEGACY_TOKEN }}",
			tokenBrokerURLEnv:      "https://not-runtime.invalid",
		}},
	}
	require.ErrorContains(t, NewCompiler().validateTokenBrokerInference(data), "renewable token-broker")
}

func TestTokenBrokerWithoutTaskConsumer(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker: true\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest.", "broker.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
	assert.NotContains(t, output, tokenBrokerURLEnv+":")
	assert.NotContains(t, output, `\"_token_broker\"`)
}

func TestTokenBrokerDoesNotGrantOIDCToCustomJobs(t *testing.T) {
	compiler := NewCompiler()
	source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker: true\nsafe-outputs:\n  assign-to-agent:\n  threat-detection: false\njobs:\n  custom:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo custom\n        env:\n          GH_AW_TOKEN_BROKER_URL: ${{ vars.GH_AW_TOKEN_BROKER_URL || secrets.GH_AW_TOKEN_BROKER_URL }}\n---\nTest."
	data, err := compiler.ParseWorkflowString(source, "broker.md")
	require.NoError(t, err)
	_, err = compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
	job, exists := compiler.jobManager.GetJob("custom")
	require.True(t, exists)
	assert.NotContains(t, job.Permissions, "id-token")
}

func TestTokenBrokerRejectsAuxiliaryCopilotInference(t *testing.T) {
	for _, config := range []struct {
		name   string
		fields string
		job    string
	}{
		{"detector", "safe-outputs:\n  assign-to-agent:\n  threat-detection:\n    engine: copilot\n", "detection"},
		{"evals", "safe-outputs:\n  assign-to-agent:\n  threat-detection: false\nevals:\n  model: copilot/claude-sonnet-4.5\n  questions:\n    - id: result\n      question: Was the task completed?\n", "evals"},
		{"github-provider", "engine:\n  id: codex\n  model-provider: github\nsafe-outputs:\n  noop:\n  threat-detection: false\n", "agent"},
	} {
		t.Run(config.name, func(t *testing.T) {
			compiler := NewCompiler()
			engine := "engine: claude\n"
			if config.name == "github-provider" {
				engine = ""
			}

			source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker: true\n" + engine + config.fields + "---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.ErrorContains(t, err, "renewable token-broker credential provider")
			require.ErrorContains(t, err, config.job+" job")
			assert.Empty(t, output)
		})
	}
}

func TestTokenBrokerGitHubProviderAcrossEngines(t *testing.T) {
	for _, engineID := range []string{"copilot", "claude", "codex", "pi"} {
		t.Run(engineID, func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker: true\nengine:\n  id: " + engineID + "\n  model-provider: github\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.ErrorContains(t, err, "renewable token-broker credential provider")
			assert.Empty(t, output)
		})
	}
}

func TestTokenBrokerRejectsDefaultCopilotEvals(t *testing.T) {
	data := &WorkflowData{
		TokenBrokerEnabled: true,
		Evals:              &EvalsConfig{Questions: []EvalDefinition{{ID: "result", Question: "Done?"}}},
	}
	assert.Equal(t, "copilot", NewCompiler().getEvalsEngineID(data))
	require.ErrorContains(t, NewCompiler().validateTokenBrokerInference(data), "evals job")
}

func TestTokenBrokerRejectsCopilotFallback(t *testing.T) {
	data := &WorkflowData{
		TokenBrokerEnabled: true,
		EngineConfig: &EngineConfig{
			ID: "claude", FallbackModels: []string{"copilot/claude-sonnet-4.5"},
		},
	}
	require.ErrorContains(t, NewCompiler().validateTokenBrokerInference(data), "agent job")
}

func TestTokenBrokerRejectsDirectCopilotDeclaredProvider(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker: true\nengine:\n  id: copilot\n  model-provider: " + provider + "\nsandbox:\n  agent: false\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.ErrorContains(t, err, "renewable token-broker credential provider")
			assert.Empty(t, output)
		})
	}
}

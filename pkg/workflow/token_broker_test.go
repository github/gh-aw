//go:build !integration

package workflow

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenBrokerExperimentalWarning(t *testing.T) {
	const warning = "Using experimental feature: token-broker"
	for _, tc := range []struct {
		name    string
		request bool
		task    bool
	}{
		{name: "disabled"},
		{name: "request-only", request: true},
		{name: "task-only", task: true},
		{name: "both", request: true, task: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, batchMode := range []bool{false, true} {
				t.Run(map[bool]string{false: "single", true: "batch"}[batchMode], func(t *testing.T) {
					enabled := tc.request || tc.task
					compiler := NewCompiler()
					compiler.SetBatchMode(batchMode)
					var output bytes.Buffer
					compiler.emitExperimentalFeatureWarningsTo(&WorkflowData{
						TokenBrokerEnabled:        enabled,
						TokenBrokerCopilotRequest: tc.request,
						TokenBrokerCopilotTask:    tc.task,
					}, &output)

					if enabled {
						assert.Equal(t, 1, compiler.GetWarningCount())
						if batchMode {
							assert.Empty(t, output.String())
							assert.Equal(t, 1, compiler.GetExperimentalFeatureUsage()[warning])
						} else {
							assert.Contains(t, output.String(), warning)
						}
					} else {
						assert.Empty(t, output.String())
						assert.Zero(t, compiler.GetWarningCount())
						assert.Zero(t, compiler.GetExperimentalFeatureUsage()[warning])
					}
				})
			}
		})
	}
}

func TestTokenBrokerCompileOptIn(t *testing.T) {
	t.Setenv(tokenBrokerURLEnv, "https://compile-time.invalid/never")
	for _, tc := range []struct {
		name   string
		optIn  string
		broker bool
		fail   bool
	}{
		{name: "disabled"},
		{name: "task-only", optIn: "token-broker:\n  copilot-task: true\n", broker: true},
		{name: "request-only", optIn: "token-broker:\n  copilot-request: true\n", broker: true, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\nengine: copilot\n" + tc.optIn + "safe-outputs:\n  noop:\n  threat-detection: false\n---\nTest broker."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			assert.Equal(t, tc.broker, data.TokenBrokerEnabled)
			output, err := compiler.CompileToYAML(data, "broker.md")
			if tc.fail {
				require.ErrorContains(t, err, "renewable credential provider")
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
	for _, field := range []string{"", "token-broker:\n  copilot-task: true\n"} {
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
					assert.Equal(t, map[string]any{"copilot-task": true}, config["_token_broker"])
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
			source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker:\n  copilot-task: true\nsafe-outputs:\n  threat-detection: false\n" + handler + "---\nTest task."
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
						ID   string            `yaml:"id"`
						Name string            `yaml:"name"`
						If   string            `yaml:"if"`
						Env  map[string]string `yaml:"env"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(output), &compiled))
			consumers := 0
			invalidations := 0
			for name, job := range compiled.Jobs {
				for _, step := range job.Steps {
					if step.ID == "process_safe_outputs" {
						assert.Equal(t, tokenBrokerURLExpression, step.Env[tokenBrokerURLEnv])
						assert.Equal(t, "process_safe_outputs", step.ID)
						assert.Equal(t, "write", job.Permissions["id-token"])
						var config map[string]any
						require.NoError(t, json.Unmarshal([]byte(step.Env["GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG"]), &config))
						assert.Equal(t, map[string]any{"copilot-task": true}, config["_token_broker"])
						consumers++
					}
					if step.Name == "Invalidate brokered Copilot credentials" {
						assert.Equal(t, "always()", step.If)
						assert.Equal(t, tokenBrokerURLExpression, step.Env[tokenBrokerURLEnv])
						assert.Equal(t, "write", job.Permissions["id-token"])
						invalidations++
					}
				}
				if name != "safe_outputs" && name != "admission" && name != "conclusion_admission" {
					assert.NotContains(t, job.Permissions, "id-token", name)
				}
			}
			assert.Equal(t, 1, consumers)
			assert.Equal(t, 1, invalidations)
		})
	}
}

func TestTokenBrokerRequestOnlyPreservesTaskCredentials(t *testing.T) {
	for _, tc := range []struct {
		handler string
		env     string
	}{
		{handler: "  assign-to-agent:\n", env: "GH_AW_ASSIGN_TO_AGENT_TOKEN"},
		{handler: "  create-agent-session:\n", env: "GH_AW_AGENT_SESSION_TOKEN"},
		{handler: "  create-issue:\n    assignees: [copilot]\n", env: "GH_AW_ASSIGN_TO_AGENT_TOKEN"},
		{handler: "  create-pull-request:\n    assignees: [copilot]\n", env: "GH_AW_ASSIGN_TO_AGENT_TOKEN"},
	} {
		t.Run(strings.TrimSpace(tc.handler), func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker:\n  copilot-request: true\nsafe-outputs:\n  threat-detection: false\n" + tc.handler + "---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			_, err = compiler.CompileToYAML(data, "broker.md")
			require.NoError(t, err)
			job, exists := compiler.jobManager.GetJob("safe_outputs")
			require.True(t, exists)
			steps := strings.Join(job.Steps, "")
			assert.Contains(t, steps, tc.env+": ${{ secrets.GH_AW_AGENT_TOKEN")
			assert.NotContains(t, steps, tokenBrokerURLEnv+":")
			assert.NotContains(t, steps, `\"_token_broker\"`)
		})
	}
}

func TestTokenBrokerTaskOnlyDoesNotInvalidateEvalsCredentials(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI:                     "claude",
		EngineConfig:           &EngineConfig{ID: "claude"},
		TokenBrokerEnabled:     true,
		TokenBrokerCopilotTask: true,
		SafeOutputs: &SafeOutputsConfig{
			AssignToAgent: &AssignToAgentConfig{},
		},
		Evals: &EvalsConfig{Questions: []EvalDefinition{{ID: "result", Question: "Done?"}}},
	}
	require.True(t, usesTokenBrokerTasks(data))
	authData, _ := compiler.buildEvalsJobPermissions(data)
	assert.False(t, usesTokenBrokerTasks(authData))
	assert.NotNil(t, data.SafeOutputs)
	job, err := compiler.buildEvalsJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.NotContains(t, job.Permissions, "id-token")
	assert.NotContains(t, strings.Join(job.Steps, ""), tokenBrokerURLEnv+":")
	assert.NotContains(t, strings.Join(job.Steps, ""), "Invalidate brokered Copilot credentials")
}

func TestTokenBrokerFrontmatterModel(t *testing.T) {
	config, err := ParseFrontmatterConfig(map[string]any{})
	require.NoError(t, err)
	assert.Nil(t, config.TokenBroker)
	for _, values := range []map[string]any{
		{"copilot-request": true},
		{"copilot-task": true},
		{"copilot-request": true, "copilot-task": true},
	} {
		config, err := ParseFrontmatterConfig(map[string]any{"token-broker": values})
		require.NoError(t, err)
		require.NotNil(t, config.TokenBroker)
		assert.Equal(t, values["copilot-request"] == true, config.TokenBroker.CopilotRequest)
		assert.Equal(t, values["copilot-task"] == true, config.TokenBroker.CopilotTask)
	}
}

func TestTokenBrokerDetectionInheritance(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		detection := buildThreatDetectionWorkflowData(&WorkflowData{
			TokenBrokerEnabled:        enabled,
			TokenBrokerCopilotRequest: enabled,
			TokenBrokerCopilotTask:    enabled,
			AI:                        "claude",
		}, "copilot")
		assert.Equal(t, enabled, detection.TokenBrokerEnabled)
		assert.Equal(t, enabled, detection.TokenBrokerCopilotRequest)
		assert.Equal(t, enabled, detection.TokenBrokerCopilotTask)
		assert.Equal(t, enabled, usesTokenBrokerInference(detection))
	}
}

func TestTokenBrokerStrictDefault(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nengine: claude\ntoken-broker:\n  copilot-task: true\nsafe-outputs:\n  assign-to-agent:\n  threat-detection: false\n---\nTest.", "broker.md")
	require.NoError(t, err)
	_, err = compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
}

func TestTokenBrokerRejectsUnsupportedInference(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		TokenBrokerEnabled:        true,
		TokenBrokerCopilotRequest: true,
		AI:                        "copilot",
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{
			Type: SandboxTypeAWF, Version: "v0.25.0",
		}},
	}
	require.ErrorIs(t, compiler.validateTokenBrokerInference(data), errTokenBrokerInferenceRequiresRenewal)
	data.TokenBrokerCopilotRequest = false
	require.NoError(t, compiler.validateTokenBrokerInference(data))
}

func TestTokenBrokerDoesNotReplaceBYOK(t *testing.T) {
	data := &WorkflowData{
		TokenBrokerEnabled:        true,
		TokenBrokerCopilotRequest: true,
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
		TokenBrokerEnabled:        true,
		TokenBrokerCopilotRequest: true,
		EngineConfig: &EngineConfig{ID: "copilot", Env: map[string]string{
			"COPILOT_GITHUB_TOKEN": "${{ secrets.LEGACY_TOKEN }}",
			tokenBrokerURLEnv:      "https://not-runtime.invalid",
		}},
	}
	require.ErrorContains(t, NewCompiler().validateTokenBrokerInference(data), "renewable credential provider")
}

func TestTokenBrokerWithoutTaskConsumer(t *testing.T) {
	compiler := NewCompiler()
	data, err := compiler.ParseWorkflowString("---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker:\n  copilot-task: true\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest.", "broker.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
	assert.NotContains(t, output, tokenBrokerURLEnv+":")
	assert.NotContains(t, output, `\"_token_broker\"`)
}

func TestTokenBrokerDoesNotGrantOIDCToCustomJobs(t *testing.T) {
	compiler := NewCompiler()
	source := "---\non: workflow_dispatch\nstrict: false\nengine: claude\ntoken-broker:\n  copilot-task: true\nsafe-outputs:\n  assign-to-agent:\n  threat-detection: false\njobs:\n  custom:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo custom\n        env:\n          GH_AW_TOKEN_BROKER_URL: ${{ vars.GH_AW_TOKEN_BROKER_URL || secrets.GH_AW_TOKEN_BROKER_URL }}\n---\nTest."
	data, err := compiler.ParseWorkflowString(source, "broker.md")
	require.NoError(t, err)
	_, err = compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
	job, exists := compiler.jobManager.GetJob("custom")
	require.True(t, exists)
	assert.NotContains(t, job.Permissions, "id-token")
}

func TestTokenBrokerSupportsDetectionInference(t *testing.T) {
	compiler := NewCompiler()
	source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker:\n  copilot-request: true\nengine: claude\nsafe-outputs:\n  noop:\n  threat-detection:\n    engine: copilot\n---\nTest."
	data, err := compiler.ParseWorkflowString(source, "broker.md")
	require.NoError(t, err)
	output, err := compiler.CompileToYAML(data, "broker.md")
	require.NoError(t, err)
	assert.Contains(t, output, "name: detection")
	assert.Contains(t, output, "id-token: write")
	assert.Contains(t, output, "id: token_broker_copilot_request")
	assert.Contains(t, output, "getBrokerToken('copilot-request', { mask: token => core.setSecret(token) }, 'github')")
	assert.Contains(t, output, "COPILOT_GITHUB_TOKEN: "+tokenBrokerCopilotRequestOutput)
	assert.Contains(t, output, "name: Invalidate brokered Copilot credentials\n        if: always()")
	assert.NotContains(t, output, "copilot-requests: write")
	job, exists := compiler.jobManager.GetJob("detection")
	require.True(t, exists)
	assertTokenBrokerInvalidationLast(t, job)
}

func TestTokenBrokerSupportsEvalsInference(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI:                        "copilot",
		EngineConfig:              &EngineConfig{ID: "copilot"},
		TokenBrokerEnabled:        true,
		TokenBrokerCopilotRequest: true,
		Evals:                     &EvalsConfig{Questions: []EvalDefinition{{ID: "result", Question: "Done?"}}},
	}
	job, err := compiler.buildEvalsJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Contains(t, job.Permissions, "id-token: write")
	assert.NotContains(t, job.Permissions, "copilot-requests: write")
	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	assert.Contains(t, steps, "id: token_broker_copilot_request")
	assert.Contains(t, steps, "COPILOT_GITHUB_TOKEN: "+tokenBrokerCopilotRequestOutput)
	assert.Contains(t, strings.Join(job.Steps, ""), "name: Invalidate brokered Copilot credentials\n        if: always()")
	assertTokenBrokerInvalidationLast(t, job)
}

func assertTokenBrokerInvalidationLast(t *testing.T, job *Job) {
	t.Helper()
	var compiled struct {
		Steps []struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
			If   string `yaml:"if"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+strings.Join(job.Steps, "")), &compiled))
	require.NotEmpty(t, compiled.Steps)
	requestIndex := -1
	invalidationIndex := -1
	for index, step := range compiled.Steps {
		if step.ID == "token_broker_copilot_request" {
			requestIndex = index
		}
		if step.Name == "Invalidate brokered Copilot credentials" {
			assert.Equal(t, "always()", step.If)
			invalidationIndex = index
		}
	}
	require.NotEqual(t, -1, requestIndex)
	assert.Greater(t, invalidationIndex, requestIndex)
	assert.Equal(t, len(compiled.Steps)-1, invalidationIndex)
}

func TestTokenBrokerGitHubProviderInAgentRemainsUnsupported(t *testing.T) {
	for _, engineID := range []string{"copilot", "claude", "codex", "pi"} {
		t.Run(engineID, func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker:\n  copilot-request: true\nengine:\n  id: " + engineID + "\n  model-provider: github\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.ErrorContains(t, err, "renewable credential provider")
			assert.Empty(t, output)
		})
	}
}

func TestTokenBrokerRejectsCopilotFallback(t *testing.T) {
	data := &WorkflowData{
		TokenBrokerEnabled:        true,
		TokenBrokerCopilotRequest: true,
		EngineConfig: &EngineConfig{
			ID: "claude", FallbackModels: []string{"copilot/claude-sonnet-4.5"},
		},
	}
	require.ErrorContains(t, NewCompiler().validateTokenBrokerInference(data), "main agent job")
}

func TestTokenBrokerDoesNotReplaceNonGitHubCopilotProvider(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic"} {
		t.Run(provider, func(t *testing.T) {
			compiler := NewCompiler()
			source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker:\n  copilot-request: true\nengine:\n  id: copilot\n  model-provider: " + provider + "\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest."
			data, err := compiler.ParseWorkflowString(source, "broker.md")
			require.NoError(t, err)
			output, err := compiler.CompileToYAML(data, "broker.md")
			require.NoError(t, err)
			assert.NotContains(t, output, "id: token_broker_copilot_request")
		})
	}
}

func TestTokenBrokerRequestRejectsCopilotRequestsPermission(t *testing.T) {
	data := &WorkflowData{TokenBrokerCopilotRequest: true}
	permissions := NewPermissions()
	permissions.Set(PermissionCopilotRequests, PermissionWrite)
	require.ErrorIs(t, validateTokenBrokerRequestPermissions(data, permissions), errTokenBrokerRequestPermissionConflict)
	permissions.Set(PermissionCopilotRequests, PermissionNone)
	require.NoError(t, validateTokenBrokerRequestPermissions(data, permissions))
}

func TestTokenBrokerRequestPermissionCompilerError(t *testing.T) {
	compiler := NewCompiler()
	source := "---\non: workflow_dispatch\nstrict: false\ntoken-broker:\n  copilot-request: true\npermissions:\n  copilot-requests: write\nengine: claude\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nTest."
	data, err := compiler.ParseWorkflowString(source, "broker.md")
	require.NoError(t, err)
	_, err = compiler.CompileToYAML(data, "broker.md")
	require.ErrorContains(t, err, "token-broker.copilot-request is incompatible")
}

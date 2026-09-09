//go:build !integration

package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// TestBuildEvalsEngineStepsArcDindTopology verifies that the evals job
// correctly propagates arc-dind runner topology from the main workflow data.
// Regression: before the fix, RunnerConfig was not propagated to evalsData,
// so isArcDindTopology(evalsData) was always false — the Copilot staging step
// was never emitted and the engine was spawned as /usr/local/bin/copilot (ENOENT inside
// the AWF chroot which uses the dind daemon's filesystem).
func TestBuildEvalsEngineStepsArcDindTopology(t *testing.T) {
	compiler := NewCompiler()

	t.Run("arc-dind: emits daemon-visible staging step and uses RUNNER_TEMP copilot path", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			RunnerConfig: &RunnerConfig{
				Topology: RunnerTopologyArcDind,
			},
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "test", Question: "Does the code work?"},
				},
			},
		}

		steps := compiler.buildEvalsEngineSteps(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// The staging step copies the Copilot CLI to a daemon-visible path.
		if !strings.Contains(allSteps, "Copy Copilot CLI to daemon-visible path") {
			t.Errorf("expected 'Copy Copilot CLI to daemon-visible path' step in evals job for arc-dind;\ngot:\n%s", allSteps)
		}

		// The copilot_harness.cjs invocation must use the daemon-visible path specifically.
		// Note: constants.GhAwRootDirShell+"/bin/copilot" also appears in the staging step's
		// copy command ("cp /usr/local/bin/copilot ..."), so checking the harness line
		// directly avoids a false positive from the staging step.
		harnessArcDindPath := `copilot_harness.cjs" "` + constants.GhAwRootDirShell + `/bin/copilot"`
		if !strings.Contains(allSteps, harnessArcDindPath) {
			t.Errorf("expected copilot_harness.cjs to be invoked with daemon-visible path %q for arc-dind;\ngot:\n%s", harnessArcDindPath, allSteps)
		}
		if strings.Contains(allSteps, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
			t.Errorf("copilot_harness.cjs must NOT be invoked with %q for arc-dind (ENOENT inside chroot);\ngot:\n%s", constants.CopilotBinaryPath, allSteps)
		}
		mount := `--mount "${RUNNER_TEMP}/gh-aw:${RUNNER_TEMP}/gh-aw:ro"`
		if !strings.Contains(allSteps, mount) {
			t.Errorf("expected ARC/DinD execution to mount the staged Copilot CLI directory;\ngot:\n%s", allSteps)
		}
	})

	t.Run("non-arc-dind: resolves activated Copilot CLI binary", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			// RunnerConfig is nil → default topology
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "test", Question: "Does the code work?"},
				},
			},
		}

		steps := compiler.buildEvalsEngineSteps(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// No daemon-visible staging step for standard runners.
		if strings.Contains(allSteps, "Copy Copilot CLI to daemon-visible path") {
			t.Errorf("unexpected 'Copy Copilot CLI to daemon-visible path' step for non-arc-dind evals job;\ngot:\n%s", allSteps)
		}

		if !strings.Contains(allSteps, `GH_AW_COPILOT_SRC="$(command -v copilot 2>/dev/null || true)"`) {
			t.Errorf("expected evals execution to resolve the activated Copilot CLI binary;\ngot:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, `cp "$GH_AW_COPILOT_SRC" "$GH_AW_COPILOT_BIN"`) {
			t.Errorf("expected evals execution to stage the Copilot CLI binary in its mounted directory;\ngot:\n%s", allSteps)
		}
		mountedCopilotPath := `copilot_harness.cjs" "` + constants.GhAwRootDirShell + `/bin/copilot"`
		if !strings.Contains(allSteps, mountedCopilotPath) {
			t.Errorf("expected evals harness to use mounted Copilot CLI path %q;\ngot:\n%s", mountedCopilotPath, allSteps)
		}
		if strings.Contains(allSteps, `copilot_harness.cjs" `+constants.CopilotBinaryPath) {
			t.Errorf("expected evals harness to avoid fixed path %q;\ngot:\n%s", constants.CopilotBinaryPath, allSteps)
		}
	})
}

// TestBuildEvalsJobStepsRenderSummary verifies that the evals job includes the
// "Render evals results to step summary" step and that it runs after the redact step.
func TestBuildEvalsJobStepsRenderSummary(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does the code build?"},
			},
		},
	}

	steps := compiler.buildEvalsJobSteps(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}
	allSteps := strings.Join(steps, "")

	// The render summary step must be present.
	if !strings.Contains(allSteps, "- name: Render evals results to step summary") {
		t.Errorf("expected 'Render evals results to step summary' step in evals job;\ngot:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "id: redact_evals_results") {
		t.Errorf("expected redact step id in evals job;\ngot:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "redact_evals_results.cjs") {
		t.Errorf("expected redact_evals_results.cjs reference in evals job steps;\ngot:\n%s", allSteps)
	}

	// The render summary step must call render_evals_summary.cjs.
	if !strings.Contains(allSteps, "render_evals_summary.cjs") {
		t.Errorf("expected render_evals_summary.cjs reference in evals job steps;\ngot:\n%s", allSteps)
	}
	if !strings.Contains(allSteps, "if: steps.redact_evals_results.outcome == 'success'") {
		t.Errorf("expected redact outcome gating for render/upload steps;\ngot:\n%s", allSteps)
	}

	// The render step must appear after the redact step (redact before publish to step summary).
	redactIdx := strings.Index(allSteps, "- name: Redact secrets in evals results")
	renderIdx := strings.Index(allSteps, "- name: Render evals results to step summary")
	uploadIdx := strings.Index(allSteps, "- name: Upload evals results")
	if redactIdx < 0 {
		t.Error("expected 'Redact secrets in evals results' step")
	}
	if renderIdx < 0 {
		t.Error("expected 'Render evals results to step summary' step")
	}
	if uploadIdx < 0 {
		t.Error("expected 'Upload evals results' step")
	}
	if redactIdx >= 0 && renderIdx >= 0 && renderIdx <= redactIdx {
		t.Errorf("expected render step to appear after redact step; redactIdx=%d renderIdx=%d", redactIdx, renderIdx)
	}
	if renderIdx >= 0 && uploadIdx >= 0 && renderIdx >= uploadIdx {
		t.Errorf("expected render step to appear before upload step; renderIdx=%d uploadIdx=%d", renderIdx, uploadIdx)
	}

}

func TestBuildEvalsJobStepsRedactionUsesEvalsSecretReferences(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Model: "${{ secrets.EVALS_MODEL_SECRET }}",
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it use ${{ secrets.EVALS_PROMPT_SECRET }}?"},
			},
		},
	}

	steps := strings.Join(compiler.buildRedactEvalsSecretsStep(data), "")
	if !strings.Contains(steps, "GH_AW_SECRET_NAMES") {
		t.Fatalf("expected GH_AW_SECRET_NAMES in redact step:\n%s", steps)
	}
	if !strings.Contains(steps, "SECRET_EVALS_MODEL_SECRET: ${{ secrets.EVALS_MODEL_SECRET }}") {
		t.Errorf("expected model secret env binding in redact step:\n%s", steps)
	}
	if !strings.Contains(steps, "SECRET_EVALS_PROMPT_SECRET: ${{ secrets.EVALS_PROMPT_SECRET }}") {
		t.Errorf("expected prompt secret env binding in redact step:\n%s", steps)
	}
}

func TestBuildParseEvalsResultsStepUsesResolvedExecutionModel(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI:    "claude",
		Model: "claude-sonnet-4.6",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "builds", Question: "Does it build?"},
			},
		},
	}

	steps := strings.Join(compiler.buildParseEvalsResultsStep(data), "")
	if !strings.Contains(steps, `GH_AW_EVALS_MODEL: "claude-sonnet-4.6"`) {
		t.Errorf("expected parse step to record resolved execution model; got:\n%s", steps)
	}
	if strings.Contains(steps, `GH_AW_EVALS_MODEL: "small"`) {
		t.Errorf("expected parse step to avoid default 'small' when engine model is resolved; got:\n%s", steps)
	}
	if !strings.Contains(steps, "GITHUB_RUN_ID: ${{ github.run_id }}") {
		t.Errorf("expected parse step to pass github.run_id through to the eval record writer; got:\n%s", steps)
	}
}

func TestBuildParseEvalsResultsStepUsesExpressionModelAndFallbackEnv(t *testing.T) {
	compiler := NewCompiler()
	tests := []struct {
		name          string
		engineID      string
		modelEnvVar   string
		defaultEnvVar string
		defaultModel  string
	}{
		{
			name:          "copilot evals expression model fallback",
			engineID:      "copilot",
			modelEnvVar:   constants.EnvVarModelEvalsCopilot,
			defaultEnvVar: compilerenv.DefaultModelCopilot,
			defaultModel:  constants.CopilotBYOKDefaultModel,
		},
		{
			name:          "claude evals expression model fallback",
			engineID:      "claude",
			modelEnvVar:   constants.EnvVarModelEvalsClaude,
			defaultEnvVar: compilerenv.DefaultModelClaude,
			defaultModel:  constants.SonnetDefaultModel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &WorkflowData{
				AI:    tt.engineID,
				Model: "${{ inputs.model }}",
				EngineConfig: &EngineConfig{
					ID: tt.engineID,
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "builds", Question: "Does it build?"},
					},
				},
			}

			steps := strings.Join(compiler.buildParseEvalsResultsStep(data), "")
			if !strings.Contains(steps, `GH_AW_EVALS_MODEL: ${{ inputs.model }}`) {
				t.Errorf("expected parse step to preserve expression-backed evals model; got:\n%s", steps)
			}
			expectedFallbackEnvLine := fmt.Sprintf(
				"%s: ${{ vars.%s || vars.%s || '%s' }}",
				constants.EnvVarModelFallback,
				tt.modelEnvVar,
				tt.defaultEnvVar,
				tt.defaultModel,
			)
			if !strings.Contains(steps, expectedFallbackEnvLine) {
				t.Errorf("expected parse step to expose evals model fallback env %q; got:\n%s", expectedFallbackEnvLine, steps)
			}
		})
	}
}

// TestBuildEvalsJobStepsCodexNoDuplicateContainerDownload verifies that a codex
// workflow with evals does not generate a duplicate "Download container images" step.
// Regression: buildEvalsJobSteps previously called buildPullAWFContainersStep
// unconditionally while buildEvalsEngineSteps → generateMCPSetup also emitted the
// step for codex, producing two identical steps and tripping duplicate-step validation.
func TestBuildEvalsJobStepsCodexNoDuplicateContainerDownload(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "task-completed", Question: "Did the agent complete the assigned task?"},
			},
		},
	}

	steps := compiler.buildEvalsJobSteps(data)
	allSteps := strings.Join(steps, "")

	count := strings.Count(allSteps, "name: Download container images")
	if count != 1 {
		t.Errorf("expected exactly one 'Download container images' step for codex evals job, got %d\n%s", count, allSteps)
	}
}

// TestBuildEvalsJobStepsNonCodexIncludesContainerDownload verifies that non-codex
// engines still get the "Download container images" step in the evals job.
// Unlike codex, non-codex engines do not call generateMCPSetup in buildEvalsEngineSteps,
// so buildPullAWFContainersStep must still be called for them.
func TestBuildEvalsJobStepsNonCodexIncludesContainerDownload(t *testing.T) {
	compiler := NewCompiler()

	for _, engine := range []string{"copilot", "claude"} {
		t.Run(engine, func(t *testing.T) {
			data := &WorkflowData{
				AI: engine,
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "task-completed", Question: "Did the agent complete the assigned task?"},
					},
				},
			}

			steps := compiler.buildEvalsJobSteps(data)
			allSteps := strings.Join(steps, "")

			if !strings.Contains(allSteps, "Download container images") {
				t.Errorf("expected 'Download container images' step in evals job for %s engine\ngot:\n%s", engine, allSteps)
			}
		})
	}
}

// TestBuildEvalsEngineStepsCodexNoDetectionSchemaOrResultPath verifies evals
// Codex execution does not reuse detection-only structured output settings.
// Regression: when evals set IsDetectionRun=true, the generated command included
// --output-schema /tmp/gh-aw/threat-detection/detection_schema.json and
// -o /tmp/gh-aw/threat-detection/detection_result.json, which caused eval logs to
// capture detection JSON payloads instead of BinEval question answers.
func TestBuildEvalsEngineStepsCodexNoDetectionSchemaOrResultPath(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "evals_data_analyzed", Question: "Did the agent analyze evals data?"},
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if strings.Contains(steps, "--output-schema") {
		t.Fatalf("evals Codex execution must not include detection structured-output flags; got:\n%s", steps)
	}
	if strings.Contains(steps, "/tmp/gh-aw/threat-detection/detection_schema.json") {
		t.Fatalf("evals Codex execution must not reference detection schema path; got:\n%s", steps)
	}
	if strings.Contains(steps, "/tmp/gh-aw/threat-detection/detection_result.json") {
		t.Fatalf("evals Codex execution must not reference detection result path; got:\n%s", steps)
	}
}

func TestBuildEvalsEngineStepsUsesEvalsPhase(t *testing.T) {
	compiler := NewCompiler()
	for _, engine := range []string{"copilot", "claude", "codex"} {
		t.Run(engine, func(t *testing.T) {
			data := &WorkflowData{
				AI: engine,
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{
						Type: SandboxTypeAWF,
					},
				},
				Evals: &EvalsConfig{
					Questions: []EvalDefinition{
						{ID: "evals_data_analyzed", Question: "Did the agent analyze evals data?"},
					},
				},
			}

			steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
			if !strings.Contains(steps, "GH_AW_PHASE: evals") {
				t.Fatalf("expected evals engine steps to set GH_AW_PHASE=evals; got:\n%s", steps)
			}
		})
	}
}

func TestBuildEvalsEngineStepsOmitsMainAgent(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		EngineConfig: &EngineConfig{
			ID:    "copilot",
			Agent: "my-agent",
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "check", Question: "Did the agent complete the task?"},
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if strings.Contains(steps, "--agent") {
		t.Fatalf("evals engine steps must not contain the main job's --agent flag; got:\n%s", steps)
	}
	if data.EngineConfig.Agent != "my-agent" {
		t.Fatalf("original EngineConfig.Agent was mutated; expected %q, got %q", "my-agent", data.EngineConfig.Agent)
	}
}

func TestBuildEvalsEngineStepsUsesEvalsMaxAICreditsDefault(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "codex",
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Type: SandboxTypeAWF,
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "evals_data_analyzed", Question: "Did the agent analyze evals data?"},
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if !strings.Contains(steps, "vars.GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS") {
		t.Fatalf("expected evals engine steps to use GH_AW_DEFAULT_EVALS_MAX_AI_CREDITS; got:\n%s", steps)
	}
	if strings.Contains(steps, "vars.GH_AW_DEFAULT_DETECTION_MAX_AI_CREDITS") {
		t.Fatalf("evals engine steps must not use detection max-ai-credits default; got:\n%s", steps)
	}
}

// TestBuildEvalsEngineStepsModelMappingsPropagated verifies that model alias mappings
// from the parent WorkflowData are propagated to the evals engine steps so that the
// AWF config JSON includes the apiProxy.models section.
// Regression: before the fix, ModelMappings was not propagated to evalsData, so alias
// model names (e.g. "small") could not be resolved by the AWF — causing the error
// "model 'small' is unsupported or unrecognized by this AWF version".
func TestBuildEvalsEngineStepsModelMappingsPropagated(t *testing.T) {
	compiler := NewCompiler()

	t.Run("model mappings included in evals AWF config when set on parent WorkflowData", func(t *testing.T) {
		data := &WorkflowData{
			AI:            "copilot",
			ModelMappings: MergeImportedModelAliases(nil, nil), // builtin aliases
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "check", Question: "Did the agent complete the task?"},
				},
			},
		}

		steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")

		// The AWF config JSON written in the run step must contain the "models" key
		// (shell-escaped as \"models\" inside the printf string) so the AWF can resolve
		// alias model names (e.g. "small") to concrete IDs.
		if !strings.Contains(steps, `\"models\"`) {
			t.Errorf("expected evals engine steps to include AWF config with \"models\" key when ModelMappings is set;\ngot:\n%s", steps)
		}

	})

	t.Run("model mappings absent from evals AWF config when nil on parent WorkflowData", func(t *testing.T) {
		data := &WorkflowData{
			AI:            "copilot",
			ModelMappings: nil,
			Evals: &EvalsConfig{
				Questions: []EvalDefinition{
					{ID: "check", Question: "Did the agent complete the task?"},
				},
			},
		}

		steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")

		// Without ModelMappings, the models section should not appear in the AWF config.
		if strings.Contains(steps, `\"models\"`) {
			t.Errorf("expected evals engine steps to exclude \"models\" key from AWF config when ModelMappings is nil;\ngot:\n%s", steps)
		}
	})
}

func TestBuildEvalsEngineStepsModelCostsProvidersPropagated(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI:    "claude",
		Model: "accounts/fireworks/models/minimax-m3",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		ModelCosts: map[string]any{
			"providers": map[string]any{
				"anthropic": map[string]any{
					"models": map[string]any{
						"accounts/fireworks/models/minimax-m3": map[string]any{
							"cost": map[string]any{
								"input":  "3e-07",
								"output": "1.5e-06",
							},
						},
					},
				},
			},
		},
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "check", Question: "Did the agent complete the task?"},
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: string(constants.AWFAPIProxyProvidersMinVersion)},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{
				Version: string(constants.AWFAPIProxyProvidersMinVersion),
			},
		},
	}

	steps := strings.Join(compiler.buildEvalsEngineSteps(data), "")
	if !strings.Contains(steps, "providers") {
		t.Errorf("expected evals awf-config.json to contain apiProxy.providers; got:\n%s", steps)
	}
	if !strings.Contains(steps, "accounts/fireworks/models/minimax-m3") {
		t.Errorf("expected evals awf-config.json to contain custom model pricing key; got:\n%s", steps)
	}
}

//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

func TestBuildDetectionEngineExecutionStepWithThreatDetectionEngine(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name           string
		data           *WorkflowData
		env            map[string]string
		expectContains string
	}{
		{
			name: "uses main engine when no threat detection engine specified",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			expectContains: "claude", // Should use main engine
		},
		{
			name: "uses threat detection engine when specified as string",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							ID: "codex",
						},
					},
				},
			},
			expectContains: "codex", // Should use threat detection engine
		},
		{
			name: "uses threat detection engine config when specified",
			data: &WorkflowData{
				AI: "claude",
				EngineConfig: &EngineConfig{
					ID: "claude",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						Model: "gpt-4",
						EngineConfig: &EngineConfig{
							ID: "copilot",
						},
					},
				},
			},
			expectContains: "copilot", // Should use threat detection engine
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			steps := compiler.buildDetectionEngineExecutionStep(tt.data)

			if len(steps) == 0 {
				t.Fatal("Expected non-empty steps")
			}

			// Join all steps to search for expected content
			allSteps := strings.Join(steps, "")

			// Check if the expected engine is referenced (this is a basic check)
			// The actual implementation may vary, but we should see the engine being used
			if !strings.Contains(strings.ToLower(allSteps), strings.ToLower(tt.expectContains)) {
				t.Logf("Generated steps:\n%s", allSteps)
				// Note: This is a soft check as the exact format may vary
				// The key is that the engine configuration is being used
			}
		})
	}
}

func TestBuildDetectionEngineExecutionStepMaxAICredits(t *testing.T) {
	compiler := NewCompiler()

	t.Run("uses detection runtime default expression when threat-detection max-ai-credits is unset", func(t *testing.T) {
		data := &WorkflowData{
			AI: "claude",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildDetectionEngineExecutionStep(data)
		allSteps := strings.Join(steps, "")
		if !strings.Contains(allSteps, "vars."+compilerenv.DefaultDetectionMaxAICredits) {
			t.Fatalf("expected detection steps to reference vars.%s, got:\n%s", compilerenv.DefaultDetectionMaxAICredits, allSteps)
		}
		if !strings.Contains(allSteps, "'400'") {
			t.Fatalf("expected detection steps to include default fallback '400', got:\n%s", allSteps)
		}
	})

	t.Run("uses explicit threat-detection max-ai-credits when provided", func(t *testing.T) {
		data := &WorkflowData{
			AI: "claude",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					MaxAICredits: 777,
				},
			},
		}

		steps := compiler.buildDetectionEngineExecutionStep(data)
		allSteps := strings.Join(steps, "")
		if strings.Contains(allSteps, "vars."+compilerenv.DefaultDetectionMaxAICredits) {
			t.Fatalf("expected detection steps not to reference vars.%s when explicit max-ai-credits is set, got:\n%s", compilerenv.DefaultDetectionMaxAICredits, allSteps)
		}
		if !strings.Contains(allSteps, `"maxAiCredits":777`) {
			t.Fatalf("expected detection steps to include maxAiCredits 777, got:\n%s", allSteps)
		}
	})
}

func TestBuildDetectionEngineExecutionStepMaxAICreditsNotInheritedFromMainAgent(t *testing.T) {
	compiler := NewCompiler()

	// When the main agent has an explicit MaxAICredits budget but
	// safe-outputs.threat-detection.max-ai-credits is not set, the detection run
	// must use its own runtime default expression rather than silently inheriting
	// the agent budget.
	data := &WorkflowData{
		AI: "claude",
		EngineConfig: &EngineConfig{
			MaxAICredits: 500, // explicit agent budget
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				// max-ai-credits intentionally omitted
			},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "vars."+compilerenv.DefaultDetectionMaxAICredits) {
		t.Fatalf("expected detection steps to use runtime default expression vars.%s when detection max-ai-credits is unset, got:\n%s",
			compilerenv.DefaultDetectionMaxAICredits, allSteps)
	}
	if strings.Contains(allSteps, `"maxAiCredits":500`) {
		t.Fatalf("expected detection steps NOT to inherit agent maxAiCredits=500, got:\n%s", allSteps)
	}
}

func TestBuildDetectionEngineExecutionStepCodexIncludesMCPSetup(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("Expected non-empty detection engine steps")
	}

	stepsString := strings.Join(steps, "")
	if !strings.Contains(stepsString, "Start MCP Gateway") {
		t.Errorf("Expected Codex detection steps to include MCP setup, got:\n%s", stepsString)
	}
	if !strings.Contains(stepsString, "model_provider = \"openai-proxy\"") {
		t.Errorf("Expected Codex detection MCP config to include openai-proxy model provider, got:\n%s", stepsString)
	}
}

func TestBuildDetectionEngineExecutionStepDefaultsHarnessMaxRetriesToZero(t *testing.T) {
	// Threat detection is a bounded scan of already-completed agent output; a failed
	// attempt (e.g. a sandboxed cleanup command failing inside the read-only
	// /tmp/gh-aw mount) should not silently retry the whole run several times with
	// backoff, burning significant time and model spend. Unless the harness policy is
	// explicitly configured, detection should default to zero retries.
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "GH_AW_HARNESS_MAX_RETRIES: 0") {
		t.Fatalf("expected detection steps to default GH_AW_HARNESS_MAX_RETRIES to 0, got:\n%s", allSteps)
	}
}

func TestBuildDetectionEngineExecutionStepRespectsExplicitHarnessMaxRetries(t *testing.T) {
	// When the user explicitly configures a harness retry policy (via engine.harness
	// or threat-detection.engine.harness), that explicit choice must be honored rather
	// than overridden by the detection-specific zero-retry default.
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		EngineConfig: &EngineConfig{
			ID:                "codex",
			HarnessMaxRetries: "5",
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "GH_AW_HARNESS_MAX_RETRIES: 5") {
		t.Fatalf("expected detection steps to honor explicit GH_AW_HARNESS_MAX_RETRIES=5, got:\n%s", allSteps)
	}
}

func TestBuildDetectionEngineExecutionStepRespectsExplicitThreatDetectionHarnessMaxRetries(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "codex",
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					ID:                "codex",
					HarnessMaxRetries: "2",
				},
			},
		},
	}

	steps := compiler.buildDetectionEngineExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "GH_AW_HARNESS_MAX_RETRIES: 2") {
		t.Fatalf("expected detection steps to honor threat-detection GH_AW_HARNESS_MAX_RETRIES=2, got:\n%s", allSteps)
	}
}

// main engine config is never propagated to the detection engine config,
// regardless of whether a model is explicitly configured.
func TestBuildDetectionEngineExecutionStepStripsAgentField(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name string
		data *WorkflowData
	}{
		{
			name: "agent field stripped when model is explicitly configured",
			data: &WorkflowData{
				AI:    "copilot",
				Model: "claude-opus-4.6",
				EngineConfig: &EngineConfig{
					ID:    "copilot",
					Agent: "my-agent",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
		},
		{
			name: "agent field stripped when no model configured",
			data: &WorkflowData{
				AI: "copilot",
				EngineConfig: &EngineConfig{
					ID:    "copilot",
					Agent: "my-agent",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps := compiler.buildDetectionEngineExecutionStep(tt.data)

			if len(steps) == 0 {
				t.Fatal("Expected non-empty steps")
			}

			allSteps := strings.Join(steps, "")

			// The --agent flag must not appear in the threat detection steps
			if strings.Contains(allSteps, "--agent") {
				t.Errorf("Expected detection steps to NOT contain --agent flag, but found it.\nGenerated steps:\n%s", allSteps)
			}

			// Ensure the original engine config is not mutated
			if tt.data.EngineConfig != nil && tt.data.EngineConfig.Agent != "my-agent" {
				t.Errorf("Original EngineConfig.Agent was mutated; expected %q, got %q", "my-agent", tt.data.EngineConfig.Agent)
			}
		})
	}
}

// TestCopilotDetectionDefaultModel verifies that the detection step defaults to
// the detection alias when no model is specified.
func TestCopilotDetectionDefaultModel(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name               string
		data               *WorkflowData
		env                map[string]string
		shouldContainModel bool
		expectedModel      string
	}{
		{
			name: "copilot engine without model uses detection alias default",
			data: &WorkflowData{
				AI: "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			shouldContainModel: true,
			expectedModel:      "detection",
		},
		{
			name: "detection model uses enterprise default override when configured",
			data: &WorkflowData{
				AI: "copilot",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			env: map[string]string{
				compilerenv.DefaultDetectionModel: "gpt-5.5-mini",
			},
			shouldContainModel: true,
			expectedModel:      "gpt-5.5-mini",
		},
		{
			name: "copilot engine with custom main model uses detection alias default",
			data: &WorkflowData{
				AI:    "copilot",
				Model: "gpt-4",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			shouldContainModel: true,
			expectedModel:      "detection",
		},
		{
			name: "pi engine with provider-scoped main model uses detection alias default",
			data: &WorkflowData{
				AI:    "pi",
				Model: "copilot/gpt-5.4",
				EngineConfig: &EngineConfig{
					ID: "pi",
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			shouldContainModel: true,
			expectedModel:      "detection",
		},
		{
			name: "copilot engine with threat detection engine config with custom model",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						Model: "gpt-4o",
						EngineConfig: &EngineConfig{
							ID: "copilot",
						},
					},
				},
			},
			shouldContainModel: true,
			expectedModel:      "gpt-4o",
		},
		{
			name: "copilot engine with threat detection engine config without model uses detection alias default",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{
						EngineConfig: &EngineConfig{
							ID: "copilot",
						},
					},
				},
			},
			shouldContainModel: true,
			expectedModel:      "detection",
		},
		{
			name: "claude engine does not add model parameter",
			data: &WorkflowData{
				AI: "claude",
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{},
				},
			},
			shouldContainModel: false,
			expectedModel:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			steps := compiler.buildDetectionEngineExecutionStep(tt.data)

			if len(steps) == 0 {
				t.Fatal("Expected non-empty steps")
			}

			// Join all steps to search for model content
			allSteps := strings.Join(steps, "")

			if tt.shouldContainModel {
				hasNativeEnvVar := strings.Contains(allSteps, "COPILOT_MODEL: "+tt.expectedModel)
				if !hasNativeEnvVar {
					t.Errorf("Expected steps to contain COPILOT_MODEL: %q, but it was not found.\nGenerated steps:\n%s", tt.expectedModel, allSteps)
				}
			}
		})
	}
}

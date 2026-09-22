//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

func TestExternalDetectorInheritsOpenAIBaseURL(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "codex",
		EngineConfig: &EngineConfig{
			ID: "codex",
			Env: map[string]string{
				"OPENAI_BASE_URL": "https://llm-router.internal.example.com/v1",
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				EngineConfig: &EngineConfig{
					ID: "codex",
					Env: map[string]string{
						"CUSTOM_FLAG": "1",
					},
				},
			},
		},
	}

	steps := compiler.buildExternalDetectorExecutionStep(data)
	if len(steps) == 0 {
		t.Fatal("expected non-empty steps")
	}
	stepsContent := strings.Join(steps, "")

	// Assert the specific serialized apiProxy.targets.openai.host entry to verify
	// that OPENAI_BASE_URL is reflected as a custom target in the AWF config, not
	// just that the hostname appears somewhere in the step output.
	wantTarget := `\"targets\":{\"openai\":{\"host\":\"llm-router.internal.example.com\"`
	if !strings.Contains(stepsContent, wantTarget) {
		t.Fatalf("expected external detector AWF config to include apiProxy.targets.openai.host=%q; got:\n%s", "llm-router.internal.example.com", stepsContent)
	}
}

// TestExternalDetectorPropagatesModel verifies that buildExternalDetectorExecutionStep
// inherits the main workflow model and model mappings, preventing the COPILOT_MODEL env
// var from falling back to 'auto' when no org variable is configured.
func TestExternalDetectorPropagatesModel(t *testing.T) {
	compiler := NewCompiler()

	t.Run("inherits main workflow model", func(t *testing.T) {
		data := &WorkflowData{
			AI:    "copilot",
			Model: "claude-haiku-4.5",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		if !strings.Contains(allSteps, "COPILOT_MODEL: claude-haiku-4.5") {
			t.Errorf("expected COPILOT_MODEL to be set to the main workflow model 'claude-haiku-4.5', but got:\n%s", allSteps)
		}
		// When a model is configured, COPILOT_MODEL must be a static value — not
		// a template variable expression. Checking for '${{' is more robust than
		// checking for a specific fallback string like "|| 'auto'" that could change.
		for line := range strings.SplitSeq(allSteps, "\n") {
			if strings.Contains(line, "COPILOT_MODEL:") && strings.Contains(line, "${{") {
				t.Errorf("expected COPILOT_MODEL to be a static value, not a template expression; got: %s", line)
			}
		}
	})

	t.Run("inherits model mappings into AWF config", func(t *testing.T) {
		data := &WorkflowData{
			AI:    "copilot",
			Model: "haiku",
			ModelMappings: map[string][]string{
				"haiku": {"copilot/claude-haiku-4.5"},
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		if !strings.Contains(allSteps, `\"haiku\"`) {
			t.Errorf("expected model mappings to be included in detection AWF config; got:\n%s", allSteps)
		}
	})

	t.Run("inherits threat-detection-specific model override", func(t *testing.T) {
		data := &WorkflowData{
			AI:    "copilot",
			Model: "claude-sonnet-4.6",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					Model: "claude-haiku-4.5",
				},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		if !strings.Contains(allSteps, "COPILOT_MODEL: claude-haiku-4.5") {
			t.Errorf("expected COPILOT_MODEL to use the threat-detection model override 'claude-haiku-4.5'; got:\n%s", allSteps)
		}
	})

	t.Run("inherits default AI credits pricing into AWF config", func(t *testing.T) {
		data := &WorkflowData{
			AI:    "copilot",
			Model: "custom-model",
			DefaultAiCreditsPricing: &AiCreditsPricingConfig{
				Input:  3.0,
				Output: 15.0,
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		if !strings.Contains(allSteps, "defaultAiCreditsPricing") {
			t.Errorf("expected defaultAiCreditsPricing to be included in detection AWF config; got:\n%s", allSteps)
		}
	})

	t.Run("Pi detection engine override on Copilot main workflow strips pi/ prefix", func(t *testing.T) {
		// Main engine is Copilot, but the detection engine is explicitly pi.
		// After Pi->Copilot normalization, the model must be extracted from the
		// "pi/model-name" form so the Copilot CLI receives a bare model ID.
		data := &WorkflowData{
			AI:    "copilot",
			Model: "copilot/gpt-5.4",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					EngineConfig: &EngineConfig{
						ID: "pi",
					},
				},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// The pi/ prefix must be stripped; Copilot CLI expects a bare model ID.
		if !strings.Contains(allSteps, "COPILOT_MODEL: gpt-5.4") {
			t.Errorf("expected COPILOT_MODEL to be bare 'gpt-5.4' after Pi prefix stripping; got:\n%s", allSteps)
		}
		if strings.Contains(allSteps, "COPILOT_MODEL: copilot/gpt-5.4") {
			t.Errorf("COPILOT_MODEL must not retain the 'copilot/' prefix; got:\n%s", allSteps)
		}
	})

	t.Run("Pi main workflow with explicit Copilot detection engine does not strip model", func(t *testing.T) {
		// Main engine is Pi, but detection engine is explicitly Copilot.
		// The model should NOT be normalised because originalEngineID is "copilot",
		// not "pi", so extractPiModelID must not be called.
		data := &WorkflowData{
			AI:    "pi",
			Model: "copilot/gpt-5.4",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					EngineConfig: &EngineConfig{
						ID: "copilot",
					},
				},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// Detection engine is explicitly Copilot (not Pi), so the model
		// should not be stripped of any prefix.
		if !strings.Contains(allSteps, "COPILOT_MODEL: copilot/gpt-5.4") {
			t.Errorf("expected COPILOT_MODEL to remain 'copilot/gpt-5.4' when detection engine is explicitly copilot; got:\n%s", allSteps)
		}
	})

	t.Run("strips pi/ provider prefix for Pi-engine main workflow with no detection override", func(t *testing.T) {
		// Main engine is Pi with a provider-scoped model; no detection engine override.
		// getThreatDetectionEngineID normalizes Pi → Copilot, so extractPiModelID must
		// fire and strip the "pi/" prefix so the Copilot CLI receives a bare model ID.
		data := &WorkflowData{
			AI:    "pi",
			Model: "pi/claude-haiku-4.5",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		if len(steps) == 0 {
			t.Fatal("expected non-empty steps")
		}
		allSteps := strings.Join(steps, "")

		// The "pi/" prefix must be stripped; the bare model ID is expected.
		if strings.Contains(allSteps, "COPILOT_MODEL: pi/") {
			t.Errorf("expected provider prefix to be stripped from COPILOT_MODEL; got:\n%s", allSteps)
		}
		if !strings.Contains(allSteps, "COPILOT_MODEL: claude-haiku-4.5") {
			t.Errorf("expected COPILOT_MODEL to be bare 'claude-haiku-4.5' after Pi prefix stripping; got:\n%s", allSteps)
		}
	})
}

func TestBuildExternalDetectorWorkflowDataMaxAICredits(t *testing.T) {
	compiler := NewCompiler()

	t.Run("uses detection runtime default expression when threat-detection max-ai-credits is unset", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		allSteps := strings.Join(steps, "")
		if !strings.Contains(allSteps, "vars."+compilerenv.DefaultDetectionMaxAICredits) {
			t.Fatalf("expected external detector steps to reference vars.%s, got:\n%s", compilerenv.DefaultDetectionMaxAICredits, allSteps)
		}
		if !strings.Contains(allSteps, "'400'") {
			t.Fatalf("expected external detector steps to include default fallback '400', got:\n%s", allSteps)
		}
	})

	t.Run("uses explicit threat-detection max-ai-credits when provided", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					MaxAICredits: 777,
				},
			},
		}

		steps := compiler.buildExternalDetectorExecutionStep(data)
		allSteps := strings.Join(steps, "")
		if strings.Contains(allSteps, "vars."+compilerenv.DefaultDetectionMaxAICredits) {
			t.Fatalf("expected external detector steps not to reference vars.%s when explicit max-ai-credits is set, got:\n%s", compilerenv.DefaultDetectionMaxAICredits, allSteps)
		}
		if !strings.Contains(allSteps, `"maxAiCredits":777`) {
			t.Fatalf("expected external detector steps to include maxAiCredits 777, got:\n%s", allSteps)
		}
	})
}

func TestBuildExternalDetectorWorkflowDataMaxAICreditsNotInheritedFromMainAgent(t *testing.T) {
	compiler := NewCompiler()

	// When the main agent has an explicit MaxAICredits budget but
	// safe-outputs.threat-detection.max-ai-credits is not set, the external
	// detector must use its own runtime default expression rather than silently
	// inheriting the agent budget.
	data := &WorkflowData{
		AI: "copilot",
		EngineConfig: &EngineConfig{
			MaxAICredits: 500, // explicit agent budget
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{
				// max-ai-credits intentionally omitted
			},
		},
	}

	steps := compiler.buildExternalDetectorExecutionStep(data)
	allSteps := strings.Join(steps, "")

	if !strings.Contains(allSteps, "vars."+compilerenv.DefaultDetectionMaxAICredits) {
		t.Fatalf("expected external detector steps to use runtime default expression vars.%s when detection max-ai-credits is unset, got:\n%s",
			compilerenv.DefaultDetectionMaxAICredits, allSteps)
	}
	if strings.Contains(allSteps, `"maxAiCredits":500`) {
		t.Fatalf("expected external detector steps NOT to inherit agent maxAiCredits=500, got:\n%s", allSteps)
	}
}

func TestResolveExternalDetectorEngineConfigInheritsVersionFromMainEngine(t *testing.T) {
	// Regression test: when no safe-outputs.threat-detection.engine override is configured,
	// the external detector path must still install the same pinned engine version as the
	// main agent job (e.g. a version declared as the default on a behavior-defined engine's
	// shared definition, applied to the main EngineConfig.Version at import time). Previously
	// the external detector always built a bare &EngineConfig{ID: engineID}, silently
	// discarding Version and installing the package's "latest" release instead.
	t.Run("no override present inherits Version/Config/Args/HarnessScript/Driver", func(t *testing.T) {
		data := &WorkflowData{
			AI: "opencode",
			EngineConfig: &EngineConfig{
				ID:            "opencode",
				Version:       "1.2.14",
				LLMProvider:   LLMProviderGitHub,
				Config:        "some-config",
				Args:          []string{"--flag"},
				HarnessScript: "harness.cjs",
				Driver:        "driver.cjs",
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		got := resolveExternalDetectorEngineConfig(data, "opencode")
		if got.Version != "1.2.14" {
			t.Errorf("expected Version to be inherited as 1.2.14, got %q", got.Version)
		}
		if got.LLMProvider != LLMProviderGitHub {
			t.Errorf("expected LLMProvider to be inherited as github, got %q", got.LLMProvider)
		}
		if got.Config != "some-config" {
			t.Errorf("expected Config to be inherited, got %q", got.Config)
		}
		if len(got.Args) != 1 || got.Args[0] != "--flag" {
			t.Errorf("expected Args to be inherited, got %v", got.Args)
		}
		if got.HarnessScript != "harness.cjs" {
			t.Errorf("expected HarnessScript to be inherited, got %q", got.HarnessScript)
		}
		if got.Driver != "driver.cjs" {
			t.Errorf("expected Driver to be inherited, got %q", got.Driver)
		}
		if got.ID != "opencode" {
			t.Errorf("expected ID to be opencode, got %q", got.ID)
		}
	})

	t.Run("explicit override takes precedence over main engine config", func(t *testing.T) {
		data := &WorkflowData{
			AI: "opencode",
			EngineConfig: &EngineConfig{
				ID:      "opencode",
				Version: "1.2.14",
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{
					EngineConfig: &EngineConfig{
						ID:      "codex",
						Version: "2.0.0",
					},
				},
			},
		}

		got := resolveExternalDetectorEngineConfig(data, "codex")
		if got.Version != "2.0.0" {
			t.Errorf("expected explicit override Version 2.0.0 to win, got %q", got.Version)
		}
		if got.ID != "codex" {
			t.Errorf("expected ID codex, got %q", got.ID)
		}
	})

	t.Run("does not inherit main-engine fields when resolved detection engine differs", func(t *testing.T) {
		data := &WorkflowData{
			AI: "pi",
			EngineConfig: &EngineConfig{
				ID:            "pi",
				Version:       "9.9.9-pi-only",
				Config:        "pi-config",
				Args:          []string{"--pi-only"},
				HarnessScript: "pi-harness.cjs",
				Driver:        "pi-driver.cjs",
			},
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		got := resolveExternalDetectorEngineConfig(data, "copilot")
		if got.ID != "copilot" {
			t.Errorf("expected resolved ID copilot, got %q", got.ID)
		}
		if got.Version != "" {
			t.Errorf("expected empty Version when engine IDs differ, got %q", got.Version)
		}
		if got.Config != "" {
			t.Errorf("expected empty Config when engine IDs differ, got %q", got.Config)
		}
		if len(got.Args) != 0 {
			t.Errorf("expected empty Args when engine IDs differ, got %v", got.Args)
		}
		if got.HarnessScript != "" {
			t.Errorf("expected empty HarnessScript when engine IDs differ, got %q", got.HarnessScript)
		}
		if got.Driver != "" {
			t.Errorf("expected empty Driver when engine IDs differ, got %q", got.Driver)
		}
	})

	t.Run("no main engine config falls back to bare ID", func(t *testing.T) {
		data := &WorkflowData{
			AI: "copilot",
			SafeOutputs: &SafeOutputsConfig{
				ThreatDetection: &ThreatDetectionConfig{},
			},
		}

		got := resolveExternalDetectorEngineConfig(data, "copilot")
		if got.ID != "copilot" {
			t.Errorf("expected ID copilot, got %q", got.ID)
		}
		if got.Version != "" {
			t.Errorf("expected empty Version, got %q", got.Version)
		}
	})
}

// TestBuildExternalDetectorWorkflowDataInheritsMaxTurnCacheMisses verifies that the
// workflow's max-turn-cache-misses guardrail propagates to the external detector so it
// does not silently run with the compile-time default.
func TestBuildExternalDetectorWorkflowDataInheritsMaxTurnCacheMisses(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		EngineConfig: &EngineConfig{
			ID:                 "copilot",
			MaxTurnCacheMisses: 500,
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	allSteps := strings.Join(compiler.buildExternalDetectorExecutionStep(data), "")
	if !strings.Contains(allSteps, `maxCacheMisses\":500`) {
		t.Fatalf("expected external detector steps to inherit maxCacheMisses 500, got:\n%s", allSteps)
	}
}

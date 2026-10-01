//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// TestModelEnvVarInjectionForAgentJob tests that agent jobs get the correct model environment variable
func TestModelEnvVarInjectionForAgentJob(t *testing.T) {
	tests := []struct {
		name                    string
		engine                  string
		expectedEnvVar          string
		expectedCommand         string
		expectedDefault         string
		expectedDefaultOverride string
	}{
		{
			name:                    "Claude agent uses GH_AW_MODEL_AGENT_CLAUDE",
			engine:                  "claude",
			expectedEnvVar:          constants.EnvVarModelAgentClaude,
			expectedCommand:         "${" + constants.EnvVarModelAgentClaude + ":+ --model",
			expectedDefault:         constants.SonnetDefaultModel,
			expectedDefaultOverride: compilerenv.DefaultModelClaude,
		},
		{
			name:                    "Codex agent uses GH_AW_MODEL_AGENT_CODEX",
			engine:                  "codex",
			expectedEnvVar:          constants.EnvVarModelAgentCodex,
			expectedCommand:         "${" + constants.EnvVarModelAgentCodex + `:+ --model "`,
			expectedDefault:         "",
			expectedDefaultOverride: compilerenv.DefaultModelCodex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a simple workflow with the specified engine
			// Add SafeOutputs to distinguish from detection jobs
			workflowData := &WorkflowData{
				Name: "test-workflow",
				AI:   tt.engine,
				Tools: map[string]any{
					"bash": []any{"echo"},
				},
				SafeOutputs: &SafeOutputsConfig{
					// Just enough to make it an agent job
				},
			}

			// Get the engine
			engine, err := GetGlobalEngineRegistry().GetEngine(tt.engine)
			if err != nil {
				t.Fatalf("Failed to get engine: %v", err)
			}

			// Get execution steps
			steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

			// Convert steps to string for analysis
			var stepsStr strings.Builder
			for _, step := range steps {
				for _, line := range step {
					stepsStr.WriteString(line)
					stepsStr.WriteString("\n")
				}
			}
			stepsContent := stepsStr.String()

			// Check that the environment variable is present
			if !strings.Contains(stepsContent, tt.expectedEnvVar+":") {
				t.Errorf("Expected environment variable %s not found in steps:\n%s", tt.expectedEnvVar, stepsContent)
			}

			// Check that the command uses the env var conditionally
			if !strings.Contains(stepsContent, tt.expectedCommand) {
				t.Errorf("Expected command pattern '%s' not found in steps:\n%s", tt.expectedCommand, stepsContent)
			}

			// Verify env var has the correct fallback value
			var expectedEnvLine string
			if tt.expectedDefault != "" {
				expectedEnvLine = tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || vars." + tt.expectedDefaultOverride + " || '" + tt.expectedDefault + "' }}"
			} else {
				expectedEnvLine = tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || vars." + tt.expectedDefaultOverride + " || '' }}"
			}
			if !strings.Contains(stepsContent, expectedEnvLine) {
				t.Errorf("Expected env var line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
			}
		})
	}
}

// TestModelEnvVarInjectionForDetectionJob tests that detection jobs get the correct model environment variable
func TestModelEnvVarInjectionForDetectionJob(t *testing.T) {
	tests := []struct {
		name                    string
		engine                  string
		expectedEnvVar          string
		expectedDefault         string
		expectedDefaultOverride string
	}{
		{
			name:                    "Claude detection uses GH_AW_MODEL_DETECTION_CLAUDE",
			engine:                  "claude",
			expectedEnvVar:          constants.EnvVarModelDetectionClaude,
			expectedDefault:         constants.SonnetDefaultModel,
			expectedDefaultOverride: compilerenv.DefaultModelClaude,
		},
		{
			name:                    "Codex detection uses GH_AW_MODEL_DETECTION_CODEX",
			engine:                  "codex",
			expectedEnvVar:          constants.EnvVarModelDetectionCodex,
			expectedDefault:         "",
			expectedDefaultOverride: compilerenv.DefaultModelCodex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal detection workflow (no SafeOutputs)
			workflowData := &WorkflowData{
				Name:        "test-detection",
				AI:          tt.engine,
				SafeOutputs: nil, // This makes it a detection job
				Tools: map[string]any{
					"bash": []any{"cat", "grep"},
				},
			}

			// Get the engine
			engine, err := GetGlobalEngineRegistry().GetEngine(tt.engine)
			if err != nil {
				t.Fatalf("Failed to get engine: %v", err)
			}

			// Get execution steps
			steps := engine.GetExecutionSteps(workflowData, "/tmp/detection.log")

			// Convert steps to string for analysis
			var stepsStr strings.Builder
			for _, step := range steps {
				for _, line := range step {
					stepsStr.WriteString(line)
					stepsStr.WriteString("\n")
				}
			}
			stepsContent := stepsStr.String()

			// Check that the environment variable is present
			if !strings.Contains(stepsContent, tt.expectedEnvVar+":") {
				t.Errorf("Expected environment variable %s not found in detection steps:\n%s", tt.expectedEnvVar, stepsContent)
			}

			// For Copilot, verify it has the default detection model as fallback
			if tt.expectedDefault != "" {
				expectedEnvLine := tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || vars." + tt.expectedDefaultOverride + " || '" + tt.expectedDefault + "' }}"
				if !strings.Contains(stepsContent, expectedEnvLine) {
					t.Errorf("Expected env var line with default '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
				}
			} else {
				// For other engines, verify empty string fallback
				expectedEnvLine := tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || vars." + tt.expectedDefaultOverride + " || '' }}"
				if !strings.Contains(stepsContent, expectedEnvLine) {
					t.Errorf("Expected env var line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
				}
			}
		})
	}
}

func TestClaudeEvalsModelEnvVarInjectionForEvalsPhase(t *testing.T) {
	engine, err := GetGlobalEngineRegistry().GetEngine("claude")
	if err != nil {
		t.Fatalf("Failed to get engine: %v", err)
	}

	t.Run("unset model uses GH_AW_MODEL_EVALS_CLAUDE fallback", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:       "test-evals-claude",
			AI:         "claude",
			IsEvalsRun: true,
			Tools: map[string]any{
				"bash": []any{"echo"},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/evals.log")
		var stepsStr strings.Builder
		for _, step := range steps {
			for _, line := range step {
				stepsStr.WriteString(line)
				stepsStr.WriteString("\n")
			}
		}
		stepsContent := stepsStr.String()

		expectedEnvLine := constants.EnvVarModelEvalsClaude + ": ${{ vars." + constants.EnvVarModelEvalsClaude + " || vars." + compilerenv.DefaultModelClaude + " || '" + constants.SonnetDefaultModel + "' }}"
		if !strings.Contains(stepsContent, expectedEnvLine) {
			t.Errorf("Expected evals env var line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
		}
		if strings.Contains(stepsContent, constants.EnvVarModelAgentClaude+":") {
			t.Errorf("Agent model env var %s should not be present in evals phase:\n%s", constants.EnvVarModelAgentClaude, stepsContent)
		}
	})

	t.Run("expression model uses evals fallback env", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:       "test-evals-expression-claude",
			AI:         "claude",
			IsEvalsRun: true,
			Model:      "${{ inputs.model }}",
			Tools: map[string]any{
				"bash": []any{"echo"},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/evals-expression.log")
		var stepsStr strings.Builder
		for _, step := range steps {
			for _, line := range step {
				stepsStr.WriteString(line)
				stepsStr.WriteString("\n")
			}
		}
		stepsContent := stepsStr.String()

		expectedFallbackLine := constants.EnvVarModelFallback + ": ${{ vars." + constants.EnvVarModelEvalsClaude + " || vars." + compilerenv.DefaultModelClaude + " || '" + constants.SonnetDefaultModel + "' }}"
		if !strings.Contains(stepsContent, expectedFallbackLine) {
			t.Errorf("Expected evals fallback env line '%s' not found in steps:\n%s", expectedFallbackLine, stepsContent)
		}
	})
}

// TestExplicitModelConfigOverridesEnvVar tests that explicit model configuration takes precedence
func TestExplicitModelConfigOverridesEnvVar(t *testing.T) {
	workflowData := &WorkflowData{
		Name:  "test-explicit-model",
		AI:    "copilot",
		Model: "gpt-4",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		Tools: map[string]any{
			"bash": []any{"echo"},
		},
		SafeOutputs: &SafeOutputsConfig{
			// Just enough to make it an agent job
		},
	}

	engine, err := GetGlobalEngineRegistry().GetEngine("copilot")
	if err != nil {
		t.Fatalf("Failed to get engine: %v", err)
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	// Convert steps to string
	var stepsStr strings.Builder
	for _, step := range steps {
		for _, line := range step {
			stepsStr.WriteString(line)
			stepsStr.WriteString("\n")
		}
	}
	stepsContent := stepsStr.String()

	// When model is explicitly configured, the GH_AW_ fallback env var should NOT be present
	if strings.Contains(stepsContent, constants.EnvVarModelAgentCopilot+":") {
		t.Errorf("Fallback env var %s should not be present when model is explicitly configured", constants.EnvVarModelAgentCopilot)
	}
	if strings.Contains(stepsContent, constants.EnvVarModelFallback+":") {
		t.Errorf("Fallback env var %s should not be present for a literal configured model", constants.EnvVarModelFallback)
	}

	// The model should be passed via the native COPILOT_MODEL env var (not via --model flag)
	expectedEnvLine := constants.CopilotCLIModelEnvVar + ": gpt-4"
	if !strings.Contains(stepsContent, expectedEnvLine) {
		t.Errorf("Expected native env var line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
	}

	// The --model flag should NOT appear in the shell command (model is via env var)
	if strings.Contains(stepsContent, "--model gpt-4") {
		t.Errorf("--model flag should not be in command when model is set via native env var:\n%s", stepsContent)
	}
}

// TestAutoModelPassedToCopilotAsIs tests that model: auto is passed to the Copilot CLI
// via COPILOT_MODEL=auto without any transformation or fallback env var.
func TestAutoModelPassedToCopilotAsIs(t *testing.T) {
	workflowData := &WorkflowData{
		Name:  "test-auto-model",
		AI:    "copilot",
		Model: "auto",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		Tools: map[string]any{
			"bash": []any{"echo"},
		},
		SafeOutputs: &SafeOutputsConfig{},
	}

	engine, err := GetGlobalEngineRegistry().GetEngine("copilot")
	if err != nil {
		t.Fatalf("Failed to get engine: %v", err)
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/test-auto.log")

	var stepsStr strings.Builder
	for _, step := range steps {
		for _, line := range step {
			stepsStr.WriteString(line)
			stepsStr.WriteString("\n")
		}
	}
	stepsContent := stepsStr.String()

	// "auto" is a native Copilot model ID and must be passed as-is via COPILOT_MODEL
	expectedEnvLine := constants.CopilotCLIModelEnvVar + ": auto"
	if !strings.Contains(stepsContent, expectedEnvLine) {
		t.Errorf("Expected '%s' not found in steps (auto must be passed as-is to Copilot):\n%s", expectedEnvLine, stepsContent)
	}

	// No fallback env var should be set for a literal model like "auto"
	if strings.Contains(stepsContent, constants.EnvVarModelFallback+":") {
		t.Errorf("Fallback env var %s should not be present for literal model 'auto'", constants.EnvVarModelFallback)
	}

	// "auto" should not appear as a --model CLI flag
	if strings.Contains(stepsContent, "--model auto") {
		t.Errorf("--model flag should not be used; model must be passed via COPILOT_MODEL:\n%s", stepsContent)
	}
}

// TestAutoModelFallbackForNonCopilotEngine verifies that the "auto" alias provides a
// provider-agnostic fallback path for non-Copilot engines. For a Claude workflow with
// model: auto, the alias map must route through "large" (a provider-agnostic alias) and
// must not expose "copilot/auto" as a reachable model in the large fallback chain.
func TestAutoModelFallbackForNonCopilotEngine(t *testing.T) {
	workflowData := &WorkflowData{
		Name:  "test-auto-claude",
		AI:    "claude",
		Model: "auto",
		EngineConfig: &EngineConfig{
			ID: "claude",
		},
		Tools: map[string]any{
			"bash": []any{"echo"},
		},
		SafeOutputs:   &SafeOutputsConfig{},
		ModelMappings: MergeImportedModelAliases(nil, nil),
	}

	// "auto" must include "large" as the non-Copilot fallback entry
	autoResolution := workflowData.ModelMappings["auto"]
	if len(autoResolution) == 0 {
		t.Fatal("auto alias must be present in ModelMappings")
	}
	foundLarge := false
	for _, m := range autoResolution {
		if m == "large" {
			foundLarge = true
		}
	}
	if !foundLarge {
		t.Errorf("auto alias must include 'large' as a non-Copilot fallback; got %v", autoResolution)
	}

	// "large" must not contain copilot-specific entries — it is the provider-agnostic fallback
	largeResolution := workflowData.ModelMappings["large"]
	if len(largeResolution) == 0 {
		t.Fatal("large alias must be present in ModelMappings")
	}
	for _, m := range largeResolution {
		if strings.HasPrefix(m, "copilot/") {
			t.Errorf("large alias must not contain Copilot-specific models; found %q in large: %v", m, largeResolution)
		}
	}
}

// TestCopilotFallbackModelMapsToNativeEnvVar tests that when model is not explicitly configured,
// the Copilot engine maps the GitHub org variable to the native COPILOT_MODEL env var instead
// of using the broken --model CLI flag.
func TestCopilotFallbackModelMapsToNativeEnvVar(t *testing.T) {
	tests := []struct {
		name           string
		safeOutputs    *SafeOutputsConfig
		expectedOrgVar string
		features       map[string]any
		expectedTail   string
	}{
		{
			name:           "Agent job maps GH_AW_MODEL_AGENT_COPILOT to COPILOT_MODEL",
			safeOutputs:    &SafeOutputsConfig{},
			expectedOrgVar: constants.EnvVarModelAgentCopilot,
			expectedTail:   "vars." + compilerenv.DefaultModelCopilot + " || '" + constants.CopilotBYOKDefaultModel + "'",
		},
		{
			name:           "Detection job maps GH_AW_MODEL_DETECTION_COPILOT to COPILOT_MODEL",
			safeOutputs:    nil,
			expectedOrgVar: constants.EnvVarModelDetectionCopilot,
			expectedTail:   "vars." + compilerenv.DefaultModelCopilot + " || '" + constants.CopilotBYOKDefaultModel + "'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name: "test-workflow",
				AI:   "copilot",
				Tools: map[string]any{
					"bash": []any{"echo"},
				},
				SafeOutputs: tt.safeOutputs,
				Features:    tt.features,
			}

			engine, err := GetGlobalEngineRegistry().GetEngine("copilot")
			if err != nil {
				t.Fatalf("Failed to get engine: %v", err)
			}

			steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

			var stepsStr strings.Builder
			for _, step := range steps {
				for _, line := range step {
					stepsStr.WriteString(line)
					stepsStr.WriteString("\n")
				}
			}
			stepsContent := stepsStr.String()

			// The model must be passed via COPILOT_MODEL env var pointing to the org variable
			expectedEnvLine := constants.CopilotCLIModelEnvVar + ": ${{ vars." + tt.expectedOrgVar + " || " + tt.expectedTail + " }}"
			if !strings.Contains(stepsContent, expectedEnvLine) {
				t.Errorf("Expected env line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
			}

			// The --model flag must NOT appear in the shell command
			if strings.Contains(stepsContent, "--model") {
				t.Errorf("--model flag should not appear in command (model is passed via COPILOT_MODEL env var):\n%s", stepsContent)
			}

			// The org variable must NOT appear as its own env block key
			if strings.Contains(stepsContent, tt.expectedOrgVar+":") {
				t.Errorf("Org var %s should not appear as env block key (only as value in COPILOT_MODEL):\n%s", tt.expectedOrgVar, stepsContent)
			}
		})
	}
}

// TestExpressionModelUsesEnvVar tests that when model is a GitHub Actions expression,
// it is set as an environment variable rather than embedded directly in the shell command.
// This prevents template injection validation failures.
func TestExpressionModelUsesEnvVar(t *testing.T) {
	tests := []struct {
		name                 string
		engine               string
		model                string
		expectedModelEnvVar  string
		expectedModelEnvVal  string
		expectedFallbackVal  string
		expectShellExpansion bool // whether command should use ${VAR:+ --model "$VAR"}
	}{
		{
			name:                 "Copilot agent keeps pure expression model and adds JS fallback env",
			engine:               "copilot",
			model:                "${{ inputs.model }}",
			expectedModelEnvVar:  constants.CopilotCLIModelEnvVar,
			expectedModelEnvVal:  "${{ inputs.model }}",
			expectedFallbackVal:  "${{ vars." + constants.EnvVarModelAgentCopilot + " || vars." + compilerenv.DefaultModelCopilot + " || '" + constants.CopilotBYOKDefaultModel + "' }}",
			expectShellExpansion: false, // Copilot reads COPILOT_MODEL natively, no shell expansion needed
		},
		{
			name:                 "Copilot agent keeps composite expression model and adds JS fallback env",
			engine:               "copilot",
			model:                "${{ inputs.provider }}/${{ inputs.model }}",
			expectedModelEnvVar:  constants.CopilotCLIModelEnvVar,
			expectedModelEnvVal:  "${{ inputs.provider }}/${{ inputs.model }}",
			expectedFallbackVal:  "${{ vars." + constants.EnvVarModelAgentCopilot + " || vars." + compilerenv.DefaultModelCopilot + " || '" + constants.CopilotBYOKDefaultModel + "' }}",
			expectShellExpansion: false,
		},
		{
			name:                 "Claude agent keeps expression model and adds JS fallback env",
			engine:               "claude",
			model:                "${{ inputs.model }}",
			expectedModelEnvVar:  constants.ClaudeCLIModelEnvVar,
			expectedModelEnvVal:  "${{ inputs.model }}",
			expectedFallbackVal:  "${{ vars." + constants.EnvVarModelAgentClaude + " || vars." + compilerenv.DefaultModelClaude + " || '" + constants.SonnetDefaultModel + "' }}",
			expectShellExpansion: false, // Claude reads ANTHROPIC_MODEL natively, no shell expansion needed
		},
		{
			name:                 "Claude agent keeps composite expression model and adds JS fallback env",
			engine:               "claude",
			model:                "${{ inputs.provider }}/${{ inputs.model }}",
			expectedModelEnvVar:  constants.ClaudeCLIModelEnvVar,
			expectedModelEnvVal:  "${{ inputs.provider }}/${{ inputs.model }}",
			expectedFallbackVal:  "${{ vars." + constants.EnvVarModelAgentClaude + " || vars." + compilerenv.DefaultModelClaude + " || '" + constants.SonnetDefaultModel + "' }}",
			expectShellExpansion: false,
		},
		{
			name:                 "Codex agent keeps composite expression model and adds JS fallback env",
			engine:               "codex",
			model:                "${{ inputs.provider }}/${{ inputs.model }}",
			expectedModelEnvVar:  constants.EnvVarModelAgentCodex,
			expectedModelEnvVal:  "${{ inputs.provider }}/${{ inputs.model }}",
			expectedFallbackVal:  "${{ vars." + constants.EnvVarModelAgentCodex + " || vars." + compilerenv.DefaultModelCodex + " || '' }}",
			expectShellExpansion: true, // Codex has no native model env var, uses shell expansion
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workflowData := &WorkflowData{
				Name:  "test-expression-model",
				AI:    tt.engine,
				Model: tt.model,
				EngineConfig: &EngineConfig{
					ID: tt.engine,
				},
				Tools: map[string]any{
					"bash": []any{"echo"},
				},
				SafeOutputs: &SafeOutputsConfig{},
			}

			engine, err := GetGlobalEngineRegistry().GetEngine(tt.engine)
			if err != nil {
				t.Fatalf("Failed to get engine: %v", err)
			}

			steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

			var stepsStr strings.Builder
			for _, step := range steps {
				for _, line := range step {
					stepsStr.WriteString(line)
					stepsStr.WriteString("\n")
				}
			}
			stepsContent := stepsStr.String()

			// The expression must NOT appear directly in the shell command run: block
			// (it should be in the env: block only)
			if strings.Contains(stepsContent, "--model ${{") || strings.Contains(stepsContent, "--model \"${{") {
				t.Errorf("Model expression should not be embedded directly in shell command in steps:\n%s", stepsContent)
			}

			// The env var must be set to the expression value
			expectedModelEnvLine := tt.expectedModelEnvVar + ": " + tt.expectedModelEnvVal
			if !strings.Contains(stepsContent, expectedModelEnvLine) {
				t.Errorf("Expected env line '%s' not found in steps:\n%s", expectedModelEnvLine, stepsContent)
			}
			expectedFallbackEnvLine := constants.EnvVarModelFallback + ": " + tt.expectedFallbackVal
			if !strings.Contains(stepsContent, expectedFallbackEnvLine) {
				t.Errorf("Expected fallback env line '%s' not found in steps:\n%s", expectedFallbackEnvLine, stepsContent)
			}

			// Check shell expansion expectation
			shellExpansionPattern := "${" + tt.expectedModelEnvVar + ":+"
			hasShellExpansion := strings.Contains(stepsContent, shellExpansionPattern)
			if tt.expectShellExpansion && !hasShellExpansion {
				t.Errorf("Expected conditional env var usage '${%s:+' not found in steps:\n%s", tt.expectedModelEnvVar, stepsContent)
			} else if !tt.expectShellExpansion && hasShellExpansion {
				t.Errorf("Unexpected conditional env var usage '${%s:+' found in steps (should use native env var):\n%s", tt.expectedModelEnvVar, stepsContent)
			}
		})
	}
}

// TestExpressionModelDetectionJobUsesEnvVar tests that detection jobs with expression model
// for Copilot use the native COPILOT_MODEL environment variable.
func TestExpressionModelDetectionJobUsesEnvVar(t *testing.T) {
	workflowData := &WorkflowData{
		Name:  "test-detection-expression-model",
		AI:    "copilot",
		Model: "${{ inputs.model }}",
		EngineConfig: &EngineConfig{
			ID: "copilot",
		},
		Tools: map[string]any{
			"bash": []any{"cat", "grep"},
		},
		SafeOutputs: nil, // detection job
	}

	engine, err := GetGlobalEngineRegistry().GetEngine("copilot")
	if err != nil {
		t.Fatalf("Failed to get engine: %v", err)
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/detection.log")

	var stepsStr strings.Builder
	for _, step := range steps {
		for _, line := range step {
			stepsStr.WriteString(line)
			stepsStr.WriteString("\n")
		}
	}
	stepsContent := stepsStr.String()

	// Detection job for Copilot should keep the configured expression and expose the runtime fallback.
	expectedModelEnvLine := constants.CopilotCLIModelEnvVar + ": ${{ inputs.model }}"
	if !strings.Contains(stepsContent, expectedModelEnvLine) {
		t.Errorf("Expected env line '%s' not found in steps:\n%s", expectedModelEnvLine, stepsContent)
	}
	expectedFallbackEnvLine := constants.EnvVarModelFallback + ": ${{ vars." + constants.EnvVarModelDetectionCopilot + " || vars." + compilerenv.DefaultModelCopilot + " || '" + constants.CopilotBYOKDefaultModel + "' }}"
	if !strings.Contains(stepsContent, expectedFallbackEnvLine) {
		t.Errorf("Expected fallback env line '%s' not found in steps:\n%s", expectedFallbackEnvLine, stepsContent)
	}

	// Must not embed expression directly in shell command
	if strings.Contains(stepsContent, "--model ${{") {
		t.Errorf("Model expression should not be embedded directly in shell command:\n%s", stepsContent)
	}
}

// TestGetModelEnvVarName tests that engines return the correct native model env var name.
func TestGetModelEnvVarName(t *testing.T) {
	tests := []struct {
		engine   string
		expected string
	}{
		{"copilot", constants.CopilotCLIModelEnvVar}, // "COPILOT_MODEL"
		{"claude", constants.ClaudeCLIModelEnvVar},   // "ANTHROPIC_MODEL"
		{"codex", ""}, // no native model env var
		{"gemini", constants.GeminiCLIModelEnvVar}, // "GEMINI_MODEL"
	}

	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			eng, err := GetGlobalEngineRegistry().GetEngine(tt.engine)
			if err != nil {
				t.Fatalf("Failed to get engine %s: %v", tt.engine, err)
			}
			provider, ok := eng.(ModelEnvVarProvider)
			if !ok {
				t.Fatalf("Engine %s does not implement ModelEnvVarProvider", tt.engine)
			}
			if got := provider.GetModelEnvVarName(); got != tt.expected {
				t.Errorf("Engine %s: GetModelEnvVarName() = %q, want %q", tt.engine, got, tt.expected)
			}
		})
	}
}

// TestCodexModelFlagPositionAfterExec verifies that the --model flag appears after the exec
// subcommand in the generated Codex command, not before it.
// Regression test for: Codex lock compiler places --model flag before exec subcommand.
func TestCodexModelFlagPositionAfterExec(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-codex-model-position",
		AI:   "codex",
		Tools: map[string]any{
			"bash": []any{"echo"},
		},
		SafeOutputs: &SafeOutputsConfig{},
	}

	engine, err := GetGlobalEngineRegistry().GetEngine("codex")
	if err != nil {
		t.Fatalf("Failed to get engine: %v", err)
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/test.log")

	var stepsStr strings.Builder
	for _, step := range steps {
		for _, line := range step {
			stepsStr.WriteString(line)
			stepsStr.WriteString("\n")
		}
	}
	stepsContent := stepsStr.String()

	// Find the model shell expansion pattern in the generated command
	modelPattern := "${" + constants.EnvVarModelAgentCodex + ":+"
	beforeModel, _, found := strings.Cut(stepsContent, modelPattern)
	if !found {
		t.Fatalf("Model expansion pattern '%s' not found in steps:\n%s", modelPattern, stepsContent)
	}

	// Find "codex exec" before the model pattern. Using "codex exec" (not just "exec") avoids
	// false positives from unrelated occurrences like "GH_AW_NODE_EXEC" in the step content.
	execMarker := "codex exec"
	execIdx := strings.LastIndex(beforeModel, execMarker)
	if execIdx == -1 {
		t.Errorf("'codex exec' must appear before the model flag '%s' in the generated command.\n"+
			"This indicates the model flag is placed before 'exec', causing Codex to ignore it.\n"+
			"Got:\n%s", modelPattern, stepsContent)
	}
}

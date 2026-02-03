//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

// TestModelEnvVarInjectionForAgentJob tests that agent jobs get the correct model environment variable
func TestModelEnvVarInjectionForAgentJob(t *testing.T) {
	tests := []struct {
		name            string
		engine          string
		expectedEnvVar  string
		expectedCommand string
	}{
		{
			name:            "Copilot agent uses GH_AW_MODEL_AGENT_COPILOT",
			engine:          "copilot",
			expectedEnvVar:  constants.EnvVarModelAgentCopilot,
			expectedCommand: "${" + constants.EnvVarModelAgentCopilot + ":+ --model",
		},
		{
			name:            "Claude agent uses GH_AW_MODEL_AGENT_CLAUDE",
			engine:          "claude",
			expectedEnvVar:  constants.EnvVarModelAgentClaude,
			expectedCommand: "${" + constants.EnvVarModelAgentClaude + ":+ --model",
		},
		{
			name:            "Codex agent uses GH_AW_MODEL_AGENT_CODEX",
			engine:          "codex",
			expectedEnvVar:  constants.EnvVarModelAgentCodex,
			expectedCommand: "${" + constants.EnvVarModelAgentCodex + ":+-c model=",
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

			// Verify env var has fallback to empty string for agent jobs
			expectedEnvLine := tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || '' }}"
			if !strings.Contains(stepsContent, expectedEnvLine) {
				t.Errorf("Expected env var line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
			}
		})
	}
}

// TestModelEnvVarInjectionForDetectionJob tests that detection jobs get the correct model environment variable
func TestModelEnvVarInjectionForDetectionJob(t *testing.T) {
	tests := []struct {
		name            string
		engine          string
		expectedEnvVar  string
		expectedDefault string
	}{
		{
			name:            "Copilot detection uses GH_AW_MODEL_DETECTION_COPILOT",
			engine:          "copilot",
			expectedEnvVar:  constants.EnvVarModelDetectionCopilot,
			expectedDefault: "", // No builtin default, CLI will use its own
		},
		{
			name:            "Claude detection uses GH_AW_MODEL_DETECTION_CLAUDE",
			engine:          "claude",
			expectedEnvVar:  constants.EnvVarModelDetectionClaude,
			expectedDefault: "", // Claude has no default detection model
		},
		{
			name:            "Codex detection uses GH_AW_MODEL_DETECTION_CODEX",
			engine:          "codex",
			expectedEnvVar:  constants.EnvVarModelDetectionCodex,
			expectedDefault: "", // Codex has no default detection model
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
				expectedEnvLine := tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || '" + tt.expectedDefault + "' }}"
				if !strings.Contains(stepsContent, expectedEnvLine) {
					t.Errorf("Expected env var line with default '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
				}
			} else {
				// For other engines, verify empty string fallback
				expectedEnvLine := tt.expectedEnvVar + ": ${{ vars." + tt.expectedEnvVar + " || '' }}"
				if !strings.Contains(stepsContent, expectedEnvLine) {
					t.Errorf("Expected env var line '%s' not found in steps:\n%s", expectedEnvLine, stepsContent)
				}
			}
		})
	}
}

// TestExplicitModelConfigOverridesEnvVar tests that explicit model configuration takes precedence
func TestExplicitModelConfigOverridesEnvVar(t *testing.T) {
	workflowData := &WorkflowData{
		Name: "test-explicit-model",
		AI:   "copilot",
		EngineConfig: &EngineConfig{
			ID:    "copilot",
			Model: "gpt-4",
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

	// When model is explicitly configured, the env var should NOT be present
	if strings.Contains(stepsContent, constants.EnvVarModelAgentCopilot+":") {
		t.Errorf("Environment variable %s should not be present when model is explicitly configured", constants.EnvVarModelAgentCopilot)
	}

	// The explicit model should be in the command
	if !strings.Contains(stepsContent, "--model gpt-4") {
		t.Errorf("Explicit model 'gpt-4' not found in command:\n%s", stepsContent)
	}
}

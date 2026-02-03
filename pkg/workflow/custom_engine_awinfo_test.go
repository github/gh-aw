//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestGenerateCreateAwInfoCustomEngine(t *testing.T) {
	// Create a compiler instance
	c := NewCompiler()

	t.Run("custom engine with explicit model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID:    "custom",
				Model: "anthropic/claude-3-5-sonnet-20241022",
			},
		}

		engine := NewCustomEngine()

		var yaml strings.Builder
		c.generateCreateAwInfo(&yaml, workflowData, engine)

		result := yaml.String()

		// Check that the explicit model is used directly
		if !strings.Contains(result, `model: "anthropic/claude-3-5-sonnet-20241022"`) {
			t.Error("Expected explicit model to be used directly in aw_info.json for custom engine")
		}

		// Should not contain process.env reference when model is explicit
		if strings.Contains(result, "process.env."+constants.EnvVarModelAgentCustom) {
			t.Error("Should not use environment variable when model is explicitly configured")
		}
	})

	t.Run("custom engine without explicit model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "custom",
				// No explicit model
			},
		}

		engine := NewCustomEngine()

		var yaml strings.Builder
		c.generateCreateAwInfo(&yaml, workflowData, engine)

		result := yaml.String()

		// Check that the custom model environment variable is used
		expectedEnvVar := "process.env." + constants.EnvVarModelAgentCustom + " || \"\""
		if !strings.Contains(result, expectedEnvVar) {
			t.Errorf("Expected custom engine to use environment variable %s in aw_info.json, got:\n%s", constants.EnvVarModelAgentCustom, result)
		}

		// Should not have incomplete process.env. syntax
		if strings.Contains(result, "process.env. || \"\"") {
			t.Error("Found incomplete 'process.env. || \"\"' syntax - this should not happen")
		}
	})

	t.Run("copilot engine without explicit model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
		}

		engine := NewCopilotEngine()

		var yaml strings.Builder
		c.generateCreateAwInfo(&yaml, workflowData, engine)

		result := yaml.String()

		// Check that the copilot model environment variable is used
		expectedEnvVar := "process.env." + constants.EnvVarModelAgentCopilot + " || \"\""
		if !strings.Contains(result, expectedEnvVar) {
			t.Errorf("Expected copilot engine to use environment variable %s in aw_info.json", constants.EnvVarModelAgentCopilot)
		}
	})

	t.Run("claude engine without explicit model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
		}

		engine := NewClaudeEngine()

		var yaml strings.Builder
		c.generateCreateAwInfo(&yaml, workflowData, engine)

		result := yaml.String()

		// Check that the claude model environment variable is used
		expectedEnvVar := "process.env." + constants.EnvVarModelAgentClaude + " || \"\""
		if !strings.Contains(result, expectedEnvVar) {
			t.Errorf("Expected claude engine to use environment variable %s in aw_info.json", constants.EnvVarModelAgentClaude)
		}
	})

	t.Run("codex engine without explicit model", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
			},
		}

		engine := NewCodexEngine()

		var yaml strings.Builder
		c.generateCreateAwInfo(&yaml, workflowData, engine)

		result := yaml.String()

		// Check that the codex model environment variable is used
		expectedEnvVar := "process.env." + constants.EnvVarModelAgentCodex + " || \"\""
		if !strings.Contains(result, expectedEnvVar) {
			t.Errorf("Expected codex engine to use environment variable %s in aw_info.json", constants.EnvVarModelAgentCodex)
		}
	})
}

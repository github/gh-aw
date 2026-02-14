package workflow

import (
	"strings"
	"testing"
)

// TestEngineAWFEnableApiProxy tests that engines with LLM gateway support
// include --enable-api-proxy flag in AWF commands.
func TestEngineAWFEnableApiProxy(t *testing.T) {
	t.Run("Claude AWF command includes enable-api-proxy flag", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "claude",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewClaudeEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		if !strings.Contains(stepContent, "--enable-api-proxy") {
			t.Error("Expected Claude AWF command to contain '--enable-api-proxy' flag")
		}
	})

	t.Run("Copilot AWF command does not include enable-api-proxy flag (returns -1)", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		if strings.Contains(stepContent, "--enable-api-proxy") {
			t.Error("Expected Copilot AWF command to NOT contain '--enable-api-proxy' flag")
		}
	})

	t.Run("Codex AWF command includes enable-api-proxy flag", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "codex",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled: true,
				},
			},
		}

		engine := NewCodexEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		if !strings.Contains(stepContent, "--enable-api-proxy") {
			t.Error("Expected Codex AWF command to contain '--enable-api-proxy' flag")
		}
	})
}

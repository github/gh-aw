//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestFirewallLogLevelParsing tests that the log-level field is correctly parsed
func TestFirewallLogLevelParsing(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)

	t.Run("log-level is parsed from network.firewall object", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"firewall": map[string]any{
					"log-level": "debug",
				},
			},
		}

		networkPerms := compiler.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Network permissions should not be nil")
		}

		if networkPerms.Firewall == nil {
			t.Fatal("Firewall config should not be nil")
		}

		if networkPerms.Firewall.LogLevel != "debug" {
			t.Errorf("Expected log-level 'debug', got '%s'", networkPerms.Firewall.LogLevel)
		}
	})

	t.Run("log-level defaults to empty string when not specified", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"firewall": map[string]any{
					"version": "v1.0.0",
				},
			},
		}

		networkPerms := compiler.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Network permissions should not be nil")
		}

		if networkPerms.Firewall == nil {
			t.Fatal("Firewall config should not be nil")
		}

		if networkPerms.Firewall.LogLevel != "" {
			t.Errorf("Expected log-level to be empty string, got '%s'", networkPerms.Firewall.LogLevel)
		}
	})

	t.Run("log-level works with other firewall fields", func(t *testing.T) {
		frontmatter := map[string]any{
			"network": map[string]any{
				"firewall": map[string]any{
					"version":   "v1.0.0",
					"log-level": "info",
					"args":      []any{"--custom-arg"},
				},
			},
		}

		networkPerms := compiler.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Network permissions should not be nil")
		}

		if networkPerms.Firewall == nil {
			t.Fatal("Firewall config should not be nil")
		}

		if networkPerms.Firewall.LogLevel != "info" {
			t.Errorf("Expected log-level 'info', got '%s'", networkPerms.Firewall.LogLevel)
		}

		if networkPerms.Firewall.Version != "v1.0.0" {
			t.Errorf("Expected version 'v1.0.0', got '%s'", networkPerms.Firewall.Version)
		}

		if len(networkPerms.Firewall.Args) != 1 {
			t.Errorf("Expected 1 arg, got %d", len(networkPerms.Firewall.Args))
		}
	})

	t.Run("different log-level values are parsed correctly", func(t *testing.T) {
		logLevels := []string{"debug", "info", "warn", "error"}

		for _, level := range logLevels {
			frontmatter := map[string]any{
				"network": map[string]any{
					"firewall": map[string]any{
						"log-level": level,
					},
				},
			}

			networkPerms := compiler.extractNetworkPermissions(frontmatter)
			if networkPerms == nil {
				t.Fatalf("Network permissions should not be nil for log-level '%s'", level)
			}

			if networkPerms.Firewall == nil {
				t.Fatalf("Firewall config should not be nil for log-level '%s'", level)
			}

			if networkPerms.Firewall.LogLevel != level {
				t.Errorf("Expected log-level '%s', got '%s'", level, networkPerms.Firewall.LogLevel)
			}
		}
	})
}

// TestFirewallLogLevelInCopilotEngine tests that the log-level is used in the copilot engine
func TestFirewallLogLevelInCopilotEngine(t *testing.T) {
	t.Run("default log-level is 'info' when not specified", func(t *testing.T) {
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

		// Check that the command contains --log-level info (default)
		if !strings.Contains(stepContent, "--log-level info") {
			t.Errorf("Expected command to contain '--log-level info' (default), got:\n%s", stepContent)
		}
	})

	t.Run("custom log-level is used when specified", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				ID: "copilot",
			},
			NetworkPermissions: &NetworkPermissions{
				Firewall: &FirewallConfig{
					Enabled:  true,
					LogLevel: "debug",
				},
			},
		}

		engine := NewCopilotEngine()
		steps := engine.GetExecutionSteps(workflowData, "test.log")

		if len(steps) == 0 {
			t.Fatal("Expected at least one execution step")
		}

		stepContent := strings.Join(steps[0], "\n")

		// Check that the command contains --log-level debug
		if !strings.Contains(stepContent, "--log-level debug") {
			t.Errorf("Expected command to contain '--log-level debug', got:\n%s", stepContent)
		}
	})

	t.Run("log-level can be set to different values", func(t *testing.T) {
		logLevels := []string{"debug", "info", "warn", "error"}

		for _, level := range logLevels {
			workflowData := &WorkflowData{
				Name: "test-workflow",
				EngineConfig: &EngineConfig{
					ID: "copilot",
				},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{
						Enabled:  true,
						LogLevel: level,
					},
				},
			}

			engine := NewCopilotEngine()
			steps := engine.GetExecutionSteps(workflowData, "test.log")

			if len(steps) == 0 {
				t.Fatalf("Expected at least one execution step for log-level '%s'", level)
			}

			stepContent := strings.Join(steps[0], "\n")

			expectedFlag := "--log-level " + level
			if !strings.Contains(stepContent, expectedFlag) {
				t.Errorf("Expected command to contain '%s', got:\n%s", expectedFlag, stepContent)
			}
		}
	})
}

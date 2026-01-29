//go:build integration

package workflow

import (
	"strings"
	"testing"
)

func TestFirewallDisableIntegration(t *testing.T) {
	t.Run("firewall disable with allowed domains warns", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "workflow_dispatch",
			"engine": "copilot",
			"network": map[string]any{
				"allowed":  []any{"example.com"},
				"firewall": "disable",
			},
			"tools": map[string]any{
				"web-fetch": nil,
			},
		}

		compiler := NewCompiler(
			WithVersion("test"),
			WithSkipValidation(true),
		)

		// Extract network permissions
		networkPerms := compiler.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		// Check firewall config
		if networkPerms.Firewall == nil {
			t.Fatal("Expected firewall config to be extracted")
		}
		if networkPerms.Firewall.Enabled {
			t.Error("Firewall should be disabled when set to 'disable'")
		}

		// Check validation triggers warning
		engine := NewCopilotEngine()
		initialWarnings := compiler.warningCount
		err := compiler.checkFirewallDisable(engine, networkPerms)
		if err != nil {
			t.Errorf("Expected no error in non-strict mode, got: %v", err)
		}
		if compiler.warningCount != initialWarnings+1 {
			t.Error("Should emit warning when firewall is disabled with allowed domains")
		}
	})

	t.Run("firewall disable in strict mode errors", func(t *testing.T) {
		frontmatter := map[string]any{
			"on":     "workflow_dispatch",
			"engine": "copilot",
			"strict": true,
			"network": map[string]any{
				"allowed":  []any{"example.com"},
				"firewall": "disable",
			},
		}

		compiler := NewCompiler()
		compiler.strictMode = true
		compiler.SetSkipValidation(true)

		networkPerms := compiler.extractNetworkPermissions(frontmatter)
		if networkPerms == nil {
			t.Fatal("Expected network permissions to be extracted")
		}

		engine := NewCopilotEngine()
		err := compiler.checkFirewallDisable(engine, networkPerms)
		if err == nil {
			t.Error("Expected error in strict mode when firewall is disabled with allowed domains")
		}
		if !strings.Contains(err.Error(), "strict mode") {
			t.Errorf("Error should mention strict mode, got: %v", err)
		}
	})
}
